//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Distinct helper names (synth* prefix) to avoid colliding with upstream
// openai_opencode_session_test.go if it is ever pulled into this tree.

func synthSessionTestContext(t *testing.T, headers map[string]string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.Request = req
	return c
}

func synthSessionTestAccount() *Account {
	return &Account{ID: 24, Type: AccountTypeAPIKey}
}

const synthGoTarget = "https://opencode.ai/zen/go/v1/responses"

func TestSynthOpenCodeTrustBoundary(t *testing.T) {
	tests := []struct {
		name      string
		targetURL string
		want      string
	}{
		{"official origin", synthGoTarget, "conv-1"},
		{"official root path", "https://opencode.ai/zen/v1/chat/completions", "conv-1"},
		{"lookalike origin", "https://opencode.ai.evil.example/v1/responses", ""},
		{"subdomain untrusted", "https://api.opencode.ai/v1/responses", ""},
		{"plaintext rejected", "http://opencode.ai/zen/go/v1/responses", ""},
		{"other upstream", "https://api.openai.com/v1/responses", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := synthSessionTestContext(t, map[string]string{openCodeSessionHeader: "conv-1"})
			h := http.Header{}
			applyOpenCodeSessionHeader(c, synthSessionTestAccount(), tt.targetURL, h)
			require.Equal(t, tt.want, h.Get(openCodeSessionHeader))
		})
	}
}

func TestSynthCallerBeatsFixedOverride(t *testing.T) {
	c := synthSessionTestContext(t, map[string]string{openCodeSessionHeader: "caller-uuid"})
	h := http.Header{"X-Opencode-Session": []string{"fixed-account-value"}}
	applyOpenCodeSessionHeader(c, synthSessionTestAccount(), synthGoTarget, h)
	require.Equal(t, []string{"caller-uuid"}, h["X-Opencode-Session"])
}

func TestSynthOverrideKeptWhenCallerOmits(t *testing.T) {
	c := synthSessionTestContext(t, nil)
	h := http.Header{"X-Opencode-Session": []string{"fixed-account-value"}}
	applyOpenCodeSessionHeader(c, synthSessionTestAccount(), synthGoTarget, h)
	synthesizeOpenCodeSessionHeader(c, []byte(`{"model":"m"}`), synthSessionTestAccount(), synthGoTarget, h)
	require.Equal(t, "fixed-account-value", h.Get(openCodeSessionHeader))
}

func TestSynthSkipsNonAPIKey(t *testing.T) {
	c := synthSessionTestContext(t, map[string]string{openCodeSessionHeader: "conv-1"})
	h := http.Header{}
	oauth := &Account{ID: 5, Type: AccountTypeOAuth}
	applyOpenCodeSessionHeader(c, oauth, synthGoTarget, h)
	synthesizeOpenCodeSessionHeader(c, []byte(`{"model":"m"}`), oauth, synthGoTarget, h)
	require.Empty(t, h.Get(openCodeSessionHeader))
}

func TestSynthFallbackChain(t *testing.T) {
	t.Run("x-session-id", func(t *testing.T) {
		c := synthSessionTestContext(t, map[string]string{"X-Session-Id": "sid-1"})
		h := http.Header{}
		applyOpenCodeSessionHeader(c, synthSessionTestAccount(), synthGoTarget, h)
		synthesizeOpenCodeSessionHeader(c, []byte(`{"model":"m"}`), synthSessionTestAccount(), synthGoTarget, h)
		require.Equal(t, "sid-1", h.Get(openCodeSessionHeader))
	})
	t.Run("prompt_cache_key", func(t *testing.T) {
		c := synthSessionTestContext(t, nil)
		h := http.Header{}
		synthesizeOpenCodeSessionHeader(c, []byte(`{"model":"m","prompt_cache_key":"pc-1"}`), synthSessionTestAccount(), synthGoTarget, h)
		require.Equal(t, "pc-1", h.Get(openCodeSessionHeader))
	})
	t.Run("content seed stable", func(t *testing.T) {
		c := synthSessionTestContext(t, nil)
		body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
		h1, h2 := http.Header{}, http.Header{}
		synthesizeOpenCodeSessionHeader(c, body, synthSessionTestAccount(), synthGoTarget, h1)
		synthesizeOpenCodeSessionHeader(c, body, synthSessionTestAccount(), synthGoTarget, h2)
		require.NotEmpty(t, h1.Get(openCodeSessionHeader))
		require.Equal(t, h1.Get(openCodeSessionHeader), h2.Get(openCodeSessionHeader))
	})
	t.Run("scoped to official origin", func(t *testing.T) {
		c := synthSessionTestContext(t, map[string]string{"X-Session-Id": "sid-1"})
		h := http.Header{}
		synthesizeOpenCodeSessionHeader(c, []byte(`{"model":"m"}`), synthSessionTestAccount(), "https://api.openai.com/v1/responses", h)
		require.Empty(t, h.Get(openCodeSessionHeader))
	})
}
