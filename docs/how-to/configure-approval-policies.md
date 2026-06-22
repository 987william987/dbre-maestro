# How to 設定 Approval Policy

> Deprecated：Approval Policy 是舊版審批路由設定名稱。新功能與日常維運請使用 [How to 設定 Workflow Rules](configure-workflow-rules.md)。

## 現行做法

請改用 Workflow Rules：

- 使用 `data_owner` 審批一般 DDL / DML / Redis / Query Access 工單
- 使用 `security` 審批 SQL Export 與 Sensitive Query Access
- 使用 `dba` 作為 DDL / DML / Redis executor
- 用 DB connection scope 與 priority 處理不同資料源的差異化路由

完整操作步驟請看：

- [How to 設定 Workflow Rules](configure-workflow-rules.md)
- [Workflow Rules](../reference/workflow-rules.md)

## 舊資料

舊 Approval Policy 相關欄位可能仍保留在 API payload 或資料表中，目的是相容既有資料與遷移流程。新功能不應再依賴這些欄位。

若看到舊 reviewer 設定或 `reviewer` auth group，應逐步遷移到：

- `data_owner`
- `security`

## 相關文件

- [Workflow Rules](../reference/workflow-rules.md)
- [平台 Settings](../reference/settings.md)
- [Users / RBAC](../reference/users-and-rbac.md)
