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
