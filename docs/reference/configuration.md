# 設定與環境變數

本文件整理本機開發與容器啟動時會用到的主要設定來源、環境變數與限制。

## 設定來源

目前專案主要有三層設定來源：

1. `.env`
2. `docker-compose.yml`
3. AWS Secrets Manager（EKS/devops 環境）
4. 平台內的 `Settings` 頁面與 `platform_settings` 表

責任分工如下：

- `.env`：本機開發提供密鑰、密碼與 container runtime 參數
- `docker-compose.yml`：把 `.env` 的值映射進 container
- AWS Secrets Manager：EKS/devops 環境提供 `DB_DSN`、`MIGRATION_DSN`、`DBRE_ENCRYPTION_KEY`、`JWT_SECRET`
- `platform_settings`：平台運行中可調整的產品設定，例如 SQL Editor timeout 與 metadata scan

## `.env` 必填項

至少要提供：

| 變數 | 用途 | 備註 |
|---|---|---|
| `MYSQL_APP_PASSWORD` | Meta DB app user 密碼 | `make dev` 會注入 mysql 與 app |
| `MYSQL_ROOT_PASSWORD` | Meta DB root 密碼 | migration / 初始化用途 |
| `DBRE_ENCRYPTION_KEY` | 加密 DB 連線密碼、敏感設定 | 32-byte AES key，base64 編碼 |
| `JWT_SECRET` | JWT 簽章密鑰 | 任意高熵字串 |
| `MFA_ENFORCEMENT` | MFA 強制策略 | `disabled` 或 `required_for_admins` |

## 可選項

| 變數 | 用途 | 預設 |
|---|---|---|
| `PORT` | App 服務 port | `8080` |
| `APP_BASE_URL` | 前端站台 base URL，供通知內工單連結使用 | `http://localhost:5173` |
| `MIGRATION_DSN` | migration 專用 DSN | 若未指定，跟 app DSN 同邏輯 |
| `RUN_MIGRATIONS_ON_STARTUP` | 啟動 app server 前是否自動執行 migration | `true` |
| `AWS_SM_ENABLE` | 啟用 AWS Secrets Manager 讀取敏感設定 | `false` |
| `AWS_SM_REGION` | AWS Secrets Manager region | 無 |
| `AWS_SM_SECRET_ID` | AWS Secrets Manager secret id | 無 |
| `LARK_WEBHOOK_URL` | Lark webhook fallback | 僅在未配置 Settings 內的 Lark App 時使用 |
| `LARK_OAUTH_SCOPES` | Lark OAuth 授權 URL 顯式要求的 scopes，逗號分隔 | `directory:employee.base.enterprise_email:read` |
| `LARK_OAUTH_REQUIRE_ENTERPRISE_EMAIL` | Lark OAuth 是否要求企業信箱 | `true` |
| `LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS` | 允許登入的企業信箱 domain，逗號分隔 | `edgex.exchange` |
| `REFRESH_COOKIE_SECURE` | 非 production 環境強制 refresh cookie Secure | production 永遠強制 Secure |
| `DB_CONNECTION_HOST_POLICY_ENFORCEMENT` | DB Connection host policy 模式 | `off`；可設 `warn` 或 `enforce` |
| `DB_CONNECTION_HOST_ALLOWLIST` | 允許的 DB/Redis host pattern，逗號分隔 | 無；例如 `*.rds.amazonaws.com,*.cache.amazonaws.com` |
| `DB_CONNECTION_CIDR_ALLOWLIST` | 允許的解析 IP CIDR，逗號分隔 | 無；例如 `10.183.0.0/16` |
| `DB_CONNECTION_CIDR_DENYLIST` | 禁止的解析 IP CIDR，逗號分隔 | 無；建議至少包含 metadata / loopback 網段 |
| `AWS_PROFILE` | DB metadata inventory 使用的 AWS profile | `default` |
| `AWS_SDK_LOAD_CONFIG` | 啟用 shared config | Compose 預設 `1` |

## AWS Secrets Manager payload

EKS/devops 環境建議設定：

```text
AWS_SM_ENABLE=true
AWS_SM_REGION=ap-northeast-1
AWS_SM_SECRET_ID=<secret-id>
```

Secret JSON 至少需要：

```json
{
  "DB_DSN": "maestro_app:<password>@tcp(<host>:3306)/maestro?parseTime=true&charset=utf8mb4&loc=UTC",
  "MIGRATION_DSN": "maestro_migration:<password>@tcp(<host>:3306)/maestro?parseTime=true&charset=utf8mb4&loc=UTC",
  "DBRE_ENCRYPTION_KEY": "BASE64_32_BYTE_KEY",
  "JWT_SECRET": "long-random-string"
}
```

`MIGRATION_DSN` 可省略；省略時會 fallback 到 `DB_DSN`。正式環境建議提供獨立 migration DSN。

