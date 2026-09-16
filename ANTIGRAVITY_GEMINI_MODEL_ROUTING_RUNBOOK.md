# Antigravity Gemini model routing runbook

## Scope

This runbook records the September 2026 Antigravity OAuth model availability
check and the routing change for models actually reported by the upstream
`fetchAvailableModels` endpoint in `sub2api`.

## Finding

Google's public Gemini API documentation lists Gemini 3.8 Flash as GA:

<https://ai.google.dev/gemini-api/docs/latest-model>

That public release status does not grant the model to every Antigravity OAuth
subscription. A direct, metadata-only call to
`POST /v1internal:fetchAvailableModels` returned HTTP 200 for all five local
OAuth accounts, but `gemini-3.8-flash` was absent from every response. It is
therefore not advertised or enabled locally until an account reports it.

## Live Antigravity account probe

Read-only PostgreSQL inspection of `accounts` and `account_groups` found:

- 5 non-deleted Antigravity OAuth accounts.
- All 5 are `active` and `schedulable`.
- None was currently rate-limited, overloaded, temporarily unschedulable, or expired.
- 4 accounts have custom `credentials.model_mapping` objects.
- 1 account uses the default mapping.
- Only one custom mapping explicitly contained `gemini-3.7-flash-tiered`.
- All 5 accounts belong to the active `Antigravity Pool` group (group 2); group-level
  model routing is disabled, so account model mappings and scheduler eligibility
  determine availability.
- The live composite groups used by the deployment (groups 4 and 7) already have
  enabled `gemini-` prefix routes targeting `antigravity`; no new composite route
  row is required for this model.

The upstream capability probe did not make a generation request, did not refresh
tokens, did not update account metadata, and did not modify the database.

Each account reported these seven IDs that were missing from the live Sub2API
catalog at probe time:

- Public-looking generation models: `gemini-3-flash-agent`,
  `gemini-3.1-flash-lite`, `gemini-3.5-flash-extra-low`, and
  `gemini-3.5-flash-low`.
- Internal/undisplayed IDs: `chat_20706`, `chat_23310`, and
  `tab_jump_flash_lite_preview`. These are recorded for audit but are not added
  to the public model picker without upstream display/capability metadata.

## Source changes

The reported public models are exposed as exact passthroughs, never silently
rewritten to another Gemini version:

- `backend/internal/domain/constants.go` — adds
  the four reported public models to the default Antigravity mapping.
- `backend/internal/service/account.go` — auto-fills those passthroughs for
  accounts with custom mappings, so the four custom-mapped accounts do not lose
  access.
- `backend/internal/pkg/antigravity/claude_types.go` — exposes the reported
  public models through Antigravity `/v1/models` and Gemini model catalogs.
- `frontend/src/composables/useModelWhitelist.ts` — makes the four public IDs
  selectable in the admin UI.
- `backend/internal/service/billing_service.go` — adds fallback pricing for the
  reported Gemini 3.1 Flash Lite and 3.5 Flash aliases using the existing public
  pricing cards.
- `gemini-3.8-flash` is deliberately not included because no OAuth account
  reported it.
- Focused unit tests cover default mapping, custom-account passthrough,
  Antigravity model exposure, and billing.

## Activation

The checked-out source is updated and has been deployed to the local Compose
installation. For future releases, build and deploy a new image, restart only
the application, then verify:

```bash
SUB2API_DRY_RUN=1 bash deploy/deploy-sub2api.sh
bash deploy/deploy-sub2api.sh
```

The script uses the current image as a Docker cache source, takes a deployment
lock, recreates only the application, waits for its health check, verifies that
`/app/data` stayed on the same mount, and rolls back the application image if
the new container fails. It never removes persistent volumes or restarts
Postgres/Redis. Set `SUB2API_SKIP_BUILD=1` only when the desired image is
already tagged; use `SUB2API_HEALTH_TIMEOUT_SECONDS` to extend the health wait
on a slow host. Set `SUB2API_PULL_BASE_IMAGES=1` for an intentional base-image
refresh; leave it unset for the fastest cached deployment.

