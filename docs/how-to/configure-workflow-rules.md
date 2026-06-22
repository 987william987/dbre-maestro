# How to 設定 Workflow Rules

本文說明如何在 Settings 頁配置工單審批與執行路由，讓不同 DB connection、不同工單類型可以交給不同角色處理。

## 前置條件

你需要具備：

- `settings.read`
- `settings.write`
- `users.read`

被配置為 reviewer / executor 的 auth group 成員，還需要具備對應操作權限。

| Workflow | Reviewer 權限 | Executor 權限 |
|---|---|---|
| DDL / DML / Redis | `tickets.review` | `tickets.execute` |
| Query Access | `tickets.review` | 無 |
| Normal SQL Export | `sql_editor.export_review` | 無 |
| Sensitive SQL Export | `sql_editor.export_review` | 無 |
| Sensitive Query Access | `sql_editor.sensitive_review` | 無 |

## 1. 先整理角色

建議角色分工：

- `data_owner`：開發 leader 或資料 owner，審一般 DDL / DML / Redis / Query Access
- `security`：安全或資料治理團隊，審 export 與 sensitive access
- `dba`：負責 DDL / DML / Redis 最後執行

DBA 若只負責執行，不應放進一般工單的 approval auth groups。

## 2. 開啟 Workflow Rules

到 `/settings`，找到 Workflow Rules 區塊。

每條 rule 至少要確認：

- Rule Name
- Ticket Type
- DB Connection，或 All connections
- Export Sensitivity，僅 `sql_export` 需要
- Approval Enabled
- Approval Auth Groups
- Executor Auth Groups
- Priority
- Enabled

## 3. 設計 DB Connection Scope

若所有 DB 都走同一組人，可以使用 All connections。

若不同 DB 需要不同 reviewer / executor，建立更精確的 DB connection rule，並給較小 priority。例如：

| Rule | DB Connection | Priority |
|---|---|---|
| Payments DDL | payments-prod | 10 |
| Global DDL | All connections | 100 |

這樣 payments-prod 會先命中 Payments DDL，其餘 DB 仍會落到 Global DDL。

## 4. 檢查 Effective Preview

儲存前先看 effective preview。若某個 group 裡的人沒有出現在有效名單，通常是：

- user 已停用
- 缺少對應 review / execute permission
- auth group 設錯

不要只看 rule 是否填了 auth group；要看 effective users 是否正確。

## 5. 檢查 Conflict Preview

Conflict preview 會提示多條 rule 可能命中同一 workflow 條件。若衝突是刻意設計，確認 priority 是否符合預期；若不是刻意設計，應調整 DB connection scope 或停用多餘 rule。

## 6. 儲存並驗證

儲存後用測試工單驗證：

- submitter 送出工單後，只有有效 reviewer 收到通知
- reviewer 可以在 Tickets 列表看到待審工單
- DBA 若只配置為 executor，不會在 pending review 階段收到審批任務
- DDL / DML / Redis 審批通過後，executor 才收到執行任務

## 常見問題

### Admin 可以審批所有工單嗎？

Admin 若具備對應 permission，後端 action API 可以允許其執行管理操作；但是否收到審批通知，仍取決於 Workflow Rules。通知路由不應使用 permission-wide fallback。

### 工單進入 needs_admin_attention 怎麼辦？

代表 workflow resolution 找不到可用路由或有效人員。Admin 需要修正 Workflow Rules，再從工單詳情重新解析 workflow。

### 舊 Reviewer group 還能用嗎？

`reviewer` 已 deprecate。短期可能仍存在於資料中，但新 rule 應改用 `data_owner` 或 `security`。

## 相關文件

- [Workflow Rules](../reference/workflow-rules.md)
- [Tickets](../reference/tickets.md)
- [Users / RBAC](../reference/users-and-rbac.md)
