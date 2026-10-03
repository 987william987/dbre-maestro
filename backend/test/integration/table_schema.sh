#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
compose_file="$repo_root/backend/test/integration/table_schema.compose.yml"
project="maestro-table-schema-it-$$"

cleanup() {
  docker compose -p "$project" -f "$compose_file" down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose -p "$project" -f "$compose_file" up -d --wait

export TABLE_SCHEMA_INTEGRATION=1
export TABLE_SCHEMA_IT_HOST=127.0.0.1
export TABLE_SCHEMA_IT_MYSQL_PORT="${TABLE_SCHEMA_IT_MYSQL_PORT:-13317}"
export TABLE_SCHEMA_IT_MYSQL80_TARGET_PORT="${TABLE_SCHEMA_IT_MYSQL80_TARGET_PORT:-13318}"
export TABLE_SCHEMA_IT_MYSQL57_PORT="${TABLE_SCHEMA_IT_MYSQL57_PORT:-13319}"

cd "$repo_root/backend"
go test -tags=integration -count=1 ./internal/tableschema -run '^TestSyncWorkerIntegration$'