The previous 2026-09-03 image exposed `gemini-3.8-flash` before the upstream
capability check. It must be replaced with the corrected image before clients
use the model picker again. Record the new image digest and health/storage
results here after the corrective deployment.

Use the deployment's normal image/tag workflow if production does not use the
local Compose build. Do not edit `accounts.credentials` directly: the new
passthrough is applied by `GetModelMapping`, and direct SQL would bypass the
repository/cache invalidation path.

After the corrected image is running, the existing composite `gemini-` prefix
routes will dispatch the four public reported IDs to the Antigravity pool. Do
not add a `gemini-3.8-flash` route until the upstream probe reports it.

## Post-deploy verification

1. Confirm `GET /v1/models` contains the four public reported IDs and does not
   advertise `gemini-3.8-flash`.
2. Confirm a minimal non-streaming request and a streaming request through the
   intended Antigravity API route for one reported public ID.
3. Confirm the response's upstream model is the exact requested reported ID.
4. Confirm the usage record has non-zero pricing and the expected requested and
   upstream model fields.
5. Repeat the read-only upstream model probe after an OAuth token refresh or
   Antigravity release; only enable new IDs that the upstream actually reports.

## Important limitation

GA in the public Gemini API does not prove that an Antigravity OAuth subscription
exposes the model on its private `v1internal` endpoint. The current five-account
probe specifically found no `gemini-3.8-flash`; it must remain disabled until a
future upstream response includes it.

## Full OAuth model inventory — 2026-09-03

A direct upstream audit used each account's existing access token and project
ID without invoking token refresh or the generation test endpoint. All five
accounts returned HTTP 200 and the same 25 model IDs. The seven IDs absent from
the old live Sub2API catalog were:

| Upstream ID | Upstream display/capability signal | Action |
|---|---|---|
| `gemini-3-flash-agent` | Gemini 3.5 Flash (High), thinking, 65,536 output | Add public exact passthrough |
| `gemini-3.1-flash-lite` | Gemini 3.1 Flash Lite, 65,535 output | Add public exact passthrough |
| `gemini-3.5-flash-extra-low` | Gemini 3.5 Flash (Low), thinking, 65,536 output | Add public exact passthrough |
| `gemini-3.5-flash-low` | Gemini 3.5 Flash (Medium), thinking, 65,536 output | Add public exact passthrough |
| `chat_20706` | No display metadata | Record only; do not publish |
| `chat_23310` | No display metadata | Record only; do not publish |
| `tab_jump_flash_lite_preview` | No display metadata; 4,096 output | Record only; do not publish |

`gemini-3.8-flash` was absent from all five responses and is not an OAuth
available model at this time. Existing local aliases that map to a reported
upstream model remain valid; “not listed by the upstream” must not be confused
with “alias missing from the local catalog.”

## Client compatibility recommendation — 2026-09-03

For an IDE extension, use **Kilo Code in VS Code** as the primary Sub2API
client. The repository exposes OpenAI-compatible Chat Completions and
Responses routes, and Kilo supports custom OpenAI-compatible providers and
streaming. Configure the provider with:

```text
Base URL: https://<sub2api-host>/v1
API key:  <Sub2API user API key>
Model:    go-muse-spark-1.3-contributor
```

For a terminal-first workflow, use **OpenCode**. Its custom-provider
configuration supports an OpenAI-compatible `baseURL`, and its native OpenCode
Go model name is `muse-spark-1.3-contributor`; when routing through Sub2API,
use the public Sub2API alias `go-muse-spark-1.3-contributor` instead. The live
deployment verified both non-streaming and streaming requests for this alias.

Use **Claude Code** when the target route is Anthropic Messages or a Claude
OAuth account, and use **Gemini CLI** when the target is the native Gemini
CLI/Code Assist OAuth path. They are supported paths, but they are not the
best primary client for the OpenCode Go/Muse route.

