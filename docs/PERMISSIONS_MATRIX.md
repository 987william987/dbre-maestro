# 權限對照表

本文整理目前系統中的 `permission` 與頁面、API、動作之間的對照，作為 RBAC 維護基準。

最後更新日期：`2026-06-12`

## 設計原則

- 頁面可見權限與 API 寫入權限分開管理。
- `*.read` 通常對應列表/詳情/查看頁。
- `*.write` 通常對應新增/修改/刪除。
- 工單流轉相關權限不完全等於頁面權限，需另外看 `review / execute / apply`。
- 特殊審批人不再由 Settings 名單維護，統一回到 RBAC permission 管理。

## 權限清單

| Permission | 類別 | 用途 |
|---|---|---|
| `users.read` | users | 查看 Users / RBAC 工作台與 auth group 資訊 |
| `users.write` | users | 管理 user、auth group、direct permission、DB scope |
| `audit_logs.read` | audit_logs | 查看稽核日誌 |
| `audit_logs.write` | audit_logs | 匯出稽核日誌 |
| `settings.read` | settings | 查看 Settings 頁 |
| `settings.write` | settings | 修改 Settings API |
| `db_connections.read` | db_connections | 查看資料庫連線 |
| `db_connections.write` | db_connections | 新增/修改/刪除/測試資料庫連線 |
| `masking_rules.read` | masking_rules | 查看 masking rules 與 whitelist |
| `masking_rules.write` | masking_rules | 管理 masking rules 與 whitelist |
| `sql_review.read` | sql_review | 查看 SQL review rules |
| `sql_review.write` | sql_review | 修改 SQL review rules |
| `tickets.apply` | tickets | 建立 DDL / DML 工單 |
| `tickets.review` | tickets | 審批 DDL / DML 工單 |
| `tickets.execute` | tickets | 執行 DDL / DML 工單 |
| `sql_editor.query` | sql_editor | 使用 SQL Editor 查詢 |
| `sql_editor.export` | sql_editor | 從 SQL Editor 建立 `sql_export` 工單 |
| `sql_editor.export_review` | sql_editor | 審批 `sql_export` 工單 |
| `sql_editor.sensitive_apply` | sql_editor | 建立 `sensitive_query_access` 工單 |
| `sql_editor.sensitive_review` | sql_editor | 審批與撤銷 `sensitive_query_access` 工單 |
| `sql_editor.sensitive_execute` | sql_editor | 預留權限，當前版本未接到獨立前端動作 |
| `global.sensitive` | global | 永久繞過 masking 規則 |

## 頁面對照

| 頁面 | Route | 可見權限 | 可執行動作 |
|---|---|---|---|
| Tickets | `/tickets` | 任一工單工作台權限：`tickets.apply / tickets.review / tickets.execute / sql_editor.export / sql_editor.export_review / sql_editor.sensitive_apply / sql_editor.sensitive_review` | 查看工單列表 |
| Ticket Detail | `/tickets/:id` | 同 `/tickets` | 依工單類型與角色顯示 approve / reject / revoke / request-execution / execute / download |
| New Ticket | `/tickets/new` | `tickets.apply` | 建立 DDL / DML 工單 |
| SQL Editor | `/sql-editor` | `sql_editor.query` | 查詢、收藏 SQL；若另有對應權限可建立 export / sensitive access 工單 |
| Users | `/users` | `users.read` 或 `users.write` | 查看 RBAC；若有 `users.write` 可修改 |
| DB Connections | `/db-connections` | `db_connections.read` 或 `db_connections.write` | 查看；若有 `db_connections.write` 可新增/修改/刪除/測試 |
| Masking Rules | `/masking-rules` | `masking_rules.read` 或 `masking_rules.write` | 查看；若有 `masking_rules.write` 可管理規則 |
| SQL Review | `/sql-review-rules` | `sql_review.read` 或 `sql_review.write` | 查看；若有 `sql_review.write` 可修改規則 |
| Audit Logs | `/audit-logs` | `audit_logs.read` 或 `audit_logs.write` | 查看；若有 `audit_logs.write` 可匯出 |
| Settings | `/settings` | `settings.read` 或 `settings.write` | 目前主要作為平台設定入口說明；PATCH API 保留給後端 |

## API 對照

### Auth

| API | 權限 |
|---|---|
| `POST /api/auth/login` | 無 |
| `POST /api/auth/refresh` | 無 |
| `GET /api/auth/me` | 已登入 |
| `POST /api/auth/logout` | 已登入 |

### Tickets

| API | 權限 | 說明 |
|---|---|---|
| `GET /api/tickets` | 已登入 | 實際可見資料仍受後端 ticket access 控制 |
| `POST /api/tickets` | `tickets.apply` | 建立 DDL / DML 工單 |
| `GET /api/tickets/{id}` | 已登入 | 實際可見資料仍受後端 ticket access 控制 |
| `POST /api/tickets/{id}/approve` | `tickets.review` 或 `sql_editor.export_review` 或 `sql_editor.sensitive_review` | 依工單類型由後端再次校驗 |
| `POST /api/tickets/{id}/reject` | `tickets.review` 或 `sql_editor.export_review` 或 `sql_editor.sensitive_review` | 同上 |
| `POST /api/tickets/{id}/revoke` | `sql_editor.sensitive_review` | 僅限 `sensitive_query_access` |
| `POST /api/tickets/{id}/request-execution` | `tickets.execute` | 僅限 DDL / DML |
| `POST /api/tickets/{id}/execute` | `tickets.execute` | 僅限 DDL / DML |
| `POST /api/tickets/{id}/stop` | `tickets.execute` | 停止執行中工單 |

### SQL Editor / Query

