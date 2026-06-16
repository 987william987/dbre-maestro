# 平台 Settings

平台 Settings 頁管理的是運行中產品設定，不是 container 啟動參數。

## 頁面入口

- Route：`/settings`
- 可見權限：`settings.read` 或 `settings.write`
- 寫入權限：`settings.write`

## 功能分區

目前 Settings 頁分四塊：

- Lark Notifications
- SQL Editor Timeout
- Inventory Scan
- Object Scan

## Lark Notifications

這一區用來配置工單相關的 Lark 通知，不是站內信設定。

| 欄位 | 對應 key | 用途 |
|---|---|---|
| Lark App ID | `lark_app_id` | Lark app 憑證 ID |
| Lark App Secret | `lark_app_secret` | Lark app 憑證 secret，寫入時會加密保存 |

重點：

- 平台目前優先使用 `users.lark_recipient` 作為 Lark 收件人識別；若空值，才 fallback 到 `users.email`
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

## Inventory Scan

| 欄位 | 對應 key | 預設值 |
|---|---|---|
| Enable inventory scan | `db_metadata_inventory_enabled` | `true` |
| Regions | `db_metadata_inventory_regions` | `[]` |
| Engines | `db_metadata_inventory_engines` | `["aurora-mysql","aurora-postgresql","redis"]` |
| Sync interval (minutes) | `db_metadata_inventory_sync_interval_minutes` | `5` |

## Object Scan

| 欄位 | 對應 key | 預設值 |
|---|---|---|
| Enable object scan | `db_metadata_object_enabled` | `true` |
| Included DB Connections | `db_metadata_object_enabled_connection_ids` | `[]` |
| Sync interval (minutes) | `db_metadata_object_sync_interval_minutes` | `60` |

Object scan 只會對被勾選的 DB connections 生效。

## API / Interface

### `GET /api/settings`

回傳完整 `PlatformSettings`。

### `GET /api/settings/db-connections`

回傳可供 Object Scan 選取的 DB connection 清單。

### `PATCH /api/settings`

用於寫回 Lark、SQL Editor timeout 與 metadata scan 設定。

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
