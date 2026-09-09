# OpenCode Go / ChatGPT provider segregation

## Rule — pin exclusive aliases with exact routes

A provider-exclusive alias must have exactly one mapping owner AND one
`exact` composite route per composite group. Without the route, resolution
falls back to platform-level matching and spills requests onto
wrong-platform accounts in the same group.

Observed: `go-muse-spark-1.3-contributor` requests were tried on the ChatGPT
Plus OAuth account (id 5) → `upstream_400_codex_plan_gated_model` →
per-model cooldown badge on the wrong account. Same public alias, same key,
same group — only the serving account was wrong.

## Ownership — `go-muse-spark-1.3-contributor`

| Alias | Owner | Groups | Route |
|---|---|---|---|
| `go-muse-spark-1.3-contributor` | 24 OpenCode Go (`go-*` → upstream names, `force_responses`) | 6 OpenCode Go, 7 All Providers | group 7 `exact`, `openai`, `any`, priority 20, enabled |

ChatGPT OAuth accounts must NOT claim `go-*` in `model_mapping`. Account 5
has no custom mapping; it became eligible through the empty-mapping
allow-all + `go-*` platform-detection path.

## Change applied 2026-09-03 (live, verified)

```sql
-- pin: sole-owner alias gets its exact route (guarded, idempotent)
INSERT INTO composite_model_routes
  (group_id, public_model, match_type, target_platform, upstream_model,
   endpoint, priority, enabled, notes)
SELECT 7, 'go-muse-spark-1.3-contributor', 'exact', 'openai',
       'go-muse-spark-1.3-contributor', 'any', 20, TRUE,
       'OpenCode Go 1.3 Contributor - sole owner account 24'
WHERE NOT EXISTS (
  SELECT 1 FROM composite_model_routes
  WHERE group_id = 7 AND public_model = 'go-muse-spark-1.3-contributor'
    AND match_type = 'exact' AND endpoint = 'any' AND deleted_at IS NULL);

-- retire: 1.2 alias has no owner and no traffic; soft-disable, keep history
UPDATE composite_model_routes SET enabled = false
WHERE group_id = 7 AND public_model = 'go-muse-spark-1.2-contributor';

-- hygiene: drop the expired bogus cooldown (expired entries are ignored
-- by the scheduler; deletion only clears the UI badge)
UPDATE accounts
SET extra = extra #- '{model_rate_limits,go-muse-spark-1.3-contributor}'
WHERE id = 5 AND deleted_at IS NULL;
```

Verified live: `/v1/models` lists 1.3 (78 models, 1.2 gone); 1.3 request →
HTTP 200 via account 24 (`finish_reason=stop` with normal token budget;
tiny `max_tokens` ends `length` on reasoning spend, same as tiered);
1.2 alias → clean 400 `Model is not supported by composite groups`, no 500.
Billing unchanged: `muse-spark-1.3-contributor` card $0.10/$0.20 per MTok.
Group-6 resolution untouched (routes are per-group; account 5 was never a
group-6 member).

## Audit — find other at-risk aliases

```sql
WITH owners AS (
  SELECT k AS alias, a.id AS aid FROM accounts a,
    jsonb_object_keys(
      COALESCE((a.credentials->'model_mapping')::jsonb, '{}'::jsonb)) k
  WHERE a.status = 'active' AND a.schedulable)
SELECT a.alias, count(*) AS owners, array_agg(a.aid) AS aids
FROM (SELECT DISTINCT alias, aid FROM owners) a
LEFT JOIN composite_model_routes r
  ON r.public_model = a.alias AND r.match_type = 'exact'
 AND r.enabled AND r.group_id IN (4, 7)
WHERE r.id IS NULL
GROUP BY 1 ORDER BY 2, 1;
```

Single-owner aliases with no exact route are one fallback away from a
cross-platform 400. Known same-pattern family (all owned by 24, all have
group-7 exact rows — healthy): `go-qwen3.8-max`, `go-glm-5`,
`go-deepseek-v4-flash/pro`, `go-mimo-v2.5`.

## Fix 2026-09-04 — stop repeat spill + raise OpenCode Go ceiling (live, verified)

