# Pricing Mapping & Cost Backfill Runbook — sub2api (BDX)

> **For future AIs:** read this file first. It documents the exact end-to-end process used on 2026-08-28 to fix unmapped model pricing ($0 costs) and backfill historical usage so the Usage page shows accurate `Actual`/`Standard` costs at the same rate as each provider's public API.

## 1) What the user saw

Screenshot `https://gpt.bdx.market/usage` (Last 24 Hours, Admin → Usage Records):

| Model (as displayed) | Requests | Tokens | Actual | Standard |
|---|---|---|---|---|
| `gpt-5.6-luna` | 699 | 134.91M | **$9.33** | $9.33 |
| `gemini-3.7-flash-tier` (→ `gemini-3.7-flash-tiered`) | 445 | 43.04M | **$0.0000** | $0.0000 |
| `alibaba-token-plan-q…` (→ `alibaba-token-plan-qwen3.8-max`) | 57 | 3.94M | **$0.0000** | $0.0000 |
| `go-muse-spark-1.2-c` (→ `go-muse-spark-1.2-contributor`) | 1 | 84.29K | **$0.0000** | $0.0000 |
| `joyvoice-fast-audio` | 8 | 13.22K | $0.0020 | $0.0020 |

Exported CSV `sub2api_usage_full.csv` + live DB confirmed **2,366** token-bearing rows with `total_cost=0` across ~20 distinct model IDs. Root cause: the 2026-08-24 reset to pure `Wei-Shaw/main d45135d87` removed custom fallback price cards that had been added on the `ours` branch. Those cards are restored below.

## 2) Investigation — use read-only sub-agents

Per the user's instruction, use **read-only sub-agents** for every investigation step. Do not let "already verified" override a cheap local check. Three parallel sub-agents were used:

| Sub-agent | Prompt (planner/explore) | What it returned |
|---|---|---|
| **A — Pricing/Billing path** (`explore`, very thorough) | Trace `billing_service.go` / `gateway_usage_billing.go` / `openai_gateway_usage.go` / `pricing_service.go`: how `input_cost`/`output_cost`/`cache_*`/`total_cost`/`actual_cost` are computed, where `$0` is produced, which model name is used for billing, prefix handling, and whether a reprice endpoint exists. | Full formula trace + fail-open `$0` path at `gateway_usage_billing.go:1168` / `openai_gateway_usage.go:232`, trigger list for rollups, proof that **no reprice endpoint exists** — backfill must be direct SQL + aggregate refresh. `file_path:line` refs included. |
| **B — Git archaeology** (`explore`) | `git branch -a`, `git show backup-ours-20260824-043027:backend/...`, diff against `HEAD` for `billing_service.go` and `resources/model-pricing/*.json`. | Backup ref `backup-ours-20260824-043027` had `gemini-3.7-flash` (0.75/3.75/0.075) and `qwen3.8-max` (2/6/0.25) fallback cards + 8 JSON keys (`muse-spark-*`, `ox-alpha-free` family) deleted in the reset. |
| **C — Official provider pricing** (`general` + `tavily-search`) | DashScope / Google / Xiaomi / OpenAI official per-MTok prices for Qwen, Gemini, MiMo, gpt-oss. | DashScope international: `qwen3.8-max` $2/$6/$0.25, `qwen3.7-plus` $0.4/$1.2/$0.05, `qwen3.6-flash` $0.1/$0.4/$0.01, `gemini-3.7-flash` $0.75/$3.75/$0.075, `muse-spark-1.2` $1.25/$4.25/$0.15, `muse-spark-1.2-contributor` $0.10/$0.20/$0.002, `joyvoice-fast-audio` $0.30/$2.50/$0.03 (Gemini 2.5 Flash card). |

Live DB verification (psql via `docker exec sub2api-postgres`):

```sql
SELECT model, count(*) n FROM usage_logs
WHERE total_cost=0 AND created_at > now()-interval '40 days'
GROUP BY model ORDER BY n DESC;
-- 2,366 rows, top: gemini-3.7-flash-tiered 1526, alibaba-token-plan-qwen3.8-max 571, gemini-pro-agent 100, ...
```

Per-row regression on `sub2api_usage_full.csv` confirmed every non-zero row matches its card exactly (e.g. `gemini-3.6-flash` 1.5/7.5/0.15 error $0.0000).

## 3) Price cards — same as provider APIs

