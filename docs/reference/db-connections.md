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
- 可見權限：`db_connections.read` 或 `db_connections.write`
- 寫入權限：`db_connections.write`

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

MySQL / PostgreSQL 通常都需要 readonly credential；readwrite credential 則提供 ticket execute 使用。

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

### `GET /api/db-connections/{id}/bindings`

回傳資源綁定反查資訊：

- `direct_users`
- `auth_groups`
- `effective_users`

這是 Users 頁第三個 `Resources` 子頁與 DB Connections 詳情側邊資訊的資料來源。

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
- [DB Metadata](db-metadata.md)
