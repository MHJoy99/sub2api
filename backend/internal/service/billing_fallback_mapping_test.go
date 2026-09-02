package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestFallbackPricingCoversCompositeAndUnmappedAliases(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)

	tests := []struct {
		model                    string
		input, output, cacheRead float64
	}{
		{model: "gemini-3.7-flash", input: 0.75e-6, output: 3.75e-6, cacheRead: 0.075e-6},
		{model: "gemini-3.7-flash-tiered", input: 0.75e-6, output: 3.75e-6, cacheRead: 0.075e-6},
		{model: "gemini-3-flash-agent", input: 1.5e-6, output: 9e-6, cacheRead: 0.15e-6},
		{model: "gemini-3.1-flash-lite", input: 0.25e-6, output: 1.5e-6, cacheRead: 0.025e-6},
		{model: "gemini-3.5-flash-extra-low", input: 1.5e-6, output: 9e-6, cacheRead: 0.15e-6},
		{model: "gemini-3.5-flash-low", input: 1.5e-6, output: 9e-6, cacheRead: 0.15e-6},
		{model: "alibaba-token-plan-qwen3.8-max", input: 2e-6, output: 6e-6, cacheRead: 0.25e-6},
		{model: "go-qwen3.8-max", input: 2e-6, output: 6e-6, cacheRead: 0.25e-6},
		{model: "alibaba-token-plan-qwen3.7-plus", input: 0.4e-6, output: 1.2e-6, cacheRead: 0.05e-6},
		{model: "alibaba-token-plan-qwen3.7-max", input: 0.4e-6, output: 1.2e-6, cacheRead: 0.05e-6},
		{model: "alibaba-token-plan-qwen3.6-flash", input: 0.1e-6, output: 0.4e-6, cacheRead: 0.01e-6},
		{model: "alibaba-token-plan-deepseek-v4-flash-0731", input: 2.2e-7, output: 6.6e-7, cacheRead: 7e-9},
		{model: "alibaba-token-plan-deepseek-v4-pro-0813", input: 6.6e-7, output: 1.98e-6, cacheRead: 2.2e-8},
		{model: "accounts/fireworks/models/deepseek-v4-flash-0731", input: 2.2e-7, output: 6.6e-7, cacheRead: 7e-9},
		{model: "alibaba-token-plan-glm-5.2", input: 1.4e-6, output: 4.4e-6, cacheRead: 0.26e-6},
		{model: "go-glm-5", input: 1e-6, output: 3.2e-6, cacheRead: 0.2e-6},
		{model: "go-deepseek-v4-flash", input: 2.2e-7, output: 6.6e-7, cacheRead: 7e-9},
		{model: "go-deepseek-v4-pro", input: 6.6e-7, output: 1.98e-6, cacheRead: 2.2e-8},
		{model: "go-muse-spark-1.2", input: 1.25e-6, output: 4.25e-6, cacheRead: 0.15e-6},
		{model: "go-muse-spark-1.2-contributor", input: 0.1e-6, output: 0.2e-6, cacheRead: 0.002e-6},
		{model: "muse-spark-1.3-contributor", input: 0.1e-6, output: 0.2e-6, cacheRead: 0.002e-6},
		{model: "go-muse-spark-1.3-contributor", input: 0.1e-6, output: 0.2e-6, cacheRead: 0.002e-6},
		{model: "go-mimo-v2.5", input: 0.1e-6, output: 0.3e-6, cacheRead: 0.02e-6},
		{model: "gemini-pro-agent", input: 2e-6, output: 12e-6, cacheRead: 0.2e-6},
		{model: "gemini-3-pro-high", input: 2e-6, output: 12e-6, cacheRead: 0.2e-6},
		{model: "gemini-3-pro-low", input: 2e-6, output: 12e-6, cacheRead: 0.2e-6},
		{model: "tab_flash_lite_preview", input: 0.1e-6, output: 0.4e-6, cacheRead: 0.01e-6},
		{model: "gemini-2.5-flash-thinking", input: 0.3e-6, output: 2.5e-6, cacheRead: 0.03e-6},
		{model: "joyvoice-fast-audio", input: 0.3e-6, output: 2.5e-6, cacheRead: 0.03e-6},
		{model: "gpt-oss-120b-medium", input: 0.15e-6, output: 0.6e-6, cacheRead: 0.03e-6},
		{model: "ox-alpha-free", input: 0, output: 0, cacheRead: 0},
		{model: "go-ox-alpha-free", input: 0, output: 0, cacheRead: 0},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.NotNil(t, pricing)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, tt.cacheRead, pricing.CacheReadPricePerToken, 1e-12)
		})
	}
}
