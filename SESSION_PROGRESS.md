# Session Progress

- **Session Start:** 2026-08-09
- **Goal:** Investigate sub2api gateway degradation / recent git merge state and resolve issues safely.
- **Current Step:** 2026-08-24 Reset to pure Wei-Shaw origin/main d45135d87, removed custom reasoning-proxy (127.0.0.1:8087), nginx now directly proxies to sub2api 127.0.0.1:8086. Pulled official weishaw/sub2api:latest, verified health and live API. See 2026-08-24 entry below.
- **Completed Steps:**
  - [x] Read repository documentation (`AI_STATUS.md`, `DEV_GUIDE.md`, `DEV_WORKFLOW.md`, `VPS_UNCOMMITTED_STATE.md`).
  - [x] Identified container regression in unverified image `bdx-sub2api:ui-guarded-20260809` (Gin middleware panics, missing OpenCode Zen accounts, missing `claude-opus-4-7` route).
  - [x] Diagnosed and fixed `https://gpt.bdx.market/login` failure caused by root filesystem (`/dev/vda2`) disk space exhaustion (100% full, 0B available), which blocked Nginx access logging and degraded PostgreSQL container transactions.
  - [x] Cleaned orphaned build and test artifacts in `/tmp` (`sub2api-go-cache`, `go-build*`) and `/root/.cache/go-build`, immediately freeing 8.6 GB (down to 86% usage).
  - [x] Verified `sub2api-postgres` returned to healthy state, `check-gateway-health.sh` reports 62 models healthy, and `https://gpt.bdx.market/login` along with backend authentication routes (`/api/v1/auth/login`, `/api/v1/auth/me`) are functioning properly.
  - [x] Removed LibreChat completely: deleted `/var/www/librechat`, purged all associated docker volumes (`librechat_mongo_data`, `librechat_redis_data`, `librechat_uploads`), pruned docker build caches, and updated Nginx to 301 redirect `ai.bdx.market` cleanly to `https://gpt.bdx.market/`.
  - [x] Verified full VPS storage health: 8.5 GB available on root (`/dev/vda2`) and 32 GB available on bot storage (`/dev/vda4`).
  - [x] Verified all active Docker containers, core services, and websites (`gpt.bdx.market`, `bdx.market`, `mhjoygamershub.com`, `chat.mhjoygamershub.com`, `ultimate.mhjoygamershub.com`, etc.) are up and healthy.
  - [x] Refreshed OAuth tokens for all 7 Antigravity accounts (IDs 1, 3, 4, 7, 8, 9, 15) using `deploy/refresh-antigravity-accounts.sh`.
  - [x] Provisioned `OpenCode Zen` (Account 17) via `deploy/add-opencode-zen-account.sh` and restored `zen-deepseek-v4-flash-free` / `zen-mimo-v2.5-free` routes to Group 4 ("All Models").
  - [x] Fixed missing `claude-opus-4-7` model routing by adding explicit composite routes in Group 4 and Group 2 mapping `claude-opus-4-7` -> `antigravity` (`gemini-3.1-pro`).
  - [x] Swapped `sub2api` service to verified stable container image `bdx-sub2api:hotfix-fc1`. Verified clean startup and 200 OK on health endpoints.
  - [x] Popped `stash@{0}` to recover the Gemini `input_audio` endpoint feature and `sequential_cutoff` reasoning fixes.
  - [x] Built the absolute latest image `sub2api:latest` locally on the VPS using `build_image.sh` with `goproxy.cn` to bypass network timeouts.
  - [x] Discovered an orphaned `reasoning_proxy` host process (PID 4783) blocking port 8087, killed it, and successfully deployed the new `sub2api` and `reasoning-proxy` containers.
  - [x] Upgraded sub2api concurrency configuration to support multi-subagent parallel workloads (user concurrency -> 50 for admin, 30 for users; account concurrency -> 30 for Antigravity, 50 for Alibaba; RPM limit -> 300, burst size -> 50 in `config.yaml`). Re-created container and verified 200 OK.
  - [x] Aggressively boosted OpenAI (`gpt-5.6-luna` / OpenCode Zen / Fable slots) account concurrency limits to 50 parallel slots in PostgreSQL. Verified 200 OK on live gateway endpoint.
  - [x] Verified `/v1/responses` endpoint concurrency slot logic for both OpenAI (`gpt-5.6-luna`) and Antigravity (`gemini-3.6-flash`). Live requests returned 200 OK.
  - [x] Identified and safely soft-deleted duplicate OpenCode Zen account (Account 18) to prevent routing issues.
  - [x] Configured `.agents/rules/git_source_of_truth.md` and `.claude/CLAUDE.md` to establish GitHub (`mhjoy/ours`) as the mandatory single source of truth for all AI agents.
  - [x] Committed all pending local changes (commit `a00cd8b82`) and pushed to GitHub (`https://github.com/MHJoy99/sub2api.git`). Working tree is 100% clean.
  - [x] Pulled latest GitHub Actions container build (`ghcr.io/mhjoy99/sub2api:ours`) and performed fast zero-downtime deployment via `deploy/fast-deploy.sh`. Verified 100% healthy status across 48 models.
  - [x] Diagnosed root cause of Antigravity fetch errors: active Antigravity accounts (1, 3, 4, 7, 8, 15) were bound to Webshare Datacenter Proxies (IDs 1–6), causing Google CloudCode upstream to reject OAuth token refreshes and stream content with HTTP 402 `Payment Required`. Unbound proxies (`proxy_id = NULL`) on all active Antigravity accounts in DB, ran `refresh-antigravity-accounts.sh` (all returned `refresh=ok warning=none`), and verified 200 OK on live requests.
  - [x] Tested Webshare proxies directly via HTTP CONNECT tunnel: all 10 Webshare proxies returned `HTTP/1.1 402 Payment Required` (`X-Webshare-Error: 402`, `X-Webshare-Reason: bandwidthlimit`). Marked proxies 1–10 as `disabled` in PostgreSQL.
  - [x] Disabled stale host-level systemd service `reasoning-proxy.service` (PID 4602) and force-recreated compose-managed `reasoning-proxy` container on `127.0.0.1:8087->8087/tcp`. Resolved Nginx 502 Bad Gateway and restored full gateway routing across ports 8086, 8087, and 8787.
  - [x] Performed 100% safe storage cleanup (Playwright browser binaries, obsolete VS Code server builds, cached extension installers, npx/pnpm download caches, rotated log archives, and temporary staging dumps), reclaiming a total of 14 GB free disk space on root (`/dev/vda2`, 78% usage). Verified all production gateways, containers, and websites remain 100% healthy.
   - [x] Completed Doc-Keeper & Compliance Lead repository audit (2026-08-18).

