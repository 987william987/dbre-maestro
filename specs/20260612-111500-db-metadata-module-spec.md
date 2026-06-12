---
spec_issue_number:
spec_issue_url:
spec_filed_at: 2026-06-12T11:15:00+08:00
spec_branch: dev-william
spec_plan_mode: inactive
spec_executed: false
spec_worktree_path:
ttfc_ms:
tthw_ms:
---

# DB Metadata 模組、雲端實例總覽、資料庫物件快照、連線憑證角色化

## Context

接下來平台要補上 `DB Metadata` 能力，目標不是做即時監控，而是提供 DBA / 管理者可治理、可搜尋、可橫向比對的資料庫資產與物件快照視圖。

本期需求已明確收斂成兩塊：
- 雲端實例總覽：從 AWS API 定時掃描 Aurora MySQL、Aurora PostgreSQL、Redis 的實例資訊
- 資料庫物件快照：實際連進 Aurora MySQL / Aurora PostgreSQL 抓取 database / table 層級 metadata

此功能要納入現有 RBAC、導航、Settings、DB Connections 模型中；不接受把 region、憑證、mapping 規則硬寫在程式碼裡。

## Current State

已驗證現況如下：

| 元件 | 現況 | Gap |
|---|---|---|
| DB Connections | 一筆 connection 只有一組 `username/password` | 無法同時滿足 readonly 與 readwrite 使用場景 |
| SQL Editor metadata | 直接用 `db_connections` 連線查 metadata | 沒有 credential role 抽象 |
| Settings | 目前只保存特殊審批人名單 | 尚未承接 DB Metadata 的可調整平台設定 |
| Navigation / RBAC | 尚無 `DB Metadata` 頁面與 `db_metadata.read` 權限 | 無法從導覽與後端一致控管 |
| 雲資產掃描 | 尚未有 AWS inventory snapshot 模型 | 無法做定時總覽 |
| 資料庫物件快照 | 尚未有 object snapshot 模型與 job | 無法提供平面總表給 DBA 檢視 |
| Redis object metadata | 無現成資料模型 | 本期也不打算做 object-level |

已驗證檔案：
- `backend/internal/model/db_connection.go`
- `backend/internal/handler/db_connection.go`
- `backend/internal/handler/metadata.go`
- `backend/internal/model/settings.go`
- `backend/internal/repository/settings.go`
- `frontend/src/app/layout/AppShell.tsx`
- `frontend/src/modules/users/pages/UsersPage.tsx`

## Proposed Change

新增一個新的功能模組：`DB Metadata`

功能入口：
- 導航欄新增 `DB Metadata`
- route 先收斂為：
  - `/db-metadata/inventory`
  - `/db-metadata/objects`
- 此模組本期只需要單一權限：
  - `db_metadata.read`

`DB Metadata` 分成兩個子頁：

1. `實例總覽`
- 來源：AWS API
- 範圍：Aurora MySQL、Aurora PostgreSQL、Redis
- 更新方式：cron snapshot
- 頻率：預設每 `5` 分鐘
- 不顯示即時 status，因為這不是即時監控頁
- 顯示雲端實例基本資訊與 `DB Connection Mapping`

2. `資料庫物件`
- 來源：實際連線至資料庫查 metadata
- 範圍：Aurora MySQL、Aurora PostgreSQL
- Redis 本期不做 object-level
- 更新方式：cron snapshot
- 頻率：預設每 `1` 小時
- 呈現方式：平面大表，讓 DBA 可直接掃描所有物件狀態

此外，為了讓 `SQL Editor / DB Metadata / DDL-DML execution` 使用不同權限等級帳號，需同步把 `DB Connections` 從單一帳密提升為「角色化憑證」模型：
- `readonly`
- `readwrite`

## Product Decisions

### 1. 權限與導覽

- `DB Metadata` 導航與兩個子頁都由 `db_metadata.read` 控制
- 無 `db_metadata.read` 的使用者：
  - 看不到導航入口
  - 不能直接打 API 存取 inventory / object snapshot