Logs (tail 8000): 3766 `go-muse-spark-1.3-contributor` lines, all HTTP 200 via account 24, 5–8s (large 250–690KB bodies), one 76s / one 23s — zero `Too many pending requests` (tail 20000 count 0), zero `spillover`/`wait_queue_full`. Our side was NOT throttling; bottleneck was single-account ceiling (24: 30 slots, prio 50) vs user 40 slots, plus repeat wrong-account hits:

* `upstream_model_not_found_model_rate_limited account_id=5 model=go-muse-spark-1.3-contributor reason=upstream_400_codex_plan_gated_model` at 2026-09-04T07:11Z and 2026-09-05T01:11+0600 (reset +30m — matches UI badge `rate limited until 01:41`). Root: account 5 empty mapping allow-all (`account.go:874 isOpenAIOAuthServableModel`) + blacklist `openai_model_mapping.go:32-59` missing `go-/muse-spark-/mimo-/ox-` + account 5 prio 10 beats 24 prio 50 when both `openai`.

DB (no restart):

```sql
UPDATE accounts SET priority=5, concurrency=60 WHERE id=24; -- was 50/30; now beats 5 (10) and covers user max 40+20=60
UPDATE accounts SET extra = extra #- '{model_rate_limits,go-muse-spark-1.3-contributor}' WHERE id=5; -- clear badge; expired entries ignored by scheduler anyway
```

Code (permanent guard, deployed `sub2api:deploy-20260904194710-ce1d8d708`):

* `backend/internal/service/openai_model_mapping.go:32-35` added `"go-", "muse-spark-", "mimo-", "ox-"` to `openAIOAuthForeignModelPrefixes` — empty-mapping ChatGPT OAuth now skips these at scheduling, request goes straight to owner 24.
* `backend/internal/service/openai_oauth_model_support_test.go:47-56` added `go-muse-spark-1.3-contributor`, `go-qwen3.8-max`, `muse-spark-1.3-contributor`, `mimo-v2.5`, `ox-alpha-free` to foreign list. `go test -tags=unit ./internal/service -run 'TestIsModelSupported_OpenAIOAuth|TestIsOpenAIOAuthServableModel'` PASS, `go build ./...` clean, `SUB2API_DRY_RUN=1` ok, deploy healthy, first post-deploy `POST /v1/responses go-muse-spark-1.3` → 200 via account 24.

Note: explicit `IsModelSupported` mapping still wins — accounts with explicit `model_mapping` claiming these IDs are unaffected; passthrough (`openai_passthrough`) unaffected.

## Scale-out 2026-09-04 — 2 more OpenCode Go keys (live, verified)

Added accounts 26 (`OpenCode Go v3`) + 27 (`OpenCode Go v4`): exact clones of 24 (same `base_url`, same 31-key `go-*:*` mapping incl. `go-muse-spark-1.3-contributor→muse-spark-1.3-contributor`, same `extra` force_responses, prio 5 / conc 60), joined to groups 6 (prio 1) + 7 (prio 2). Secrets in DB only, never in docs/git.

Gotcha hit: direct-SQL inserts bypass the scheduler outbox, so the running snapshot kept routing 100% to 24 (12/12 identical AND 12/12 unique prompts all →24). `docker restart sub2api` (~15s, Postgres/Redis untouched) reloaded the snapshot; next 12x unique burst spread 24/26/27 (6/6 split in window), 12x200 ~1–1.9s. Prefer Admin API for future adds (emits outbox), or restart after direct SQL.

## 10-parallel think burst proof 2026-09-04 (live, single key at the time)

10x parallel `POST /v1/responses go-muse-spark-1.3-contributor` (tiny, `max_output_tokens=16`) via group 7: **10x200, 0.9–1.2s each**, all via account 24, zero `Too many pending` / `wait_queue_full` / `spillover` / `slot_acquire_failed` in logs. First bad burst (`max_output_tokens=8`) correctly 400s from upstream (`>= 16` rule) — also all routed to 24, none spilled to account 5, confirming the blacklist guard. Real 250–800KB think prompts take 5–11s upstream (reasoning compute, not queueing). Ceilings at the time: user 40+20=60, account 24 60 slots prio 5, sticky 32 / fallback 100, MaxSwitches 10. (Superseded by scale-out below: 3 keys now.)

