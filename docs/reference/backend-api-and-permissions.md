# 後端 API 與權限對照

本文件整理目前主要導航頁、route guard 與後端 API permission gate 的對應關係。

## 導航頁與 route guard

| 頁面 | Route | 前端可見條件 |
|---|---|---|
| Tickets | `/tickets` | `tickets.read` |
| New Ticket | `/tickets/new` | `tickets.apply` |
| SQL Editor | `/sql-editor` | `sql_editor.read` |
| Scheduled Reports | `/scheduled-sql-reports` | `scheduled_sql_reports.read` 或 `scheduled_sql_reports.write` |
| MySQL Binlog Export | `/dba-tools/binlog-export` | `binlog_exports.read` |
| Table Schemas | `/dba-tools/table-schemas` | `table_schemas.read` |
| Account Sessions | `/account/sessions` | 已登入 |
| Users | `/users` | `users.read` 或 `users.write` |
| Auth Groups | `/users/groups` | `users.read` 或 `users.write` |
| Resources | `/users/resources` | `users.read` 或 `users.write` |
| DB Connections | `/db-connections` | `db_connections.read` 或 `db_connections.write` |
| DB Metadata | `/db-metadata/inventory`、`/db-metadata/objects` | `db_metadata.read` |
| Masking Rules | `/masking-rules`、`/masking-rules/dsl-guide` | `masking_rules.read` 或 `masking_rules.write` |
| SQL Review Rules | `/sql-review-rules/mysql`、`/sql-review-rules/postgresql`、`/sql-review-rules/redis` | `sql_review.read` 或 `sql_review.write` |
| Audit Logs | `/audit-logs` | `audit_logs.read` 或 `audit_logs.write` |
| Settings | `/settings` | `settings.read` 或 `settings.write` |

## 設計原則

- 導航頁大多遵循 `*.read` / `*.write`
- `SQL Editor` 與 `Tickets` 的頁面入口使用 `*.read`，工作流動作用動作型 permission
- 實際可作用資料源仍受 DB Scope 限制
- 前端 route guard 只做 UX gating，真正安全邊界在後端
- admin user 與 all-permissions auth group 必須透過統一 helper 永遠取得完整 permission、DB Scope 與 grant 類能力

## 核心 permission 清單

| Permission | 用途 |
|---|---|
| `users.read` / `users.write` | Users / Auth Groups / Resources |
| `db_connections.read` / `db_connections.write` | DB Connections |
| `db_metadata.read` | DB Metadata |
| `masking_rules.read` / `masking_rules.write` | Masking Rules + Whitelist |
| `sql_review.read` / `sql_review.write` | SQL Review Rules |
| `audit_logs.read` / `audit_logs.write` | Audit Logs |
| `settings.read` / `settings.write` | Settings |
| `tickets.read` | 進入 Tickets workspace，查看自己被允許看到的工單 |
| `tickets.apply` | 建立 DDL / DML / Redis / Query Access 工單 |
| `tickets.review` | 審核 DDL / DML / Redis / Query Access 工單 |
| `tickets.execute` | 執行 DDL / DML / Redis 工單 |
| `sql_editor.read` | 進入 SQL Editor workspace |
| `sql_editor.query` | 執行 SQL Editor 查詢、查詢歷史、收藏與 metadata API |
| `sql_editor.admin` | 啟用並使用獨立的管理員 console，以 readwrite credential 直接執行單一 statement |
| `sql_editor.export` | 從 SQL Editor 建立 export 工單 |
| `sql_editor.export_review` | 審核 export 工單 |
| `sql_editor.sensitive_apply` | 建立 sensitive access 工單 |
| `sql_editor.sensitive_review` | 審核 / 撤銷 sensitive access |
| `scheduled_sql_reports.read` | 進入 Scheduled SQL Reports，查看報表與 run history |
| `scheduled_sql_reports.write` | 建立、更新、啟用、停用、刪除 Scheduled SQL Reports |
| `global.sensitive` | 永久繞過 masking |
| `binlog_exports.read` | 查看 scoped MySQL connections、binlog inventory、export jobs 與 artifacts |
| `binlog_exports.execute` | 建立、取消與重試 scoped MySQL Binlog Export jobs |
| `db_sessions.read` | Session Management AWS live topology 與即時 session 查詢 |
| `db_sessions.kill` | Session Management 單次 cancel query、terminate session 或 disconnect Redis client |
| `db_sessions.loop_kill` | 建立與停止有期限、受 kill 數量限制的 Session Management loop-kill job |
| `table_schemas.read` | 進入 Table Schemas，並在後續階段預覽及下載 scoped MySQL table schema |
| `table_schemas.sync` | 在後續階段預覽、建立、取消與重試 scoped MySQL table schema sync job |

