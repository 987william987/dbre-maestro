#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
compose_file="$repo_root/backend/test/integration/session_management.compose.yml"
project="maestro-session-it-$$"

cleanup() {
  docker compose -p "$project" -f "$compose_file" down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose -p "$project" -f "$compose_file" up -d --wait

export SESSION_MANAGEMENT_INTEGRATION=1
export SESSION_IT_HOST=127.0.0.1
export SESSION_IT_MYSQL_PORT="${SESSION_IT_MYSQL_PORT:-13316}"
export SESSION_IT_POSTGRES_PORT="${SESSION_IT_POSTGRES_PORT:-15432}"
export SESSION_IT_REDIS_PORT="${SESSION_IT_REDIS_PORT:-16379}"

cd "$repo_root/backend"
go test -tags=integration -count=1 ./internal/sessionmanagement -run '^TestSessionManagementIntegration$'