- 本期不新增 `db_metadata.write`
- 本期也不做手工觸發 job，因此只需要 read permission

### 2. 實例總覽頁

資料來源：
- 以當前 runtime 環境的 IAM Role 呼叫 AWS API
- 本期不提供 AK/SK 設定 UI
- region 清單從 Settings 讀取，不寫死在程式碼裡

支援引擎：
- `aurora-mysql`
- `aurora-postgresql`
- `redis`

列表必要欄位：
- DB identifier
- Role
- Engine
- Engine version
- Region
- AZ
- Instance class / size
- Storage type
- Cluster endpoint
- Cluster read-only endpoint
- Instance endpoint
- Mapping
- Last synced at

補充說明：
- `status` 本期不顯示
- 其他額外欄位可放在 detail drawer / detail panel 中查看

### 3. 資料庫物件頁

資料來源：
- 依 object snapshot job 定時寫入 snapshot table
- 前端只查 snapshot，不直接在頁面打資料庫

範圍：
- Aurora MySQL
- Aurora PostgreSQL

平面表格必要欄位：
- Connection name
- Engine
- Cluster name
- Node / instance name
- Database name
- Schema name
- Table name
- Data size bytes
- Index size bytes
- Snapshot time

說明：
- MySQL 的 `schema_name` 可等於 `database_name`，前端仍保留欄位，避免 MySQL/PG 兩種引擎兩套表格
- 本期先聚焦 table-level，不額外做 column-level object snapshot
- 目標是讓 DBA 可以直接排序、搜尋、篩選，不需要點很多層

### 4. Mapping 規則

`實例總覽` 頁的 `Mapping` 用於顯示雲資產與平台內 `DB Connection` 的對應關係。

本期 mapping 規則故意保持簡單：
- 以 AWS inventory 掃出的 endpoint
- 與 `db_connections.host`
- 做 exact string match

判定規則：
- 若某雲資產的任一 endpoint 與某 `db_connections.host` 完全相同，視為 matched
- 若沒有任何 `db_connections.host` 對上，視為 unmatched
- 若同一個 endpoint 同時對上多筆 `db_connections`，視為 ambiguous

呈現：
- `matched`：顯示對應 connection 名稱
- `unmatched`：顯示未映射
- `ambiguous`：顯示多筆對應，提醒人工整理

設計原則：
- mapping 只做顯示與輔助治理
- mapping 不是 object snapshot job 的前置條件
- 不做模糊比對
- 不做名稱推論
- 不做 host 正規化規則引擎

### 5. 憑證角色化

目前平台與資料庫互動的需求至少有四種：
- DDL / DML 工單執行：需要 readwrite
- SQL Editor：需要 readonly
- DB Metadata：需要 readonly
- DB Connection：目前只有單一 user 測試連線能力

因此本期定案：
- MySQL / PostgreSQL connection 要支援多組 credential
- 最少兩種 role：
  - `readonly`
  - `readwrite`

建議資料模型：
- 保留 `db_connections` 作為資產主檔
- 新增子表 `db_connection_credentials`

子表示意欄位：
- `id`
- `db_connection_id`
- `credential_role`：`readonly | readwrite`
- `username`
- `password_encrypted`
- `encryption_key_version`
- `created_at`
- `updated_at`

行為規則：
- `SQL Editor` 一律取 `readonly`
- `DB Metadata` 一律取 `readonly`
- `DDL / DML execute` 一律取 `readwrite`
- `DB Connection 測試連線`：
  - 前端需能分別測 `readonly` 與 `readwrite`
  - 若 UI 本期不做雙按鈕，至少後端模型要先支持角色化

相容策略：
- migration 期間可先把既有 `db_connections.username/password_encrypted` 視為舊資料來源
- 上線後逐步收斂到 `db_connection_credentials`
- 但新功能 spec 以 `db_connection_credentials` 為正式模型，不再在新模組上擴散單一帳密設計