DB pool 參數不是 secret，仍由 env / ConfigMap / values 管理。

## Meta DB 帳號權限

`DB_DSN` 與 `MIGRATION_DSN` 應使用不同帳號。

App runtime 帳號只負責服務日常讀寫 Meta DB：

```sql
CREATE USER 'maestro_app'@'%' IDENTIFIED BY '<app_password>';

GRANT SELECT, INSERT, UPDATE, DELETE
ON maestro.*
TO 'maestro_app'@'%';
```

若要啟用 MySQL DDL shadow validation，DBA/SRE 需在帳號建置時預先授權 app user 建立 shadow schema 內的暫存物件：

```sql
GRANT CREATE, ALTER, DROP
ON `shadow\_%`.*
TO 'maestro_app'@'%';
```

Migration 帳號只負責 `maestro` schema migration，不負責管理其他帳號授權，也不需要 `WITH GRANT OPTION`：

```sql
CREATE USER 'maestro_migration'@'%' IDENTIFIED BY '<migration_password>';

GRANT SELECT, INSERT, UPDATE, DELETE,
      CREATE, ALTER, DROP, INDEX, REFERENCES
ON maestro.*
TO 'maestro_migration'@'%';
```

## Migration 啟動策略

`RUN_MIGRATIONS_ON_STARTUP=true` 時，app server 啟動前會先用 `MIGRATION_DSN` 執行 `backend/migrations`。這是預設值，適合本機開發與單副本測試環境。

`RUN_MIGRATIONS_ON_STARTUP=false` 時，Deployment Pod 啟動不會自動跑 migration。多副本或正式環境建議使用這個設定，並改由 Kubernetes Job 執行：

```bash
/app/maestro -migrate-only
```

修改 `RUN_MIGRATIONS_ON_STARTUP` 後需要重啟 Pod 才會生效。

## DB Pool Profile 環境變數

外部資料源的連線池依 profile 拆分，所有參數都由 env 注入：

### Query

| 變數 | 預設 |
|---|---|
| `DB_POOL_QUERY_MAX_OPEN` | `10` |
| `DB_POOL_QUERY_MAX_IDLE` | `5` |
| `DB_POOL_QUERY_CONN_MAX_LIFETIME` | `5m` |
| `DB_POOL_QUERY_CONN_MAX_IDLE_TIME` | `2m` |

### Exec

| 變數 | 預設 |
|---|---|
| `DB_POOL_EXEC_MAX_OPEN` | `3` |
| `DB_POOL_EXEC_MAX_IDLE` | `1` |
| `DB_POOL_EXEC_CONN_MAX_LIFETIME` | `5m` |
| `DB_POOL_EXEC_CONN_MAX_IDLE_TIME` | `2m` |

### Metadata

| 變數 | 預設 |
|---|---|
| `DB_POOL_METADATA_MAX_OPEN` | `1` |
| `DB_POOL_METADATA_MAX_IDLE` | `1` |
| `DB_POOL_METADATA_CONN_MAX_LIFETIME` | `2m` |
| `DB_POOL_METADATA_CONN_MAX_IDLE_TIME` | `1m` |

### Scoped PostgreSQL Query

| 變數 | 預設 |
|---|---|
| `DB_POOL_SCOPED_PG_QUERY_MAX_OPEN` | `2` |
| `DB_POOL_SCOPED_PG_QUERY_MAX_IDLE` | `1` |
| `DB_POOL_SCOPED_PG_QUERY_CONN_MAX_LIFETIME` | `2m` |
| `DB_POOL_SCOPED_PG_QUERY_CONN_MAX_IDLE_TIME` | `1m` |

### Shadow Validation

| 變數 | 預設 |
|---|---|
| `DB_POOL_SHADOW_VALIDATION_MAX_OPEN` | `1` |
| `DB_POOL_SHADOW_VALIDATION_MAX_IDLE` | `1` |
| `DB_POOL_SHADOW_VALIDATION_CONN_MAX_LIFETIME` | `2m` |
| `DB_POOL_SHADOW_VALIDATION_CONN_MAX_IDLE_TIME` | `1m` |

## Compose 的實際行為

`make dev` 會使用專案根目錄的 `docker-compose.yml`。Compose 會：

- 讀取根目錄 `.env`
- 把需要的值展開到 `app` service 的 `environment`
- 將 `${HOME}/.aws` 掛進 container，供 inventory scan 使用
- 將所有 `DB_POOL_*` profile 參數映射進 app container

所以只有 `.env` 而不寫入 `docker-compose.yml` 並不等於 app 一定吃得到。前提是該變數必須先被 Compose 映射到 container 環境中。

## App 層固定 timeout

目前 server process 還有 HTTP 層 timeout：

