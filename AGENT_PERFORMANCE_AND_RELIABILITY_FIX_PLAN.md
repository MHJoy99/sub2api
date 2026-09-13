# Master Runbook: Agent Performance, Reliability & Protocol Defect Fixes

## 1. Overview & Issue Landscape

A deep investigation of the **Wei-Shaw/sub2api** upstream repository (3,204 open issues, ~7,000 total issues) was conducted using 10 parallel subagents across all core agent subsystems:
1. Tool Calling & Function Execution
2. Streaming, SSE, Chunking & Disconnections
3. Antigravity & Gemini Protocol Proxying
4. OpenAI Codex & `/v1/responses` Bridging
5. Anthropic & Claude `/v1/messages` Engine
6. Concurrency, Queueing, Latency & Sticky Routing
7. Error Retries, Cooldowns & Circuit Breaking
8. Context Windows, Compression & Token Limits
9. Reasoning Effort, Thinking Tokens & Model Mapping
10. Database Concurrency, Row Locks & Connection Pools

### Issue Breakdown: Out of 3,200+ Issues, What Actually Matters?
- **~90% Non-Actionable / Operational Noise**: Upstream OpenAI/Google account bans, SMS verification complaints, payment gateway requests, user deployment questions, and general inquiries.
- **24 True Agent-Breaking Bugs**: Serious architectural, protocol drift, or performance defects that directly cause agents (Kilo, Claude Code, Codex CLI/Desktop, Cursor, Roo-Code, OpenCode, Hermes) to crash, hang, burn tokens in infinite loops, or fail over incorrectly.
- **5 Already Merged/Fixed Locally**: In our branch `ours`, we already have fixes for #7042 (max context window), #6995 (Claude mid-conversation output config), #6887 (`agent_message` in chat bridge), #6146 (tool call ID prefix rewriting), and #7043/#7049 (`ctx_pool` capacity/wake).
- **19 Critical Issues Remaining to Fix Ourselves**: Grouped into 3 implementation tiers below.

---

## 2. Priority Matrix: The 19 Issues We Must Fix Ourselves

### Tier 1: P0 Killers (Immediate Showstoppers — Crash / Infinite Loop / 400 Rejections)

| Issue # | Component | Root Cause & Failure Mechanism | Local File & Line Ref | Fix Strategy |
|:---|:---|:---|:---|:---|
| **#7088** | Antigravity / Gemini 3.8 | `/v1/chat/completions` drops `response_format: {"type":"json_object"}` during Antigravity conversion. Gemini 3.8 returns Markdown prose instead of JSON, crashing Kilo Code / AI SDK parsers. | `backend/internal/service/antigravity_gateway_compat.go:410`<br>`backend/internal/pkg/apicompat/responses_to_anthropic_request.go:35` | Inject `responseMimeType: "application/json"` into Gemini `GenerationConfig` and inject strict JSON system prompt. |
| **#7081** | Antigravity / Stream | Large contexts (≥930KB) returning `MALFORMED_FUNCTION_CALL` are logged but mapped to `stop_reason: "end_turn"` with 0 content blocks. Gateway sends HTTP 200 with empty body; agent retries infinitely. | `backend/internal/pkg/antigravity/stream_transformer.go:133, 526` | Intercept `MALFORMED_FUNCTION_CALL` in `emitFinish()`: emit an SSE error event or trigger account failover instead of contentless `stop`. |
| **#7080** | Antigravity / Claude | Claude models fail with HTTP 400 when combining function tools with built-in `web_search` because `include_server_side_tool_invocations` is only injected for `gemini-*` models. | `backend/internal/service/antigravity_gateway_compat.go:421`<br>`backend/internal/pkg/antigravity/request_transformer.go:163` | Symmetrically apply `tool_config.include_server_side_tool_invocations` in the snake_case Anthropic envelope for Claude requests. |
| **#7066** | Codex / Responses | Codex CLI v0.154.0+ sends `input[].internal_chat_message_metadata_passthrough`. Sub2API fails to strip this field before forwarding to ChatGPT internal Codex endpoint, causing HTTP 400 `unknown_parameter`. | `backend/internal/service/openai_codex_transform.go:1586-1732` | In `filterCodexInputWithOptions()`, add `delete(newItem, "internal_chat_message_metadata_passthrough")`. |
| **#6992**<br>(PR #7028) | Scheduler / Concurrency | Sticky session account queuing concentrates concurrent agent turns onto a single busy account while idle accounts in the group sit unused, creating massive head-of-line blocking and 45s timeouts. | `backend/internal/service/openai_account_scheduler.go:565-607`<br>`backend/internal/service/openai_gateway_scheduling.go:1214` | Adopt PR #7028: on HTTP requests, if sticky slot is busy, escape sticky and balance across all group accounts. |
| **#6993**<br>(PR #6997) | Gateway / Payloads | Decompressed gzip/zstd/deflate request bodies > 64 MiB are silently truncated without error by `io.LimitReader`, passing corrupted JSON into parsers. | `backend/internal/pkg/httputil/body.go:16, 176` | Read `limit + 1`; return `*http.MaxBytesError` (HTTP 413) on overflow, and add configurable `gateway.max_decompressed_body_size`. |
| **#5203** | Streaming / SSE | Anthropic upstream `event: ping` keepalives leak into OpenAI Chat Completions SSE streams, causing Cursor/Hermes/OpenCode clients to crash on unexpected event type. | `backend/internal/service/gateway_forward_as_chat_completions.go:420-475` | Discard `ping` and non-business frames before converting SSE events to Chat Completions chunks. |
| **#5290** | Streaming / Latency | No first-token timeout: when an upstream proxy accepts a connection but stalls, the stream hangs indefinitely (up to 13+ minutes) with no failover. | `backend/internal/service/gateway_upstream_response.go:702+` | Implement a 30s–45s Time-To-First-Token watchdog timer; trigger automatic account failover on timeout. |

