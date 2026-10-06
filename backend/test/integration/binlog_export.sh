#!/usr/bin/env bash
set -euo pipefail

schema="maestro_binlog_it_$$"
user="maestro_it_$$"
password="maestro_binlog_it_pw_$$"
forward_dir="/tmp/${schema}-forward"
rollback_dir="/tmp/${schema}-rollback"
time_forward_dir="/tmp/${schema}-time-forward"
time_rollback_dir="/tmp/${schema}-time-rollback"
stats_dir="/tmp/${schema}-stats"
server_id=$((620000 + ($$ % 100000)))

mysql_root() {
  docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -Nse "$1"' sh "$1"
}

cleanup() {
  mysql_root "DROP DATABASE IF EXISTS \`${schema}\`; DROP USER IF EXISTS '${user}'@'%';" >/dev/null 2>&1 || true
  docker compose exec -T app rm -rf -- "$forward_dir" "$rollback_dir" "$time_forward_dir" "$time_rollback_dir" "$stats_dir" >/dev/null 2>&1 || true
}
trap cleanup EXIT

read -r log_bin binlog_format row_image <<< "$(mysql_root 'SELECT @@log_bin, @@binlog_format, @@binlog_row_image')"
if [[ "$log_bin" != "1" || "$binlog_format" != "ROW" || "$row_image" != "FULL" ]]; then
  echo "MySQL 必須啟用 log_bin=ON、binlog_format=ROW、binlog_row_image=FULL" >&2
  exit 1
fi

mysql_root "CREATE DATABASE \`${schema}\`; CREATE TABLE \`${schema}\`.orders (id BIGINT PRIMARY KEY, status VARCHAR(32) NOT NULL, amount INT NOT NULL); CREATE USER '${user}'@'%' IDENTIFIED BY '${password}'; GRANT SELECT, REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO '${user}'@'%'; FLUSH PRIVILEGES;"
read -r start_file start_pos _ <<< "$(mysql_root 'SHOW MASTER STATUS')"
start_time="$(date -u '+%Y-%m-%d %H:%M:%S')"
sleep 1

mysql_root "INSERT INTO \`${schema}\`.orders VALUES (1001, 'created', 120); UPDATE \`${schema}\`.orders SET status='paid', amount=150 WHERE id=1001; DELETE FROM \`${schema}\`.orders WHERE id=1001;"
read -r end_file end_pos _ <<< "$(mysql_root 'SHOW MASTER STATUS')"
sleep 1
end_time="$(date -u '+%Y-%m-%d %H:%M:%S')"
mysql_root "INSERT INTO \`${schema}\`.orders VALUES (2002, 'after-snapshot', 999);"

run_my2sql() {
  local work_type="$1"
  local output_dir="$2"
  local id="$3"
  docker compose exec -T app sh -c 'mkdir -p "$1" && printf "%s\n" "$2" | my2sql -mode repl -work-type "$3" -user "$4" -host mysql -port 3306 -server-id "$5" -output-dir "$1" -add-extraInfo -start-file "$6" -start-pos "$7" -stop-file "$8" -stop-pos "$9" -databases "${10}" -tables orders -sql insert,update,delete' sh \
    "$output_dir" "$password" "$work_type" "$user" "$id" "$start_file" "$start_pos" "$end_file" "$end_pos" "$schema"
}

run_my2sql 2sql "$forward_dir" "$server_id"
run_my2sql rollback "$rollback_dir" "$((server_id + 1))"
run_my2sql stats "$stats_dir" "$((server_id + 2))"

run_my2sql_time() {
  local work_type="$1"
  local output_dir="$2"
  local id="$3"
  docker compose exec -T app sh -c 'mkdir -p "$1" && printf "%s\n" "$2" | my2sql -mode repl -work-type "$3" -user "$4" -host mysql -port 3306 -server-id "$5" -output-dir "$1" -add-extraInfo -start-datetime "$6" -stop-datetime "$7" -stop-file "$8" -stop-pos "$9" -databases "${10}" -tables orders -sql insert,update,delete' sh \
    "$output_dir" "$password" "$work_type" "$user" "$id" "$start_time" "$end_time" "$end_file" "$end_pos" "$schema"
}

run_my2sql_time 2sql "$time_forward_dir" "$((server_id + 3))"
run_my2sql_time rollback "$time_rollback_dir" "$((server_id + 4))"

forward_sql="$(docker compose exec -T app sh -c 'find "$1" -maxdepth 1 -type f -name "*.sql" -exec cat {} \;' sh "$forward_dir")"
rollback_sql="$(docker compose exec -T app sh -c 'find "$1" -maxdepth 1 -type f -name "*.sql" -exec cat {} \;' sh "$rollback_dir")"
time_forward_sql="$(docker compose exec -T app sh -c 'find "$1" -maxdepth 1 -type f -name "*.sql" -exec cat {} \;' sh "$time_forward_dir")"
time_rollback_sql="$(docker compose exec -T app sh -c 'find "$1" -maxdepth 1 -type f -name "*.sql" -exec cat {} \;' sh "$time_rollback_dir")"
stats_output="$(docker compose exec -T app sh -c 'cat "$1/binlog_status.txt"' sh "$stats_dir")"

grep -Fq "INSERT INTO \`${schema}\`.\`orders\`" <<< "$forward_sql"
grep -Fq "UPDATE \`${schema}\`.\`orders\` SET \`status\`='paid', \`amount\`=150" <<< "$forward_sql"
grep -Fq "DELETE FROM \`${schema}\`.\`orders\` WHERE \`id\`=1001" <<< "$forward_sql"
grep -Fq "INSERT INTO \`${schema}\`.\`orders\` (\`id\`,\`status\`,\`amount\`) VALUES (1001,'paid',150)" <<< "$rollback_sql"
grep -Fq "UPDATE \`${schema}\`.\`orders\` SET \`status\`='created', \`amount\`=120" <<< "$rollback_sql"
grep -Fq "DELETE FROM \`${schema}\`.\`orders\` WHERE \`id\`=1001" <<< "$rollback_sql"
grep -Eq '# datetime=.* binlog=.* startpos=[0-9]+ stoppos=[0-9]+' <<< "$forward_sql"
grep -Eq '# datetime=.* binlog=.* startpos=[0-9]+ stoppos=[0-9]+' <<< "$rollback_sql"

grep -Fq "INSERT INTO \`${schema}\`.\`orders\`" <<< "$time_forward_sql"
grep -Fq "DELETE FROM \`${schema}\`.\`orders\` WHERE \`id\`=1001" <<< "$time_rollback_sql"
if grep -Fq "2002" <<< "$time_forward_sql$time_rollback_sql"; then
  echo "Time range included an event after the fixed position snapshot" >&2
  exit 1
fi
if ! grep -Fq "$schema" <<< "$stats_output" || ! grep -Eq '[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}:[0-9]{2}:[0-9]{2}' <<< "$stats_output"; then
  echo "Unexpected my2sql timestamp stats output:" >&2
  echo "$stats_output" >&2
  exit 1
fi

echo "MySQL binlog export integration passed for position and time snapshots (${start_file}:${start_pos} -> ${end_file}:${end_pos})"