## Admin / All Permissions 工程規範

新增 API 或 workflow 時，必須遵守以下規則：

| 權限類型 | 必須使用的後端 helper | 原因 |
|---|---|---|
| 頁面 / API permission | `GetEffectivePermissionKeys()` | 內建 admin user / all-permissions auth group 展開 |
| DB connection scope | `GetEffectiveDBConnectionIDs()` | 內建 admin user / all-permissions auth group 可作用所有 connection |
| 額外授權表，例如 query access grant | `HasAllPermissions()` | grant 檢查前先放行平台全權限身分 |

不要在 handler 或 service 內自行判斷 `admin` 字串，也不要只查新功能自己的 grant table。只查 grant table 會導致 admin user / admin auth group 在新功能中被誤擋。

## API 入口

所有主要 API 都掛在 `/api` 下，路由集中定義於 `backend/cmd/server/main.go`。

### Auth

| API | Gate |
|---|---|
| `POST /api/auth/login` | 無 |
| `POST /api/auth/mfa/verify` | MFA challenge token |
| `POST /api/auth/refresh` | 無 |
| `GET /api/auth/me` | 已登入 + active |
| `POST /api/auth/logout` | 已登入 + active |
| `GET /api/auth/sessions` | 已登入 + active |
| `DELETE /api/auth/sessions/{id}` | 已登入 + active |
| `DELETE /api/auth/sessions` | 已登入 + active |

### Tickets