## 2026-09-07 — deployed + live-verified (background deploy, exit 0)

First deploy `sub2api:deploy-20260907214750` (global-allowlist design) was
superseded same night by rewrite `sub2api:deploy-20260907215850` (upstream
#6581 scoping + local synthesis). Both via
`SUB2API_BUILD_PROGRESS=plain bash deploy/deploy-sub2api.sh` in background:
backend `go build` re-ran, container recreated, Postgres/Redis untouched,
`/app/data` preserved, health `healthy`, `/health` 200. Live probes
`POST /v1/responses go-muse-spark-1.3-contributor` (`max_output_tokens=16`)
on final image: **200 without client `X-OpenCode-Session` (1.14s)** and
**200 with explicit header (0.98s)**.
(`incomplete/max_output_tokens` is the expected tiny-probe truncation.)

## 2026-09-07 — upstream `x-opencode-session` 400 diagnosis (NOT a spill, no drift)

Screenshot: `Error from provider (Console Go): Request is missing
x-opencode-session and cannot be routed efficiently` (HTTP 400) on
`go-muse-spark-1.3-contributor` via Kilo Code VS Code + Qwen Code.

Verdict (10 read-only subagents, no build):

* NOT sub2api misrouting. Signature proves request reached correct
  Console Go upstream (`https://opencode.ai/zen/go/v1`, accounts
  24/26/27). Spill to ChatGPT OAuth id 5 would be
  `upstream_400_codex_plan_gated_model` + 30m badge, not this text.
  Routing pin (group 7 `exact` `openai` `any` prio 20) + OAuth
  blacklist (`openai_model_mapping.go:32-36` `go-/muse-spark-/mimo-/ox-`)
  both intact — no code drift (`git diff` is exactly the documented
  2026-09-04 guard, uncommitted but matching this runbook).
* Upstream contract change: https://opencode.ai/docs/go/#where-can-i-use-it
  now requires stable `x-opencode-session` per conversation + own
  `User-Agent`. Validated: Kilo CLI only (PR #13752); VS Code extension
  NOT fixed (issue #13723). Qwen Code / generic OpenAI clients also send
  nothing. Sub2api compounds it: inbound `X-OpenCode-Session` is read
  for sticky hash (`openai_gateway_scheduling.go:24-26,44-55,151`) but
  stripped on egress — allowlists `openai_gateway_service.go:73-106`
  (`openaiAllowedHeaders` / `openaiPassthroughAllowedHeaders`) + raw-CC
  `openai_gateway_chat_completions_raw.go:33-36` contain no
  `x-opencode-session`; enforced
  `openai_gateway_forward.go:1356-1364`,
  `openai_gateway_passthrough.go:618-628,986-993`. Sub2api never 400s
  itself on missing header; the 400 is pure upstream passthrough
  (`openai_gateway_upstream_errors.go:665-671`,
  `openai_upstream_client_error.go:39-57` preserve status+message).
* Nginx `underscores_in_headers on` (README:219-227) is unrelated:
  `x-opencode-session` uses dashes and survives; only `session_id`
  needs it.

Workaround without build (do first):

```bash
# prove upstream needs the header (bypass sub2api sticky, direct key):
curl -s -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $GO_KEY" \
  -H 'Content-Type: application/json' \
  -H 'User-Agent: sub2api-probe/1.0' \
  -d '{"model":"muse-spark-1.3-contributor","input":"ping","max_output_tokens":16}' \
  https://opencode.ai/zen/go/v1/responses  # expect 400 without header
curl -s -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $GO_KEY" \
  -H 'Content-Type: application/json' \
  -H 'User-Agent: sub2api-probe/1.0' \
  -H "X-OpenCode-Session: $(uuidgen)" \
  -d '{"model":"muse-spark-1.3-contributor","input":"ping","max_output_tokens":16}' \
  https://opencode.ai/zen/go/v1/responses  # expect 200

# via sub2api (same header must survive to upstream after code fix;
# today it 400s even with header — confirms strip path above):
curl -s -H "Authorization: Bearer $SUB2API_KEY" \
  -H 'Content-Type: application/json' \
  -H "X-OpenCode-Session: test-123" \
  -d '{"model":"go-muse-spark-1.3-contributor","input":"ping","max_output_tokens":16}' \
  https://<sub2api-host>/v1/responses
```

Client fix: Kilo VS Code → add custom header `x-opencode-session:
<stable-uuid-per-conversation>` + distinct `User-Agent` if supported
(`opencode.json` / provider `options.headers`); else use Kilo CLI
(PR #13752), OpenCode, Pi, jcode>=0.81.6, Hermes with PR #101864.
Qwen Code: same header injection or wait for upstream client fix
(#7531/#7536 class).

Future code patch (APPLIED 2026-09-07, rewritten to upstream design +
local synthesis; vet+build clean; full service test binary OOMs on the 7.8GB
box — CI must run it): new `openai_opencode_session.go` mirrors upstream
#6581 verbatim (`applyOpenCodeSessionHeader`: caller value forwarded only to
exact `https://opencode.ai`, apikey-only, applied AFTER account overrides so
caller beats fixed values) plus local `synthesizeOpenCodeSessionHeader`
(same origin scope; explicit session/`prompt_cache_key` → deterministic
tenant-isolated content-seed UUID → random UUID) — Kilo VS Code works with
zero client change, which pure upstream still 400s. Wired into
`buildUpstreamRequest` (`openai_gateway_forward.go`),
`buildUpstreamRequestOpenAIPassthrough` (`openai_gateway_passthrough.go`),
`sendCCUpstreamRequest` (`openai_gateway_cc_pipeline.go`), and Anthropic
`buildUpstreamRequest` (`gateway_upstream_request.go`). Deliberately NOT
global-allowlisted (reverted): stable IDs stay off unrelated upstreams.
Tests: `openai_opencode_session_synthesis_test.go` (distinct `synth*` names
to avoid colliding with upstream's test file on future pulls).

## 2026-09-09 — Test modal still 400s: direct probe bypassed gateway fix (fixed, needs deploy)

Screenshot: Admin `Test model` (`Default request`) on OpenCode Go apikey
account → `API returned 400: {"type":"MissingSessionID","message":"Error from
provider (Console Go): Request is missing x-opencode-session ..."}` with
`Using model: muse-spark-1.3-contributor`.

Root: `POST /admin/accounts/:id/test` (`AccountTestService.TestAccountConnection`
`backend/internal/service/account_test_service.go:274`) builds direct upstream
HTTP (`testOpenAIAccountConnection:647`, `testOpenAIChatCompletionsConnection:1965`,
`testOpenAICompactConnection:2030`) — it never calls the gateway builders where
`dce574fff` wired `apply/synthesizeOpenCodeSessionHeader`. So live `/v1/responses`
traffic was fixed, but the modal always sent no `X-OpenCode-Session` → upstream 400.
No revert; `git log` shows `dce574fff` + `667c035bd` intact.

Fix (`go vet ./internal/service` clean, `gofmt` clean; full service
test binary still OOMs on 7.8GB box as before — CI must run it):

* `backend/internal/service/openai_opencode_session.go:55-73`
  `ensureOpenCodeSessionForAccountTest(headers, account, targetURL)` — apikey-only,
  exact `https://opencode.ai` origin only, keeps admin override, else random UUID.
* Wired AFTER `ApplyHeaderOverrides` in all three apikey probe paths:
  `backend/internal/service/account_test_service.go:792-796` (Responses),
  `:1997-2001` (Chat Completions), `:2134-2137` (Compact).
* Verify after deploy: open account → Test model → Default request →
  expect 200 stream, not `MissingSessionID`. Real gateway traffic unchanged.

## Rollback

```sql
DELETE FROM composite_model_routes
WHERE group_id = 7 AND public_model = 'go-muse-spark-1.3-contributor'
  AND match_type = 'exact' AND endpoint = 'any' AND deleted_at IS NULL;
UPDATE composite_model_routes SET enabled = true
WHERE group_id = 7 AND public_model = 'go-muse-spark-1.2-contributor';
```

Cooldown re-add is unnecessary (entries expire on their own). No code or
redeploy involved in either direction; routes are read per request.
