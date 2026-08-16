# 平台 Settings

平台 Settings 頁管理的是運行中產品設定，不是 container 啟動參數。

## 頁面入口

- Route：`/settings`
- 可見權限：`settings.read` 或 `settings.write`
- 寫入權限：`settings.write`

## 功能分區

目前 Settings 頁分成以下區塊：

- Lark Notifications and OAuth
- SQL Editor Timeout
- MySQL Rollback
- Workflow Rules
- Inventory Scan
- Object Scan

這些設定是「運行中行為」設定，不是 `.env` 啟動參數編輯器。

## Lark Notifications and OAuth

這一區用來配置工單相關的 Lark 通知與 Lark OAuth 登入，不是站內信設定。

| 欄位 | 對應 key | 用途 |
|---|---|---|
| Lark App ID | `lark_app_id` | Lark app 憑證 ID |
| Lark App Secret | `lark_app_secret` | Lark app 憑證 secret，寫入時會加密保存 |
| Enable Lark OAuth Login | `lark_oauth_enabled` | 是否在登入頁顯示並啟用 Lark 登入 |
| Lark Site | `lark_oauth_site` | `lark` 或 `feishu` |
| OAuth Redirect URL | `lark_oauth_redirect_url` | Lark app 後台允許的 callback URL |

重點：

- 平台目前使用 `users.lark_recipient` 作為 Lark 收件人識別，值必須是可投遞的 `open_id`
- `App Secret` 不會明文回填到前端；若已配置，畫面只會顯示已配置狀態
- 第一次配置時，`Lark App ID` 與 `Lark App Secret` 必須一起提供
- 後續若只修改其他 Settings，可將 `App Secret` 留白，系統會保留既有 secret
- 若未配置 App 模式，但 process env 有 `LARK_WEBHOOK_URL`，後端仍會 fallback 使用 webhook 模式
- 啟用 OAuth 時，`OAuth Redirect URL` 必須和 Lark app 後台允許的 redirect URL 完全一致

目前會收到 Lark 通知的對象以工單流程為主，包含 submitter、reviewer、executor 等角色的狀態更新。

Lark OAuth 登入行為：

- 使用 Lark OAuth 取得 `open_id` / `union_id` / `enterprise_email`
- 若 `open_id` 或 `union_id` 已綁定既有 user，直接登入該 user
- 若尚未綁定，會用 `enterprise_email` 匹配既有 user，匹配成功後自動綁定 Lark identity
- 若找不到既有 user，系統會建立普通 user 並加入 developer auth group
- OAuth 建立的新 user 會停用密碼登入，不會產生可用密碼
- protected admin 不允許透過 Lark email 自動綁定，避免 bootstrap admin 被接管

enterprise email policy 不是 Settings，而是 deploy env：

```text
LARK_OAUTH_REQUIRE_ENTERPRISE_EMAIL=true
LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS=edgex.exchange
```

這代表平台只接受 Lark 回傳的企業信箱。personal email 不會寫進 `users.email`，也不會拿來建立或匹配平台 user。

## SQL Editor / Export Timeout

SQL Editor timeout 只作用於 SQL Editor `/api/query`：

| 欄位 | 對應 key | 預設值 | 用途 |
|---|---|---|---|
| App timeout (seconds) | `sql_editor_app_timeout_seconds` | `30` | API request 生存時間上限 |
| MySQL max_execution_time (ms) | `sql_editor_mysql_max_execution_time_ms` | `25000` | MySQL session timeout |
| PostgreSQL statement_timeout (ms) | `sql_editor_postgres_statement_timeout_ms` | `25000` | PostgreSQL session timeout |

SQL Export timeout 作用於 export download query：

| 欄位 | 對應 key | 預設值 | 用途 |
|---|---|---|---|
| App timeout (seconds) | `sql_export_app_timeout_seconds` | `30` | Export query 生存時間上限 |
| MySQL max_execution_time (ms) | `sql_export_mysql_max_execution_time_ms` | `25000` | MySQL session timeout |
| PostgreSQL statement_timeout (ms) | `sql_export_postgres_statement_timeout_ms` | `25000` | PostgreSQL session timeout |