## Reasoning capability verification — 2026-09-03

The upstream OpenCode Go endpoint accepts these reasoning efforts for
`muse-spark-1.3-contributor`:

```text
none, minimal, low, medium, high, xhigh
```

`max` is **not** accepted. A live request through the deployed Sub2API route
with `reasoning.effort = max` returned an upstream validation error stating that
`max` is unknown. The same route with `reasoning.effort = xhigh` completed with
HTTP success, returned a `reasoning` output item, and reported reasoning tokens
in usage. Therefore `xhigh` is the highest supported upstream effort for this
model; it is not equivalent to a `max` request parameter.

The catalog card declares Responses mode and a 65,536 maximum output-token
limit. That output limit is separate from reasoning effort and does not mean
that every request will consume 65,536 reasoning tokens. Kilo/OpenCode clients
must send `xhigh` for the highest setting, or Sub2API must explicitly map a
client-side `max` value to upstream `xhigh` before forwarding.

This was independently confirmed on the direct OpenCode Go endpoint
`https://opencode.ai/zen/go/v1/responses` using the active account's existing
credential: `reasoning.effort = max` returned the same upstream validation error,
while `reasoning.effort = xhigh` was accepted and reported reasoning tokens.

## 16) Gemini 3.7 Flash SafetySettings & ThinkingConfig Fix — 2026-09-03

- Injected `DefaultSafetySettings` (`Threshold: "OFF"` across all 5 harm categories) in `request_transformer.go` and `gemini_messages_compat_service.go` to prevent Google from triggering silent content blocks with `finishReason: "SAFETY"`.
- Added `thinkingConfig` extraction (`includeThoughts: true`, `thinkingBudget: ...`) in `convertClaudeGenerationConfig`.
- In `StreamingProcessor.emitFinish()`, added explicit fallback notice for `SAFETY` / `BLOCKLIST` when zero content is generated to prevent empty-turn freezes in IDE clients.
- Deployed and verified healthy via `deploy/deploy-sub2api.sh`.

## 17) JoyVoice `joyvoice-fast-audio` alias restore — 2026-09-03

`joyvoice-fast-audio` is a Sub2API-only custom alias, never an upstream
Google model. History (`sub2api_usage_full.csv`, 221 rows, last 2026-08-08)
shows it served as `requested_model` on `/v1/chat/completions` and
`/chat/completions`, mapped to upstream `gemini-2.5-flash` via
`/v1internal:streamGenerateContent` at ~1.1-2.1s per transcription. The
audio fast-path code (`buildAntigravityAudioGeminiBody`, apicompat
`input_audio` bridges, billing fallback) was intact, but the alias was
missing from every code catalog, so `/v1/models` and the scheduler dropped
it after the late-August catalog rebuilds. It now rides the fast path again:
`input_audio` parts bypass the Claude translation and go direct to Gemini
`inlineData`.

Code (exact restore, upstream stays `gemini-2.5-flash`):

- `backend/internal/domain/constants.go` — `"joyvoice-fast-audio":
  "gemini-2.5-flash"` in `DefaultAntigravityModelMapping`.
- `backend/internal/pkg/antigravity/claude_types.go` — `joyvoice-fast-audio`
  entry in `geminiModels` (drives `/v1/models` + Gemini `v1beta/models`).
- `backend/internal/service/account.go` — `applyAntigravityJoyVoiceAlias`
  fills `joyvoice-fast-audio -> gemini-2.5-flash` on custom-mapped accounts
  when absent (an identity passthrough would be wrong here: upstream has no
  `joyvoice-*` model).
- `frontend/src/composables/useModelWhitelist.ts` — alias in
  `antigravityModels` so the picker/whitelist does not filter it.
- Billing needs no change: `billing_service.go` already has the exact
  `joyvoice-fast-audio` card plus the `strings.Contains(modelLower,
  "joyvoice")` rule ($0.30/$2.50/$0.03 per MTok).

