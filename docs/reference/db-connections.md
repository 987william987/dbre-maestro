# DB Connections

DB Connections 模組管理平台可使用的資料源。它不是單純保存一組 host / port，而是資料治理的基礎資源模型。

## 功能定位

- 定義可被 SQL Editor、Tickets、Metadata 使用的資料源
- 保存資料庫類型、預設 database、SSL mode
- 拆分 readonly / readwrite endpoint
- 管理 readonly / readwrite credential
- 作為 DB Scope 綁定單位

## 頁面入口

- Route：`/db-connections`
- Overview：`/db-connections/{id}/overview`，需要 `db_connections.overview`
- Databases：`/db-connections/{id}/databases`，需要 `db_connections.databases`
- Accounts：`/db-connections/{id}/accounts`，需要 `db_connections.accounts`
- 寫入權限：`db_connections.write`

既有 `db_connections.read` / `db_connections.write` 持有者在 migration 時會取得 Overview 與 Databases。Accounts 含資料庫原生帳號與授權資訊，只預設授予 admin、dba 與 protected users；之後可在 Users / Auth Groups 個別授權。三個子頁仍會檢查使用者的 DB Connection Scope。

## 支援類型

| DB Type | 用途 |
|---|---|
| `mysql` | SQL Editor、Tickets、Metadata |
| `postgres` / `postgresql` | SQL Editor、Tickets、Metadata |
| `redis` | SQL Editor、Redis ticket |

## 讀寫 endpoint 模型

每個 connection 可配置兩組 endpoint：

- readonly endpoint
- readwrite endpoint

用途分工：

- readonly：SQL Editor、metadata、匯出、敏感查詢分析
- readwrite：DDL / DML / Redis ticket execute

若未單獨配置 readwrite，系統會回退使用 readonly endpoint。

## Credential Role

目前 credential 以角色管理：

- `readonly`
- `readwrite`
- `rollback`

MySQL / PostgreSQL 通常都需要 readonly credential；readwrite credential 則提供 ticket execute 使用。

MySQL rollback generation 會使用 rollback credential 讀取 binlog 或產生 `my2sql` rollback SQL。若使用 `prior_backup` engine，備份表是在 ticket execution 的 readwrite connection 上建立，因此不依賴 rollback credential；但 `hybrid` fallback 到 `my2sql` 時仍需要 rollback credential。

Redis 也可使用同樣的 role 概念，但實際命令能力仍由目標實例 ACL 決定。

## API

### `GET /api/db-connections`

列出目前使用者可見的 DB connections。

- `db_connections.write` 可看到全部
- 只有 `db_connections.read` 時，後端仍會再依使用者有效 DB Scope 過濾

### `POST /api/db-connections`

建立新 connection。

核心欄位：

| 欄位 | 說明 |
|---|---|
| `name` | 顯示名稱 |
| `db_type` | `mysql` / `postgres` / `redis` |
| `readonly_host` / `readonly_port` | 讀取 endpoint |
| `readwrite_host` / `readwrite_port` | 寫入 endpoint，可省略後回退 readonly |
| `database_name` | 預設 database；PostgreSQL 未填時後端會補 `postgres` |
| `ssl_mode` | `prefer` / `disable` / `require` |
| `credentials[]` | 依 role 的帳密 |

### `PATCH /api/db-connections/{id}`

更新 connection。未提供欄位則保留原值。

更新後後端會：

- 失效既有 SQL / Redis pool cache
- 讓後續請求用新的 endpoint / credential 建立連線

### `POST /api/db-connections/{id}/test`

測試連線。

目前行為：

- 若未指定 `credential_role`，會測 `readonly` 與 `readwrite`
- 回傳逐角色結果
- 同時更新 `last_test_status`、`last_test_error`、`last_tested_at`

### `POST /api/db-connections/{id}/test-rollback`

測試 MySQL rollback capability。

目前行為：

- 只支援 MySQL connection
- 需要 Settings 已啟用 `mysql_rollback_enabled`
- `prior_backup` engine 只確認 parser 能力會在 ticket execution 時檢查
- `my2sql` / `hybrid` 會檢查 my2sql path、rollback credential、rollback connection、binlog 設定與目前 binlog position

### `GET /api/db-connections/{id}/bindings`

回傳資源綁定反查資訊：

- `direct_users`
- `auth_groups`
- `effective_users`

這是 Users 頁第三個 `Resources` 子頁與 DB Connections 詳情側邊資訊的資料來源。

### Connection 詳情 API

| API | 資料內容 |
|---|---|
| `GET /api/db-connections/{id}/overview` | connection 設定、endpoint、credential roles 與測試狀態 |
| `GET /api/db-connections/{id}/databases` | database、table 數量、data/index size、character set 與 collation |
| `GET /api/db-connections/{id}/accounts` | MySQL/PostgreSQL account、role、grant 與最近掃描狀態 |

Databases 與既有 Object Scan 共用掃描排程，但會另外保存 database 層級摘要，因此沒有 table 的空 database 仍會顯示。Accounts 使用獨立排程；啟用狀態、cron 與 connection scope 均在 Settings 設定。取消某個 connection 的掃描範圍時，該 connection 的 account/grant snapshot 會被清除。

掃描使用 readonly credential。Object Scan 需要讀取 `information_schema`（MySQL）或 PostgreSQL catalog 並連入各 database；Account Scan 需要讀取 MySQL `mysql.user`、grant metadata 並對帳號執行 `SHOW GRANTS`，或讀取 PostgreSQL `pg_roles`、role membership 與 grant metadata。MySQL Accounts 頁優先呈現 `SHOW GRANTS` 的原生 SQL；PostgreSQL 會略過 `pg_*` 內置 role，並呈現 login、inheritance、create role/database、replication、bypass RLS、有效期限及結構化 GRANT statement。若受管資料庫限制這些 catalog 權限，Accounts 會保留最後一次成功快照並顯示最近失敗狀態。快照不保存 password hash、authentication string 或 token。

## 前端展示重點

DB Connections 頁目前除了列表，還會展示：

- readonly endpoint
- readwrite endpoint
- test status
- resource bindings

因此這個頁面不只是連線 CRUD，也承擔資源治理視角。

## 與其他模組的關係

- SQL Editor：從這裡取可查詢的資料源，實際清單再受 DB Scope 過濾
- Tickets：建立與執行工單時，根據 ticket type 使用對應 endpoint
- DB Metadata：object scan 以被選中的 DB connections 為掃描目標
- Users / Auth Groups：以 connection 為 DB Scope 綁定單位

## 相關文件

- [Users / RBAC](users-and-rbac.md)
- [SQL Editor](sql-editor.md)
- [Tickets](tickets.md)
- [MySQL Rollback](mysql-rollback.md)
- [DB Metadata](db-metadata.md)
