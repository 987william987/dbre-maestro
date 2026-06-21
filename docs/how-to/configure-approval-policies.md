# How to 設定 Approval Policy

本文說明如何在 Settings 頁配置工單審批策略，讓不同 workflow 可以指派不同審批人，同時保留既有 permission 安全邊界。

## 前置條件

你需要具備：

- `settings.read`：進入 Settings 頁
- `settings.write`：儲存 Settings
- `users.read`：確認 user / auth group 的權限配置

被配置為審批人的 user 還需要具備對應 review permission。

| Workflow | 必要審批權限 |
|---|---|
| DDL / DML / Redis / Query Access | `tickets.review` |
| Normal SQL Export / Sensitive SQL Export | `sql_editor.export_review` |
| Sensitive Query Access | `sql_editor.sensitive_review` |

## 1. 先確認審批人權限

到 `Users / Auth Groups` 確認審批人或審批群組已具備必要 review permission。

例如 DBA group 只負責執行 DDL / DML / Redis，不負責開發工單審批時，可以保留：

- `tickets.execute`

不要把 DBA group 加進 DDL / DML / Redis 的 Approval Policy reviewer；這樣 DBA 就不會在工單等待審批時收到審批任務。

若開發 leader 負責一般工單審批，應讓開發 leader 或 leader group 同時滿足：

- 具備 `tickets.review`
- 被 DDL / DML / Redis / Query Access policy 指定

## 2. 進入 Settings

開啟 `/settings`，找到 Approval Policy 區塊。

每個 workflow 都可以設定：

- 是否啟用
- reviewer users
- reviewer auth groups

建議優先用 auth group 配置穩定角色，例如 `dev-leads`、`dba`、`security-reviewers`。只有臨時或少數例外才直接指定 user。

## 3. 檢查 Effective Reviewer Preview

Settings 頁會顯示每個 workflow 的 effective reviewers。

有效審批人的計算規則是：

1. 從 policy 指定的 user 與 auth group 成員收集候選人
2. 排除 inactive user
3. 排除缺少該 workflow review permission 的候選人

如果某個候選人被排除，通常是以下原因之一：

- user 已停用
- auth group 成員沒有必要 review permission
- policy 指定了 user ID，但該 user 已不存在

## 4. 儲存設定

按下 Save 後，後端會再次驗證所有啟用 workflow。

若任一啟用 workflow 沒有有效審批人，系統會拒絕儲存並回傳 `422`。這代表設定本身不可用，需要回到 Approval Policy 或 Users / Auth Groups 修正。

建立 ticket 不會因為 policy 暫時無有效審批人而被擋。這是刻意設計：設定錯誤應由管理者在 Settings 修正，不應讓申請人在建單時承擔治理配置問題。

## 5. 驗證通知與可見性

完成後可以用測試工單驗證：

- 提交 DDL / DML / Redis / Query Access 後，只有具備 `tickets.review` 且被 policy 指定的 reviewer 會收到審批任務
- 提交普通或敏感 SQL Export 後，只有具備 `sql_editor.export_review` 且被 policy 指定的 reviewer 會收到審批任務
- 提交 Sensitive Query Access 後，只有具備 `sql_editor.sensitive_review` 且被 policy 指定的 reviewer 會收到審批任務
- 只被配置為 executor 的 DBA，應在工單完成審批進入待執行階段後才看到可執行任務

## 普通導出是否需要審批

Settings 另有 `Require approval for non-sensitive exports` 開關。

- 開啟：普通 SQL Export 與敏感 SQL Export 都走審批流程
- 關閉：普通 SQL Export 不走人工審批，但仍建立 export ticket 作為稽核紀錄

敏感 SQL Export 永遠需要審批，不受此開關影響。

## 常見問題

### 儲存時提示沒有有效審批人

先看 effective reviewer preview。若候選人存在但沒有出現在 effective reviewers，通常代表缺少必要 review permission。

修正方式：

- 給 user 或 auth group 補上對應 review permission
- 或把 policy 改成指向已具備該 permission 的 user / auth group

### DBA 有 `tickets.review` 但不該審開發工單

不要把 DBA user / group 加進 DDL / DML / Redis / Query Access 的 Approval Policy reviewer。保留 `tickets.execute`，讓 DBA 只在 execution 階段處理工單。

### reviewer 看不到 Tickets 頁

確認 reviewer 具備 `tickets.read`。`tickets.review` 是審批資格，不是 Tickets 頁入口。

## 相關文件

- [權限模型](../explanation/permission-model.md)
- [平台 Settings](../reference/settings.md)
- [Tickets](../reference/tickets.md)
- [Users / RBAC](../reference/users-and-rbac.md)
