# 後端維護參考

本文件整理後端的主要 public surface、初始化順序與維護規則。它不是 API 清單；API 與 permission 對照請看 [後端 API 與權限對照](backend-api-and-permissions.md)。

## Runtime

| 項目 | 值 |
|---|---|
| 語言 | Go |
| Module | `github.com/dbre-maestro/maestro` |
| 入口 | `backend/cmd/server/main.go` |
| HTTP router | `github.com/go-chi/chi/v5` |
| Meta DB | MySQL, via `sqlx` |
| Migration | `golang-migrate/migrate/v4` |
| MySQL driver | `github.com/go-sql-driver/mysql` |
| PostgreSQL driver | `github.com/jackc/pgx/v5` |
| Redis client | `github.com/redis/go-redis/v9` |
| SQL parser | TiDB parser for MySQL, `pg_query_go` for PostgreSQL |

## 啟動順序

server 啟動時依序執行：

1. 讀取 env 設定。
2. 如果 `AWS_SM_ENABLE=true`，從 AWS Secrets Manager 載入 secret。
3. 建立 DB Connection host policy。
4. 視 `RUN_MIGRATIONS_ON_STARTUP` 或 `-migrate-only` 執行 migration。
5. 開啟 Meta DB。
6. 設定 DB pool profiles。
7. 建立 repository、handler、notification dispatcher、masking engine、event broker。
8. 啟動 background jobs。
9. 註冊 route 與 static files。
10. 啟動 HTTP server。

`-migrate-only` 只跑 migration，成功後退出。`-reset-mfa-username <username>` 是 break-glass MFA reset，會重設指定使用者 MFA 並撤銷 sessions。

## Route 與 Middleware 規則

所有 `/api` route 都在 `backend/cmd/server/main.go` 註冊。

敏感 API 的標準 middleware 順序是：

1. `RequireAuth`
2. `RequireActiveUser`
3. `InjectPermissions`
4. `RequirePermission`

不需要登入的 route 目前限於：

- `GET /api/health`
- `GET /api/setup/status`
- `POST /api/setup`
- `POST /api/auth/login`
- `POST /api/auth/mfa/verify`
- `POST /api/auth/refresh`

`GET /api/events/stream` 是 SSE 長連線，不套一般 request timeout，但仍要求登入與 active user。

## Handler / Repository 分工

Handler 負責：

- request validation
- permission、DB scope、workflow state 檢查
- 呼叫 parser、masking、notification、pool 等 domain service
- audit log
- HTTP response

Repository 負責：

- Meta DB SQL
- transaction
- model mapping
- credential encrypt/decrypt 的 repository-side 讀寫流程

不要把 HTTP status、request body 或 cookie 行為放進 repository。

## 外部資料源連線角色

DB Connection credential 分角色：

| 角色 | 使用場景 |
|---|---|
| `readonly` | SQL Editor、Export、Scheduled Report、metadata read path、ticket review read path |
| `readwrite` | DDL / DML / Redis ticket execute |

新增外部 DB 連線路徑時，先判斷它是 read path 還是 execute path。read path 不應 fallback 到 readwrite credential。

## DB Pool Profiles

| Profile | 用途 |
|---|---|
| `query` | SQL Editor、Export、Scheduled Report |
| `exec` | Ticket execute |
| `metadata` | Metadata object scan |
| `scoped_pg_query` | PostgreSQL scoped query |
| `shadow_validation` | MySQL DDL shadow validation |

每個 profile 都有獨立 `DB_POOL_*` env。調整後需要重啟 Pod。

## Audit Log 規則

以下操作應寫 audit log：

- 權限、auth group、DB scope 變更
- DB Connection 新增、修改、測試、刪除
- Masking rule、whitelist、Redis sensitive key prefix 變更
- Ticket approve、reject、execute、stop、withdraw、revoke
- SQL query blocked by sensitive policy
- Export download 成功或失敗
- host policy violation
- break-glass MFA reset

Audit log 可以記錄 actor、resource id、action、reason，但不可記錄 secret 值、DB password、MFA secret、JWT、export token。

## 測試命令

後端測試要在 `backend/` 目錄執行：

```bash
cd backend
go test ./...
```

如果只改特定模組，可先跑較小範圍：

```bash
cd backend
go test ./internal/handler ./internal/repository ./internal/netguard ./internal/config ./internal/queryaccess ./internal/sqlreview
```

repo root 的 `make test` 會執行完整後端測試。

## 新增後端功能檢查表

- route 是否有正確 auth / active user / permission middleware
- handler 是否驗證 resource ownership、DB scope、workflow state
- 是否需要 audit log
- 是否會輸出或記錄 secret
- 是否要套 masking、query access、SQL review、host policy
- 是否需要 migration
- 是否需要 frontend type 與 API client
- 是否已補 test