| API | Gate | 備註 |
|---|---|---|
| `GET /api/tickets` | `requireTicketsRead` | 實際結果仍受 ticket access 控制 |
| `GET /api/tickets/workflow-dashboard-summary` | `requireTicketsRead` | 回傳目前使用者可見的 workflow dashboard 聚合資料 |
| `GET /api/tickets/{id}` | `requireTicketsRead` | 實際結果仍受 ticket access 控制；DDL detail 同時回傳 `online_ddl_runs` 與不含 secret 的 mode enablement |
| `GET /api/tickets/connections` | `requireTicketsApply` | DB 清單再受 DB Scope 過濾 |
| `GET /api/tickets/connections/{id}/databases` | `requireTicketsApply` | 目標 DB 或 Redis DB index 選單 |
| `POST /api/tickets/review` | `requireTicketsApply` | SQL / Redis review、parser、policy、validation |
| `POST /api/tickets/retry-workflow-resolution-batch` | `requireSettingsWrite` | 批次重試需要管理員處理的 workflow resolution |
| `POST /api/tickets` | `requireTicketsApply` | 建立 DDL / DML / Redis / Query Access 工單；不接受 `sql_export` 與 `sensitive_query_access` |
| `POST /api/tickets/{id}/approve` | `requireTicketWorkflowReview` | 依 ticket type 二次檢查 reviewer 權限 |
| `POST /api/tickets/{id}/reject` | `requireTicketWorkflowReject` | reviewer 可拒絕；DDL / DML / Redis 的 DBA 也可於 `approved` / `pending_execution` 階段拒絕 |
| `POST /api/tickets/{id}/withdraw` | `requireTicketsApply` | 僅 submitter 可於 `pending_review` 收回 |
| `POST /api/tickets/{id}/execute` | `requireTicketsExecute` | 只適用 DDL / DML / Redis |
| `POST /api/tickets/{id}/stop` | `requireTicketsExecute` | 停止執行中 ticket |
| `POST /api/tickets/{id}/executions/{executionID}/execute` | `requireTicketsExecute` | 執行單一 statement；無 body 或 `mode=native` 走原生 DDL，`mode=gh-ost/pt-osc` 會驗證 typed parameters 並原子建立 queued run；handler 仍會檢查 executor eligibility |
| `POST /api/tickets/{id}/executions/{executionID}/stop` | `requireTicketsExecute` | 停止單一執行中的 statement；handler 仍會檢查 executor eligibility |
| `POST /api/tickets/{id}/online-ddl/dry-run` | `requireTicketsExecute` | body 以 `execution_id` 指定 approved DDL statement；使用 readwrite credential 執行 gh-ost noop 或 pt-osc `--dry-run`，回傳受限且遮罩後的工具輸出；結果不限制正式執行 |
| `GET /api/tickets/{id}/executions/{executionID}/online-ddl` | `requireTicketsRead` | 實際結果仍受 ticket view access 控制；不存在時回 `404` |
| `POST /api/tickets/{id}/executions/{executionID}/online-ddl/pause` | `requireTicketsExecute` | handler 仍會重驗 `can_stop` 與 DB Scope |
| `POST /api/tickets/{id}/executions/{executionID}/online-ddl/resume` | `requireTicketsExecute` | handler 重驗 `can_execute`、DB Scope與 execution owner |
| `POST /api/tickets/{id}/executions/{executionID}/online-ddl/cancel` | `requireTicketsExecute` | handler 仍會重驗 `can_stop` 與 DB Scope |
| `PATCH /api/tickets/{id}/executions/{executionID}/online-ddl/runtime-parameters` | `requireTicketsExecute` | handler 重驗 `can_execute`、DB Scope與 execution owner；只接受 typed patch + OCC version |
| `GET /api/tickets/{id}/rollbacks/preview` | `requireTicketsApply` | 預覽已產生的 MySQL DML rollback SQL |
| `POST /api/tickets/{id}/rollbacks/create-ticket` | `requireTicketsApply` | 用選定 rollback SQL 建立新的 DML ticket |
| `POST /api/tickets/{id}/rollbacks/{rollbackID}/create-ticket` | `requireTicketsApply` | legacy 單筆 rollback ticket 建立 API |
| `POST /api/tickets/{id}/retry-workflow-resolution` | `requireSettingsWrite` | 重試單一需要管理員處理的 workflow resolution |
| `POST /api/tickets/{id}/revoke` | `requireSensitiveReview` | 只適用 sensitive access |

### SQL Editor / Query

| API | Gate | 備註 |
|---|---|---|
| `GET /api/query/connections` | `requireSQLEditorQuery` | 回傳使用者可用 DB connections |
| `GET /api/query/constraints` | `requireSQLEditorRead` | 回傳 limit / timeout 約束，供 SQL Editor 初始 UI 使用 |
| `POST /api/query` | `requireSQLEditorQuery` | 單 statement 唯讀查詢 |
| `POST /api/query/cancel` | `requireSQLEditorQuery` | 取消目前使用者以 `query_execution_id` 識別的查詢 |
| `POST /api/query/admin/activate` | `requireSQLEditorAdmin` | 啟用當次管理員 console session，檢查 DB Scope 並記錄 audit |
| `POST /api/query/admin/execute` | `requireSQLEditorAdmin` | 以 readwrite endpoint / credential 執行單一 SQL statement 或 Redis command |
| `POST /api/query/sensitive-access` | `requireSQLEditorSensitiveApply` | 建立 sensitive query access 工單 |
| `GET /api/query/history` | `requireSQLEditorQuery` | 查詢歷史 |
| `GET /api/query/saved-queries` | `requireSQLEditorQuery` | 常用 SQL |
| `POST /api/query/saved-queries` | `requireSQLEditorQuery` | 新增常用 SQL |
| `DELETE /api/query/saved-queries/{id}` | `requireSQLEditorQuery` | 刪除常用 SQL |

### SQL Editor Metadata

| API | Gate |
|---|---|
| `GET /api/db-connections/{id}/metadata` | `requireSQLEditorQuery` |
| `GET /api/db-connections/{id}/metadata/search-index` | `requireSQLEditorQuery` |
| `GET /api/db-connections/{id}/metadata/{schema}/{table}/columns` | `requireSQLEditorQuery` |
| `GET /api/db-connections/{id}/metadata/{schema}/{table}/definition` | `requireSQLEditorQuery` |

