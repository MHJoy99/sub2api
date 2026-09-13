# OpenCode Go Endpoint Split + Clean Alias Naming Runbook

Created 2026-09-10. Supersedes the single-mode `force_responses` layout of
`OPENCODE_GO_SEGREGATION_RUNBOOK.md` for account topology; that file's
exact-route/owner rule still applies.

## 1) Why the split exists (upstream endpoint contract)

OpenCode Go serves its catalog over **two incompatible upstream formats per
model**. Probed live 2026-09-10 with account keys against
`https://opencode.ai/zen/go/v1/{chat/completions,responses}` (docs:
https://opencode.ai/docs/go/#endpoints):

| Class | Models | chat/completions | responses |
|---|---|---|---|
| Both | `deepseek-flash`, `deepseek-v4-flash`, `deepseek-v4-flash-vision-exp`, `deepseek-v4-pro` | 200 | 200 |
| Responses-only | `muse-spark-1.3-contributor`, `gpt-5.6-luna`, `grok-4.6` | 500/401 | 200 |
| Chat-only | `glm-5.1/5.2/5.3/5.3-flash`, `qwen3.6-plus/3.7-max/3.7-plus/3.8-max`, `kimi-k2.6/k2.7-code/k3`, `minimax-m2.5/m2.7/m3`, `mimo-v2.5/v2.5-pro`, `longcat-2.0`, `hy3` | 200 | 401/500 |

Sub2api picks the upstream format **per account** via
`accounts.extra.openai_responses_mode` (`auto` / `force_responses` /
`force_chat_completions`); there is no per-model override
(`backend/internal/pkg/openai_compat/upstream_capability.go:38-59`).
A single account therefore cannot serve both classes — the old all-`go-*`
accounts forced responses and silently broke every chat-only model.

Upstream-dead models removed 2026-09-10 (chat/completions 400 "Model is
unavailable"; zero usage in 7d): `glm-5`, `grok-4.5`, `kimi-k2.5`,
`qwen3.5-plus`, `mimo-v2-omni`, `mimo-v2-pro`, `hy3-preview` (plus the
already-disabled `go-muse-spark-1.2-contributor` route). `minimax-m2.7`
was left mapped: upstream itself was returning 5xx in both formats at
verification time (not a sub2api defect).

## 2) Live account topology (DB ids)

| ID | Name | Mode | Mapping keys | Groups |
|---:|---|---|---:|---|
| 24 | OpenCode Go Responses 1 | `force_responses` | 13 | 6 (prio 1), 7 (prio 20) |
| 26 | OpenCode Go Responses 2 | `force_responses` | 13 | 6 (prio 1), 7 (prio 20) |
| 27 | OpenCode Go Responses 3 | `force_responses` | 13 | 6 (prio 1), 7 (prio 2) |
| 28 | OpenCode Go Chat 1 | `force_chat_completions` | 36 | 6 (prio 1), 7 (prio 2) |
| 29 | OpenCode Go Chat 2 | `force_chat_completions` | 36 | 6 (prio 1), 7 (prio 2) |
| 30 | OpenCode Go Chat 3 | `force_chat_completions` | 36 | 6 (prio 1), 7 (prio 2) |

All six: platform `openai`, type `apikey`, base_url
`https://opencode.ai/zen/go/v1`, concurrency 60, priority 5, active +
schedulable. 28/29/30 clone the same three Go keys as 24/26/27 (one key
per protocol account). Secrets live in DB only, never in docs/git.

Responses mapping (13): `go-muse-spark-1.3-contributor`,
`muse-spark-1.3-contributor`, `go-gpt-5.6-luna` (no bare form — it
collides with OAuth account 5's public model), `go-grok-4.6`, `grok-4.6`,
`go-deepseek-v4-flash`, `deepseek-v4-flash`, `go-deepseek-v4-pro`,
`deepseek-v4-pro`, `go-deepseek-v4-flash-vision-exp`,
`deepseek-v4-flash-vision-exp`, `go-deepseek-flash`,
`deepseek-v4.1-flash` → `deepseek-flash`.

Chat mapping (36): bare upstream id + legacy `go-<id>` for
`glm-5.1/5.2/5.3/5.3-flash`, `qwen3.6-plus/3.7-max/3.7-plus/3.8-max`,
`kimi-k2.6/k2.7-code/k3`, `minimax-m2.5/m2.7/m3`, `mimo-v2.5/v2.5-pro`,
`longcat-2.0`, `hy3`.

## 3) Naming scheme (nicer aliases alongsidse `go-*`)

- Legacy `go-<upstream>` aliases stay for compatibility.
- New **clean aliases** are the bare upstream id (except DeepSeek V4.1
  Flash, exposed as `deepseek-v4.1-flash`, and `gpt-5.6-luna`, excluded to
  avoid multi-owner collision).
- Every clean alias is a **mapping key with exactly one owner set** (the
  protocol that upstream serves) and has a group-7 `exact` route,
  `target_platform=openai`, `endpoint=any`, priority 20
  (`POST /api/v1/admin/groups/7/composite-routes`). Group 6 is platform
  `openai` (non-composite) and resolves via mapping keys only.
- `/v1/models` (group 6) = exactly **49** Go aliases after the cleanup.

## 4) Applied via Admin API (no SQL)

1. `POST /api/v1/admin/accounts/bulk-update` with full replacement
   `credentials.model_mapping` per set (bulk merge replaces the
   `model_mapping` top-level key; `api_key`/`base_url` preserved).
2. `POST /api/v1/admin/accounts` for 28/29/30 (extra:
   `openai_responses_mode=force_chat_completions`,
   `openai_responses_supported=false`, `upstream_billing_probe_enabled=false`,
   `upstream_billing_rate_sync_enabled=false`).
3. `POST /api/v1/admin/groups/7/composite-routes` for each new alias.
4. `DELETE /api/v1/admin/groups/7/composite-routes/:id` for the 15 dead
   routes (7 retired models × 2 + disabled 1.2 route).
5. Renames via `accounts update <id> --json '{"name":...}'`.

## 5) Verification (live, 2026-09-10)

- `GET /v1/models`: group 6 = 49 models (both `deepseek-v4.1-flash` and
  `muse-spark-1.3-contributor` present); group 7 = 132.
- 49-alias sequential sweep via group 7 `/v1/chat/completions` with a
  proper client User-Agent: **47×200**, only `minimax-m2.7` (both aliases)
  502 — upstream 5xx, reproduced on direct probes.
- `/v1/responses` `deepseek-v4.1-flash` + `reasoning.effort=medium` → 200,
  output items `[reasoning, message]`, `reasoning_tokens=14`.
- `usage_logs`: chat aliases route to 28/29/30, responses aliases to
  24/26/27; costs non-zero except `hy3`/`longcat-2.0` (see §6).
- `composite-routes/preview` for `deepseek-v4.1-flash` → `source=route`.

## 6) Pricing

- Clean aliases resolve through the same fallback cards as `go-*`
  (`stripCompositeBillingPrefix` → bare name).
- Added exact fallback cards (code, **deploy pending at time of writing**):
  `longcat-2.0` $0.30/$1.20, cache-read $0.006 per MTok;
  `hy3` $0.14/$0.58, cache-read $0.035 per MTok
  (`backend/internal/service/billing_service.go:896` and `:905`, rules
  `:1128-1133`, tests `billing_fallback_mapping_test.go:40,42`).
  `gofmt`/`go vet ./internal/service`/targeted unit test clean.
- Until that image is deployed, `hy3`/`longcat-2.0` record $0 with
  `openai_usage.pricing_missing_record_zero_cost` warning
  (`backend/internal/service/openai_gateway_usage.go:264`).
- `deepseek-v4.1-flash` bills on the DeepSeek flash card via the
  `deepseek-` prefix policy (`billing_service.go:1851`), peak-adjusted.

## 7) Operational gotchas (learned live)

- **Cloudflare 403 → account cooldown → snapshot exclusion.** Upstream
  fronts Go with Cloudflare; a request with a generic/blocked User-Agent
  gets `403 error code: 1010` / "blocked based on your browser's
  signature". Sub2api then writes `temp_unschedulable_until` = +10 min
  ("OpenAI 403 temporary cooldown (1/3)"), `IsSchedulable()` returns
  false (`backend/internal/service/account.go:195`), and the scheduler
  snapshot rebuild drops the accounts → group-wide 503
  "Service temporarily unavailable" / 404 model_not_found with
  `pool=1, filtered: model_not_supported=1`.
  Fix: `accounts reset-temp-unschedulable <id>` (Admin API
  `DELETE /api/v1/admin/accounts/:id/temp-unschedulable`); the outbox
  event rebuilds buckets within seconds. Always test with a real client
  User-Agent; never a bare Python/urllib default.
- **Scheduler snapshot is Redis-backed**, keys
  `sched:<group>:<platform>:<mode>:v<ver>` with `mode ∈
  {single,forced}` plus `sched:active/ready/epoch:*`, watermark
  `sched:outbox:watermark` (`backend/internal/repository/scheduler_cache.go:18-29`).
  Verify membership with `ZRANGE`; all six accounts must appear in both
  `6:*` and `7:*` openai buckets.
- Admin account writes emit outbox events; no `docker restart` is needed
  once cooldowns are cleared. (Direct SQL would still require a restart.)
- `minimax-m2.7` upstream is currently broken in both formats; leave the
  aliases mapped and retest later.

## 8) Rollback

- Delete the 28/29/30 accounts (or set `schedulable=false`), restore
  24/26/27 `model_mapping` from the pre-change backup
  (`/tmp/kilo/map24.json` at change time; rebuild from
  `PRICING_MAPPING_RUNBOOK.md` conventions otherwise), re-create the 7
  retired `go-*` routes if needed, and disable the new clean routes with
  `DELETE /api/v1/admin/groups/7/composite-routes/:id`.
- Identical keys are held by two accounts per key by design; deleting the
  chat twin does not affect the responses account.

## 9) Upstream repo status (deferred merge, 2026-09-10)

`origin/main` = v0.2.4 (`98d86915b`). `ours` is 23 ahead / 263 behind
(merge-base `5097b3145`). **No full merge performed** (slow VPS; live
traffic). Critical items to pick up later, in priority order:

1. `99eba19ff` (PR #6814) go-redis v9.22.0 nil-ctx **panic fix**.
2. `c227863d5` image URL backfill **SSRF fix** (private destinations).
3. `7ccc8a6f5` Image 2.5 + OAuth image main model (supersedes our
   `c1bb1dbad`).
4. `620eb3fd0` upstream #6581 OpenCode session forwarding (overlaps our
   `dce574fff`/`fd655795f`; reconcile carefully).
5. `cc91155fe` DeepSeek output-limit clamp across API protocols.
6. `dc46daa69` OpenAI reasoning-effort mapping deny.
7. Breaking: `cff3f8985` group model-allowlist enforcement + migration.

Merge on an idle window: `git merge origin/main` from `ours`, rebuild via
`SUB2API_BUILD_PROGRESS=plain bash deploy/deploy-sub2api.sh`.
