# Users / RBAC

Users 模組同時管理三個視角：

- Users
- Auth Groups
- Resources

它們共用同一組導航 permission：`users.read` / `users.write`。

## 頁面入口

| 子頁 | Route |
|---|---|
| Users | `/users` |
| Auth Groups | `/users/groups` |
| Resources | `/users/resources` |

## 權限原則

- `users.read`：可查看這三個子頁
- `users.write`：可建立、編輯、刪除、綁定與解除綁定

這符合系統的頁面型權限原則：導航頁面使用 `*.read` / `*.write`，不是零散按鈕 permission。

Workbench 的權限邊界分成頁面入口與操作權限：

| 權限 | 類型 | 用途 |
|---|---|---|
| `tickets.read` | 頁面入口 | 進入 Tickets workspace |
| `tickets.apply` | 操作 | 建立 DDL / DML / Redis / Query Access 工單 |
| `tickets.review` | 操作 | 具備一般工單審批資格 |
| `tickets.execute` | 操作 | 具備 DDL / DML / Redis 執行資格 |
| `sql_editor.read` | 頁面入口 | 進入 SQL Editor |
| `sql_editor.query` | 操作 | 執行查詢、讀取查詢相關 metadata |
| `sql_editor.export` | 操作 | 發起 SQL Export |
| `sql_editor.export_review` | 操作 | 具備 SQL Export 審批資格 |
| `sql_editor.sensitive_apply` | 操作 | 發起 Sensitive Query Access |
| `sql_editor.sensitive_review` | 操作 | 具備 Sensitive Query Access 審批 / revoke 資格 |
| `scheduled_sql_reports.read` | 頁面入口 | 進入 Scheduled SQL Reports |
| `scheduled_sql_reports.write` | 操作 | 建立、更新、啟用、停用、刪除 Scheduled SQL Reports |

## Users 視角

User detail 目前管理：

- `username`
- `email`
- `lark_recipient`
- `is_active`
- `auth group memberships`
- `direct permissions`
- `direct DB connection scope`

說明：

- `email` 是平台帳號資料，保持唯一
- `lark_recipient` 目前保存 Lark `open_id`
- `is_active = false` 代表不可登入

## Auth Groups 視角

Auth Group 是權限與 DB Scope 的群組綁定單位，可管理：

- 群組顯示名稱與說明
- 群組 permissions
- 群組 DB connection scope
- 群組成員

使用者最終有效權限，會是：

- direct permissions
- 加上所有 auth group permissions

同理，最終可用 DB Scope 也會是 direct scope 與 group scope 的聯集。

## Resources 視角

Resources 子頁是資源反查視角，重點不是編輯，而是檢查：

- 某個 DB Connection 是否被直接綁到 user
- 哪些 auth group 持有這個 connection
- 哪些 user 經由群組或直接綁定而最終有效可用

這對排查「某個人為什麼看得到某個 DB」特別重要。

## API

### Users

| API | 用途 |
|---|---|
| `GET /api/users` | 列表 |
| `POST /api/users` | 建立 |
| `GET /api/users/{id}` | 詳情 |
| `PATCH /api/users/{id}` | 更新 |
| `DELETE /api/users/{id}` | 刪除 |
| `POST /api/users/{id}/memberships` | 加入 auth group |
| `DELETE /api/users/{id}/memberships/{group}` | 移出 auth group |
| `POST /api/users/{id}/permissions` | 新增 direct permission |
| `DELETE /api/users/{id}/permissions/{permissionKey}` | 移除 direct permission |
| `POST /api/users/{id}/db-connections` | 新增 direct DB scope |
| `DELETE /api/users/{id}/db-connections/{connID}` | 移除 direct DB scope |

### Auth Groups

| API | 用途 |
|---|---|
| `GET /api/auth-groups` | 列表 |
| `POST /api/auth-groups` | 建立 |
| `GET /api/auth-groups/{group}` | 詳情 |
| `PATCH /api/auth-groups/{group}` | 更新 |
| `DELETE /api/auth-groups/{group}` | 刪除 |
| `POST /api/auth-groups/{group}/permissions` | 新增群組 permission |
| `DELETE /api/auth-groups/{group}/permissions/{permissionKey}` | 移除群組 permission |
| `POST /api/auth-groups/{group}/db-connections` | 新增群組 DB scope |
| `DELETE /api/auth-groups/{group}/db-connections/{connID}` | 移除群組 DB scope |

### Resources

Resources 子頁目前不是獨立後端 namespace，而是前端組合：

- `GET /api/users/db-connections`
- `GET /api/db-connections/{id}/bindings`

## 與 SQL Editor / Tickets 的關係

Users / Auth Groups 並不直接決定 SQL 能不能執行哪種語句，而是決定：

- 看不看得到哪個功能頁
- 可不可進入某個 workspace
- 哪些 DB Connections 屬於有效作用範圍

例如：

- 有 `sql_editor.read` 才可以進 SQL Editor，有 `sql_editor.query` 才可以執行查詢
- 有 `tickets.read` 才可以進 Tickets workspace，有 `tickets.apply` 才可以建立一般工單
- 有 `scheduled_sql_reports.read` 才可以進 Scheduled SQL Reports，有 `scheduled_sql_reports.write` 才可以維護報表
- 但真正可選哪些資料源，還要看 direct / group DB Scope 綜合結果

審批還需要搭配 Workflow Rules。`tickets.review`、`sql_editor.export_review`、`sql_editor.sensitive_review` 只代表使用者具備審批資格；是否會收到某類 workflow 的審批任務，還要看 Workflow Rules 是否把此使用者所屬 auth group 列為候選審批人。

## 相關文件

- [權限模型](../explanation/permission-model.md)
- [後端 API 與權限對照](backend-api-and-permissions.md)
- [DB Connections](db-connections.md)
- [Workflow Rules](workflow-rules.md)
- [How to 設定 Workflow Rules](../how-to/configure-workflow-rules.md)