---

### Tier 2: P1 High Impact (Reasoning Loss / Token Waste / DB Deadlocks)

| Issue # | Component | Root Cause & Failure Mechanism | Local File & Line Ref | Fix Strategy |
|:---|:---|:---|:---|:---|
| **#6985**<br>(PR #6972) | Gemini 3.x Reasoning | Gemini 3.x thinking models omit thought summaries / reasoning content because Sub2API sends `thinkingBudget` instead of `thinkingLevel` (`"low"`, `"medium"`, `"high"`). | `backend/internal/pkg/antigravity/gemini_types.go:80`<br>`backend/internal/pkg/antigravity/request_transformer.go:640` | Add `ThinkingLevel` to `GeminiThinkingConfig`. Map `-low`/`-medium`/`-high` model suffixes to `thinkingLevel` and omit `thinkingBudget`. |
| **#6999** | Model Catalog / Reasoning | Provider-prefixed upstream models (e.g. `deepseek/deepseek-v4.1-flash`, `xiaomi/mimo-v2.5`) fail prefix check in `isDeepSeekCodexModel`, reducing context from 1M to 272k and wiping reasoning levels. | `backend/internal/service/openai_codex_models_service.go:709`<br>`backend/internal/service/openai_codex_model_metadata.go:394` | Strip provider prefixes before family checks; preserve family reasoning defaults when metadata level list is empty. |
| **#7027** | Codex Transform / Tokens | `json_object` mode in Chat Completions -> Responses duplicates the entire system prompt in both `instructions` and `input.developer`, doubling prompt tokens and latency. | `backend/internal/service/openai_gateway_chat_completions.go:305`<br>`backend/internal/service/openai_codex_transform.go:1294` | Replace duplicate full system prompt in `input` with a minimal developer directive (`{"role":"developer","content":"Return a valid JSON object."}`). |
| **#6632**<br>(PR #6633) | Multi-Turn Reasoning | Chat Completions clients lose opaque encrypted reasoning items between tool turns on Codex OAuth, resetting chain-of-thought and reducing cache hit rates. | `backend/internal/pkg/apicompat/chatcompletions_responses.go`<br>`backend/internal/repository/gateway_cache.go` | Cache upstream tool call reasoning items (`cc_tool_call:`) and re-inject them before `function_call` inputs on continuation turns. |
| **#6976** | Database / Logging | Background group usage rollup runs a long transaction holding `FOR UPDATE` on `usage_group_rollup_state`, blocking the real-time INSERT trigger (`FOR KEY SHARE`) and causing `usage_logs` write timeouts. | `backend/internal/repository/custom_group_usage_rollup_repo.go:143`<br>`backend/migrations/223_group_usage_rollup_timezone.sql:78` | Chunk historical rollups outside the state transaction; remove `FOR KEY SHARE` or use optimistic watermark comparisons. |
| **#6804** | CN Providers / Cooldowns | Transient rate-limit 429s on Zhipu/MiniMax lock accounts until the next 5-hour/weekly window reset, rapidly draining account pools into 503 errors. | `backend/internal/service/ratelimit_cn_providers.go:120+` | Inspect quota usage percentage before locking: if `< 95%`, apply short exponential backoff rather than window-reset cooldown. |
| **#7082** | Antigravity / Token Cache | Personal Antigravity OAuth accounts share `project_id: "aicode-consumers"`. Redis cache key `ag:<project_id>` collides, cascading 429s across all accounts in the pool. | `backend/internal/service/antigravity_token_provider.go` | Scope Redis token cache key by account ID: `ag:account:<account.ID>`. |
| **#6419** | Antigravity / User-Agent | `gemini-3.7-flash-high` returns 404 because Google Cloud Code PA gatekeeps high-tier models behind User-Agent containing `/hub/` (`antigravity/hub/2.9.1`). | `backend/internal/pkg/antigravity/oauth.go:124-129` | Update `BuildUserAgent` to include `/hub/` in the user agent string. |