重點：

- 這些值不影響 Ticket execute
- SQL Editor 與 SQL Export 使用不同 timeout key
- Export download 仍使用 DB connection 的 readonly endpoint 與 readonly credential

## MySQL Rollback

MySQL Rollback 是 DML ticket 的 beta 輔助功能，用來在 statement 執行成功後產生可預覽、可重新送審的 rollback SQL。

| 欄位 | 對應 key | 預設值 | 用途 |
|---|---|---|---|
| Enable MySQL rollback generation | `mysql_rollback_enabled` | `false` | 是否啟用 MySQL DML rollback generation |
| Rollback Engine | `mysql_rollback_engine` | `hybrid` | `hybrid`、`prior_backup` 或 `my2sql` |
| my2sql path | `mysql_rollback_my2sql_path` | `my2sql` | `my2sql` binary 路徑或 PATH 中指令名 |
| Generation timeout (seconds) | `mysql_rollback_generation_timeout_seconds` | `30` | `my2sql` 產生 rollback SQL 的 timeout |
| Max rollback SQL bytes | `mysql_rollback_max_sql_bytes` | `5242880` | 可保存 rollback SQL 大小上限 |

重點：

- 目前只支援 MySQL DML ticket rollback
- 這個功能仍是 beta，rollback SQL 必須先預覽，再建立新的 DML ticket
- `hybrid` 會優先使用 prior backup parser，parser 不支援時 fallback 到 `my2sql`
- `prior_backup` 不依賴 binlog，但需要 ticket execution user 能建立 `maestro_rollback` database/table
- `my2sql` 需要 rollback credential、binlog ROW format 與 FULL row image
- rollback generation 失敗不會阻塞原本的 ticket execution

## Workflow Rules

Workflow Rules 決定不同 workflow 送出後路由給哪些審批人與執行人。它是審批 / 執行路由設定，不是 permission 授權本身。

| Workflow | 必要審批權限 |
|---|---|
| `ddl` | `tickets.review` |
| `dml` | `tickets.review` |
| `redis_command` | `tickets.review` |
| `query_access` | `tickets.review` |
| `sql_export_normal` | `sql_editor.export_review` |
| `sql_export_sensitive` | `sql_editor.export_review` |
| `sensitive_query_access` | `sql_editor.sensitive_review` |

每個 rule 可以指定：

- `db_connection_id`
- `approval_enabled`
- `approval_auth_groups`
- `executor_auth_groups`
- `priority`
- `enabled`

普通 SQL Export 是否需要審批，由 `sql_export_normal` 對應 rule 的 `approval_enabled` 控制。敏感 SQL Export 應維持需要審批。

有效審批人 / 執行人會由候選 auth group 成員合併後計算，並排除 inactive user 與缺少必要權限的候選人。

Settings 頁會顯示 effective preview 與 conflict preview，讓管理者在儲存前看見每個 workflow 最終會通知誰，以及哪些規則可能互相覆蓋。儲存時若啟用 rule 無法解析出有效人員，後端會拒絕儲存。

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

回傳舊 Approval Policy 的審批人解析結果。此 API 保留作相容用途，新功能應使用 Workflow Rules preview。

### `GET /api/settings/workflow-rules`

回傳 Workflow Rules。

### `PUT /api/settings/workflow-rules`

整批替換 Workflow Rules。

### `POST /api/settings/workflow-rules/effective-preview`

回傳 Workflow Rules 的 effective preview 與 conflict preview。

### `PATCH /api/settings`

用於寫回 Lark、SQL Editor timeout 與 metadata scan 等平台設定。

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
- [MySQL Rollback](mysql-rollback.md)
- [DB Metadata](db-metadata.md)
- [Workflow Rules](workflow-rules.md)
- [How to 設定 Workflow Rules](../how-to/configure-workflow-rules.md)
