package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// #6985: Gemini 3.x must receive thinkingLevel (not thinkingBudget),
// otherwise upstream returns zero thought summaries.
func TestBuildGenerationConfig_Gemini3ThinkingLevel(t *testing.T) {
	cases := []struct {
		name       string
		model      string
		budget     int
		wantLevel  string
		wantBudget int
	}{
		{name: "tiered defaults to high", model: "gemini-3.8-flash-tiered", budget: 0, wantLevel: "high"},
		{name: "low suffix", model: "gemini-3.8-flash-low", budget: 2048, wantLevel: "low"},
		{name: "medium suffix", model: "gemini-3.8-flash-medium", budget: 2048, wantLevel: "medium"},
		{name: "small budget downgrades tiered to low", model: "gemini-3.8-flash-tiered", budget: 1024, wantLevel: "low"},
		{name: "mid budget downgrades tiered to medium", model: "gemini-3.8-flash-tiered", budget: 8192, wantLevel: "medium"},
		{name: "bare model defaults to high", model: "gemini-3-pro", budget: 0, wantLevel: "high"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ClaudeRequest{
				Model:     tc.model,
				MaxTokens: 64000,
				Thinking:  &ThinkingConfig{Type: "enabled", BudgetTokens: tc.budget},
			}
			cfg := buildGenerationConfig(req)
			require.NotNil(t, cfg.ThinkingConfig)
			require.True(t, cfg.ThinkingConfig.IncludeThoughts)
			require.Equal(t, tc.wantLevel, cfg.ThinkingConfig.ThinkingLevel)
			require.Equal(t, 0, cfg.ThinkingConfig.ThinkingBudget)
		})
	}

	t.Run("gemini 2.5 keeps budget without level", func(t *testing.T) {
		req := &ClaudeRequest{
			Model:     "gemini-2.5-flash",
			MaxTokens: 64000,
			Thinking:  &ThinkingConfig{Type: "enabled", BudgetTokens: 4096},
		}
		cfg := buildGenerationConfig(req)
		require.NotNil(t, cfg.ThinkingConfig)
		require.Empty(t, cfg.ThinkingConfig.ThinkingLevel)
		require.Equal(t, 4096, cfg.ThinkingConfig.ThinkingBudget)
	})
}

// #7080: Gemini 3+ mixed search+functions keeps both tool types with the
// toolConfig flag; on older models the search builtin is dropped and
// function declarations preserved (upstream rejects the mix otherwise).
func TestBuildToolsForModel_MixedSearchRouting(t *testing.T) {
	mixed := []ClaudeTool{
		{Name: "bash", InputSchema: map[string]any{"type": "object"}},
		{Type: "web_search_20250305", Name: "web_search"},
	}

	t.Run("gemini 3 keeps search with flag", func(t *testing.T) {
		decls := buildToolsForModel(mixed, "gemini-3.8-flash-tiered")
		require.Len(t, decls, 2)
		require.NotEmpty(t, decls[0].FunctionDeclarations)
		require.NotNil(t, decls[1].GoogleSearch)
		require.True(t, hasMixedToolInvocations(decls))
	})

	t.Run("older model drops search keeps functions", func(t *testing.T) {
		decls := buildToolsForModel(mixed, "gemini-2.5-flash")
		require.Len(t, decls, 1)
		require.NotEmpty(t, decls[0].FunctionDeclarations)
		require.Nil(t, decls[0].GoogleSearch)
		require.False(t, hasMixedToolInvocations(decls))
	})

	t.Run("claude model drops search keeps functions", func(t *testing.T) {
		decls := buildToolsForModel(mixed, "claude-sonnet-4-6")
		require.Len(t, decls, 1)
		require.NotEmpty(t, decls[0].FunctionDeclarations)
	})

	t.Run("pure search keeps fallback entry", func(t *testing.T) {
		decls := buildToolsForModel([]ClaudeTool{
			{Type: "web_search_20250305", Name: "web_search"},
		}, "gemini-2.5-flash")
		require.Len(t, decls, 1)
		require.NotNil(t, decls[0].GoogleSearch)
	})

	t.Run("IsGemini3OrNewer", func(t *testing.T) {
		require.True(t, IsGemini3OrNewer("gemini-3.8-flash-tiered"))
		require.True(t, IsGemini3OrNewer("gemini-3.6-flash-high"))
		require.False(t, IsGemini3OrNewer("gemini-2.5-flash"))
		require.False(t, IsGemini3OrNewer("claude-sonnet-4-6"))
	})
}

func TestEnsureToolConfigSnakeCaseEnvelope(t *testing.T) {
	mixed := []ClaudeTool{
		{Name: "bash", InputSchema: map[string]any{"type": "object"}},
		{Type: "web_search_20250305", Name: "web_search"},
	}
	body, err := TransformClaudeToGeminiWithOptions(&ClaudeRequest{
		Model:    "gemini-3.8-flash-tiered",
		Messages: []ClaudeMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		Tools:    mixed,
	}, "project-1", "gemini-3.8-flash-tiered", DefaultTransformOptions())
	require.NoError(t, err)

	var env map[string]any
	require.NoError(t, json.Unmarshal(body, &env))
	req, ok := env["request"].(map[string]any)
	require.True(t, ok)
	toolCfg, ok := req["toolConfig"].(map[string]any)
	require.True(t, ok, "camelCase toolConfig must exist")
	require.Equal(t, true, toolCfg["includeServerSideToolInvocations"])
	require.Equal(t, true, toolCfg["include_server_side_tool_invocations"])
	snake, ok := req["tool_config"].(map[string]any)
	require.True(t, ok, "snake_case tool_config envelope must exist")
	require.Equal(t, true, snake["include_server_side_tool_invocations"])
}

func TestEnsureToolConfigSnakeCaseEnvelope_EdgeCases(t *testing.T) {
	t.Run("no tools leaves envelope untouched", func(t *testing.T) {
		out := ensureToolConfigSnakeCaseEnvelope([]byte(`{"request":{"contents":[]}}`))
		require.NotContains(t, string(out), "tool_config")
	})

	t.Run("invalid json passes through", func(t *testing.T) {
		raw := []byte(`not-json`)
		require.Equal(t, raw, ensureToolConfigSnakeCaseEnvelope(raw))
	})
}