### 6. AWS 掃描與 DB 掃描設定

Settings 承接的是掃描配置，不承接 AWS AK/SK，也不承接 DB 帳密。

新增 settings keys：
- `db_metadata_inventory_enabled`
- `db_metadata_inventory_regions`
- `db_metadata_inventory_engines`
- `db_metadata_inventory_sync_interval_minutes`
- `db_metadata_object_enabled`
- `db_metadata_object_enabled_connection_ids`
- `db_metadata_object_sync_interval_minutes`

已明確定案的 key：
- `db_metadata_inventory_sync_interval_minutes`
- `db_metadata_object_sync_interval_minutes`

預設值建議：
- `db_metadata_inventory_enabled = true`
- `db_metadata_inventory_regions = ["ap-northeast-1"]` 或依部署環境初始化
- `db_metadata_inventory_engines = ["aurora-mysql", "aurora-postgresql", "redis"]`
- `db_metadata_inventory_sync_interval_minutes = 5`
- `db_metadata_object_enabled = true`
- `db_metadata_object_enabled_connection_ids = []`
- `db_metadata_object_sync_interval_minutes = 60`

說明：
- `db_metadata_object_enabled_connection_ids` 用於明確限制哪些 connection 會進物件掃描
- 這避免所有 inventory 只要被掃到就直接嘗試連 DB

### 7. Object Snapshot 啟用條件

object snapshot job 是否掃描某個資料庫，需同時滿足：
- `db_metadata_object_enabled = true`
- connection 屬於 `db_metadata_object_enabled_connection_ids`
- connection 類型是 MySQL / PostgreSQL
- connection 存在 `readonly` credential

不要求：
- 不要求 inventory mapping 先成功

原因：
- 有些平台內 connection 可能本來就不是從 AWS inventory 反查出來
- mapping 是治理視角，不應阻擋 object metadata 能力

### 8. Redis 範圍

Redis 本期只做 instance-level：
- 出現在 `實例總覽`
- 不出現在 `資料庫物件`
- 不做 Redis object metadata 掃描

原因：
- Redis object-level metadata 難以統一定義
- 容易因掃描策略不慎影響效能
- 本期先避免引入高風險設計

### 9. Job 執行模型

Inventory job：
- cron 週期執行
- 預設每 `5` 分鐘
- 每次跑完整個 region x engine 掃描
- 將結果落地為 snapshot

Object snapshot job：
- cron 週期執行
- 預設每 `60` 分鐘
- 多個 connection 之間可以併發
- 同一個 connection 內部 SQL 必須串行執行

本期不做：
- 手工觸發 job
- job queue 管理頁
- 即時執行進度 UI

## Implementation Details

### Data Model

新增 permission：
- `db_metadata.read`

Settings model 擴充：
- `backend/internal/model/settings.go`
- `frontend/src/shared/types/settings.ts`

建議新增 inventory snapshot 表：
- `cloud_db_inventory_snapshots`

欄位示意：
- `id`
- `snapshot_at`
- `engine`
- `provider`，固定為 `aws`
- `region`
- `az`
- `account_id` 或保留欄位
- `db_identifier`
- `cluster_identifier`
- `instance_identifier`
- `role`
- `engine_version`
- `instance_class`
- `storage_type`
- `cluster_endpoint`
- `cluster_reader_endpoint`
- `instance_endpoint`
- `raw_payload_json`

建議新增 object snapshot 表：
- `db_object_snapshots`

欄位示意：
- `id`
- `snapshot_at`
- `db_connection_id`
- `connection_name_snapshot`
- `engine`
- `cluster_name`
- `node_name`
- `database_name`
- `schema_name`
- `table_name`
- `data_size_bytes`
- `index_size_bytes`

建議新增 credentials 子表：
- `db_connection_credentials`