| Model (billing name) | Input $/MTok | Output $/MTok | Cache-read $/MTok | Notes |
|---|---|---|---|---|
| `gemini-3.7-flash` (+ `-tiered`) | 0.75 | 3.75 | 0.075 | Google, backup card |
| `gemini-3-pro-preview` / `-high` / `-low` / `gemini-pro-agent` | 2.0 | 12.0 | 0.2 | Gemini 3 Pro family |
| `gemini-2.5-flash` / `-thinking` / `joyvoice-fast-audio` | 0.30 | 2.50 | 0.03 | Gemini 2.5 Flash; joyvoice is token-billed on this card |
| `gemini-2.5-flash-lite` / `tab_flash_lite_preview` | 0.10 | 0.40 | 0.01 | Flash-Lite class |
| `qwen3.8-max` / `alibaba-token-plan-qwen3.8-max` / `go-qwen3.8-max` / `dashscope/qwen3.8-max` | 2.0 | 6.0 | 0.25 | DashScope |
| `qwen3.7-plus` / `-max` / `go-qwen3.7-plus` | 0.40 | 1.20 | 0.05 | DashScope |
| `qwen3.6-flash` | 0.10 | 0.40 | 0.01 | DashScope |
| `muse-spark-1.2` / `go-muse-spark-1.2` / `muse-spark-1.1` | 1.25 | 4.25 | 0.15 | Bundled catalog + fallback |
| `muse-spark-1.2-contributor` / `go-muse-spark-1.2-contributor` | 0.10 | 0.20 | 0.002 | Contributor tier |
| `mimo-v2.5` / `go-mimo-v2.5` | 0.10 | 0.30 | 0.02 | Xiaomi |
| `gpt-oss-120b` / `gpt-oss-120b-medium` | 0.15 | 0.60 | 0.03 | OpenAI open-weights (Fireworks/hosted) |
| `deepseek-v4-flash` / `alibaba-token-plan-deepseek-v4-flash-0731` / `accounts/fireworks/models/deepseek-v4-flash-0731` | 0.14 | 0.28 | 0.0028 | DeepSeek (existing) |
| `deepseek-v4-pro` | 0.435 | 0.87 | 0.003625 | DeepSeek |
| `glm-5.2` / `alibaba-token-plan-glm-5.2` | 1.40 | 4.40 | 0.26 | z.ai |
| `glm-5` / `go-glm-5` | 1.00 | 3.20 | 0.20 | z.ai |
| `ox-alpha-free` / `go-ox-alpha-free` | 0 | 0 | 0 | **Free tier — $0 is correct** |

## 4) Code changes — 1–2 command mapping

All future requests are fixed by code + catalog; no per-group DB config needed.

### 4.1 `backend/internal/service/billing_service.go`

* `initFallbackPricing()` — added fallback entries: `gemini-3.7-flash`, `gemini-3-pro-preview`, `gemini-2.5-flash`, `gemini-2.5-flash-lite`, `qwen3.8-max`, `qwen3.7-plus`, `qwen3.6-flash`, `mimo-v2.5`, `gpt-oss-120b`, `muse-spark-1.2`, `muse-spark-1.2-contributor`, `joyvoice-fast-audio`, `ox-alpha-free`.
* `stripCompositeBillingPrefix()` — new helper; strips `alibaba-token-plan-` and `go-` then re-resolves the bare upstream model so `alibaba-token-plan-qwen3.8-max` → `qwen3.8-max`, `go-muse-spark-1.2` → `muse-spark-1.2`, `alibaba-token-plan-deepseek-v4-flash-0731` → `deepseek-v4-flash`, etc. Placed **after** the DeepSeek exact-match block.
* `getFallbackPricing()` — added rules: `gemini-3.7-flash`, `gemini-3-pro` family (covers `-high`/`-low`/`-preview`/`gemini-pro-agent`), `gemini-2.5-flash-lite`, `gemini-2.5-flash` (covers `-thinking`), `joyvoice`, `gpt-oss-120b`, `qwen3.8-max` exact, `muse-spark-*`, `ox-alpha-free`, `qwen3.7`/`qwen3.6`/`mimo` bare-name rules.

Key invariant: pricing resolution is `Group → Channel → LiteLLM catalog → Fallback`. Fallback order is **exact → prefix-strip → family-contains**. The strip uses `strings.CutPrefix` so only exact prefixed forms are stripped; bare `qwen*` still requires its family rule.

### 4.2 `backend/internal/service/pricing_service.go`

* `normalizeGeminiThinkingTierAlias()` — extended to map `gemini-3-pro-high`/`-low` → `gemini-3-pro-preview` so the LiteLLM catalog hit succeeds before fallback.

