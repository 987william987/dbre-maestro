# 後端 API 與權限對照

本文件整理目前主要導航頁、route guard 與後端 API permission gate 的對應關係。

## 導航頁與 route guard

| 頁面 | Route | 前端可見條件 |
|---|---|---|
| Tickets | `/tickets` | `tickets.apply`、`tickets.review`、`tickets.execute`、`sql_editor.export`、`sql_editor.export_review`、`sql_editor.sensitive_apply`、`sql_editor.sensitive_review` 任一 |
| New Ticket | `/tickets/new` | `tickets.apply` |
| SQL Editor | `/sql-editor` | `sql_editor.query` |
| Users | `/users` | `users.read` 或 `users.write` |
| Auth Groups | `/users/groups` | `users.read` 或 `users.write` |
| DB Connections | `/db-connections` | `db_connections.read` 或 `db_connections.write` |
| DB Metadata | `/db-metadata/inventory`、`/db-metadata/objects` | `db_metadata.read` |
| Masking Rules | `/masking-rules`、`/masking-rules/dsl-guide` | `masking_rules.read` 或 `masking_rules.write` |
| SQL Review Rules | `/sql-review-rules` | `sql_review.read` 或 `sql_review.write` |
| Audit Logs | `/audit-logs` | `audit_logs.read` 或 `audit_logs.write` |
| Settings | `/settings` | `settings.read` 或 `settings.write` |

## 設計原則

- 導航頁大多遵循 `*.read` / `*.write`
- `SQL Editor` 與 `Tickets` 採動作型 permission
- 實際可作用資料源仍受 DB Scope 限制
- 前端 route guard 只做 UX gating，真正安全邊界在後端

## 核心 permission 清單

| Permission | 用途 |
|---|---|
| `users.read` / `users.write` | Users / Auth Groups |
| `db_connections.read` / `db_connections.write` | DB Connections |
| `db_metadata.read` | DB Metadata |
| `masking_rules.read` / `masking_rules.write` | Masking Rules + Whitelist |
| `sql_review.read` / `sql_review.write` | SQL Review Rules |
| `audit_logs.read` / `audit_logs.write` | Audit Logs |
| `settings.read` / `settings.write` | Settings |
| `tickets.apply` | 建立 DDL / DML 工單，並可進入 ticket workspace |
| `tickets.review` | 審核 DDL / DML 工單 |
| `tickets.execute` | 執行 DDL / DML 工單 |
| `sql_editor.query` | 使用 SQL Editor |
| `sql_editor.export` | 從 SQL Editor 建立 export 工單 |
| `sql_editor.export_review` | 審核 export 工單 |
| `sql_editor.sensitive_apply` | 建立 sensitive access 工單 |
| `sql_editor.sensitive_review` | 審核 / 撤銷 sensitive access |
| `global.sensitive` | 永久繞過 masking |

## API 入口

所有主要 API 都掛在 `/api` 下，路由集中定義於 `backend/cmd/server/main.go`。

### Auth

| API | Gate |
|---|---|
| `POST /api/auth/login` | 無 |
| `POST /api/auth/refresh` | 無 |
| `GET /api/auth/me` | 已登入 + active |
| `POST /api/auth/logout` | 已登入 + active |

### Tickets

| API | Gate | 備註 |
|---|---|---|
| `GET /api/tickets` | `requireTicketsWorkspaceRead` | 實際結果仍受 ticket access 控制 |
| `GET /api/tickets/{id}` | `requireTicketsWorkspaceRead` | 同上 |
| `GET /api/tickets/connections` | `requireTicketsApply` | DB 清單再受 DB Scope 過濾 |
| `GET /api/tickets/connections/{id}/databases` | `requireTicketsApply` | 目標 DB 選單 |
| `POST /api/tickets/review` | `requireTicketsApply` | SQL review / parser / policy 檢測 |
| `POST /api/tickets` | `requireTicketsApply` | 建立 DDL / DML 工單 |
| `POST /api/tickets/{id}/approve` | `requireTicketWorkflowReview` | 依 ticket type 二次檢查 reviewer 權限 |
| `POST /api/tickets/{id}/reject` | `requireTicketWorkflowReview` | 同上 |
| `POST /api/tickets/{id}/request-execution` | `requireTicketsExecute` | 只適用 DDL / DML |
| `POST /api/tickets/{id}/execute` | `requireTicketsExecute` | 只適用 DDL / DML |
| `POST /api/tickets/{id}/stop` | `requireTicketsExecute` | 停止執行中 ticket |
| `POST /api/tickets/{id}/revoke` | `requireSensitiveReview` | 只適用 sensitive access |

### SQL Editor / Query

| API | Gate | 備註 |
|---|---|---|
| `GET /api/query/connections` | `requireSQLEditorQuery` | 回傳使用者可用 DB connections |
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
| `POST /api/exports` | `sql_editor.export` | 從 SQL Editor 建立 export ticket |
| `GET /api/exports/download/{token}` | token-based | 不走 Bearer gate，但有下載頻率限制 |

### DB Connections

| API | Gate |
|---|---|
| `GET /api/db-connections` | `requireDBConnectionsRead` |
| `POST /api/db-connections` | `requireDBConnectionsWrite` |
| `PATCH /api/db-connections/{id}` | `requireDBConnectionsWrite` |
| `POST /api/db-connections/{id}/test` | `requireDBConnectionsWrite` |
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
| `GET /api/auth-groups` | `requireUsersRead` |
| `POST /api/auth-groups` | `requireUsersWrite` |
| `GET /api/auth-groups/{group}` | `requireUsersRead` |
| `PATCH /api/auth-groups/{group}` | `requireUsersWrite` |
| `DELETE /api/auth-groups/{group}` | `requireUsersWrite` |

### Audit Logs / Settings / Notifications

| API | Gate |
|---|---|
| `GET /api/audit-logs` | `requireAuditLogsRead` |
| `GET /api/audit-logs/export` | `requireAuditLogsWrite` |
| `GET /api/settings` | `requireSettingsRead` |
| `GET /api/settings/db-connections` | `requireSettingsRead` |
| `PATCH /api/settings` | `requireSettingsWrite` |
| `GET /api/notifications` | 已登入 |
| `POST /api/notifications/read-all` | 已登入 |
| `POST /api/notifications/{id}/read` | 已登入 |

## Ticket 類型與 workflow 權限

| Ticket Type | Reviewer | Executor |
|---|---|---|
| `ddl` | `tickets.review` | `tickets.execute` |
| `dml` | `tickets.review` | `tickets.execute` |
| `sql_export` | `sql_editor.export_review` | 無獨立 execute，approve 後可下載 |
| `sensitive_query_access` | `sql_editor.sensitive_review` | 無獨立 execute，approve 後 scope 生效 |

## 與兩條 RBAC 原則的對齊

| 類別 | 是否符合原則一 | 是否符合原則二 |
|---|---|---|
| CRUD 型導航頁 | 是 | 不適用 |
| SQL Editor | 是，使用動作型 permission 代表頁面入口 | 是，DB 作用範圍靠 DB Scope |
| Tickets | 是，`tickets.apply` 作為工作台最小入口 | 是，DB 作用範圍靠 DB Scope |

## 相關文件

- [權限模型](../explanation/permission-model.md)
- [Tickets](tickets.md)
- [SQL Editor](sql-editor.md)