### Exports

| API | Gate | 備註 |
|---|---|---|
| `POST /api/exports` | `sql_editor.export` | 從 SQL Editor 建立 export ticket，需填寫導出原因 |
| `GET /api/exports/{id}/download` | authenticated user | 24 小時內可重複下載；限 requester、approver 或 `sql_editor.export_review`；每個使用者對每個 export 每分鐘最多 5 次 |
| `GET /api/exports/download/{token}` | authenticated user | legacy download route；不再由新 UI 產生 |

### MySQL Binlog Export

所有 routes 都需要登入、active user、對應 permission，並在 handler 內再次檢查 DB Scope。

| API | Gate | 備註 |
|---|---|---|
| `GET /api/binlog-exports` | `binlog_exports.read` | 只回傳使用者目前 DB Scope 內的 jobs |
| `GET /api/binlog-exports/connections` | `binlog_exports.read` | 只回傳 scoped active MySQL connections |
| `GET /api/binlog-exports/connections/{connectionID}/binlogs` | `binlog_exports.read` + DB Scope | 使用 rollback credential 執行 `SHOW BINARY LOGS` |
| `POST /api/binlog-exports/connections/{connectionID}/binlogs/timestamps` | `binlog_exports.read` + DB Scope | body 為 `{ "file": "..." }`；以 Go MySQL replication client 探測單檔第一個 event 時間 |
| `GET /api/binlog-exports/connections/{connectionID}/databases` | `binlog_exports.read` + DB Scope | 排除 MySQL system schemas |
| `GET /api/binlog-exports/connections/{connectionID}/tables?database=...` | `binlog_exports.read` + DB Scope | 只列 base tables |
| `POST /api/binlog-exports` | `binlog_exports.execute` + DB Scope | 建立 queued job；後端強制驗證 range、filters 與 acknowledgement |
| `GET /api/binlog-exports/{id}` | `binlog_exports.read` + DB Scope | job detail |
| `POST /api/binlog-exports/{id}/cancel` | `binlog_exports.execute` + DB Scope | 只允許 queued / running job |
| `POST /api/binlog-exports/{id}/retry` | `binlog_exports.execute` + DB Scope | 只允許 failed / cancelled / interrupted job |
| `GET /api/binlog-exports/{id}/artifacts/{kind}` | `binlog_exports.read` + DB Scope | `kind` 為 `forward_sql` 或 `rollback_sql`；過期 artifact 不可下載 |
| `GET /api/binlog-exports/{id}/artifacts/{kind}/preview` | `binlog_exports.read` + DB Scope | 最多預覽 64 KiB，並重新檢查 expiry、checksum 與 DB Scope |

Binlog Export 的 error response 固定包含 `code` 與 `error`。前端或 API consumer 應依 `code` 分支，不應解析英文訊息。

### Session Management

目前已提供 AWS topology discovery、唯讀 session list、單一 session 操作、一次性 prefix cancel、bounded loop-kill job，以及 auto refresh、filters 與分頁 UI。所有 routes 都要求登入、active user、對應 permission 與 DB Scope。AWS regions 取自 Settings 的 `db_metadata_inventory_regions`；頁面首次載入與手動 Refresh clusters 直接查詢 AWS API，後續操作使用 server snapshot，不讀取或回退 Metadata Inventory snapshot。