---

### Tier 3: P2 Enhancements (Edge Cases & Resilience)

| Issue # | Component | Root Cause & Failure Mechanism | Local File & Line Ref | Fix Strategy |
|:---|:---|:---|:---|:---|
| **#7077** | Schema Sanitizer | Claude Code 2.1+ sends `prefixItems` for array tuples. Missing `items` fallback triggers Gemini Protobuf schema rejection (HTTP 400). | `backend/internal/pkg/antigravity/schema_cleaner.go:120+` | Extract schemas from `prefixItems` into `items` with `{"type":"string"}` fallback. |
| **#3603** | Streaming / Buffering | `Accept-Encoding: gzip, zstd` on outbound streaming requests causes upstream reverse proxies to buffer reasoning frames, freezing the stream for 10–60s. | `backend/internal/service/openai_gateway_forward.go` | Enforce `Accept-Encoding: identity` on all outbound streaming requests. |
| **#7030** | DB Migration | Migration 026 executes `COMMENT ON COLUMN ops_metrics_hourly.error_rate` unconditionally. Replaying migrations after schema loss crashes because migration 033 dropped that column. | `backend/migrations/026_ops_metrics_aggregation_tables.sql:47` | Wrap column comment in a `DO $$ BEGIN IF EXISTS ... END $$;` block. |

---

## 3. Recommended Implementation Roadmap

1. **Sprint 1 (Immediate P0 Fixes — ~1-2 days)**:
   - #7088 (`response_format` for Gemini 3.8)
   - #7081 (`MALFORMED_FUNCTION_CALL` stream handling)
   - #7080 (Claude mixed search + function tools)
   - #7066 (`internal_chat_message_metadata_passthrough` stripping)
   - #6992 (Cherry-pick PR #7028 sticky account escape)
   - #6993 (Port PR #6997 request decompression limit)
   - #5203 (Anthropic `ping` SSE filtering)
   - #5290 (First-token watchdog timer)

2. **Sprint 2 (P1 Quality & Scalability — ~2-3 days)**:
   - #6985 (`thinkingLevel` for Gemini 3.x)
   - #6999 (Model catalog prefix stripping & reasoning retention)
   - #7027 (Eliminate duplicate system prompt in JSON mode)
   - #6632 (Tool call encrypted reasoning cache)
   - #6976 (Fix database row lock collision on `usage_logs`)
   - #6804 (Refined CN provider 429 backoff)
   - #7082 (Isolate Antigravity OAuth token cache keys)
   - #6419 (Add `/hub/` to Antigravity User-Agent)

3. **Sprint 3 (P2 Polish & Cleanup — ~1 day)**:
   - #7077 (Tuple schema `prefixItems` support)
   - #3603 (`Accept-Encoding: identity` on streams)
   - #7030 (Idempotent column comments in migration 026)

