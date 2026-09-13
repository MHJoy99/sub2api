# New Model Onboarding Runbook

How fast a new upstream model becomes usable in Sub2API. Facts from code, no assumptions.

## Resolution order (pricing, fail-closed, never $0)

`backend/internal/service/model_pricing_resolver.go:71-128`:
`Group → Channel → LiteLLM → Fallback`, fail-closed `ErrModelPricingUnavailable` (`backend/internal/service/billing_service.go:226,1390`).

* Group: `matchGroupModelPricing` `model_pricing_resolver.go:146-167`
* Channel: `lookupChannelPricingNormalized` `:190-202`
* Base: `billingService.GetModelPricing` `billing_service.go:1334-1391` → `pricingService.GetModelPricing` `pricing_service.go:957-985` (`:956` comment) (LiteLLM) else `billing_service.go:966-1267 getFallbackPricing` (Fallback)
* Catalog JSON: `backend/resources/model-pricing/model_prices_and_context_window.json` (merged via `parsePricingData:423-535` (`:422` comment), `mergeFallbackPricingData:837-866`). New entry here = LiteLLM path, no code change (image-only entries skip token pricing via `TokenPricingAbsent`, `billing_service.go:1346-1348`).

## Exposure layers (3, independent)

1. **Account `model_mapping` = visibility.** `GET /v1/models` unions mapping **keys** of schedulable accounts: `backend/internal/service/gateway_service.go:1376-1458`, `backend/internal/handler/gateway_handler.go:1104-1172`. No mapping key = invisible (except static defaults). Frontend `frontend/src/composables/useModelWhitelist.ts:6-272` is helper only, backend does not validate against it.
2. **`composite_model_routes` = dispatch (composite groups only).** Never read by `/v1/models`. Priority `explicit route > ownership > detector`: `backend/internal/service/composite_route_resolver.go:25-103` (`:38-59` explicit, `:61-88` ownership, `:90-100` detector, `:101-102` fail-closed). Detector families: `composite_platform.go:90-154` (`claude-`, `gpt-/o1,o3-o5/codex-/dall-e-/tts-/whisper-` — no `o2` —, `gemini-/learnlm-`, `grok`, `k3/kimi-/moonshot-`, `glm-`, `deepseek-`). Admin CRUD: `POST /api/v1/admin/groups/:id/composite-routes`, `POST /:id/composite-routes/preview` (`backend/internal/server/routes/admin.go:337-341` full 5-route block, `POST` pair `:338-339`, `handler/admin/group_handler.go:293-391`, `service/admin_group.go:103-181`).
3. **Antigravity defaults = auto-availability.** `domain/constants.go:99 DefaultAntigravityModelMapping` + `service/account.go:622 resolveModelMapping` + `ensureAntigravityDefaultPassthroughs:736` + `claude_types.go:169 geminiModels` → `DefaultModels:208`. Note: antigravity defaults reach `/v1/models` via `resolveModelMapping→GetAvailableModels→writeModelsList`, not via `gateway_handler.go:1170` fallback (that fallback is `pkg/claude/constants.go:130` Anthropic-only).

## Fast path — known family, no redeploy (2–5 min)

Use when public ID matches detector family AND billing fallback `Contains` covers it (e.g. `gemini-3.x-flash`, `gpt-`, `claude-`, `grok-`, `kimi-k3/k2`, `glm-5.x`, `deepseek-v4`, `qwen3.x`, `mimo-`, `joyvoice`).

1. Add `<new-id>:<upstream-id>` (usually same) to one schedulable account's `model_mapping` in target group (Admin UI CreateAccountModal or account API). Custom Antigravity mappings auto-keep passthroughs unless wildcard overrides (`account.go:655-673,721-736`).
2. If composite group (4,7): `POST /api/v1/admin/groups/:gid/composite-routes {"public_model":"<new-id>","match_type":"exact","target_platform":"<anthropic|openai|gemini|antigravity|grok|kimi|zhipu|deepseek>","upstream_model":"<upstream>","endpoint":"any","priority":100,"enabled":true}`. Empty `upstream_model` backfilled to `public_model` for exact only (`composite_model_route.go:137-139`).
3. Verify: `POST .../preview {"model":"<new-id>","endpoint":"any"}`, `GET /v1/models` contains ID, tiny `POST /v1/chat/completions`, check `usage_logs.actual_cost != 0`.
4. No code, no restart except `ModelsListCache` TTL expiry (`config.go:1090`; `:1089` is `UserGroupRateCache`).

## Code path — new Antigravity public ID as default (10–20 min + deploy)

Needed when you want zero-touch for empty-mapping accounts + picker + pricing. Example: 2026-09-03 added 4 IDs (see `ANTIGRAVITY_GEMINI_MODEL_ROUTING_RUNBOOK.md:51-71`).