| API | Gate | 備註 |
|---|---|---|
| `GET /api/dba-tools/session-management/aws/clusters?connection_id={id}&refresh=true` | `db_sessions.read` + DB Scope | `refresh=true` 直接掃描 AWS 並替換 Session Management snapshot；只回傳 readonly/readwrite endpoint 能證明歸屬的 cluster |
| `GET /api/dba-tools/session-management/aws/topology?connection_id={id}&region={region}&cluster_id={id}` | `db_sessions.read` + DB Scope + cached ownership validation | 從最近一次 Session Management AWS snapshot 回傳 physical nodes |
| `GET /api/dba-tools/session-management/connections` | `db_sessions.read` + DB Scope | 回傳 MySQL／PostgreSQL／Redis connection 與 operations credential 配置狀態 |
| `POST /api/dba-tools/session-management/sessions` | `db_sessions.read` + DB Scope + target validation | 以 operations credential 讀取最多 1,000 筆 live sessions；AWS node 使用 Session Management snapshot，manual target 每次重驗 host policy |
| `POST /api/dba-tools/session-management/sessions/{id}/cancel` | `db_sessions.kill` + DB Scope + target/identity revalidation | MySQL／PostgreSQL cancel query；Redis 不支援。AWS target 使用 Session Management snapshot，manual target 重驗 host policy |
| `POST /api/dba-tools/session-management/sessions/{id}/terminate` | `db_sessions.kill` + DB Scope + target/identity revalidation | MySQL／PostgreSQL terminate session；Redis disconnect client。AWS target 使用 Session Management snapshot，manual target 重驗 host policy |
| `POST /api/dba-tools/session-management/prefix/preview` | `db_sessions.kill` + DB Scope + target validation | MySQL／PostgreSQL only；database 必填，prefix 至少 16 個非空白字元，只匹配 active/running、未 protected 且達 minimum age 的 session。回傳 60 秒一次性 token、去 literal shape/hash 與匹配 identity |
| `POST /api/dba-tools/session-management/prefix/cancel` | `db_sessions.kill` + DB Scope + live target/preview revalidation | token 綁定 actor、connection 與 target 且只能消耗一次；只 cancel preview 當時已存在且 identity、database、prefix、active state、minimum age 仍符合的 sessions |
| `GET /api/dba-tools/session-management/loop-jobs?limit={n}&offset={n}` | `db_sessions.read` + DB Scope | 回傳 scope 內 jobs；不回傳 encrypted prefix |
| `POST /api/dba-tools/session-management/loop-jobs` | `db_sessions.loop_kill` + DB Scope + live target/preview revalidation | 消耗 60 秒 preview token 建立 job；interval 預設 2 秒、範圍 1-30 秒，duration 預設 600 秒、上限 3,600 秒，max kills 預設 100、上限 1,000 |
| `POST /api/dba-tools/session-management/loop-jobs/{id}/stop` | `db_sessions.loop_kill` + DB Scope | 只允許停止 pending/running job；terminal job 回 409 |

自訂 CNAME 或非 AWS connection 不會被視為 AWS ownership。Manual target session request 必須每次經過 host 格式、port 與 `DB_CONNECTION_HOST_POLICY_*` 檢查。單一操作前，後端會重新讀取 session 並比對 user、database、client、query hash；PostgreSQL 另比對 `backend_start`，避免 PID 重用造成誤殺。tool/system protected session 一律拒絕。成功、失敗、stale skip 與拒絕結果會寫入 audit，僅保存 query hash 與 identity metadata，不保存原始 SQL。

Prefix preview token 僅保存在 application process memory；同 actor／connection／target 的新 preview 會取代舊 token，全域最多保存 1,000 筆，且不保留 raw SQL。Application restart 後所有尚未使用的 preview token 失效。

Loop job 原始 normalized prefix 以 `DBRE_ENCRYPTION_KEY` 加密，只在 pending/running 期間保存；進入 completed/stopped/expired/limit_reached/failed/interrupted 任一終態時會與 active target lock 一起清除。同 physical node 由 DB unique key 保證最多一個 active job。Worker 每輪重驗 operations credential、AWS ownership 或 manual host policy；連續三輪錯誤後標記 failed。Server 啟動時會把殘留 running job 標記 interrupted，不自動恢復。

真實 adapter fixture 可用 `make test-session-management-integration` 驗證。測試會啟動隔離的 MySQL 8、PostgreSQL 16 與 Redis 7 containers，只 signal 測試自行建立並預先記錄 ID 的 session，結束時刪除 containers 與 volumes。預設 host ports 為 `13316`、`15432`、`16379`，可用 `SESSION_IT_MYSQL_PORT`、`SESSION_IT_POSTGRES_PORT`、`SESSION_IT_REDIS_PORT` 覆寫。

### Table Schema Management

Table Schema Management 已啟用 readonly metadata、Schema Export、Sync Preview 與持久化 Sync Job。Sync preview 強制 Source readonly、Target readwrite、雙邊 DB Scope、fresh target table、external dependency 與 target capability preflight，成功時簽發 60 秒 bounded one-time token。Job 依 dependency order 執行，支援 scoped list/detail、cooperative cancel 與 ownership/hash revalidation retry。