## 2026-08-24 (Reset to Wei-Shaw Pure - Reasoning-Proxy Removal & Live Deploy)
- **Action:** User reported custom `ms/time` hacks and reasoning-proxy broke quality. Backed up `ours` to `backup-ours-20260824-043027` (17 commits), `git reset --hard origin/main d45135d87` on both `main` and `ours`, deleted `reasoning_proxy.go`, `responses_reasoning_visibility.go` and related custom gateway code (openai_compat_model 245->117 lines).
- **Deploy Phase 1:** `docker pull weishaw/sub2api:latest ecf9d61dbad7 (75f88be, 2026-08-20)` - verified 2 days behind `d45135d87`, `sed -i s/8087/8086/ /etc/nginx/sites-available/gpt.bdx.market`, `nginx -t && reload`, `docker rm -f reasoning-proxy`, `docker compose up -d sub2api` -> live but outdated.
- **Deploy Phase 2 (LIVE):** Built true `d45135d87` locally: `DOCKER_BUILDKIT=1 docker build -t weishaw/sub2api:latest -t sub2api:d45135d87 -f Dockerfile .` frontend `vue-tsc 2m28s` + backend `go build 316s` -> `062f3d27a10e 188MB` `affa08bb1f48`. Fixed `deploy/docker-entrypoint.sh` perms `600->755` (chmod +x produced 711), `docker compose up -d sub2api` -> `sub2api weishaw/sub2api:latest 062f3d27 Up (healthy)` `postgres Healthy` `redis Healthy` `curl https://gpt.bdx.market/health {"status":"ok"}` `curl https://gpt.bdx.market/v1/chat/completions alibaba-token-plan-deepseek-v4-flash-0731` -> `reasoning_content` OK, `curl https://gpt.bdx.market/v1/models` OK. Nginx `upstream sub2api_backend 127.0.0.1:8086`.
- **Result:** Repo `HEAD==origin/main==origin/HEAD d45135d87` `git diff 0`, image `062f3d27a10e` exactly `Wei-Shaw` `d45135d87`. To make GitHub identical: `git push mhjoy ours --force`.

## 2026-08-22 (Gemini Tool Syntax Leak Recovery & `[calling tool]` UX Suppression)
- **Problem:** Gemini 3.7 Flash Thinking models emitted `!call:default_api:Bash{...}` into text during long reasoning turns, resulting in `finishReason: "stop"` and halted turns. In addition, previous Cloud Code hotfix placeholder `[calling tool]` was leaking into downstream client UI.
- **Solution:**
  1. Created `internal/pkg/antigravity/tool_recovery.go` using a quote-aware, brace-balanced scanner to recover leaked tool calls, parse relaxed arguments, and emit structured `tool_use` with `stop_reason: tool_use`.
  2. Injected `<tool_protocol>` guardrail into `visible_thinking.go`.
  3. Suppressed `[calling tool]` sentinels from downstream client responses across `response_transformer.go` and `anthropic_to_responses.go` while keeping Cloud Code upstream history valid.
  4. Added regression tests (`tool_recovery_test.go`, `response_transformer_recovery_test.go`, `response_transformer_calling_tool_test.go`).
  5. Audited and verified with `gpt-5.6-sol` via `SolVerifier` skill.

## 2026-08-18 (Sub2API Multi-Turn & Tool Execution Fix)
- Inspected `antigravity_gateway_gemini.go` (`cleanGeminiRequest`, `filterEmptyPartsFromGeminiRequest`) and `gemini_native_signature_cleaner.go` (`CleanGeminiNativeThoughtSignatures`).
- Found precision loss bug when handling `codeExecution` output or `parallel tool declarations` where `json.Unmarshal` converts big integers to `float64`.
- Updated all JSON unmarshal routines to use `json.Decoder` with `.UseNumber()` to prevent data corruption upon re-marshaling payloads.
- Verified changes are safe and tested via standard tooling.