本期不建議：
- 在 `db_connections` 上直接加兩組 `readonly_* / readwrite_*` 欄位

理由：
- schema 會快速失控
- 之後若增加 credential role 很難延展
- 與資產主檔概念混在一起，不利維護

### API

新增 API：
- `GET /api/db-metadata/inventory`
- `GET /api/db-metadata/objects`

可選 detail API：
- `GET /api/db-metadata/inventory/{id}`
- `GET /api/db-metadata/objects/{id}`

Settings API 擴充：
- `GET /api/settings`
- `PATCH /api/settings`

DB Connections API 需後續擴充：
- connection create / patch payload 需支持 credential roles
- test connection API 需支持指定 credential role

### Backend Modules

建議新增：
- `backend/internal/handler/db_metadata.go`
- `backend/internal/repository/db_metadata.go`
- `backend/internal/job/db_metadata_inventory.go`
- `backend/internal/job/db_metadata_objects.go`

既有模組需調整：
- `backend/internal/handler/db_connection.go`
- `backend/internal/handler/metadata.go`
- `backend/internal/handler/query.go`
- `backend/internal/handler/export.go`
- `backend/internal/repository/db_connection.go`

調整原則：
- 任何需要資料庫 readonly 行為的地方，統一走 credential role resolve
- 不要讓各 handler 自己各寫一套「抓帳密」邏輯

### Frontend Modules

建議新增：
- `frontend/src/modules/db-metadata/pages/DBMetadataInventoryPage.tsx`
- `frontend/src/modules/db-metadata/pages/DBMetadataObjectsPage.tsx`
- `frontend/src/modules/db-metadata/api.ts`

既有模組需調整：
- `frontend/src/app/layout/AppShell.tsx`
- `frontend/src/App.tsx`
- `frontend/src/modules/users/pages/UsersPage.tsx`
- `frontend/src/modules/settings/pages/SettingsPage.tsx`
- `frontend/src/modules/db-connections/pages/DBConnectionsPage.tsx`

UI 原則：
- `實例總覽` 優先強調資產對照與 mapping
- `資料庫物件` 優先強調大表格、篩選、排序、搜尋
- 不做即時刷新錯覺；頁面明確顯示 `Last synced at`

## Acceptance Criteria

1. 導航欄新增 `DB Metadata`，僅有 `db_metadata.read` 的使用者可見。
2. `/db-metadata/inventory` 與 `/db-metadata/objects` 兩個頁面都受 `db_metadata.read` 保護，無權限直接打 API 也會被拒絕。
3. `實例總覽` 能顯示 Aurora MySQL、Aurora PostgreSQL、Redis 三類 inventory snapshot。
4. `實例總覽` 不顯示即時 status，且頁面可看到每筆資料的 `Last synced at`。
5. `實例總覽` 的 `Mapping` 依 endpoint exact match 顯示 `matched / unmatched / ambiguous`。
6. `資料庫物件` 只顯示 MySQL / PostgreSQL object snapshot，不顯示 Redis。
7. `資料庫物件` 以單一平面表呈現，至少包含 connection、cluster/node、database/schema、table、data size、index size、snapshot time。
8. inventory job 預設每 `5` 分鐘執行，object snapshot job 預設每 `60` 分鐘執行。
9. object snapshot job 對多個 connection 可併發，但同一 connection 內查詢必須串行執行。
10. Settings 可配置 inventory / object job 的 enabled、regions、engines、connection scope、sync interval。
11. AWS inventory 掃描只依賴 runtime IAM role，本期沒有 AK/SK 設定介面。
12. MySQL / PostgreSQL connection 模型可同時保存 `readonly` 與 `readwrite` credential。
13. `SQL Editor` 與 `DB Metadata` 使用 `readonly` credential；`DDL / DML execute` 使用 `readwrite` credential。
14. object snapshot job 不依賴 inventory mapping 是否成功；只依賴 settings 啟用與 connection readonly credential 是否存在。
15. Redis 本期只出現在 inventory，不做 object-level snapshot。

