# Hot Cache Deploy Runbook (read BEFORE starting any engine work)
Goal: small changes go live in minutes, not tens of minutes. The caches
below are HOT as of 2026-09-13 (full upstream-merge deploy
`sub2api:deploy-20260913022432-c6e6208a7`). Keep them hot.

## What is hot and where it lives
- Docker BuildKit named caches (survive across builds, keyed by ID):
  - `sub2api-pnpm-store` -> `/root/.local/share/pnpm/store` (`Dockerfile:33`)
  - `sub2api-gomod` -> `/go/pkg/mod` (`Dockerfile:77,88`)
  - `sub2api-gobuild` -> `/root/.cache/go-build` (`Dockerfile:89`)
- Host Go build cache: `/root/.cache/go-build` (shared by every checkout
  and scratch worktree on this machine — a trial-worktree `go build`
  warms the deploy too).
- Docker layer cache: unchanged Dockerfile stages (frontend deps, backend
  deps) rebuild in seconds; only edited stages re-run.
- Deploy script also reuses the previous image via
  `--cache-from "$compose_image"` (`deploy/deploy-sub2api.sh:136-140`).

## The fast path (every engine, every time)
1. Edit, then verify locally with the shared host cache (fast):
   `cd backend && /usr/local/go/bin/go build ./...`
   (`go` is NOT on PATH; full path required.)
2. Targeted `go test` for touched packages only (service tests link
   sqlite and take minutes — scope with `-run`).
3. Deploy: `SUB2API_DRY_RUN=1 bash deploy/deploy-sub2api.sh && bash deploy/deploy-sub2api.sh`
4. Verify: `GET /v1/models` entry count + one tiny live request +
   non-zero `usage_logs.total_cost`.

Backend-only change = a few minutes (only `backend-builder` re-runs).
Docs/MD-only change needs NO deploy (not served from the image).

## Rules — do NOT do these or the cache goes cold
- Never `docker builder prune` / `docker system prune` on this host.
- Never `docker build --no-cache` for this project.
- Never remove the named cache volumes/IDs above or rename the IDs in
  the Dockerfile without updating this file.
- Never `rm -rf /root/.cache/go-build` or `go clean -cache`.
- `gofmt -l` before deploy; a red build wastes a full cycle.
- Long commands (>10s) must run detached (`setsid ... & disown`) with
  output to `/tmp/*.log`, then poll — never block the turn.

## Fresh-session checklist (run before touching any engine)
- `docker images sub2api | head -3` — latest deploy tag present?
- `git -C /root/sub2api log --oneline -2` — on `ours`, at expected commit?
- `curl -s -m 10 http://127.0.0.1:8086/v1/models -H "Authorization: Bearer <all_models key from deploy/owner-api-keys.json>"` — 200?
- If any answer is no, stop and fix that first; do not start engine work
  on a cold or broken base.

MD docs updated — stale MDs deleted, new MDs created where missing.