| API | Gate |
|---|---|
| `GET /api/dba-tools/table-schemas/connections` | `table_schemas.read` + DB Scope；列出可存取的 MySQL connections |
| `GET /api/dba-tools/table-schemas/connections/{connectionID}/databases` | `table_schemas.read` + DB Scope；排除 MySQL system schemas |
| `GET /api/dba-tools/table-schemas/connections/{connectionID}/tables?database={database}` | `table_schemas.read` + DB Scope；回傳 base tables、source options 與 foreign key dependencies |
| `POST /api/dba-tools/table-schemas/export/preview` | `table_schemas.read` + DB Scope；回傳 ordered DDL、來源／輸出 options、dependency warnings |
| `POST /api/dba-tools/table-schemas/export` | `table_schemas.read` + DB Scope；直接下載 bounded `.sql`，不建立 job 或保存 artifact |
| `POST /api/dba-tools/table-schemas/sync/preview` | `table_schemas.sync` + Source/Target DB Scope；只讀 preflight 並簽發一次性 preview token |
| `POST /api/dba-tools/table-schemas/sync/jobs` | `table_schemas.sync` |
| `GET /api/dba-tools/table-schemas/sync/jobs` | `table_schemas.read` |
| `GET /api/dba-tools/table-schemas/sync/jobs/{id}` | `table_schemas.read` |
| `POST /api/dba-tools/table-schemas/sync/jobs/{id}/cancel` | `table_schemas.sync` |
| `POST /api/dba-tools/table-schemas/sync/jobs/{id}/retry` | `table_schemas.sync` |

### Scheduled SQL Reports

| API | Gate | 備註 |
|---|---|---|
| `GET /api/scheduled-sql-reports` | `requireScheduledSQLReportsRead` | report 列表；read 或 write permission 均可使用 |
| `GET /api/scheduled-sql-reports/{id}` | `requireScheduledSQLReportsRead` | report 詳情與 run history；read 或 write permission 均可使用 |
| `GET /api/scheduled-sql-reports/connections` | `requireScheduledSQLReportsRead` | 可用 DB connections，仍受 DB Scope 過濾 |
| `GET /api/scheduled-sql-reports/recipients` | `requireScheduledSQLReportsRead` | 可選 Lark recipients |
| `POST /api/scheduled-sql-reports` | `requireScheduledSQLReportsWrite` | 建立 report，會檢查 query access 與敏感欄位 |
| `PATCH /api/scheduled-sql-reports/{id}` | `requireScheduledSQLReportsWrite` | 更新 report，會重新檢查 query access 與敏感欄位 |
| `DELETE /api/scheduled-sql-reports/{id}` | `requireScheduledSQLReportsWrite` | 刪除 report |

### DB Connections

| API | Gate |
|---|---|
| `GET /api/db-connections` | `requireDBConnectionsRead` |
| `GET /api/db-connections/{id}/bindings` | `requireDBConnectionsRead` |
| `GET /api/db-connections/{id}/overview` | `db_connections.overview` + DB Scope |
| `GET /api/db-connections/{id}/databases` | `db_connections.databases` + DB Scope |
| `GET /api/db-connections/{id}/accounts` | `db_connections.accounts` + DB Scope |
| `POST /api/db-connections` | `requireDBConnectionsWrite` |
| `PATCH /api/db-connections/{id}` | `requireDBConnectionsWrite` |
| `POST /api/db-connections/{id}/test` | `requireDBConnectionsWrite` |
| `POST /api/db-connections/{id}/test-rollback` | `requireDBConnectionsWrite` |
| `DELETE /api/db-connections/{id}` | `requireDBConnectionsWrite` |

### DB Metadata

| API | Gate |
|---|---|
| `GET /api/db-metadata/inventory` | `requireDBMetadataRead` |
| `GET /api/db-metadata/objects` | `requireDBMetadataRead` |

### Masking