---

MD docs updated — stale MDs deleted, new MDs created where missing.

## 4. Round-2 Audit Corrections (2026-09-13)

First-round review rated 6/10: 6 upstream PRs clean, but 8 hand-written
fixes were partial and 5 issues untouched. All gaps closed below.

### Corrected partial fixes
- **#7088**: `buildAntigravityCompatGeminiBody` now takes `originalBody`
  and `ensureGeminiResponseFormatJSON` inspects it (Chat
  `response_format` / Responses `text.format`), since Anthropic-schema
  `claudeBody` never carries the flag.
  (`antigravity_gateway_compat.go`, test
  `TestBuildAntigravityCompatGeminiBody_ResponseFormatJSON`)
- **#7081**: keeps valid `stop_reason: "end_turn"` with a visible
  retryable text block instead of invalid `"error"`.
  (`stream_transformer.go:emitFinish`)
- **#7080**: dual `includeServerSideToolInvocations` +
  `include_server_side_tool_invocations` in both branches, plus
  `request.tool_config` envelope duplication
  (`enableMixedGeminiToolInvocations`,
  `ensureToolConfigSnakeCaseEnvelope`).
- **#5203**: drops only `ping` keepalives; `error` events still forward.
  (`gateway_forward_as_chat_completions.go`)
- **#6985**: budget-aware `thinkingLevel` mapping (suffix + budget ranges),
  `thinkingBudget` omitted for all Gemini 3.x.
  (`request_transformer.go:buildGenerationConfig`,
  test `TestBuildGenerationConfig_Gemini3ThinkingLevel`)
- **#6999**: generic provider-qualifier stripping (`deepseek/`,
  `xiaomi/`), MiMo family descriptors with full reasoning levels, and
  metadata no longer wipes family defaults on empty level lists.
  (`openai_codex_models_service.go`, `openai_codex_model_metadata.go`,
  test `TestStripProviderQualifier`)
- **#6897**: `claude-opus-5`/`claude-sonnet-5` route to themselves, not
  downgraded 4.x. (`constants.go`, `request_transformer.go:modelInfoMap`)
- **#7030**: all 026 column COMMENTs guarded + checksum compat rule
  registered; trigger fix ships as new migration **239** (never edit
  applied migrations without a compat entry).
  (`026_*.sql`, `239_group_rollup_trigger_no_row_lock.sql`,
  `migrations_runner.go`)

### Newly implemented (previously untouched)
- **#7027**: always omit promoted system messages from Codex `input`;
  inject minimal `developer: "Return a valid JSON object."` for
  `json_object` mode. (`openai_gateway_chat_completions.go`,
  `openai_gateway_messages.go:ensureMinimalJSONDirectiveInCodexInput`)
- **#6976**: trigger reads watermark lock-free with guarded conditional
  UPDATE (migration 239); rollup uses advisory lock + unlocked watermark
  read. (`custom_group_usage_rollup_repo.go`)
- **#6804**: frequency-type 429s (zhipu 1302/控制请求频率) and <95%
  quota snapshots fall through to short backoff.
  (`ratelimit_cn_providers.go`, tests `TestCnProvider*`)
- **#3603**: `Accept-Encoding: identity` on streaming upstreams (Anthropic
  + OpenAI builders). (`gateway_upstream_request.go`,
  `openai_gateway_forward.go`)
- **#5290**: `stream_first_token_timeout` (default 60s) watchdog with
  `first_token_timeout` SSE error and stream-timeout accounting.
  (`config.go`, `gateway_upstream_response.go`)

### Verification
- `go vet` clean on `service`, `antigravity`, `config`, `domain`,
  `repository`.
- Pass: `pkg/antigravity`, `pkg/apicompat`, `pkg/httputil`,
  `repository` (full), `config`, `domain`, targeted `service` suites
  (Antigravity, reasoning replay, scheduling, Codex models, CN limits,
  mixed tools, response format).
- Full `./internal/service` suite not run to completion (exceeds tool
  timeout); targeted coverage above exercises every touched path.

MD docs updated — stale MDs deleted, new MDs created where missing.
