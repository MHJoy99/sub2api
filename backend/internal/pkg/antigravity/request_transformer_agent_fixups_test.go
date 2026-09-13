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

// #7080: Antigravity v1internal rejects search+function mixing on every
// tested model, so the search builtin is dropped and function declarations
// preserved (LiteLLM degrades the same way to avoid the 400).
func TestBuildToolsForModel_MixedSearchRouting(t *testing.T) {
	mixed := []ClaudeTool{
		{Name: "bash", InputSchema: map[string]any{"type": "object"}},
		{Type: "web_search_20250305", Name: "web_search"},
	}

	t.Run("mixed drops search keeps functions", func(t *testing.T) {
		decls := buildTools(mixed)
		require.Len(t, decls, 1)
		require.NotEmpty(t, decls[0].FunctionDeclarations)
		require.Nil(t, decls[0].GoogleSearch)
		require.False(t, hasMixedToolInvocations(decls))
	})

	t.Run("pure search keeps fallback entry", func(t *testing.T) {
		decls := buildTools([]ClaudeTool{
			{Type: "web_search_20250305", Name: "web_search"},
		})
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

func TestTransform_MixedSearchDroppedBeforeFlag(t *testing.T) {
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

	// Mixed search is dropped before the flag stage, so the envelope must
	// carry function declarations with no googleSearch and no flag.
	var env map[string]any
	require.NoError(t, json.Unmarshal(body, &env))
	req, ok := env["request"].(map[string]any)
	require.True(t, ok)
	tools, ok := req["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	first, _ := tools[0].(map[string]any)
	require.Contains(t, first, "functionDeclarations")
	require.NotContains(t, first, "googleSearch")
	require.NotContains(t, string(body), "includeServerSideToolInvocations")
}