| API | Gate |
|---|---|
| `GET /api/masking-rules/redis-prefixes` | `requireMaskingRulesRead` |
| `POST /api/masking-rules/redis-prefixes` | `requireMaskingRulesWrite` |
| `PATCH /api/masking-rules/redis-prefixes/{id}` | `requireMaskingRulesWrite` |
| `DELETE /api/masking-rules/redis-prefixes/{id}` | `requireMaskingRulesWrite` |
| `GET /api/masking-rules` | `requireMaskingRulesRead` |
| `POST /api/masking-rules` | `requireMaskingRulesWrite` |
| `PATCH /api/masking-rules/{id}` | `requireMaskingRulesWrite` |
| `DELETE /api/masking-rules/{id}` | `requireMaskingRulesWrite` |
| `GET /api/masking-whitelist` | `requireMaskingRulesRead` |
| `GET /api/masking-whitelist/connections` | `requireMaskingRulesRead` |
| `GET /api/masking-whitelist/connections/{id}/metadata` | `requireMaskingRulesRead` |
| `GET /api/masking-whitelist/connections/{id}/metadata/{schema}/{table}/columns` | `requireMaskingRulesRead` |
| `POST /api/masking-whitelist` | `requireMaskingRulesWrite` |
| `PATCH /api/masking-whitelist/{id}` | `requireMaskingRulesWrite` |
| `DELETE /api/masking-whitelist/{id}` | `requireMaskingRulesWrite` |

### SQL Review Rules

| API | Gate |
|---|---|
| `GET /api/sql-review-rules` | `requireSQLReviewRead` |
| `PATCH /api/sql-review-rules/{name}` | `requireSQLReviewWrite` |

### Users / Auth Groups

| API | Gate |
|---|---|
| `GET /api/users` | `requireUsersRead` |
| `GET /api/users/db-connections` | `requireUsersRead` |
| `GET /api/users/query-access-rules` | `requireUsersRead` |
| `POST /api/users/query-access-rules` | `requireUsersWrite` |
| `PUT /api/users/query-access-rules/{id}` | `requireUsersWrite` |
| `POST /api/users/query-access-rules/{id}/revoke` | `requireUsersWrite` |
| `POST /api/users` | `requireUsersWrite` |
| `GET /api/users/{id}` | `requireUsersRead` |
| `PATCH /api/users/{id}` | `requireUsersWrite` |
| `DELETE /api/users/{id}` | `requireUsersWrite` |
| `POST /api/users/{id}/memberships` | `requireUsersWrite` |
| `DELETE /api/users/{id}/memberships/{group}` | `requireUsersWrite` |
| `POST /api/users/{id}/permissions` | `requireUsersWrite` |
| `DELETE /api/users/{id}/permissions/{permissionKey}` | `requireUsersWrite` |
| `POST /api/users/{id}/db-connections` | `requireUsersWrite` |
| `DELETE /api/users/{id}/db-connections/{connID}` | `requireUsersWrite` |
| `GET /api/users/{id}/sessions` | `requireUsersRead` |
| `DELETE /api/users/{id}/sessions/{sessionID}` | `requireUsersWrite` |
| `DELETE /api/users/{id}/sessions` | `requireUsersWrite` |
| `POST /api/users/{id}/mfa/reset` | `requireUsersWrite` |
| `GET /api/auth-groups` | `requireUsersRead` |
| `POST /api/auth-groups` | `requireUsersWrite` |
| `GET /api/auth-groups/{group}` | `requireUsersRead` |
| `PATCH /api/auth-groups/{group}` | `requireUsersWrite` |
| `DELETE /api/auth-groups/{group}` | `requireUsersWrite` |
| `POST /api/auth-groups/{group}/permissions` | `requireUsersWrite` |
| `DELETE /api/auth-groups/{group}/permissions/{permissionKey}` | `requireUsersWrite` |
| `POST /api/auth-groups/{group}/db-connections` | `requireUsersWrite` |
| `DELETE /api/auth-groups/{group}/db-connections/{connID}` | `requireUsersWrite` |

### Audit Logs / Settings / Notifications / Realtime

