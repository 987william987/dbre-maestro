#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
compose_file="$repo_root/backend/test/integration/online_ddl.compose.yml"
project="maestro-online-ddl-it-$$"

cleanup() {
  docker compose -p "$project" -f "$compose_file" down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose -p "$project" -f "$compose_file" up -d --wait mysql80-source mysql80-replica
docker compose -p "$project" -f "$compose_file" exec -T mysql80-source \
  mysql -uroot -ponline_ddl_it -e "CREATE USER IF NOT EXISTS 'replica'@'%' IDENTIFIED WITH mysql_native_password BY 'replica_it'; GRANT REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO 'replica'@'%'; FLUSH PRIVILEGES;"
docker compose -p "$project" -f "$compose_file" exec -T mysql80-replica \
  mysql -uroot -ponline_ddl_it -e "STOP REPLICA; RESET REPLICA ALL; CHANGE REPLICATION SOURCE TO SOURCE_HOST='mysql80-source', SOURCE_USER='replica', SOURCE_PASSWORD='replica_it', SOURCE_AUTO_POSITION=1, GET_SOURCE_PUBLIC_KEY=1; START REPLICA;"

replica_ready=0
for _ in $(seq 1 30); do
  if docker compose -p "$project" -f "$compose_file" exec -T mysql80-replica \
    mysql -N -uroot -ponline_ddl_it -e "SELECT COUNT(*) FROM performance_schema.replication_connection_status WHERE SERVICE_STATE='ON'" | grep -q '^1$'; then
    replica_ready=1
    break
  fi
  sleep 1
done
if [ "$replica_ready" -ne 1 ]; then
  docker compose -p "$project" -f "$compose_file" exec -T mysql80-replica mysql -uroot -ponline_ddl_it -e "SHOW REPLICA STATUS\G"
  echo "replica did not become ready" >&2
  exit 1
fi

docker compose -p "$project" -f "$compose_file" run --build --rm online-ddl-test \
  -test.v -test.count=1 -test.timeout=8m -test.run '^TestOnlineDDLToolsIntegration$'
