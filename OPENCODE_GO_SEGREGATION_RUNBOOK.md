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