| API | Gate |
|---|---|
| `GET /api/audit-logs` | `requireAuditLogsRead` |
| `GET /api/audit-logs/export` | `requireAuditLogsWrite` |
| `GET /api/settings` | `requireSettingsRead` | 回傳 Online DDL enablement，並以 `online_ddl_tools` 顯示 image 內 gh-ost／pt-osc readiness 與 canonical version；不回傳 binary path 或 raw command output |
| `GET /api/settings/db-connections` | `requireSettingsRead` |
| `GET /api/settings/users` | `requireSettingsRead` |
| `GET /api/settings/approval-resolution` | `requireSettingsRead` |
| `GET /api/settings/workflow-rules` | `requireSettingsRead` |
| `PUT /api/settings/workflow-rules` | `requireSettingsWrite` |
| `POST /api/settings/workflow-rules/preview` | `requireSettingsRead` |
| `POST /api/settings/workflow-rules/effective-preview` | `requireSettingsRead` |
| `POST /api/settings/workflow-rules/simulate` | `requireSettingsRead` |
| `PATCH /api/settings` | `requireSettingsWrite` |
| `GET /api/notifications` | 已登入 |
| `GET /api/notifications/summary` | 已登入 |
| `POST /api/notifications/read-all` | 已登入 |
| `POST /api/notifications/{id}/read` | 已登入 |
| `GET /api/events/stream` | 已登入 + active |

補充：

- `GET /api/events/stream` 是 SSE stream endpoint，不是一般短請求 API
- 它與一般 REST 共用同一台 server，但不套用一般 request timeout，且只在該 request 內清除 write deadline
- 這個設計是為了讓通知與工單狀態更新可穩定長連線，同時保留其他 API 的 timeout 保護
- `GET /api/exports/{id}/download` 與 legacy `GET /api/exports/download/{token}` 也不套一般 request timeout，且會在該 request 內清除 write deadline；查詢熔斷由 SQL Export Timeout settings 控制

## Ticket 類型與 workflow 權限

| Ticket Type | Reviewer | Executor |
|---|---|---|
| `ddl` | `tickets.review` | `tickets.execute` |
| `dml` | `tickets.review` | `tickets.execute` |
| `redis_command` | `tickets.review` | `tickets.execute` |
| `query_access` | `tickets.review` | 無獨立 execute，approve 後 scope 生效 |
| `sql_export` | `sql_editor.export_review` | 無獨立 execute，approve 後可下載 |
| `sensitive_query_access` | `sql_editor.sensitive_review` | 無獨立 execute，approve 後 scope 生效 |

審批人還需要被 Workflow Rules 指定。Permission 只代表具備審批資格；Workflow Rules 決定該 workflow 會路由給哪些候選人。有效審批人會排除 inactive user，以及缺少該 workflow review permission 的候選人。

## Ticket 通知與角色對照

| 事件 | 提交人 | 審批人 | 執行人 |
|---|---|---|---|
| submit | 否 | 是 | 否 |
| withdraw | 否 | 是 | 否 |
| review reject | 是 | 否 | 否 |
| review approve: `ddl` / `dml` / `redis_command` | 否 | 否 | 是 |
| review approve: `sql_export` / `sensitive_query_access` | 是 | 否 | 否 |
| execution reject | 是 | 否 | 否 |
| execution success | 是 | 否 | 否 |
| execution failed | 是 | 否 | 是 |

補充：

- `submitter = executor` 時，仍需滿足上述規則
- 例如執行成功時，即使執行者本人同時也是提交人，也應收到成功通知

## 與兩條 RBAC 原則的對齊

| 類別 | 是否符合原則一 | 是否符合原則二 |
|---|---|---|
| CRUD 型導航頁 | 是 | 不適用 |
| SQL Editor | 是，使用 `sql_editor.read` 代表頁面入口 | 是，DB 作用範圍靠 DB Scope |
| Tickets | 是，使用 `tickets.read` 代表頁面入口 | 是，DB 作用範圍靠 DB Scope |
| Resources 子頁 | 是，隸屬 `users.read` / `users.write` workspace | 不適用 |

## 相關文件

- [權限模型](../explanation/permission-model.md)
- [Tickets](tickets.md)
- [SQL Editor](sql-editor.md)
- [Scheduled SQL Reports](scheduled-sql-reports.md)
- [Workflow Rules](workflow-rules.md)
- [登入安全與 Session](auth-and-sessions.md)
- [Users / RBAC](users-and-rbac.md)
