# 後端 API 與權限對照

本文件整理目前主要導航頁、route guard 與後端 API permission gate 的對應關係。

## 導航頁與 route guard

| 頁面 | Route | 前端可見條件 |
|---|---|---|
| Tickets | `/tickets` | `tickets.read` |
| New Ticket | `/tickets/new` | `tickets.apply` |
| SQL Editor | `/sql-editor` | `sql_editor.read` |
| Scheduled Reports | `/scheduled-sql-reports` | `scheduled_sql_reports.read` 或 `scheduled_sql_reports.write` |
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
| `sql_editor.export` | 從 SQL Editor 建立 export 工單 |
| `sql_editor.export_review` | 審核 export 工單 |
| `sql_editor.sensitive_apply` | 建立 sensitive access 工單 |
| `sql_editor.sensitive_review` | 審核 / 撤銷 sensitive access |
| `scheduled_sql_reports.read` | 進入 Scheduled SQL Reports，查看報表與 run history |
| `scheduled_sql_reports.write` | 建立、更新、啟用、停用、刪除 Scheduled SQL Reports |
| `global.sensitive` | 永久繞過 masking |

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
| `GET /api/tickets` | `requireTicketsWorkspaceRead` | 實際結果仍受 ticket access 控制 |
| `GET /api/tickets/{id}` | `requireTicketsWorkspaceRead` | 同上 |
| `GET /api/tickets/connections` | `requireTicketsApply` | DB 清單再受 DB Scope 過濾 |
| `GET /api/tickets/connections/{id}/databases` | `requireTicketsApply` | 目標 DB 或 Redis DB index 選單 |
| `POST /api/tickets/review` | `requireTicketsApply` | SQL / Redis review、parser、policy、validation |
| `POST /api/tickets` | `requireTicketsApply` | 建立 DDL / DML / Redis / Query Access 工單；不接受 `sql_export` 與 `sensitive_query_access` |
| `POST /api/tickets/{id}/approve` | `requireTicketWorkflowReview` | 依 ticket type 二次檢查 reviewer 權限 |
| `POST /api/tickets/{id}/reject` | `requireTicketWorkflowReject` | reviewer 可拒絕；DDL / DML / Redis 的 DBA 也可於 `approved` / `pending_execution` 階段拒絕 |
| `POST /api/tickets/{id}/withdraw` | `requireTicketsApply` | 僅 submitter 可於 `pending_review` 收回 |
| `POST /api/tickets/{id}/execute` | `requireTicketsExecute` | 只適用 DDL / DML / Redis |
| `POST /api/tickets/{id}/stop` | `requireTicketsExecute` | 停止執行中 ticket |
| `GET /api/tickets/{id}/rollbacks/preview` | `requireTicketsApply` | 預覽已產生的 MySQL DML rollback SQL |
| `POST /api/tickets/{id}/rollbacks/create-ticket` | `requireTicketsApply` | 用選定 rollback SQL 建立新的 DML ticket |
| `POST /api/tickets/{id}/rollbacks/{rollbackID}/create-ticket` | `requireTicketsApply` | legacy 單筆 rollback ticket 建立 API |
| `POST /api/tickets/{id}/revoke` | `requireSensitiveReview` | 只適用 sensitive access |

### SQL Editor / Query

| API | Gate | 備註 |
|---|---|---|
| `GET /api/query/connections` | `requireSQLEditorQuery` | 回傳使用者可用 DB connections |
| `GET /api/query/constraints` | `requireSQLEditorRead` | 回傳 limit / timeout 約束，供 SQL Editor 初始 UI 使用 |
| `POST /api/query` | `requireSQLEditorQuery` | 單 statement 唯讀查詢 |
| `POST /api/query/sensitive-access` | `requireSQLEditorSensitiveApply` | 建立 sensitive query access 工單 |
| `GET /api/query/history` | `requireSQLEditorQuery` | 查詢歷史 |
| `GET /api/query/saved-queries` | `requireSQLEditorQuery` | 常用 SQL |
| `POST /api/query/saved-queries` | `requireSQLEditorQuery` | 新增常用 SQL |
| `DELETE /api/query/saved-queries/{id}` | `requireSQLEditorQuery` | 刪除常用 SQL |

### SQL Editor Metadata

| API | Gate |
|---|---|
| `GET /api/db-connections/{id}/metadata` | `requireSQLEditorQuery` |
| `GET /api/db-connections/{id}/metadata/{schema}/{table}/columns` | `requireSQLEditorQuery` |
| `GET /api/db-connections/{id}/metadata/{schema}/{table}/definition` | `requireSQLEditorQuery` |

### Exports

| API | Gate | 備註 |
|---|---|---|
| `POST /api/exports` | `sql_editor.export` | 從 SQL Editor 建立 export ticket，需填寫導出原因 |
| `GET /api/exports/{id}/download` | authenticated user | 24 小時內可重複下載；限 requester、approver 或 `sql_editor.export_review`；每個使用者對每個 export 每分鐘最多 5 次 |
| `GET /api/exports/download/{token}` | authenticated user | legacy download route；不再由新 UI 產生 |

### Scheduled SQL Reports

| API | Gate | 備註 |
|---|---|---|
| `GET /api/scheduled-sql-reports` | `scheduled_sql_reports.read` | report 列表 |
| `GET /api/scheduled-sql-reports/{id}` | `scheduled_sql_reports.read` | report 詳情與 run history |
| `GET /api/scheduled-sql-reports/connections` | `scheduled_sql_reports.read` | 可用 DB connections，仍受 DB Scope 過濾 |
| `GET /api/scheduled-sql-reports/recipients` | `scheduled_sql_reports.read` | 可選 Lark recipients |
| `POST /api/scheduled-sql-reports` | `scheduled_sql_reports.write` | 建立 report，會檢查 query access 與敏感欄位 |
| `PATCH /api/scheduled-sql-reports/{id}` | `scheduled_sql_reports.write` | 更新 report，會重新檢查 query access 與敏感欄位 |
| `DELETE /api/scheduled-sql-reports/{id}` | `scheduled_sql_reports.write` | 刪除 report |

### DB Connections

| API | Gate |
|---|---|
| `GET /api/db-connections` | `requireDBConnectionsRead` |
| `GET /api/db-connections/{id}/bindings` | `requireDBConnectionsRead` |
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
| `GET /api/settings` | `requireSettingsRead` |
| `GET /api/settings/db-connections` | `requireSettingsRead` |
| `GET /api/settings/approval-resolution` | `requireSettingsRead` |
| `GET /api/settings/workflow-rules` | `requireSettingsRead` |
| `PUT /api/settings/workflow-rules` | `requireSettingsWrite` |
| `POST /api/settings/workflow-rules/preview` | `requireSettingsRead` |
| `POST /api/settings/workflow-rules/effective-preview` | `requireSettingsRead` |
| `POST /api/settings/workflow-rules/simulate` | `requireSettingsRead` |
| `PATCH /api/settings` | `requireSettingsWrite` |
| `GET /api/notifications` | 已登入 |
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