### 4.3 `backend/resources/model-pricing/model_prices_and_context_window.json`

Text-inserted 8 deleted keys from `backup-ours-20260824-043027` preserving original formatting (`1.25e-6` not `1.25e-06`):

`muse-spark-1.2`, `go-muse-spark-1.2`, `muse-spark-1.2-contributor`, `go-muse-spark-1.2-contributor`, `muse-spark-1.1`, `go-muse-spark-1.1`, `ox-alpha-free`, `go-ox-alpha-free`.

### 4.4 Tests

New file `backend/internal/service/billing_fallback_mapping_test.go:1` — `TestFallbackPricingCoversCompositeAndUnmappedAliases` covers 26 aliases with `NewBillingService(&config.Config{}, nil)` (catalog=nil → fallback-only) asserting `InputPricePerToken`/`OutputPricePerToken`/`CacheReadPricePerToken` via `GetModelPricing`. Run:

```bash
export PATH=$PATH:/usr/local/go/bin
go test ./internal/service -run FallbackPricingCoversComposite -count=1
# ok   github.com/Wei-Shaw/sub2api/internal/service  0.10s
go test ./internal/service -count=1   # full suite: ok 120s
go build ./...                        # clean
```

## 5) Backfill — one SQL, one deploy

### 5.1 Dry-run (psql)

```sql
WITH cards(model,i,o,cr) AS (VALUES (...)) -- 19 rows, see /tmp/kilo/backfill_usage_costs.sql
SELECT model, count(*), round(sum((input_tokens*c.i+output_tokens*c.o+cache_read_tokens*c.cr)*COALESCE(rate_multiplier,1))::numeric,6)
FROM usage_logs JOIN cards USING(model)
WHERE total_cost=0 AND actual_cost=0 AND (input_tokens+output_tokens+cache_read_tokens)>0
GROUP BY model;
-- 2,366 rows, ~$51.42 (top: gemini-3.7-flash-tiered $24.15, alibaba-token-plan-qwen3.8-max $21.76, gemini-pro-agent $5.38, ...)
```

### 5.2 Snapshot for rollback

```sql
CREATE TABLE usage_logs_cost_backup_20260828 AS
SELECT id,input_cost,output_cost,cache_read_cost,cache_creation_cost,total_cost,actual_cost
FROM usage_logs WHERE total_cost=0 AND actual_cost=0 AND model IN (...);
```

### 5.3 UPDATE (idempotent, token-mode only)

```sql
-- /tmp/kilo/backfill_usage_costs.sql
WITH cards(model,i,o,cr) AS (VALUES (...))
UPDATE usage_logs u SET
  input_cost = u.input_tokens*c.i,
  output_cost = u.output_tokens*c.o,
  cache_read_cost = u.cache_read_tokens*c.cr,
  cache_creation_cost = 0,
  total_cost = u.input_tokens*c.i + u.output_tokens*c.o + u.cache_read_tokens*c.cr,
  actual_cost = (u.input_tokens*c.i + u.output_tokens*c.o + u.cache_read_tokens*c.cr)*COALESCE(u.rate_multiplier,1)
FROM cards c
WHERE u.model=c.model AND u.total_cost=0 AND u.actual_cost=0
  AND (u.input_tokens+u.output_tokens+u.cache_read_tokens)>0
  AND COALESCE(u.billing_mode,'token')='token';
-- UPDATE 2366 (2026-08-28 run)
```

Formula matches `billing_service.go:1261` (`InputCost = tokens*c.i`, `Total = sum`, `Actual = Total*rate_multiplier`). `billing_mode` guard avoids touching `image`/`video`/`per_request` rows.

**What the UPDATE triggers do** (`migrations/222_group_usage_daily_rollups.sql:126` + `223`):

* `AFTER UPDATE OF actual_cost` on `usage_logs` → `invalidate_group_usage_rollup_state()` backs off `usage_group_rollup_state.closed_before` → next `SyncGroupUsageRollups` (scheduled every minute + `runStartupGroupUsageSync` on restart) rebuilds `usage_group_daily_rollups` from `SUM(actual_cost)`.
* `usage_dashboard_hourly`/`_daily`/`_users` have **no trigger** on `usage_logs`; they are rebuilt via `DashboardAggregationService`.

### 5.4 Refresh pre-aggregated dashboard tables

`usage_dashboard_hourly`/`_daily` use `ON CONFLICT DO UPDATE` (so a re-aggregate overwrites). Two supported refresh paths:

* **Admin backfill endpoint** (requires `dashboard_aggregation.backfill_enabled=true`):
  ```bash
  # enable once in the runtime config (volume: /srv/bot-storage/docker/volumes/deploy_sub2api_data/_data/config.yaml)
  dashboard_aggregation:
    backfill_enabled: true
    backfill_max_days: 45
  # then:
  docker compose -f deploy/docker-compose.yml up -d sub2api
  # build jwtgen, copy into container, exec with DATA_DIR=/app/data
  go build -o /tmp/kilo/jwtgen ./cmd/jwtgen
  docker cp /tmp/kilo/jwtgen sub2api:/tmp/jwtgen
  docker exec sub2api /tmp/jwtgen   # → JWT
  curl -X POST https://gpt.bdx.market/api/v1/admin/dashboard/aggregation/backfill \
    -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" \
    -d '{"start":"2026-08-01T00:00:00Z","end":"2026-08-28T23:59:59Z"}'
  # then optionally set backfill_enabled: false and restart once more
  ```
* **Startup recompute** (`recompute_days`, default 2) rebuilds the last N days via `backfillRange` → `AggregateRange` (upsert). For older history (July 29–Aug 26) the backfill endpoint is needed.

The **Usage Records** page (`GET /api/v1/admin/usage/stats` → `usage_log_repo_stats.go:657` `SUM(total_cost)`/`SUM(actual_cost)` on `usage_logs`) and **User dashboard** (`GetUserDashboardStats` always on `usage_logs`) are **immediately correct** after the UPDATE — no aggregate refresh needed for the numbers the user screenshots.

### 5.5 Deploy the new image so new requests bill correctly

```bash
DOCKER_BUILDKIT=1 docker build -t weishaw/sub2api:latest -t sub2api:pricing-backfill-20260828 \
  --build-arg GOPROXY=https://goproxy.cn,direct \
  -f Dockerfile .
docker compose -f deploy/docker-compose.yml up -d sub2api
curl -s http://127.0.0.1:8086/health  # {"status":"ok"}
docker logs sub2api --tail 20
```

Build time ~14s with cache (cold ~6 min: `vue-tsc` 2m28s + `go build` 316s).

### 5.6 What is NOT backfilled

`billing_usage_entries` (ledger of balance deductions) and user balances are **not** retroactively charged. The UPDATE fixes **reporting** (`usage_logs` + rollups + dashboard). Retroactive charging would double-bill and is intentionally not done; document this decision.

## 6) Verification after the change

```bash
# code
export PATH=$PATH:/usr/local/go/bin
go test ./internal/service -run FallbackPricingCoversComposite -count=1 -v
go build ./...

# DB — remaining $0 token rows should only be free-tier or zero-token error rows
docker exec -i sub2api-postgres psql -U sub2api -d sub2api -c "
  SELECT model, count(*) FROM usage_logs
  WHERE total_cost=0 AND (input_tokens+output_tokens+cache_read_tokens)>0
  GROUP BY model ORDER BY count DESC;"
# → 0 rows (or only ox-alpha-free at $0 by design)

# UI
# https://gpt.bdx.market/usage → Last 24 Hours → Model Distribution
# gemini-3.7-flash-tiered / alibaba-token-plan-qwen3.8-max / go-muse-spark-1.2-contributor now show Actual $>0, Standard $>0.
```

Edge cases verified:
* `accounts/fireworks/models/deepseek-v4-flash-0731` — `lastSegment` + `deepseek-v4-flash` contains → billed via existing rule.
* `go-*` / `alibaba-token-plan-*` — stripped then re-resolved; `go-muse-spark-1.2-contributor` checked before `muse-spark-1.2`.
* `gemini-3-pro-image` — not stolen by the `gemini-3-pro` family rule because the LiteLLM catalog hit wins first (has image price + `TokenPricingAbsent` guard).
* Image/video/per_request modes — `billing_mode` guard skips them.
* Ox free tier — explicit zero card so `HasIdentifiedTokenPricing` returns true at $0 and never guesses a family price.

## 7) Rollback

```sql
-- if needed, restore from the snapshot taken in 5.2
UPDATE usage_logs u SET
  input_cost=b.input_cost, output_cost=b.output_cost,
  cache_read_cost=b.cache_read_cost, cache_creation_cost=b.cache_creation_cost,
  total_cost=b.total_cost, actual_cost=b.actual_cost
FROM usage_logs_cost_backup_20260828 b WHERE u.id=b.id;
```