| API | 權限 | 說明 |
|---|---|---|
| `POST /api/query` | `sql_editor.query` | 執行查詢 |
| `POST /api/query/sensitive-access` | `sql_editor.sensitive_apply` | 建立 `sensitive_query_access` 工單 |
| `GET /api/query/history` | `sql_editor.query` | 查詢歷史 |
| `GET /api/query/saved-queries` | `sql_editor.query` | 常用 SQL |
| `POST /api/query/saved-queries` | `sql_editor.query` | 儲存常用 SQL |
| `DELETE /api/query/saved-queries/{id}` | `sql_editor.query` | 刪除常用 SQL |

### Exports

| API | 權限 | 說明 |
|---|---|---|
| `POST /api/exports` | `sql_editor.export` | 從 SQL Editor 建立 `sql_export` 工單 |
| `GET /api/exports/download/{token}` | token-based | 下載 ready export，不走 Bearer 權限判定 |

### Metadata

| API | 權限 |
|---|---|
| `GET /api/db-connections/{id}/metadata` | `sql_editor.query` |
| `GET /api/db-connections/{id}/metadata/{schema}/{table}/columns` | `sql_editor.query` |

### Notifications

| API | 權限 |
|---|---|
| `GET /api/notifications` | 已登入 |
| `POST /api/notifications/read-all` | 已登入 |
| `POST /api/notifications/{id}/read` | 已登入 |

### Users / RBAC

| API | 權限 |
|---|---|
| `GET /api/users` | `users.read` 或 `users.write` |
| `POST /api/users` | `users.write` |
| `GET /api/users/{id}` | `users.read` 或 `users.write` |
| `PATCH /api/users/{id}` | `users.write` |
| `DELETE /api/users/{id}` | `users.write` |
| `POST /api/users/{id}/memberships` | `users.write` |
| `DELETE /api/users/{id}/memberships/{group}` | `users.write` |
| `POST /api/users/{id}/permissions` | `users.write` |
| `DELETE /api/users/{id}/permissions/{permissionKey}` | `users.write` |
| `POST /api/users/{id}/db-connections` | `users.write` |
| `DELETE /api/users/{id}/db-connections/{connID}` | `users.write` |
| `GET /api/auth-groups` | `users.read` 或 `users.write` |
| `POST /api/auth-groups` | `users.write` |
| `GET /api/auth-groups/{group}` | `users.read` 或 `users.write` |
| `PATCH /api/auth-groups/{group}` | `users.write` |
| `DELETE /api/auth-groups/{group}` | `users.write` |
| `POST /api/auth-groups/{group}/permissions` | `users.write` |
| `DELETE /api/auth-groups/{group}/permissions/{permissionKey}` | `users.write` |
| `POST /api/auth-groups/{group}/db-connections` | `users.write` |
| `DELETE /api/auth-groups/{group}/db-connections/{connID}` | `users.write` |

### Governance

| API | 權限 |
|---|---|
| `GET /api/db-connections` | `db_connections.read` 或 `db_connections.write` |
| `POST /api/db-connections` | `db_connections.write` |
| `PATCH /api/db-connections/{id}` | `db_connections.write` |
| `POST /api/db-connections/{id}/test` | `db_connections.write` |
| `DELETE /api/db-connections/{id}` | `db_connections.write` |
| `GET /api/masking-rules` | `masking_rules.read` 或 `masking_rules.write` |
| `POST /api/masking-rules` | `masking_rules.write` |
| `PATCH /api/masking-rules/{id}` | `masking_rules.write` |
| `DELETE /api/masking-rules/{id}` | `masking_rules.write` |
| `GET /api/masking-whitelist` | `masking_rules.read` 或 `masking_rules.write` |
| `POST /api/masking-whitelist` | `masking_rules.write` |
| `PATCH /api/masking-whitelist/{id}` | `masking_rules.write` |
| `DELETE /api/masking-whitelist/{id}` | `masking_rules.write` |
| `GET /api/sql-review-rules` | `sql_review.read` 或 `sql_review.write` |
| `PATCH /api/sql-review-rules/{name}` | `sql_review.write` |
| `GET /api/audit-logs` | `audit_logs.read` 或 `audit_logs.write` |
| `GET /api/audit-logs/export` | `audit_logs.write` |
| `GET /api/settings` | `settings.read` 或 `settings.write` |
| `PATCH /api/settings` | `settings.write` |

## 工單類型與 reviewer permission

| 工單類型 | reviewer permission | executor permission |
|---|---|---|
| `ddl` | `tickets.review` | `tickets.execute` |
| `dml` | `tickets.review` | `tickets.execute` |
| `sql_export` | `sql_editor.export_review` | 無獨立 executor；approve 後生成 ready export |
| `sensitive_query_access` | `sql_editor.sensitive_review` | 無獨立 executor；approve 後依 scope 生效 |

## 通知類型

| 通知 type | 來源 | 常見收件者 |
|---|---|---|
| `ticket_submitted` | submit 成功 | submitter |
| `ticket_pending_review` | 建單後 | reviewer |
| `ticket_approved` | approve 後 | submitter |
| `ticket_rejected` | reject 後 | submitter |
| `ticket_pending_execution` | request execution 後 | executor |
| `ticket_executed` | execute complete / failed 後 | submitter |
| `ticket_revoked` | revoke 後 | submitter |

## 備註

- `sql_editor.sensitive_execute` 目前仍存在 permission catalog，但當前版本沒有單獨對應的頁面按鈕或 API gate。
- `global.sensitive` 是資料遮罩例外權限，不對應單一頁面。
- 若新增 permission，請同步更新：
  1. migration
  2. Users 頁 permission catalog
  3. 前端 route guard / navigation
  4. 後端 middleware gate
  5. 本文件
