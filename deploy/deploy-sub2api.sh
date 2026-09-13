#!/usr/bin/env bash
# Fast, storage-safe deployment for the local Sub2API Compose installation.
#
# This script deliberately recreates only the application container. It never
# runs `down -v`, deletes volumes, prunes Docker data, or restarts PostgreSQL or
# Redis. Database migrations are applied by the application on startup.

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${SUB2API_COMPOSE_FILE:-${SCRIPT_DIR}/docker-compose.yml}"
SERVICE="${SUB2API_SERVICE:-sub2api}"
LOCK_FILE="${SUB2API_DEPLOY_LOCK:-/tmp/sub2api-deploy.lock}"
HEALTH_TIMEOUT_SECONDS="${SUB2API_HEALTH_TIMEOUT_SECONDS:-180}"
HEALTH_INTERVAL_SECONDS="${SUB2API_HEALTH_INTERVAL_SECONDS:-3}"
BUILD_PROGRESS="${SUB2API_BUILD_PROGRESS:-auto}"
PULL_BASE_IMAGES="${SUB2API_PULL_BASE_IMAGES:-0}"
SKIP_BUILD="${SUB2API_SKIP_BUILD:-0}"
DRY_RUN="${SUB2API_DRY_RUN:-0}"

log() {
  printf '[deploy] %s\n' "$*"
}

die() {
  printf '[deploy] ERROR: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

compose() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

mount_identity() {
  docker inspect "$SERVICE" --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{if .Name}}{{.Name}}{{else}}{{.Source}}{{end}}{{end}}{{end}}' 2>/dev/null || true
}

wait_for_healthy() {
  local deadline=$((SECONDS + HEALTH_TIMEOUT_SECONDS))
  local status

  while (( SECONDS < deadline )); do
    status="$(docker inspect "$SERVICE" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' 2>/dev/null || true)"
    if [[ "$status" == "healthy" ]]; then
      return 0
    fi
    sleep "$HEALTH_INTERVAL_SECONDS"
  done

  log "application did not become healthy within ${HEALTH_TIMEOUT_SECONDS}s (state: ${status:-missing})"
  compose logs --tail=80 "$SERVICE" >&2 || true
  return 1
}

previous_image_id=""
previous_storage=""
had_previous_container=0
deployment_started=0

rollback() {
  if (( ! deployment_started )) || [[ -z "$previous_image_id" ]]; then
    log "no previous application image available for rollback"
    return 0
  fi

  log "rolling back application image; persistent storage is left untouched"
  if ! docker tag "$previous_image_id" "$compose_image"; then
    log "rollback could not restore the previous image tag"
    return 1
  fi
  if ! compose up -d --no-build --no-deps --force-recreate "$SERVICE"; then
    log "rollback container recreation failed"
    return 1
  fi
  if ! wait_for_healthy; then
    log "rollback container did not become healthy"
    return 1
  fi
  log "rollback completed"
}

on_error() {
  local exit_code=$?
  trap - ERR
  log "deployment failed (exit ${exit_code})"
  rollback || log "WARNING: rollback was not confirmed healthy"
  exit "$exit_code"
}

trap on_error ERR

require_command docker
require_command flock
[[ -f "$COMPOSE_FILE" ]] || die "Compose file not found: $COMPOSE_FILE"

exec 9>"$LOCK_FILE"
flock -n 9 || die "another Sub2API deployment is already running"

compose config --services | grep -Fxq "$SERVICE" || die "Compose service not found: $SERVICE"

# Resolve the image name from the service definition so custom Compose files
# continue to receive the newly built image without changing their storage
# configuration.
compose_image="$(compose config | awk -v service="$SERVICE" '
  $0 == "  " service ":" { in_service = 1; next }
  in_service && $0 ~ /^  [A-Za-z0-9_.-]+:/ { exit }
  in_service && $1 == "image:" { print $2; exit }
')"
[[ -n "$compose_image" ]] || die "could not resolve image for Compose service: $SERVICE"

if docker inspect "$SERVICE" >/dev/null 2>&1; then
  had_previous_container=1
  previous_image_id="$(docker inspect "$SERVICE" --format '{{.Image}}')"
  previous_storage="$(mount_identity)"
fi

if [[ "$DRY_RUN" == "1" ]]; then
  log "dry run: would build ${compose_image}, recreate only ${SERVICE}, and verify /app/data"
  [[ -n "$previous_storage" ]] && log "dry run: current /app/data mount ${previous_storage}"
  exit 0
fi

if [[ "$SKIP_BUILD" == "1" ]]; then
  log "using the existing image tag ${compose_image} (SUB2API_SKIP_BUILD=1)"
  deployment_started=1
else
  require_command git
  build_stamp="$(date -u +%Y%m%d%H%M%S)"
  git_revision="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || printf 'nogit')"
  build_image="sub2api:deploy-${build_stamp}-${git_revision}"
  log "building ${build_image} with Docker's cache"
  build_args=(
    --tag "$build_image"
    --file "${REPO_ROOT}/Dockerfile"
    --cache-from "$compose_image"
  )
  if [[ "$PULL_BASE_IMAGES" == "1" ]]; then
    build_args+=(--pull)
  fi
  DOCKER_BUILDKIT=1 docker build \
    --progress="$BUILD_PROGRESS" \
    "${build_args[@]}" \
    "$REPO_ROOT"
  docker tag "$build_image" "$compose_image"
  deployment_started=1
fi

log "recreating only ${SERVICE}; Postgres, Redis, and /app/data remain in place"
compose up -d --no-build --no-deps --force-recreate "$SERVICE"
deployment_started=1

wait_for_healthy

current_storage="$(mount_identity)"
[[ -n "$current_storage" ]] || die "storage safety check failed: /app/data is not mounted"
if [[ -n "$previous_storage" && "$previous_storage" != "$current_storage" ]]; then
  die "storage safety check failed: /app/data changed from ${previous_storage} to ${current_storage}"
fi

if (( had_previous_container )); then
  log "deployment healthy; preserved /app/data mount ${current_storage}"
else
  log "deployment healthy; initialized /app/data mount ${current_storage}"
fi
log "image active: ${compose_image}"
