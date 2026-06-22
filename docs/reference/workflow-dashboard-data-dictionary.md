# Workflow 與 Dashboard 統計資料字典

本文定義工單審批、執行、通知與 dashboard 統計應使用的穩定資料來源。

## 設計原則

- Dashboard 統計必須基於 ticket 建立或 workflow retry 當下保存的 snapshot。
- Dashboard 不應直接用目前的 `workflow_rules` 回推歷史工單的 reviewer / executor。
- `workflow_rules` 是當前配置；`ticket_workflow_snapshots` 是歷史事實。
- 實際審批人、實際執行人仍以 `tickets.reviewer_id`、`tickets.executor_id` 和 audit log 為準。

## 核心資料表

### `tickets`

| 欄位 | 用途 |
| --- | --- |
| `id` | 工單主鍵。 |
| `ticket_no` | 工單編號。 |
| `ticket_type` | 工單類型：`ddl`、`dml`、`redis_command`、`query_access`、`sql_export`、`sensitive_query_access`。 |
| `contains_sensitive` | SQL Export 是否包含敏感資料；`true` 表示敏感導出，`false` 表示普通導出。 |
| `db_connection_id` | workflow rule matching 的主要 scope。 |
| `database_name` | 使用者提交時指定的 database。 |
| `status` | 當前工單狀態。 |
| `submitter_id` | 申請人。 |
| `reviewer_id` | 實際完成審批的人。 |
| `executor_id` | 實際接手或執行的人。 |
| `created_at` | 工單建立時間，用於申請統計。 |
| `updated_at` | 工單最後更新時間。 |

### `ticket_workflow_snapshots`

| 欄位 | 用途 |
| --- | --- |
| `ticket_id` | 對應 `tickets.id`。 |
| `workflow_rule_id` | 當下匹配到的 workflow rule id。 |
| `workflow_rule_name` | 當下匹配到的 workflow rule name。 |
| `approval_enabled` | 當下是否需要審批。普通導出免審時為 `false`。 |
| `approval_user_ids` | 當下解析出的有效審批人。 |
| `executor_user_ids` | 當下解析出的有效執行人。 |
| `admin_user_ids` | resolution 失敗時應通知的 admin users。 |
| `error_code` | resolution 失敗原因，例如 `no_matching_rule`、`no_effective_approval_users`、`no_effective_executor_users`。 |
| `error_message` | resolution 失敗說明。 |
| `resolution_trace` | 完整 resolver 輸出，包含 missing groups、excluded users、approval/executor/admin recipients。 |
| `resolved_at` | 這次 workflow resolution 的時間。 |

### `audit_logs`

| 欄位 | 用途 |
| --- | --- |
| `action_type` | 事件類型，例如 `ticket_submit`、`ticket_approve`、`ticket_reject`、`ticket_execute`、`workflow_resolution_retry`、`notification_delivery`。 |
| `actor_id` | 實際操作人。 |
| `resource_type` | 目前 workflow 相關事件多為 `ticket` 或 `export`。 |
| `resource_id` | 對應 resource id。 |
| `details` | 事件細節；workflow 事件會包含 resolution trace 摘要，notification 事件會包含 intended/delivered recipients 與 Lark delivery 狀態。 |
| `created_at` | 事件發生時間。 |

### `notification_deliveries`

| 欄位 | 用途 |
| --- | --- |
| `notification_type` | 通知類型，例如 `ticket_pending_review`、`ticket_needs_admin_attention`、`export_approved`。 |
| `resource_type` | 通知關聯資源，通常是 `ticket` 或 `export`。 |
| `resource_id` | 關聯資源 id。 |
| `user_id` | 預期通知對象。 |
| `channel` | 通知渠道：`in_app` 或 `lark`。 |
| `status` | Delivery 狀態：`sent`、`failed`、`skipped`。 |
| `attempts` | 發送嘗試次數；Lark 未設定或沒有 Open ID 時通常為 0。 |
| `error_message` | 失敗或 skipped reason，例如 `lark_not_configured`、`no_lark_recipient_open_id`。 |
| `created_at` | delivery 紀錄建立時間。 |

`audit_logs` 用於追蹤一次通知事件的整體 routing 結果；`notification_deliveries` 用於查詢單一 user / channel 的 delivery 狀態。

## 常用統計口徑

### 敏感導出申請人統計

資料來源：

- `tickets.ticket_type = 'sql_export'`
- `tickets.contains_sensitive = true`
- group by `tickets.submitter_id`

審批人來源：

- 候選審批人：`ticket_workflow_snapshots.approval_user_ids`
- 實際審批人：`tickets.reviewer_id`

### 普通導出申請人統計

資料來源：

- `tickets.ticket_type = 'sql_export'`
- `tickets.contains_sensitive = false`
- group by `tickets.submitter_id`

審批狀態：

- `ticket_workflow_snapshots.approval_enabled = false` 表示普通導出免審。
- `ticket_workflow_snapshots.approval_enabled = true` 表示普通導出仍走審批。

### 一般工單審批統計

資料來源：

- `tickets.ticket_type IN ('ddl', 'dml', 'redis_command', 'query_access')`
- 實際審批人：`tickets.reviewer_id`
- 審批候選人：`ticket_workflow_snapshots.approval_user_ids`

### DBA 執行統計

資料來源：

- `tickets.ticket_type IN ('ddl', 'dml', 'redis_command')`
- 實際執行人：`tickets.executor_id`
- 候選執行人：`ticket_workflow_snapshots.executor_user_ids`

### Workflow 配置異常統計

資料來源：

- `tickets.status = 'needs_admin_attention'`
- `ticket_workflow_snapshots.error_code`
- `ticket_workflow_snapshots.error_message`

## 不建議的統計方式

- 不要用目前的 `workflow_rules` 推算歷史工單 reviewer / executor。
- 不要只用 permission 推算誰應該收到通知。
- 不要把候選審批人等同於實際審批人。
- 不要把 `sql_editor.export_review` 視為敏感導出 reviewer；是否為實際 reviewer 必須看 snapshot。