## Testing Plan

| Layer | What | Count |
|---|---|---|
| Unit | endpoint mapping `matched / unmatched / ambiguous` 判定 | +4 |
| Unit | credential role resolve：readonly / readwrite / missing credential | +5 |
| Unit | settings 解析與預設值處理 | +4 |
| Integration | inventory snapshot job 依 regions / engines 掃描並正確落表 | +4 |
| Integration | object snapshot job 只掃 enabled connection，且同一 connection 內 SQL 串行 | +4 |
| Integration | `db_metadata.read` API gate 與無權限拒絕 | +3 |
| Integration | SQL Editor / metadata / execute 分別取對應 credential role | +4 |
| Frontend | 導航權限顯示、inventory 列表、object 平面表、last synced 顯示 | +6 |

## Rollback Plan

- 若 `DB Metadata` 模組上線後出現問題，可先隱藏導航與 route，不影響既有 SQL Editor / Tickets 主流程。
- inventory / object snapshot 採 additive table，不覆蓋既有業務資料。
- credentials 子表可先與舊 `db_connections.username/password_encrypted` 並存，避免一次性切換失敗。
- 若 object snapshot job 有壓力問題，可先只停用 `db_metadata_object_enabled`，保留 inventory 頁。

## Effort Estimate

- 後端 migration / settings / credentials 模型：~8h
- 後端 inventory / object snapshot job：~12h
- 後端 handler / permission / credential role 接線：~8h
- 前端 navigation / pages / settings / db connections：~10h
- 測試補齊：~8h

總計：約 `38h` 到 `46h`

## Files Reference

| File | Change |
|---|---|
| `backend/internal/model/db_connection.go` | 連線主檔與 credential role 模型調整 |
| `backend/internal/repository/db_connection.go` | connection / credential CRUD 與帳密解析 |
| `backend/internal/handler/db_connection.go` | create / patch / test connection 支援角色化憑證 |
| `backend/internal/handler/metadata.go` | 改走 readonly credential resolve |
| `backend/internal/handler/query.go` | SQL Editor 改走 readonly credential |
| `backend/internal/handler/export.go` | export/read-only 路徑改走 readonly credential |
| `backend/internal/handler/ticket.go` | DDL / DML execute 改走 readwrite credential |
| `backend/internal/model/settings.go` | DB Metadata settings key 擴充 |
| `backend/internal/repository/settings.go` | settings 存取擴充 |
| `backend/internal/handler/settings.go` | settings API 擴充 |
| `backend/internal/handler/db_metadata.go` | inventory / objects 查詢 API |
| `backend/internal/repository/db_metadata.go` | snapshot 查詢 repository |
| `backend/internal/job/db_metadata_inventory.go` | AWS inventory cron job |
| `backend/internal/job/db_metadata_objects.go` | DB object snapshot cron job |
| `frontend/src/app/layout/AppShell.tsx` | 新增 DB Metadata 導航 |
| `frontend/src/App.tsx` | 新增 route 與 route guard |
| `frontend/src/modules/users/pages/UsersPage.tsx` | permission catalog 新增 `db_metadata.read` |
| `frontend/src/modules/settings/pages/SettingsPage.tsx` | DB Metadata 設定 UI |
| `frontend/src/modules/db-connections/pages/DBConnectionsPage.tsx` | credential role 配置 UI |
| `frontend/src/modules/db-metadata/*` | 新增頁面與 API |
| `docs/PERMISSIONS_MATRIX.md` | implementation 時需同步更新權限與頁面/API 對照 |

## Out of Scope

- 即時狀態監控
- 手工觸發 inventory / object job
- Redis object-level metadata
- AWS AK/SK 設定頁
- 自動智慧 mapping
- 根據 cluster name / identifier 做模糊關聯
- DB object column-level snapshot
- 物件變更 diff 視圖
- SSE / WebSocket 即時推送 metadata 更新
