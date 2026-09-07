package service

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const openCodeSessionHeader = "X-OpenCode-Session"

// applyOpenCodeSessionHeader forwards the caller-owned conversation identifier
// only to OpenCode's official API origin. The caller applies this after account
// header overrides so a per-conversation value cannot be replaced by a fixed
// account-wide override.
//
// Mirrors upstream Wei-Shaw/sub2api#6581 (fixes #6556): the header is a stable
// identifier, so it is bound to the actual HTTPS target host instead of a
// global OpenAI-compatible whitelist that would disclose it to unrelated
// upstreams.
func applyOpenCodeSessionHeader(c *gin.Context, account *Account, targetURL string, headers http.Header) {
	if c == nil || c.Request == nil || account == nil || account.Type != AccountTypeAPIKey || headers == nil {
		return
	}

	if !isOpenCodeOfficialTarget(targetURL) {
		return
	}

	sessionID := strings.TrimSpace(c.GetHeader(openCodeSessionHeader))
	if sessionID == "" {
		return
	}
	for key := range headers {
		if strings.EqualFold(key, openCodeSessionHeader) {
			delete(headers, key)
		}
	}
	headers.Set(openCodeSessionHeader, sessionID)
}

// isOpenCodeOfficialTarget reports whether targetURL is OpenCode's official API
// origin (exactly https://opencode.ai). Lookalikes, subdomains, and plaintext
// HTTP are rejected so the stable session identifier never leaks off-origin.
func isOpenCodeOfficialTarget(targetURL string) bool {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "https") && strings.EqualFold(parsed.Hostname(), "opencode.ai")
}

// synthesizeOpenCodeSessionHeader injects a stable per-conversation ID when the
// caller omitted one (e.g. Kilo Code VS Code extension, generic OpenAI
// clients). Local extension beyond upstream #6581, which deliberately leaves
// omission untouched — without this, header-less clients still get upstream
// 400 `missing x-opencode-session`.
//
// Priority after applyOpenCodeSessionHeader ran: explicit session identity
// (X-Session-Id / X-Session-Affinity / session_id / prompt_cache_key), then a
// deterministic tenant-isolated UUID from the content seed (stable across turns
// sharing the same system/first-prompt prefix), then a random UUID last resort
// (satisfies the presence check with no cache affinity — clients should send a
// stable ID). Scoped to the official origin like the verbatim path.
func synthesizeOpenCodeSessionHeader(c *gin.Context, body []byte, account *Account, targetURL string, headers http.Header) {
	if headers == nil || account == nil || account.Type != AccountTypeAPIKey {
		return
	}
	if account.UsesOpenAICodexProtocol() {
		return
	}
	if !isOpenCodeOfficialTarget(targetURL) {
		return
	}
	if strings.TrimSpace(headers.Get(openCodeSessionHeader)) != "" {
		return
	}
	if raw := sanitizeSessionID(explicitOpenAIRequestSessionID(c, body)); raw != "" {
		headers.Set(openCodeSessionHeader, raw)
		return
	}
	var apiKeyID int64
	if c != nil {
		apiKeyID = getAPIKeyIDFromContext(c)
	}
	if seed := deriveOpenAIContentSessionSeed(body); seed != "" {
		headers.Set(openCodeSessionHeader, generateSessionUUID(
			fmt.Sprintf("opencode-go:%d:%d:%s", apiKeyID, account.ID, seed)))
		return
	}
	headers.Set(openCodeSessionHeader, generateSessionUUID(""))
}
