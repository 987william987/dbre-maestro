# 平台 Settings

平台 Settings 頁管理的是運行中產品設定，不是 container 啟動參數。

## 頁面入口

- Route：`/settings`
- 可見權限：`settings.read` 或 `settings.write`
- 寫入權限：`settings.write`

## 功能分區

目前 Settings 頁分成以下區塊：

- Lark Notifications
- SQL Editor Timeout
- Export Approval
- Approval Policy
- Inventory Scan
- Object Scan

這些設定是「運行中行為」設定，不是 `.env` 啟動參數編輯器。

## Lark Notifications

這一區用來配置工單相關的 Lark 通知，不是站內信設定。

| 欄位 | 對應 key | 用途 |
|---|---|---|
| Lark App ID | `lark_app_id` | Lark app 憑證 ID |
| Lark App Secret | `lark_app_secret` | Lark app 憑證 secret，寫入時會加密保存 |

重點：

- 平台目前使用 `users.lark_recipient` 作為 Lark 收件人識別，值必須是可投遞的 `open_id`
- `App Secret` 不會明文回填到前端；若已配置，畫面只會顯示已配置狀態
- 第一次配置時，`Lark App ID` 與 `Lark App Secret` 必須一起提供
- 後續若只修改其他 Settings，可將 `App Secret` 留白，系統會保留既有 secret
- 若未配置 App 模式，但 process env 有 `LARK_WEBHOOK_URL`，後端仍會 fallback 使用 webhook 模式

目前會收到 Lark 通知的對象以工單流程為主，包含 submitter、reviewer、executor 等角色的狀態更新。

## SQL Editor Timeout

這三個值只作用於 SQL Editor `/api/query`：

| 欄位 | 對應 key | 預設值 | 用途 |
|---|---|---|---|
| App timeout (seconds) | `sql_editor_app_timeout_seconds` | `30` | API request 生存時間上限 |
| MySQL max_execution_time (ms) | `sql_editor_mysql_max_execution_time_ms` | `25000` | MySQL session timeout |
| PostgreSQL statement_timeout (ms) | `sql_editor_postgres_statement_timeout_ms` | `25000` | PostgreSQL session timeout |

重點：

- 這些值不影響 Ticket execute
- 這些值不直接控制 export download
- 它們是 SQL Editor 查詢保護機制的一部分

## Export Approval

| 欄位 | 對應 key | 預設值 | 用途 |
|---|---|---|---|
| Require approval for non-sensitive exports | `require_non_sensitive_export_review` | `true` | 控制普通 SQL Export 是否需要審批 |

敏感 SQL Export 永遠需要審批，不受此開關影響。

當 `require_non_sensitive_export_review = false` 時，普通導出不進人工審批，但仍會建立 `sql_export` ticket 作為稽核紀錄。Dashboard 或 audit 報表可以用 ticket 的 `contains_sensitive` 區分普通導出與敏感導出。

## Approval Policy

Approval Policy 決定不同 workflow 送出後路由給哪些審批人。它是審批路由設定，不是 permission 授權本身。

| Workflow | 必要審批權限 |
|---|---|
| `ddl` | `tickets.review` |
| `dml` | `tickets.review` |
| `redis_command` | `tickets.review` |
| `query_access` | `tickets.review` |
| `sql_export_normal` | `sql_editor.export_review` |
| `sql_export_sensitive` | `sql_editor.export_review` |
| `sensitive_query_access` | `sql_editor.sensitive_review` |

每個 policy 可以指定：

- `reviewer_user_ids`
- `reviewer_auth_groups`
- `enabled`

有效審批人會由候選 user 與 auth group 成員合併後計算，並排除 inactive user 與缺少必要審批權限的候選人。

Settings 頁會顯示 effective reviewer preview，讓管理者在儲存前看見每個 workflow 最終會通知誰。儲存時若任一啟用 workflow 沒有有效審批人，`PATCH /api/settings` 會回傳 `422`，管理者需要補齊 policy 或授權後才能儲存。

舊欄位仍保留在 API payload 中以維持相容性，但不應再作為新功能的主要配置來源：

- `sensitive_export_reviewer_user_ids`
- `sensitive_query_access_reviewer_user_ids`

## Inventory Scan

| 欄位 | 對應 key | 預設值 |
|---|---|---|
| Enable inventory scan | `db_metadata_inventory_enabled` | `true` |
| Regions | `db_metadata_inventory_regions` | `[]` |
| Engines | `db_metadata_inventory_engines` | `["aurora-mysql","aurora-postgresql","redis"]` |
| Cron | `db_metadata_inventory_cron` | `0 9 * * *` |

## Object Scan

| 欄位 | 對應 key | 預設值 |
|---|---|---|
| Enable object scan | `db_metadata_object_enabled` | `true` |
| Included DB Connections | `db_metadata_object_enabled_connection_ids` | `[]` |
| Cron | `db_metadata_object_cron` | `0 10 * * *` |

Object scan 只會對被勾選的 DB connections 生效。

Inventory Scan 與 Object Scan 使用類 crontab 表達式，例如 `0 9 * * *`。兩者共用 `db_metadata_cron_timezone`，預設為 `Asia/Taipei`。

## API / Interface

### `GET /api/settings`

回傳完整 `PlatformSettings`。

### `GET /api/settings/db-connections`

回傳可供 Object Scan 選取的 DB connection 清單。

### `GET /api/settings/approval-resolution`

回傳每個 workflow 的審批人解析結果，供 Settings 頁展示 effective reviewer preview 與排除原因。

### `PATCH /api/settings`

用於寫回 Lark、SQL Editor timeout、export approval、approval policies 與 metadata scan 設定。

若 approval policies 中任一啟用 workflow 沒有有效審批人，回傳 `422`。

## 資料持久化

Settings 存在 Meta DB 的 `platform_settings` 表中，由 `SettingsRepo` 做讀寫。

其中 `lark_app_secret` 會先經過 AES-GCM 加密後再寫入 `platform_settings.value`。

缺省值若資料庫未設定，會由後端在讀取時補上預設值。

## 與 `.env` 的差異

不要把 Settings 與 `.env` 混淆：

- `.env`：給 container / process 啟動用
- Settings：給平台運行行為調整用

例如 DB pool size 現在在 `.env`；SQL Editor timeout 在 Settings。

## 相關文件

- [設定與環境變數](configuration.md)
- [SQL Editor](sql-editor.md)
- [DB Metadata](db-metadata.md)
- [How to 設定 Approval Policy](../how-to/configure-approval-policies.md)
