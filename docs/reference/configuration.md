# 設定與環境變數

本文件整理本機開發與容器啟動時會用到的主要設定來源、環境變數與限制。

## 設定來源

目前專案主要有三層設定來源：

1. `.env`
2. `docker-compose.yml`
3. 平台內的 `Settings` 頁面與 `platform_settings` 表

責任分工如下：

- `.env`：本機或部署時提供密鑰、密碼與 container runtime 參數
- `docker-compose.yml`：把 `.env` 的值映射進 container
- `platform_settings`：平台運行中可調整的產品設定，例如 SQL Editor timeout 與 metadata scan

## `.env` 必填項

至少要提供：

| 變數 | 用途 | 備註 |
|---|---|---|
| `MYSQL_APP_PASSWORD` | Meta DB app user 密碼 | `make dev` 會注入 mysql 與 app |
| `MYSQL_ROOT_PASSWORD` | Meta DB root 密碼 | migration / 初始化用途 |
| `DBRE_ENCRYPTION_KEY` | 加密 DB 連線密碼、敏感設定 | 32-byte AES key，base64 編碼 |
| `JWT_SECRET` | JWT 簽章密鑰 | 任意高熵字串 |

## 可選項

| 變數 | 用途 | 預設 |
|---|---|---|
| `PORT` | App 服務 port | `8080` |
| `APP_BASE_URL` | 前端站台 base URL，供通知內工單連結使用 | `http://localhost:5173` |
| `MIGRATION_DSN` | migration 專用 DSN | 若未指定，跟 app DSN 同邏輯 |
| `LARK_WEBHOOK_URL` | Lark webhook fallback | 僅在未配置 Settings 內的 Lark App 時使用 |
| `AWS_PROFILE` | DB metadata inventory 使用的 AWS profile | `default` |
| `AWS_SDK_LOAD_CONFIG` | 啟用 shared config | Compose 預設 `1` |

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

## Compose 的實際行為

`make dev` 會使用專案根目錄的 `docker-compose.yml`。Compose 會：

- 讀取根目錄 `.env`
- 把需要的值展開到 `app` service 的 `environment`
- 將 `${HOME}/.aws` 掛進 container，供 inventory scan 使用

所以只有 `.env` 而不寫入 `docker-compose.yml` 並不等於 app 一定吃得到。前提是該變數必須先被 Compose 映射到 container 環境中。

## App 層固定 timeout

目前 server process 還有 HTTP 層 timeout：

| 項目 | 值 |
|---|---|
| `requestTimeout` | `45s` |
| `writeTimeout` | `45s` |

這是 API 層的 timeout，不等同於 SQL Editor 查詢 timeout，也不直接等同 ticket execute timeout。

## 平台 Settings 控制的項目

以下值不是從 `.env` 直接給 UI，而是寫在 Meta DB 的 `platform_settings`：

| Key | 預設值 | 用途 |
|---|---|---|
| `sql_editor_app_timeout_seconds` | `30` | SQL Editor `/api/query` app timeout |
| `sql_editor_mysql_max_execution_time_ms` | `25000` | MySQL session `max_execution_time` |
| `sql_editor_postgres_statement_timeout_ms` | `25000` | PostgreSQL session `statement_timeout` |
| `lark_app_id` | `""` | Lark App ID |
| `lark_app_secret` | `""` | Lark App Secret，加密保存且不會回填明文 |
| `db_metadata_inventory_enabled` | `true` | 是否啟用 inventory scan |
| `db_metadata_inventory_regions` | `[]` | AWS 掃描 region 清單 |
| `db_metadata_inventory_engines` | `aurora-mysql, aurora-postgresql, redis` | engine 篩選 |
| `db_metadata_inventory_sync_interval_minutes` | `5` | inventory scan 間隔 |
| `db_metadata_object_enabled` | `true` | 是否啟用 object scan |
| `db_metadata_object_enabled_connection_ids` | `[]` | object scan 目標連線 |
| `db_metadata_object_sync_interval_minutes` | `60` | object scan 間隔 |

## AWS 本機驗證

若要在本機驗證 inventory scan：

1. 在主機上配置好 AWS CLI profile
2. `export AWS_PROFILE=your-profile`
3. 執行 `make dev`

Compose 會把 `${HOME}/.aws` 掛到 container，因此 app 可以沿用本機 profile。

## Lark 通知設定建議

Lark 通知目前有兩種來源，優先順序如下：

1. `Settings` 頁的 `Lark App ID` + `Lark App Secret`
2. process env `LARK_WEBHOOK_URL` fallback

建議：

- 正式環境優先使用 `Settings` 頁的 App 模式，才能定向通知 submitter、reviewer、executor
- `LARK_WEBHOOK_URL` 保留作相容或過渡用途；它只能做 webhook 廣播，無法依使用者定向送達

## 範例

```dotenv
MYSQL_APP_PASSWORD=changeme_app
MYSQL_ROOT_PASSWORD=changeme_root
DBRE_ENCRYPTION_KEY=BASE64_32_BYTE_KEY
JWT_SECRET=long-random-string
AWS_PROFILE=default

DB_POOL_QUERY_MAX_OPEN=10
DB_POOL_QUERY_MAX_IDLE=5
DB_POOL_EXEC_MAX_OPEN=3
DB_POOL_METADATA_MAX_OPEN=1
DB_POOL_SCOPED_PG_QUERY_MAX_OPEN=2
```

## 相關文件

- [平台 Settings](settings.md)
- [本機開發教學](../tutorials/getting-started-local-dev.md)
- [架構總覽](../explanation/architecture-overview.md)