Live DB (if the composite rows are gone, re-add; group 7 mirrors group 4):

```sql
INSERT INTO composite_model_routes
  (group_id, public_model, match_type, target_platform, upstream_model,
   endpoint, priority, enabled, notes)
VALUES
  (4, 'joyvoice-fast-audio', 'exact', 'antigravity', 'gemini-2.5-flash',
   'any', 10, true, 'Restored'),
  (7, 'joyvoice-fast-audio', 'exact', 'antigravity', 'gemini-2.5-flash',
   'any', 10, true, 'Restored')
ON CONFLICT DO NOTHING;
```

Verify: `GET /v1/models` lists `joyvoice-fast-audio`; `POST
/v1/chat/completions {"model":"joyvoice-fast-audio", ...input_audio...}` ->
200 with a usage row (`requested_model=joyvoice-fast-audio`,
`upstream_model=gemini-2.5-flash`).

Live-verified 2026-09-03 after `deploy/deploy-sub2api.sh` (image
`sub2api:deploy-20260902225912-c103d4bb4`, `/app/data` preserved): model
list went 76 -> 77 with `joyvoice-fast-audio` present; a minimal WAV
`input_audio` transcription returned HTTP 200, upstream
`gemini-2.5-flash`, `finish_reason=stop`, 1.27s wall / 1226ms billed, usage
row `265 in / 6 out / $0.0000945` on `/v1internal:streamGenerateContent`.

Faster option (one-line change, not applied): point the alias at
`gemini-2.5-flash-lite` ($0.10/$0.40 card, distilled for latency) in
`constants.go` plus its billing card. Tradeoff is slightly lower accuracy
on noisy audio; A/B against `test.m4a` first. Do NOT point it at
`gemini-2.5-flash-native-audio-*`: those are AI-Studio-only and the Sep-03
Antigravity probe did not advertise them, so they would 404 on this path.

## 18) Upstream merge to v0.2.0 — 2026-09-03

Merged `origin/main` (0.1.183 -> 0.2.0, 236 commits, diverged since Aug 26)
into `ours` with zero conflicts. Trial-merged first in a scratch worktree:
full `go build ./...` clean, JoyVoice alias/fast-path/billing all intact and
passing on the merged tree. New upstream migrations 231-233 are schema-only
`ADD COLUMN IF NOT EXISTS` with behavior-preserving defaults; live DB was at
230 so they apply on next deploy.

One mechanical fixup: upstream aligned DeepSeek fallback prices to official
peak/off-peak rates, so `billing_fallback_mapping_test.go` DeepSeek rows were
updated to the off-peak card (flash $0.22/$0.66/$0.007, pro $0.66/$1.98/$0.022
per MTok). Live-billing notes: DeepSeek now bills 2x in weekday peak windows,
and long-context tiers are catalog-data-driven instead of hardcoded.

## 19) Upstream re-probe — 2026-09-13: two new models mapped

Read-only `POST /v1internal:fetchAvailableModels` probe (existing access
token + project ID per account, no token refresh, no generation calls) across
all 5 active Antigravity OAuth accounts (ids 1, 3, 4, 7, 25; a 6th account,
id 2, is soft-deleted): HTTP 200 everywhere, identical 27-model union on every
account (up from 25 on 2026-09-03), 1 deprecated redirect
(`gemini-3.1-pro-high` -> `gemini-pro-agent`, already handled by
`applyAntigravityGemini31ProAliases`).

New upstream IDs since the Sep-03 probe (both reported by all 5 accounts):

| Upstream ID | Upstream signal | Action |
|---|---|---|
| `gemini-3.5-flash-lite` | Display "Gemini 3.5 Flash Lite", 1M in / 65,535 out, non-thinking | Public exact passthrough, official upstream name |
| `gemini-3.8-flash-tiered` | No display name, supportsImages + supportsThinking + supportsVideo, recommended, 1M in / 65,536 out (same shape as 3.6/3.7-tiered) | Public exact passthrough, official upstream name, display "Gemini 3.8 Flash Tiered" |