Code rollback: `git checkout -- backend/internal/service/billing_service.go backend/internal/service/pricing_service.go backend/resources/model-pricing/model_prices_and_context_window.json` and rebuild.

## 8) Files changed in this run

* `backend/internal/service/billing_service.go:327` — fallback cards + `stripCompositeBillingPrefix` + alias rules
* `backend/internal/service/pricing_service.go:800` — Gemini 3 Pro tier alias normalization
* `backend/resources/model-pricing/model_prices_and_context_window.json` — 8 restored catalog keys
* `backend/internal/service/billing_fallback_mapping_test.go:1` — 26-case regression test
* `/tmp/kilo/backfill_usage_costs.sql` — the exact backfill SQL (copy to `deploy/` if you want a persistent record)
* DB — `usage_logs` 2,366 rows repriced; `usage_group_rollup_state` watermark invalidated; `usage_dashboard_*` refreshed via backfill endpoint (when enabled)

---

## Double-check (for the next AI)

1. Re-run the dry-run SELECT in 5.1 against the backup table — the sum must equal the pre-backfill `total_cost` sum for those rows.
2. `go test ./internal/service -run FallbackPricingCoversComposite` must be `ok`.
3. `curl http://127.0.0.1:8086/health` must be `{"status":"ok"}` and `docker logs sub2api` must contain no `ErrModelPricingUnavailable` for the mapped models.
4. In the UI, filter Usage Records by `Model = alibaba-token-plan-qwen3.8-max` and `gemini-3.7-flash-tiered` for Last 7 Days — both must show `Actual = Standard > $0`.
5. Do not re-run the UPDATE blindly: it is `WHERE total_cost=0` idempotent, but re-running will not double-charge. The ledger (`billing_usage_entries`) is intentionally not touched.

## 9) Incident 2026-08-28: postgres data loss during pricing deploy (and recovery)

**Symptom:** after `docker compose up -d` with the new image, `users/api_keys/usage_logs` counts dropped to 0 and the Usage page showed "No data available".

**Root cause:** the long-running `sub2api-postgres` container predated the compose `PGDATA=/var/lib/postgresql/data` fix (`deploy/docker-compose.yml:252`). Its live dataset lived in the image-declared anonymous volume at `/var/lib/postgresql`; the named volume `deploy_postgres_data` held only a stale 2026-07-29 initdb. Recreating the stack switched PGDATA to the (empty) named volume. The Aug 20–28 slice existed only in the orphaned anonymous volume and is **unrecoverable** once the container/volume is removed.

**Recovery performed (repeatable):**

```bash
docker compose -f deploy/docker-compose.yml down
docker volume rm <orphan-anon-volume>          # ONLY after confirming it holds no data
docker compose -f deploy/docker-compose.yml up -d postgres
docker exec -i sub2api-postgres psql -U sub2api -d postgres -c "DROP DATABASE IF EXISTS sub2api WITH (FORCE);" -c "CREATE DATABASE sub2api OWNER sub2api;"
cat deploy/sub2api-20260729T074814Z-before-wsv2.sql | docker exec -i sub2api-postgres psql -U sub2api -d sub2api   # 0 errors
docker cp sub2api_usage_full.csv sub2api-postgres:/tmp/full.csv
# temp-table \copy + INSERT ... JOIN api_keys/accounts (placeholders for missing accounts: status='inactive', schedulable=false)
docker compose -f deploy/docker-compose.yml up -d   # app runs migrations 190 -> latest
cat deploy/backfill_usage_costs.sql | docker exec -i sub2api-postgres psql -U sub2api -d sub2api
# dashboard refresh: jwtgen (CGO_ENABLED=0!) + POST /api/v1/admin/dashboard/aggregation/backfill
```

**Result:** 60,518 usage rows (Jul 29 → Aug 20 10:29 +06), `$0` token rows = 0, `actual_cost` sum $2,081.87; login/API keys/groups/accounts restored; 7 inactive placeholder accounts (ids 8–22) for FK integrity. **Lost forever:** Aug 20–28 usage + any api_keys/accounts created in that window (e.g. the `joy` key) — clients must mint new keys; old secrets are unrecoverable.

**Prevention rule:** before recreating `sub2api-postgres` or removing ANY orphan volume, take a `pg_dump` from the RUNNING container first. Never `docker volume rm` a postgres anonymous volume without verifying `base/<dboid>` size.

## 10) Restoring lost model routing (composite_model_routes) — 2026-08-28

After DB restore, clients got `model_not_found` / placeholders cluttered accounts. Fixed via:

```sql
DELETE FROM accounts WHERE name LIKE 'restored-placeholder-%';
UPDATE accounts SET schedulable=true, status='active' WHERE id BETWEEN 1 AND 7;
INSERT INTO composite_model_routes (group_id, public_model, match_type, target_platform, upstream_model, endpoint, priority, enabled, notes) VALUES
 (4,'gemini-3.7-flash-tiered','exact','antigravity','gemini-3.7-flash-tiered','any',10,true,'Restored'),
 (4,'joyvoice-fast-audio','exact','antigravity','gemini-2.5-flash','any',10,true,'Restored'),
 (4,'tab_flash_lite_preview','exact','antigravity','tab_flash_lite_preview','any',10,true,'Restored'),
 (4,'alibaba-token-plan-qwen3.8-max','exact','openai','qwen3.8-max','any',10,true,'Restored'),
 -- ... plus go-muse-spark-1.2[-contributor], go-mimo-v2.5, go-ox-alpha-free, go-qwen3.*, go-glm-5,
 -- go-deepseek-v4-*, alibaba-token-plan-qwen3.7-plus/3.7-max/3.6-flash/glm-5.2/deepseek-*, luna aliases
```

Route format reference: `.luna-route-backup-20260822152445.json`. `target_platform` must be in
('anthropic','openai','gemini','antigravity','grok','kimi','zhipu','deepseek') (migration 227).

**Code side:** `composite_model_routes` alone is not enough — the antigravity scheduler also requires the model in the platform catalog. Restored `gemini-3.7-flash-tiered` in:
`backend/internal/pkg/antigravity/claude_types.go:186`, `backend/internal/domain/constants.go:147`, `backend/internal/service/account.go:665`. Verified live: `POST /v1/chat/completions {"model":"gemini-3.7-flash-tiered"}` → 200 "ok", usage row billed $0.000199 at the 0.75/3.75 card.

**Historical provider state before isolation:** Alibaba account 23 and OpenCode Go account 24 were bound to group 4 and claimed exact public aliases through `credentials.model_mapping`. The broad `target_platform='openai'` composite routes were removed because explicit composite routes bypass account ownership and select the wrong OpenAI-compatible account. Both accounts use `extra.openai_responses_mode='force_chat_completions'`.

## 11) Owner-provided provider keys — 2026-08-28

Two provider accounts were created without storing secrets in this runbook:

| Account | DB id | Base URL | Ownership |
|---|---:|---|---|
| Alibaba Token Plan | 23 | `https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1` | `alibaba-token-plan-*` aliases → Alibaba model IDs |
| OpenCode Go | 24 | `https://opencode.ai/zen/go/v1` | current Go catalog → `go-*` aliases |

The account mappings are exact, for example `alibaba-token-plan-qwen3.8-max` → `qwen3.8-max` and `go-qwen3.8-max` → `qwen3.8-max`. They were initially bound to group 4 and are now isolated in groups 5 and 6 as documented below. They must remain `active`, `schedulable=true`.

Verified through the live gateway after restarting to clear the scheduler cache:

* Alibaba `qwen3.8-max` and `qwen3.7-plus` → `200 ok`.
* Go `qwen3.8-max`, `qwen3.7-plus`, `deepseek-v4-flash`, `glm-5`, `mimo-v2.5`, and `minimax-m3` → `200`.
* `/v1/models` → 85 models, including all `alibaba-token-plan-*`, current `go-*`, and `gemini-3.7-flash-tiered`.
* `muse-spark-1.2-contributor` currently returns provider HTTP 500 directly from OpenCode Go; this is upstream-side, not an account-selection failure.

The old `OpenCode Zen` account 6 remains `error`/unschedulable because its previous key returns `Invalid API key`. The provided Go key does not support Zen's `mimo-v2.5-free` / `deepseek-v4-flash-free` IDs. Re-enable account 6 only after supplying a valid Zen key; do not silently map those free aliases to paid Go models.

## 12) Dedicated provider groups and client migration - 2026-08-28

### 12.1 Safety snapshot

Before changing live bindings, the running PostgreSQL container was verified healthy and its mounts were captured:

```text
container d530f420ffe2, postgres:18-alpine, healthy
anonymous volume a484e1457b3150120c0f323ab430a373b61f10cf8edf84ee9f8ca6ba5d08c04a -> /var/lib/postgresql
named volume deploy_postgres_data -> /var/lib/postgresql/data
```

The database was dumped before writes:

```bash
docker exec -i sub2api-postgres pg_dump -U sub2api -d sub2api --format=custom --file=/tmp/provider-group-isolation-20260828.dump
docker cp sub2api-postgres:/tmp/provider-group-isolation-20260828.dump /tmp/kilo/provider-group-isolation-20260828.dump
```

Pre-change counts were 2 users, 5 API keys, 8 non-deleted accounts, 4 groups, and 46,282 usage rows. No container restart, volume removal, database drop, or restore was performed.

### 12.2 Groups and accounts

Created through the admin API (`POST /api/v1/admin/groups`), not direct SQL:

| Group | DB id | Platform | Type | Image gate | OAuth-only | Account |
|---|---:|---|---|---|---|---:|
| Alibaba Token Plan | 5 | `openai` | `subscription` | enabled | disabled | 23 |
| OpenCode Go | 6 | `openai` | `subscription` | enabled | disabled | 24 |

The `require_oauth_only=false` setting is required because accounts 23 and 24 are API-key accounts. Group 5 image generation was enabled after creation for the mapped `wan2.7-image` and `wan2.7-image-pro` models.

Owner subscriptions were assigned to user 1 through `POST /api/v1/admin/subscriptions/assign`, valid through `2100-01-01`, as subscriptions are required when creating group-bound API keys. Account bindings were then updated through `PUT /api/v1/admin/accounts/:id`:

```json
{"group_ids":[5]}
{"group_ids":[6]}
```

The resulting strict ownership is:

```sql
SELECT ag.account_id, ag.group_id, g.name, g.platform, a.status, a.schedulable
FROM account_groups ag
JOIN groups g ON g.id = ag.group_id
JOIN accounts a ON a.id = ag.account_id
WHERE ag.account_id IN (23, 24)
ORDER BY ag.account_id;
-- 23 | 5 | Alibaba Token Plan | openai | active | true
-- 24 | 6 | OpenCode Go        | openai | active | true
```

Neither account remains in group 4. The application paths that implement this behavior are `backend/internal/service/admin_group.go:1121` for API-key group policy, `backend/internal/repository/group_repo.go:161` for atomic group/account copying, and `backend/internal/handler/admin/account_handler.go:953` for account group updates.

### 12.3 Client access decision

The live group-4 key is API key id 3 and remains bound to group 4. It was heavily used for the unified client and was not moved. API key id 4 was already soft-deleted before this migration. Since `api_keys.group_id` is singular, one key cannot simultaneously route to groups 4, 5, and 6.

Two permanent owner keys were created through `POST /api/v1/keys` and stored mode `600` in the existing git-ignored `deploy/owner-api-keys.json`:

| Key name | API key id | Group |
|---|---:|---:|
| `Owner Alibaba Token Plan` | 8 | 5 |
| `Owner OpenCode Go` | 9 | 6 |

Do not copy the secret values into source-controlled documentation. Existing unified clients keep using the `all_models` key for group-4 models; Alibaba aliases must use `alibaba_token_plan`, and Go aliases must use `opencode_go` from the local ignored owner-key file.

### 12.4 Live verification

Temporary group-bound keys were created, tested, and soft-deleted before permanent keys were created. The temporary checks returned HTTP 200 and logged account 23 for `alibaba-token-plan-qwen3.7-plus` and account 24 for `go-glm-5`.

Permanent-key checks returned:

```text
Alibaba group 5: POST /v1/chat/completions alibaba-token-plan-qwen3.7-plus -> HTTP 200, upstream model qwen3.7-plus
Go group 6:       POST /v1/chat/completions go-glm-5                  -> HTTP 200, upstream model glm-5
Group 4 legacy:   POST /v1/chat/completions gemini-3.7-flash-tiered     -> HTTP 200
```

Model catalog checks returned 13 Alibaba aliases for group 5 and 31 Go aliases for group 6. The preserved group-4 key correctly returns `400 Model is not supported by composite groups` for the now-isolated Alibaba and Go aliases; this is intentional strict isolation, and clients must switch those requests to the dedicated keys.

Final database verification:

```sql
SELECT g.id, g.name,
       (SELECT COUNT(*) FROM account_groups ag WHERE ag.group_id = g.id) AS accounts,
       (SELECT COUNT(*) FROM api_keys ak WHERE ak.group_id = g.id AND ak.deleted_at IS NULL) AS live_api_keys
FROM groups g WHERE g.id IN (4, 5, 6) ORDER BY g.id;
-- 4 | All Models         | 6 | 1
-- 5 | Alibaba Token Plan | 1 | 1
-- 6 | OpenCode Go        | 1 | 1
```