1. `backend/internal/domain/constants.go:99` add `"new-id":"new-id"` (passthrough) or `"alias":"upstream"` (rewrite, e.g. `joyvoice-fast-audio:125`).
2. `backend/internal/service/account.go:656-670` add to `ensureAntigravityDefaultPassthroughs` allowlist (or alias helper like `applyAntigravityJoyVoiceAlias:746`).
3. `backend/internal/pkg/antigravity/claude_types.go:169` add to `geminiModels` with correct `IsReasoning/DisplayName` (else reasoning params stripped via `IsGeminiReasoningModel:260`).
4. `frontend/src/composables/useModelWhitelist.ts:58-102` add to `antigravityModels` (convenience only).
5. Pricing: prefer JSON catalog entry; else `billing_service.go:296-951 initFallbackPricing` + `billing_service.go:966-1267 getFallbackPricing` branch (specific-before-generic ordering, e.g. `opus-5` before `4-5`, `glm-5.x` before bare `glm-5`, `vision-exp` before `flash`), + `pricing_service.go:1118-1132 normalizeGeminiThinkingTierAlias` if tiered alias.
6. Deploy: `SUB2API_DRY_RUN=1 bash deploy/deploy-sub2api.sh && bash deploy/deploy-sub2api.sh` (never removes volumes). Verify `/v1/models`, non-stream + stream, upstream == requested, non-zero cost.

## When code/migration mandatory

* New `target_platform` outside 8: migration `CHECK` (`172_composite_model_routes.sql:15-17` + `227:5-8`), `composite_platform.go:195-203`, `group_handler.go:245 oneof` (8, no `composite`).
* Unknown `gpt-`: `billing_service.go:1214-1237` known-only (`normalizeKnownOpenAICodexModel`) else `nil :1266` → `ErrUnavailable` by design. Unknown GLM: generic `Contains glm-5 :1126` / `glm-4.5 :1153` still catch many; only truly unlisted (e.g. `glm-6`) returns nil (`:1110-1112` whitelist comment). Must add JSON or fallback entry.
* OpenCode Go segregation: provider-exclusive alias needs exactly one mapping owner + one exact route per composite group (`OPENCODE_GO_SEGREGATION_RUNBOOK.md:1-13`). Since 2026-09-10 the Go fleet is split into responses- and chat-mode account sets — add the alias only to the set matching the upstream endpoint (`OPENCODE_GO_ENDPOINT_SPLIT_RUNBOOK.md`).

## Verification checklist

* `GET /v1/models` has ID, no stale ID
* `POST` tiny request → 200, `upstream_model == requested`
* `usage_logs.total_cost/actual_cost != 0`
* Composite `preview` shows explicit route hit, not detector fallback

## Re-verified 2026-09-04

3 parallel read-only sub-agents checked every file:line above. All MATCH except fixed: `GetModelPricing 957` (not 956), `parsePricingData 423` (not 422), `concrete list 195-203`, `ModelsListCache 1090`, `o1,o3-o5` (no o2), GLM generic fallback nuance, antigravity-vs-claude fallback nuance. Deploy script `deploy/deploy-sub2api.sh:20,122-126,154,159-163` verified DRY_RUN + no-volume-remove.

## 2026-09-09 — pinned upstream model rot: OAuth image main model (fixed, needs deploy)

Symptom: `POST /v1/images/generations {"model":"gpt-image-2"}` → `503 No available compatible accounts` on group 7 despite healthy accounts. Live logs (`docker logs sub2api`) showed the real chain: OAuth account 5 selected → upstream 400 `The 'gpt-5.4-mini' model is not supported when using Codex with a ChatGPT account` (`openai_images_responses.go:925`) → failover → pool exhausted → 503 (`handler/openai_images.go:186`).

Root: `openAIImagesResponsesMainModel = "gpt-5.4-mini"` (`backend/internal/service/openai_images.go:42`, since `eea6f3888` Apr 2026) pins the reasoning `model` wrapping the `image_generation` tool on the ChatGPT Codex backend (`openai_images_responses.go:385`, `openai_codex_transform.go:1212-1215`). Upstream dropped mini support for Codex+ChatGPT accounts.

Fix: `gpt-5.4-mini` → `gpt-5.6-luna`. Evidence: `usage_logs` account 5 last 7d served `gpt-5.6-luna` 581× (latest today), `terra` 64×, `5.5` 24×, zero `5.4-mini`; luna input/output pricing also cheaper (`model_prices_and_context_window.json`). Tests referencing the constant use the symbol; hardcoded `gpt-5.4-mini` in tests are normalize-table/SSE-fixture/catalog cases, unaffected. `gofmt` + `go vet ./internal/service` clean.

Lesson: hardcoded upstream model IDs rot — when an entire endpoint family 503s with healthy accounts, check `docker logs` for the first upstream 400 before touching pool capacity. Verify after deploy: same curl → 200 + image bytes; `usage_logs` row with `account_id=5`, nonzero cost.