`gemini-3.8-flash` (bare, GA in the public Gemini API) is still absent
upstream — only the `-tiered` variant is reported. Bare `3.8-flash` stays
unmapped until an account reports it. Internal IDs `chat_20706`,
`chat_23310`, `tab_jump_flash_lite_preview` remain record-only, not published.

Source changes (same 4-layer pattern as Sep-03):

- `backend/internal/domain/constants.go` — both IDs added to
  `DefaultAntigravityModelMapping` as identity passthroughs.
- `backend/internal/service/account.go` — both IDs added to
  `ensureAntigravityDefaultPassthroughs`, so the 3 custom-mapped accounts
  (ids 1, 3, 25) auto-gain them; ids 4 and 7 use the default mapping.
- `backend/internal/pkg/antigravity/claude_types.go` — both IDs added to
  `geminiModels` (`3.8-flash-tiered` with `IsReasoning: true` per upstream
  `supportsThinking`; `3.5-flash-lite` non-reasoning). Drives `/v1/models`
  and Gemini `v1beta/models`.
- `frontend/src/composables/useModelWhitelist.ts` — both IDs added to
  `antigravityModels` (picker convenience only).
- `backend/internal/service/billing_service.go` — new `gemini-3.8-flash`
  fallback card ($0.75/$3.75/$0.075 per MTok, priced against the 3.7 tiered
  predecessor; official 3.8 pricing unpublished) plus a
  `gemini-3.8-flash` `Contains` rule so token-bearing requests never record
  $0. `gemini-3.5-flash-lite` needs no new card: the existing
  `gemini-3.5-flash` `Contains` rule already covers it.
- Tests: `billing_fallback_mapping_test.go` (+3 rows),
  `claude_types_test.go` (+2 IDs), `account_wildcard_test.go` (+2
  passthroughs), `antigravity_model_mapping_test.go` (+2 cases).

No composite-route rows needed: groups 4 and 7 both carry an enabled
`gemini-` prefix route to `antigravity`, which dispatches the new `gemini-*`
IDs. No new `target_platform`, no migration.

Deployed 2026-09-13 via `deploy/deploy-sub2api.sh` (dry-run clean, full
build clean, health check passed, `/app/data` preserved on
`deploy_sub2api_data`; image `sub2api:deploy-20260913014237-c1bb1dbad`).
Post-deploy verification, all PASS:

- `GET /v1/models` (120 entries) contains `gemini-3.5-flash-lite` and
  `gemini-3.8-flash-tiered`, and correctly does NOT advertise bare
  `gemini-3.8-flash`.
- Non-stream `POST /v1/chat/completions {"model":"gemini-3.5-flash-lite"}`
  → 200, content `OK`, `finish_reason=stop`; `usage_logs`:
  `requested=upstream=gemini-3.5-flash-lite`, cost $0.0000802, account 25.
- Stream `POST ... {"model":"gemini-3.8-flash-tiered","stream":true}` → 200,
  chunks + `data: [DONE]`; `usage_logs`: `requested=upstream`,
  cost $0.00020925. Follow-up non-stream prompt answered
  `2 + 2 equals 4.`, confirming real generation (one earlier
  `Say the word OK` probe returned empty text with 60 completion tokens —
  upstream thinking-model quirk, same class as the Sep-03 `SAFETY`-empty
  notes; routing and billing were correct in that call too).
- Unit tests: `internal/pkg/antigravity` + `internal/domain` ok; targeted
  service tests (78 subtests incl. all new mapping/pricing cases) PASS.

## 2026-09-13 — upstream merge: full Gemini 3.7/3.8 flash families

Merged `origin/main` (404 commits, `c6e6208a7`) into `ours`. Upstream had
independently added the complete 3.7 and 3.8 thinking-tier families, so this
supersedes the "bare `3.8-flash` absent" note above — both bare IDs now exist
with billing cards and `Contains` rules:

- `domain/constants.go` + `pkg/antigravity/claude_types.go`: `gemini-3.7-flash`
  and `gemini-3.8-flash` each with `-high/-low/-medium/-tiered` variants.
- `service/billing_service.go:494-517,1150-1155`: fallback price cards
  ($0.75/$3.75/$0.075 per MTok) + billable `Contains` rules for both bases.
- `resources/model-pricing/model_prices_and_context_window.json`: upstream
  litellm mirror (covers 2.5-flash/3.5-flash/3-pro-preview, so older local
  fallback cards are dormant but harmless).
- `frontend/src/composables/useModelWhitelist.ts`: picker gained the 8
  base/high/low/medium IDs (previously only `-tiered`).

Preserved our deltas: Sep-03 probe batch (`3-flash-agent`, `3.1-flash-lite`,
`3.5` extra-low/low/lite), JoyVoice alias + billing, `gemini-pro-agent` /
`gpt-oss` / `tab_flash` / longcat / hy3 billing, `synthesizeOpenCodeSessionHeader`
fallback (re-based onto upstream's #6581 session plumbing). Our three
`ensureOpenCodeSessionForAccountTest` call sites converted to upstream
`applyOpenCodeSessionHeader(c, acct, url, headers, payloadBytes)`.

Verified in trial worktree: `go build ./...` clean, `service` (27 PASS),
`antigravity` + `domain` + `handler` suites green. Pushed `mhjoy/ours`,
deployed, `/v1/models` re-checked post-deploy.

## False-429 fix — 2026-09-16 (gemini-3.8 as sub-agent)

Symptom: Codex heavy turns on `gemini-3.8-flash-high/-tiered` (55-61 tools,
~266KB body, full AGENTS.md context) failed as 429 on all 5 Antigravity
accounts in a row (15 upstream hits: 3 retries x 5 accounts, ~30s), while
tiny prompts on the same model returned 200 and Muse Spark (separate OpenAI
pool) stayed green. Failover cannot help because every account rejects the
same prompt identically.

Root cause: not real quota. Our v1internal envelope sent
`requestType: "agent"` with `userAgent: "antigravity"`. Google's Cloud Code
Assist gateway applies an instruction-hierarchy filter on `systemInstruction`
when `requestType: "agent"` is present and answers convention-heavy prompts
(RFC 2119 / `<system-conventions>` / large AGENTS.md sub-agent context) with
false `429 RESOURCE_EXHAUSTED`. Matches upstream reports oh-my-pi #11883
(fix PR #11884: omit `requestType`) and Aether PR #818 (same omission,
bisected against `daily-cloudcode-pa.googleapis.com`); the official
Antigravity client sends `userAgent` only, no `requestType`.

Fix (2 files, deployed 2026-09-16, image healthy):
- `backend/internal/pkg/antigravity/request_transformer.go:94-103`:
  `requestType := ""` (was `"agent"`); `omitempty` drops the field. Pure
  `web_search` keeps its fallback envelope.
- `backend/internal/service/antigravity_gateway_service.go:621-631`
  (`wrapV1InternalRequest`, the Codex `/v1/responses` path): removed
  `"requestType": "agent"` from the envelope map.

Verification (live, post-deploy, account 3 served all, zero failovers):
- `go test ./internal/pkg/antigravity/ -run
  "TestTransformClaudeToGemini|AgentFixup|IsGemini3"` green; `TestUnwrapV1InternalResponse` green.
- Heavy sub-agent scale: AGENTS.md instructions + 60 tools (~130KB) on
  `-high` and `-tiered` -> 200; 5x parallel fan-out -> 5x 200, no 429.
- Oversize trigger shape: RFC 2119 + AGENTS.md x9 + 58 tools (358KB, larger
  than the 266KB failure) -> 200 in ~4.7s.
