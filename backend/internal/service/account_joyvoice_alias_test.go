package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestApplyAntigravityJoyVoiceAlias(t *testing.T) {
	t.Run("absent alias maps to gemini-2.5-flash, not identity", func(t *testing.T) {
		mapping := map[string]string{"gemini-3.7-flash-tiered": "gemini-3.7-flash-tiered"}
		applyAntigravityJoyVoiceAlias(mapping)
		require.Equal(t, "gemini-2.5-flash", mapping["joyvoice-fast-audio"])
		require.Equal(t, domain.DefaultAntigravityModelMapping["joyvoice-fast-audio"], mapping["joyvoice-fast-audio"])
	})

	t.Run("explicit custom override is preserved", func(t *testing.T) {
		mapping := map[string]string{"joyvoice-fast-audio": "gemini-2.5-flash-lite"}
		applyAntigravityJoyVoiceAlias(mapping)
		require.Equal(t, "gemini-2.5-flash-lite", mapping["joyvoice-fast-audio"])
	})

	t.Run("covering wildcard is preserved", func(t *testing.T) {
		mapping := map[string]string{"joyvoice-*": "gemini-2.5-flash"}
		applyAntigravityJoyVoiceAlias(mapping)
		_, exists := mapping["joyvoice-fast-audio"]
		require.False(t, exists)
	})

	t.Run("nil mapping is safe", func(t *testing.T) {
		require.NotPanics(t, func() { applyAntigravityJoyVoiceAlias(nil) })
	})
}
