# Workflow Rules

Workflow Rules 決定工單建立後要路由給哪些審批人與執行人。它是 workflow routing 設定，不是 permission 授權本身。

Permission 仍然是資格門檻：

- 有 review permission，才具備審批資格
- 有 execute permission，才具備執行資格
- 被 Workflow Rule 指定，才會成為該 workflow 的候選人

因此有效 reviewer / executor 需要同時滿足「有 permission」與「被 rule 指定」。

## 頁面入口

Workflow Rules 位於 Settings 頁：

- Route：`/settings`
- 讀取：`settings.read`
- 修改：`settings.write`

## 初始角色

系統內建兩個較新的審批角色：

| Auth Group | 用途 |
|---|---|
| `data_owner` | 一般 DDL / DML / Redis / Query Access 工單審批 |
| `security` | SQL Export 與 Sensitive Query Access 審批 |

`reviewer` 是舊角色，已進入 deprecate 階段。新規則不應再使用 `reviewer`，應改用 `data_owner` 或 `security`。

## Rule 欄位

| 欄位 | 說明 |
|---|---|
| `rule_name` | 管理者可讀的規則名稱 |
| `ticket_type` | 對應工單類型 |
| `db_connection_id` | 目標 DB connection；空值代表 All connections |
| `export_sensitivity` | 只用於 `sql_export`，可為 `normal` 或 `sensitive` |
| `approval_enabled` | 是否需要人工審批 |
| `approval_auth_groups` | 候選審批 auth groups |
| `executor_auth_groups` | 候選執行 auth groups |
| `priority` | 規則優先序，數字越小越優先 |
| `enabled` | 是否啟用此 rule |

目前 rule matching 以 `db_connection_id` 為核心。若同一工單同時符合多條 rule，系統以 priority 較小者優先。

## 預設規則

初始 migration 會建立全域規則：

| Workflow | Approval | Executor |
|---|---|---|
| DDL | `data_owner` | `dba` |
| DML | `data_owner` | `dba` |
| Redis Command | `data_owner` | `dba` |
| Query Access | `data_owner` | 無 |
| Normal SQL Export | `security` | 無 |
| Sensitive SQL Export | `security` | 無 |
| Sensitive Query Access | `security` | 無 |

普通 SQL Export 是否需要審批，由 Normal SQL Export rule 的 `approval_enabled` 決定。敏感 SQL Export 應維持需要審批。

## Effective Preview

Settings 頁會提供 effective preview 與 conflict preview。

Effective preview 會把 rule 中的 auth groups 解析成實際 user，並排除：

- inactive user
- 缺少對應 review / execute permission 的 user
- 不存在的 user 或 auth group

Conflict preview 用於提示相同 ticket type、connection scope 與 sensitivity 下可能互相覆蓋的 rule。

## Workflow Snapshot

工單建立或重新解析 workflow 時，後端會把解析結果寫入 `ticket_workflow_snapshots`。

Snapshot 是歷史事實，用於：

- Ticket detail 顯示當時解析結果
- Dashboard 統計
- Audit 分析
- 避免目前 rule 變更後影響歷史工單解讀

Dashboard 不應用目前的 `workflow_rules` 回推歷史工單 reviewer / executor，而應使用 snapshot。

## needs_admin_attention

如果 workflow resolution 找不到有效 rule、有效審批人或有效執行人，工單可進入 `needs_admin_attention`。

此時：

- submitter 仍可看到自己的工單與狀態
- admin 可以修正 Workflow Rules 後重新解析
- needs admin attention 會通知 admin users 與 Admin group

## API

| API | 權限 | 用途 |
|---|---|---|
| `GET /api/settings/workflow-rules` | `settings.read` | 讀取 rules |
| `PUT /api/settings/workflow-rules` | `settings.write` | 整批替換 rules |
| `POST /api/settings/workflow-rules/preview` | `settings.read` | 預覽單一 rule |
| `POST /api/settings/workflow-rules/effective-preview` | `settings.read` | 預覽 rule set 與 conflicts |
| `POST /api/settings/workflow-rules/simulate` | `settings.read` | 模擬指定 ticket 條件會命中哪條 rule |

## 相關文件

- [How to 設定 Workflow Rules](../how-to/configure-workflow-rules.md)
- [Tickets](tickets.md)
- [Users / RBAC](users-and-rbac.md)
- [Workflow Dashboard Data Dictionary](workflow-dashboard-data-dictionary.md)
