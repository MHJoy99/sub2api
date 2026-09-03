# Fan-out concurrency runbook — agent bursts (10–50 subagents)

## Symptom

Launching 10–50 parallel subagents through Sub2API choked: many requests
failed with `429 Too many pending requests`, while the same fan-out against
a direct upstream account worked. Single requests were always fine
(~1.2s, $0.0002).

## Root causes (three gates, found by measurement)

Requests pass three concurrency gates in order. The burst died at gate 1,
long before the account pool was exhausted.

### Gate 1 — per-user slots: 8 (THE binding constraint)

`users.concurrency` for the owner user was **8**, with a wait queue of
`8 + 20 = 28` (`CalculateMaxWait`). Burst math matched exactly:

| Burst | Result before fix |
|---|---|
| 10 parallel | 10 × 200, avg 1.66s |
| 25 parallel | 25 × 200, avg 2.48s |
| 50 parallel | **28 × 200, 22 × 429**, avg 1.47s |

50 − 28 = 22 rejections. Proof it was this gate: the 429 bodies came from
`gateway.cc.user_slot_acquire_failed` in the container logs, and the failing
requests never appear in `usage_logs` (rejected pre-account).

### Gate 2 — sticky pinning funnels identical bursts onto one account

`GenerateSessionHash` hashes client IP + user agent + API key + body. Bursts
of identical requests share one hash (`39hwyeoy` in the logs) and stick to a
single cached account — 98/98 successes landed on account 25 while four
siblings idled. Real agent sessions have distinct hashes and spread better;
identical-burst is the worst case.

### Gate 3 — no spillover when the chosen account's queue filled

On queue-full the handler returned 429 immediately instead of trying the
next account, even though the retry loop already supported exclusion via
`FailedAccountIDs`. Idle capacity sat unused next to failing requests.

## Fix (applied 2026-09-03, all live)

1. `users.concurrency` 8 → **40** for user 1 (direct SQL, reversible, no
   restart needed). Matches the 40-slot Antigravity pool so one owner user
   can use the whole pool; per-account RPM guards still protect upstream.
2. Account 25 priority 1 → **10** (direct SQL). Equal priority lets the
   scheduler spread by load/LRU across accounts 1/3/4/25 (40 slots; account
   7 stays priority 50 / 3 slots as backup).
3. `GATEWAY_SCHEDULING_STICKY_SESSION_MAX_WAITING` default 3 → **32**
   (`deploy/docker-compose.yml`). Overflow waits seconds instead of
   instantly 429ing.
4. Code: `FailoverState.RecordSlotBusy` + spillover on queue-full in the
   chat, responses, messages, and Anthropic-native paths
   (`backend/internal/handler/failover_loop.go`, `gateway_handler*.go`).
   Bounded by `MaxSwitches`; timeouts and cancellations keep old behavior.
   Covered by `TestRecordSlotBusy` / `TestIsWaitQueueFullError`.

## Verification (same 50-burst that failed before)

| Burst | After fix |
|---|---|
| 50 parallel | **50 × 200, 0 × 429**, served across accounts 25/7/1/4/3 |
| 25 parallel | 25 × 200 |

Upstream pushed back once under the artificial hammering (30s per-account
cooldown on all five pool accounts) and self-healed; all accounts stayed
`active`/schedulable. Tail latency under extreme identical-burst: avg ~4.4s,
max ~9.3s (queueing, not failure). Varied real-world fan-out will be faster
than this worst case.

## Capacity math and limits (by design, not bugs)

- Per user: 40 slots + 20 waiters = 60 in-flight. Beyond that → 429, instantly.
- Pool: 40 Antigravity slots (+3 backup). Beyond pool + account queues → waits
  (up to 30–120s timeouts) then 429 with spillover across accounts.
- Sustained >~100-way bursts will still shed load. Next levers, only if
  needed: raise account `concurrency` (watch upstream per-account quotas),
  add accounts to the pool, or cap client fan-out with retries + backoff.
- 429s from these gates are NOT in `usage_logs` (rejected pre-account); use
  container logs (`wait_queue_full`, `slot_acquire_failed`, `spillover`).

## Rollback

- `UPDATE users SET concurrency=8 WHERE id=1;`
- `UPDATE accounts SET priority=1 WHERE id=25;`
- Revert the compose default to 3 and recreate the container; `git revert`
  the spillover commit. No data migration involved anywhere.
