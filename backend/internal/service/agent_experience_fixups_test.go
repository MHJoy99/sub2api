package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// #6804: frequency-type 429s must use short backoff, never window cooldown.
func TestCnProviderResponseIsFrequencyLimit(t *testing.T) {
	require.True(t, cnProviderResponseIsFrequencyLimit([]byte(`{"error":{"code":"1302","message":"[1302][您的账户已达到速率限制，请您控制请求频率]"}}`)))
	require.True(t, cnProviderResponseIsFrequencyLimit([]byte(`rate_limit_error: frequency exceeded`)))
	require.False(t, cnProviderResponseIsFrequencyLimit([]byte(`{"error":{"code":"insufficient_quota","message":"quota exhausted"}}`)))
	require.False(t, cnProviderResponseIsFrequencyLimit(nil))
}

func TestCnProviderQuotaNearlyExhausted(t *testing.T) {
	mkAccount := func(extra map[string]any) *Account {
		return &Account{Extra: extra}
	}
	require.True(t, cnProviderQuotaNearlyExhausted(mkAccount(map[string]any{"zhipu_5h_used_percent": 97.0})))
	require.True(t, cnProviderQuotaNearlyExhausted(mkAccount(map[string]any{"codex_5h_used_percent": 95.0})))
	require.False(t, cnProviderQuotaNearlyExhausted(mkAccount(map[string]any{"zhipu_5h_used_percent": 7.0, "zhipu_weekly_used_percent": 1.0})))
	require.False(t, cnProviderQuotaNearlyExhausted(mkAccount(map[string]any{})))
	require.False(t, cnProviderQuotaNearlyExhausted(nil))
}

// #7027: json_object mode must not duplicate the full system prompt.
func TestEnsureMinimalJSONDirectiveInCodexInput(t *testing.T) {
	t.Run("injects minimal directive", func(t *testing.T) {
		body := map[string]any{
			"input": []any{
				map[string]any{"role": "user", "content": "hello"},
			},
		}
		ensureMinimalJSONDirectiveInCodexInput(body)
		input, ok := body["input"].([]any)
		require.True(t, ok)
		require.Len(t, input, 2)
		first, ok := input[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "developer", first["role"])
		require.Contains(t, first["content"], "JSON")
	})

	t.Run("does not duplicate existing json hint", func(t *testing.T) {
		body := map[string]any{
			"input": []any{
				map[string]any{"role": "developer", "content": "Return a valid JSON object."},
				map[string]any{"role": "user", "content": "hello"},
			},
		}
		ensureMinimalJSONDirectiveInCodexInput(body)
		require.Len(t, body["input"], 2)
	})

	t.Run("nil safe", func(t *testing.T) {
		require.NotPanics(t, func() { ensureMinimalJSONDirectiveInCodexInput(nil) })
		require.NotPanics(t, func() { ensureMinimalJSONDirectiveInCodexInput(map[string]any{}) })
	})
}

// #6999: provider-qualified IDs must keep family reasoning descriptors.
func TestStripProviderQualifier(t *testing.T) {
	require.True(t, isDeepSeekCodexModel("deepseek/deepseek-v4.1-flash"))
	require.True(t, isDeepSeekCodexModel("deepseek-v4-flash"))
	require.True(t, isMimoCodexModel("xiaomi/mimo-v2.5"))
	require.True(t, isMimoCodexModel("mimo-v2.5"))
	require.False(t, isDeepSeekCodexModel("gpt-5"))
	require.False(t, isMimoCodexModel("gpt-5"))
}