`GET /health` returned `{"status":"ok"}`. No panic or fatal service log appeared during the migration, and post-migration usage recorded group 5/account 23 and group 6/account 24 with zero requests for those accounts remaining under group 4.

## 13) Full-access composite provider group - 2026-08-28

The strict provider groups remain available, while a separate composite group provides one-key access to all configured providers. This avoids changing the existing group-4 client key and avoids putting Alibaba or Go accounts back into the legacy group.

### 13.1 Group and account population

Created group 7 through `POST /api/v1/admin/groups` with this effective request:

```json
{
  "name": "All Providers",
  "description": "Full-access composite gateway for all configured provider pools",
  "platform": "composite",
  "rate_multiplier": 1,
  "is_exclusive": false,
  "subscription_type": "subscription",
  "long_context_pricing_enabled": true,
  "allow_image_generation": true,
  "require_oauth_only": false,
  "copy_accounts_from_group_ids": [4, 5, 6]
}
```

The service copied and deduplicated eight active accounts into `account_groups`:

```sql
SELECT ag.account_id, a.platform
FROM account_groups ag JOIN accounts a ON a.id = ag.account_id
WHERE ag.group_id = 7 ORDER BY ag.account_id;
-- 1/antigravity, 3/antigravity, 4/antigravity, 5/openai,
-- 6/openai, 7/antigravity, 23/openai, 24/openai
```

An owner subscription was assigned as `user_subscriptions.id=6`, valid through `2100-01-01`. The admin implementation supports copying composite and concrete provider groups in `backend/internal/service/admin_group.go:423` and validates composite-only routes in `backend/internal/service/admin_group.go:183`.

### 13.2 Exact composite routes

The eight existing group-4 routes were copied to group 7. Forty-four exact routes were added from the account mappings in accounts 23 and 24:

```text
POST /api/v1/admin/groups/7/composite-routes
{"public_model":"<account alias>","match_type":"exact","target_platform":"openai","endpoint":"any","priority":20,"enabled":true}
```

For exact routes, the application persists an empty `upstream_model` as the public alias (`backend/internal/service/composite_model_route.go:126`). This is intentional: the composite scheduler selects the account by its exact alias, then account 23 or 24 applies its own `credentials.model_mapping` to produce the correct upstream model. Hardcoding the shared bare model (for example `qwen3.7-plus`) would allow the wrong OpenAI-compatible account to be selected.

The resulting group has 52 enabled routes. Route creation is exposed at `backend/internal/server/routes/admin.go:337` and handled by `backend/internal/handler/admin/group_handler.go:300`.

### 13.3 Full-access key

Created through `POST /api/v1/keys`:

| Key name | API key id | Group |
|---|---:|---:|
| `Owner All Providers` | 10 | 7 |

The secret is stored as `all_providers` in mode-600, git-ignored `deploy/owner-api-keys.json`. Existing `all_models` remains unchanged on group 4. Dedicated keys remain `alibaba_token_plan` for group 5 and `opencode_go` for group 6.

### 13.4 Verification

The full-access key returned HTTP 200 for all of these requests:

```text
alibaba-token-plan-qwen3.7-plus -> group 7, account 23, upstream qwen3.7-plus
go-glm-5                       -> group 7, account 24, upstream glm-5
gemini-3.7-flash-tiered         -> group 7, account 4
```

`GET /v1/models` through key 10 returned 85 models, including 13 Alibaba aliases, 31 Go aliases, and `gemini-3.7-flash-tiered`. Final state is verified with:

```sql
SELECT g.id, g.name,
       (SELECT COUNT(*) FROM account_groups ag WHERE ag.group_id = g.id) AS accounts,
       (SELECT COUNT(*) FROM composite_model_routes r WHERE r.group_id = g.id AND r.enabled) AS routes,
       (SELECT COUNT(*) FROM api_keys k WHERE k.group_id = g.id AND k.deleted_at IS NULL) AS live_keys
FROM groups g WHERE g.id IN (4, 5, 6, 7) ORDER BY g.id;
-- 4 | All Models         | 6 | 8  | 1
-- 5 | Alibaba Token Plan | 1 | 0  | 1
-- 6 | OpenCode Go        | 1 | 0  | 1
-- 7 | All Providers      | 8 | 52 | 1
```

`GET /health` and both Docker health checks remain healthy. The implementation uses the group-scoped scheduler and composite resolver in `backend/internal/service/gateway_scheduling.go:40` and `backend/internal/service/composite_route_resolver.go:25`.
