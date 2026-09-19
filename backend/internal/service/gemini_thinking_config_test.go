package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeGeminiThinkingConfig(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		geminiBody   string
		originalBody string
		wantLevel    string
		wantBudget   int64
		wantThoughts bool
		unchanged    bool
	}{
		{
			name:         "chat completions reasoning_effort high",
			model:        "gemini-3.8-flash-tiered",
			geminiBody:   `{"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":10240}}}`,
			originalBody: `{"model":"gemini-3.8-flash-tiered","reasoning_effort":"high"}`,
			wantLevel:    "high",
			wantThoughts: true,
		},
		{
			name:         "responses reasoning.effort xhigh maps to high",
			model:        "gemini-3.8-flash-tiered",
			geminiBody:   `{"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":32768}}}`,
			originalBody: `{"model":"gemini-3.8-flash-tiered","reasoning":{"effort":"xhigh"}}`,
			wantLevel:    "high",
			wantThoughts: true,
		},
		{
			name:         "responses reasoning.effort max maps to high",
			model:        "gemini-3.8-flash-tiered",
			geminiBody:   `{"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":32768}}}`,
			originalBody: `{"model":"gemini-3.8-flash-tiered","reasoning":{"effort":"max"}}`,
			wantLevel:    "high",
			wantThoughts: true,
		},
		{
			name:         "anthropic output_config.effort medium",
			model:        "gemini-3.8-flash-tiered",
			geminiBody:   `{"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":4096}}}`,
			originalBody: `{"model":"gemini-3.8-flash-tiered","output_config":{"effort":"medium"}}`,
			wantLevel:    "medium",
			wantThoughts: true,
		},
		{
			name:         "no explicit effort, fallback budget <= 4096 is low",
			model:        "gemini-3.8-flash-tiered",
			geminiBody:   `{"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":2048}}}`,
			originalBody: `{"model":"gemini-3.8-flash-tiered"}`,
			wantLevel:    "low",
			wantThoughts: true,
		},
		{
			name:         "no explicit effort, fallback budget 10240 is medium",
			model:        "gemini-3.8-flash-tiered",
			geminiBody:   `{"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":10240}}}`,
			originalBody: `{"model":"gemini-3.8-flash-tiered"}`,
			wantLevel:    "medium",
			wantThoughts: true,
		},
		{
			name:         "no explicit effort, fallback budget 20000 is high",
			model:        "gemini-3.8-flash-tiered",
			geminiBody:   `{"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":20000}}}`,
			originalBody: `{"model":"gemini-3.8-flash-tiered"}`,
			wantLevel:    "high",
			wantThoughts: true,
		},
		{
			name:         "gemini 2.5 flash remains unchanged",
			model:        "gemini-2.5-flash",
			geminiBody:   `{"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":4096}}}`,
			originalBody: `{"model":"gemini-2.5-flash","reasoning_effort":"high"}`,
			unchanged:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeGeminiThinkingConfig([]byte(tc.geminiBody), tc.model, []byte(tc.originalBody))
			require.NoError(t, err)
			if tc.unchanged {
				require.JSONEq(t, tc.geminiBody, string(got))
				return
			}
			level := gjson.GetBytes(got, "generationConfig.thinkingConfig.thinkingLevel").String()
			require.Equal(t, tc.wantLevel, level)
			require.False(t, gjson.GetBytes(got, "generationConfig.thinkingConfig.thinkingBudget").Exists())
			require.Equal(t, tc.wantThoughts, gjson.GetBytes(got, "generationConfig.thinkingConfig.includeThoughts").Bool())
		})
	}
}