| 項目 | 值 |
|---|---|
| `requestTimeout` | `45s` |
| `writeTimeout` | `45s` |

這是 API 層的 timeout，不等同於 SQL Editor 查詢 timeout，也不直接等同 ticket execute timeout。

### 長請求 timeout 特例

`GET /api/events/stream`、`GET /api/exports/{id}/download` 與 legacy `GET /api/exports/download/{token}` 行為與一般短請求 REST API 不同：

- route middleware 不套用一般 `requestTimeout = 45s`
- 主 server 仍保留 `writeTimeout = 45s`
- handler 會在單一 request 內清除 write deadline，避免長連線或大型 export 被錯誤中斷
- export download 的查詢熔斷由 `sql_export_*_timeout*` settings 控制

也就是說，目前不是把整個 server 的 timeout 全部拿掉，而是只讓 SSE stream 與 export download 走例外路徑；其他 REST API 仍保有原本的 timeout 保護。

## 時間與時區

平台的時間處理規則如下：

- DB schema 新欄位一律使用 `DATETIME(6)`
- App 寫入 DB 時顯式傳入 UTC 時間，不依賴 session timezone
- 前端讀取後，預設依 browser timezone 顯示

詳細規範與盤點請參考：

- [時間欄位與時區規範](time-handling.md)

## 平台 Settings 控制的項目

以下值不是從 `.env` 直接給 UI，而是寫在 Meta DB 的 `platform_settings`：

| Key | 預設值 | 用途 |
|---|---|---|
| `sql_editor_app_timeout_seconds` | `30` | SQL Editor `/api/query` app timeout |
| `sql_editor_mysql_max_execution_time_ms` | `25000` | MySQL session `max_execution_time` |
| `sql_editor_postgres_statement_timeout_ms` | `25000` | PostgreSQL session `statement_timeout` |
| `sql_export_app_timeout_seconds` | `30` | SQL Export download query app timeout |
| `sql_export_mysql_max_execution_time_ms` | `25000` | SQL Export MySQL session `max_execution_time` |
| `sql_export_postgres_statement_timeout_ms` | `25000` | SQL Export PostgreSQL session `statement_timeout` |
| `lark_app_id` | `""` | Lark App ID |
| `lark_app_secret` | `""` | Lark App Secret，加密保存且不會回填明文 |
| `lark_oauth_enabled` | `false` | 是否顯示並啟用 Lark OAuth 登入 |
| `lark_oauth_site` | `lark` | OAuth 站點，`lark` 或 `feishu` |
| `lark_oauth_redirect_url` | `""` | Lark app 後台允許的 OAuth callback URL |
| `require_non_sensitive_export_review` | `true` | 普通 SQL Export 是否需要審批 |
| `db_metadata_inventory_enabled` | `true` | 是否啟用 inventory scan |
| `db_metadata_inventory_regions` | `[]` | AWS 掃描 region 清單 |
| `db_metadata_inventory_engines` | `aurora-mysql, aurora-postgresql, redis` | engine 篩選 |
| `db_metadata_inventory_cron` | `0 9 * * *` | inventory scan cron |
| `db_metadata_object_enabled` | `true` | 是否啟用 object scan |
| `db_metadata_object_enabled_connection_ids` | `[]` | object scan 目標連線 |
| `db_metadata_object_cron` | `0 10 * * *` | object scan cron |
| `db_metadata_cron_timezone` | `Asia/Taipei` | metadata scan cron 時區 |

## AWS 本機驗證

若要在本機驗證 inventory scan：

1. 在主機上配置好 AWS CLI profile
2. `export AWS_PROFILE=your-profile`
3. 執行 `make dev`

Compose 會把 `${HOME}/.aws` 掛到 container，因此 app 可以沿用本機 profile。

## Lark 通知與 OAuth 設定建議

Lark App 憑證同時用於通知與 OAuth 登入。通知目前有兩種來源，優先順序如下：

1. `Settings` 頁的 `Lark App ID` + `Lark App Secret`
2. process env `LARK_WEBHOOK_URL` fallback

建議：

- 正式環境優先使用 `Settings` 頁的 App 模式，才能定向通知 submitter、reviewer、executor
- `LARK_WEBHOOK_URL` 保留作相容或過渡用途；它只能做 webhook 廣播，無法依使用者定向送達
- 定向通知會使用使用者資料上的 `lark_recipient`，值必須是可投遞的 Lark `open_id`

Lark OAuth 登入的開關、站點與 redirect URL 也在 Settings 頁管理：

- `lark_oauth_enabled`
- `lark_oauth_site`
- `lark_oauth_redirect_url`

OAuth identity 寫入平台 user 時，只接受 Lark 回傳的 `enterprise_email`。personal email 不會寫入 `users.email`，也不會拿來匹配既有使用者。預設部署要求企業信箱存在，且 domain 必須符合：

