# 專案導覽

DBRE Maestro 是資料庫治理平台。它把查詢、變更、權限、審批、遮罩、通知與審計集中在一個控制平面，避免工程師直接拿資料庫帳密操作正式資料。

這份文件用來回答「我要改某個功能時，應該先看哪裡」。如果你要理解設計取捨，先讀 [架構總覽](architecture-overview.md)；如果你要找設定，讀 [設定與環境變數](../reference/configuration.md)。

## 主要目錄

| 路徑 | 責任 |
|---|---|
| `backend/cmd/server` | Go server 入口、route 註冊、migration-only、break-glass MFA reset、static file serving |
| `backend/internal/config` | env、AWS Secrets Manager、DB pool、host policy 設定讀取 |
| `backend/internal/handler` | HTTP handler，包含 auth、tickets、query、settings、users、metadata、export |
| `backend/internal/repository` | Meta DB repository，負責 users、tickets、settings、masking、metadata 等資料表存取 |
| `backend/internal/model` | 後端資料模型 |
| `backend/internal/middleware` | JWT、active user、permission injection、security headers |
| `backend/internal/masking` | masking DSL 與遮罩 engine |
| `backend/internal/queryaccess` | query access scope 抽取與授權判斷 |
| `backend/internal/sqlreview` | SQL / Redis review 規則檢查 |
| `backend/internal/job` | metadata inventory/object scan 背景 job |
| `backend/internal/netguard` | DB Connection host allowlist / CIDR denylist policy |
| `backend/internal/notification` | Lark App / webhook 通知 |
| `backend/internal/realtime` | process-local SSE event broker |
| `backend/migrations` | Meta DB schema migration |
| `frontend/src/modules` | 前端功能模組，每個模組含自己的 API 與頁面 |
| `frontend/src/shared` | 前端共用 API client、auth context、types、UI 元件 |
| `frontend/src/app` | router、layout、provider、error boundary |
| `docs` | Diataxis 文件：tutorials、how-to、reference、explanation、specs |

## 後端請求生命週期

```text
HTTP request
  |
  v
chi router in backend/cmd/server/main.go
  |
  +-- SecurityHeaders
  +-- RequestID / RealIP / redactingRequestLogger / Recoverer
  +-- request timeout, except /api/events/stream
  |
  v
auth middleware
  |
  +-- RequireAuth
  +-- RequireActiveUser
  +-- InjectPermissions
  +-- RequirePermission
  |
  v
handler
  |
  +-- repository reads/writes Meta DB
  +-- pool connects external DB/Redis when needed
  +-- masking / sqlreview / workflow logic
  |
  v
JSON response, file download, or SSE stream
```

route 註冊集中在 `backend/cmd/server/main.go`。新增後端 API 時，先找既有 route group，然後確認：

- 是否需要登入
- 是否需要 active user
- 是否需要 permission injection
- 是否需要明確 permission gate
- 是否需要 audit log
- 是否會接觸 secret、DB credential、export token、MFA secret、query result

## 前端路由模型

前端 route 在 `frontend/src/App.tsx`。主要頁面都以 lazy route 載入，並由兩層 guard 保護：

- `ProtectedRoute`：要求已登入
- `RoleRoute`：依 permission 控制頁面可見性

前端權限只負責使用者體驗，不是安全邊界。後端每個敏感 API 都必須再次檢查 permission、DB scope 與資源狀態。

## Meta DB 與外部資料源

Meta DB 是平台自己的 MySQL，儲存治理資料：

- users、auth groups、permissions、sessions、MFA challenge
- db connections、encrypted credentials、DB scope
- tickets、workflow resolution、query access、export requests
- masking rules、whitelist、Redis sensitive key prefixes
- settings、notifications、audit logs
- metadata inventory/object snapshots

外部資料源是被治理的 MySQL、PostgreSQL、Redis。平台只有在查詢、工單 review/execute、metadata scan、scheduled report、export 時才連外部資料源。

## 背景工作

server 啟動後會啟動幾類 background jobs：

- ticket scheduler：輪詢 due scheduled tickets
- scheduled SQL report scheduler：執行到期報表
- DB metadata inventory job：掃 AWS RDS / ElastiCache 資產快照
- DB metadata object job：連到 DB connections 掃 schema/table/object snapshot

這些 job 都在 app process 內執行。多副本部署時，要確認 job 是否可重入、是否有 DB lock 或序列化保護，避免同一輪工作互相覆蓋。

## 重要設計邊界

- SQL Editor 是唯讀查詢介面，不是通用 SQL console。
- DDL / DML / Redis 變更走 ticket execute，不走 SQL Editor。
- DB Connection 分 readonly / readwrite credential；read path 預期使用 readonly。
- 前端不可作為最終授權來源。
- 所有 secret 必須來自本機 `.env` 或 AWS Secrets Manager，不應寫入 Git。
- export download token、DB password、MFA secret、JWT secret、encryption key 不得出現在 log。

## 常見修改入口

| 要修改的能力 | 優先閱讀 |
|---|---|
| 登入、MFA、session | [登入安全與 Session](../reference/auth-and-sessions.md)、`backend/internal/handler/auth.go` |
| 使用者與權限 | [Users / RBAC](../reference/users-and-rbac.md)、[權限模型](permission-model.md) |
| SQL Editor | [SQL Editor](../reference/sql-editor.md)、`backend/internal/handler/query.go`、`frontend/src/modules/sql-editor` |
| 工單 | [Tickets](../reference/tickets.md)、`backend/internal/handler/ticket.go` |
| Workflow Rules | [Workflow Rules](../reference/workflow-rules.md)、`backend/internal/handler/workflow_resolution.go` |
| DB Connections | [DB Connections](../reference/db-connections.md)、`backend/internal/handler/db_connection.go` |
| Masking | [Masking 與 DSL](../reference/masking-and-dsl.md)、`backend/internal/handler/masking_runtime.go` |
| Scheduled Reports | [Scheduled SQL Reports](../reference/scheduled-sql-reports.md)、`backend/internal/handler/scheduled_sql_report.go` |
| Metadata | [DB Metadata](../reference/db-metadata.md)、`backend/internal/job/db_metadata_inventory.go`、`backend/internal/job/db_metadata_object.go` |
| Deployment | [部署到 AWS EKS](../how-to/deploy-to-aws-eks.md)、[設定與環境變數](../reference/configuration.md) |
| 線上排障 | [How to 排查線上與部署問題](../how-to/troubleshoot-operations.md) |