```dotenv
LARK_OAUTH_REQUIRE_ENTERPRISE_EMAIL=true
LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS=edgex.exchange
LARK_OAUTH_SCOPES=directory:employee.base.enterprise_email:read
```

`LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS` 支援逗號分隔。設定 `edgex.exchange` 時，`<user>@edgex.exchange` 與 `<user>@staff.edgex.exchange` 會通過，其他 domain 會被拒絕。

`LARK_OAUTH_SCOPES` 會寫入 Lark OAuth authorize URL。若 Lark app 後台已開通企業郵箱權限，但授權頁仍只顯示基本身份權限，應確認這個 env 是否包含 `directory:employee.base.enterprise_email:read`。

OAuth 登入只使用 `GET /open-apis/authen/v1/user_info` 回傳的 `enterprise_email` 匹配平台 user。`user_info.email` 可能是 personal email，不會用來匹配 `users.email`。若 Lark app 未開通或尚未發布可回傳 `enterprise_email` 的欄位權限，即使登入者完成 OAuth 授權，平台仍會因企業郵箱缺失而拒絕登入。

這兩個 enterprise email policy 是 deploy env，不是 Settings。原因是它們屬於部署安全邊界，不能讓有 Settings 權限的使用者在平台內自行放寬登入 domain。

## DB Connections 讀寫 endpoint

DB Connections 已支援雙 endpoint：

- readonly：SQL Editor、metadata、export 等 read path 使用
- readwrite：ticket execute 使用

若只配置單一 host / port，系統會把 readwrite 預設回退到 readonly。

## DB Connection Host Policy

DB Connection host policy 用來限制平台可連線的 DB / Redis endpoint，避免有 DB connection 管理權限的使用者把後端 Pod 當成內網探測或 SSRF 工具。

範例：

```dotenv
DB_CONNECTION_HOST_POLICY_ENFORCEMENT=warn
DB_CONNECTION_HOST_ALLOWLIST=*.rds.amazonaws.com,*.cache.amazonaws.com,*.edgex.internal
DB_CONNECTION_CIDR_ALLOWLIST=10.183.0.0/16,10.222.38.0/24
DB_CONNECTION_CIDR_DENYLIST=127.0.0.0/8,169.254.0.0/16,::1/128

LARK_OAUTH_REQUIRE_ENTERPRISE_EMAIL=true
LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS=edgex.exchange
LARK_OAUTH_SCOPES=directory:employee.base.enterprise_email:read
```

`DB_CONNECTION_HOST_POLICY_ENFORCEMENT` 支援：

- `off`：不檢查；未設定時的預設值
- `warn`：記錄 policy violation，但允許建立、更新與連線
- `enforce`：命中 violation 時拒絕建立、更新或連線

檢查範圍包含：

- 新增 / 修改 DB Connection 時的 readonly / readwrite endpoint
- SQL Editor、metadata、export、scheduled report、ticket execute、metadata sync 等 runtime 連線前的 resolved endpoint
- MySQL、PostgreSQL、Redis connection

這是第一階段保護：程式會在連線前檢查 host 與 DNS 解析結果。它尚未接管 MySQL / PostgreSQL / Redis driver 的 custom dialer，因此不是最終 socket 層保證。若安全要求提高，後續可再加第二階段 custom dialer。

## 範例

```dotenv
MYSQL_APP_PASSWORD=changeme_app
MYSQL_ROOT_PASSWORD=changeme_root
DBRE_ENCRYPTION_KEY=BASE64_32_BYTE_KEY
JWT_SECRET=long-random-string
MFA_ENFORCEMENT=disabled
AWS_PROFILE=default

DB_POOL_QUERY_MAX_OPEN=10
DB_POOL_QUERY_MAX_IDLE=5
DB_POOL_EXEC_MAX_OPEN=3
DB_POOL_METADATA_MAX_OPEN=1
DB_POOL_SCOPED_PG_QUERY_MAX_OPEN=2
DB_POOL_SHADOW_VALIDATION_MAX_OPEN=1

DB_CONNECTION_HOST_POLICY_ENFORCEMENT=warn
DB_CONNECTION_HOST_ALLOWLIST=*.rds.amazonaws.com,*.cache.amazonaws.com,*.edgex.internal
DB_CONNECTION_CIDR_ALLOWLIST=10.183.0.0/16,10.222.38.0/24
DB_CONNECTION_CIDR_DENYLIST=127.0.0.0/8,169.254.0.0/16,::1/128
```

## 相關文件

- [平台 Settings](settings.md)
- [登入安全與 Session](auth-and-sessions.md)
- [AWS EKS 部署流程](../how-to/deploy-to-aws-eks.md)
- [本機開發教學](../tutorials/getting-started-local-dev.md)
- [架構總覽](../explanation/architecture-overview.md)
