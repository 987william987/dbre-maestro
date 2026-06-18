---
status: active
---

# Query Access Implementation Checklist

本文件是 [查詢授權工單（Query Access Ticket）與 SQL Editor 細粒度查詢權限 Spec](./20260618-120025-query-access-ticket-and-sql-editor-query-scope-spec.md) 的實作拆解清單。

目標不是重寫 spec，而是把 spec 拆成可分批落地、可驗收、可追蹤的 implementation checklist。

---

## 實作範圍

本輪要落地的主軸：

- 新增 `query_access` ticket type
- 新增 `query_access_grants`
- `query_access` 復用既有 Tickets workspace
- `POST /api/query` 做 Query Access 校驗
- `POST /api/exports` 做 Query Access 校驗
- `Tickets > New Ticket` 支援建立 `query_access`
- `SQL Editor` 支援快捷申請 `query_access`

本輪不做：

- auth group 級 `query_access_grants`
- column-level query authorization
- row-level security
- deny 規則
- 自動把 grant 過期同步回 ticket status

---

## 實作原則

- `query_access` 是新的 ticket type，不是新的獨立頁面模組
- 建單沿用 `tickets.apply`
- 審批 / 回收沿用 `tickets.review`
- `DB Scope` 仍只控制 instance 候選範圍
- `Query Access` 只控制 SQL Editor / Export 的真正查詢執行
- `Sensitive Query Access` 只控制 unmask，不控制 query authorization

---

## Phase 0：前置對齊

### 資料模型與 enum 對齊

- [ ] 確認 `ticket_type` enum / 常數集合加入 `query_access`
- [ ] 確認前後端 ticket type label / i18n / badge 顯示可支援 `query_access`
- [ ] 確認 `redis` 已是正式 ticket type 名稱，不再混用 `redis_command`

### 權限與工作流對齊

- [ ] 確認不新增 `query_access.apply`
- [ ] 確認不新增 `query_access.review`
- [ ] 確認 `query_access` 建單走 `tickets.apply`
- [ ] 確認 `query_access` 審批與回收走 `tickets.review`
- [ ] 確認 `query_access` 不進 `tickets.execute`

### SQL 分析能力盤點

- [ ] 確認 SQL Editor 與 Export 已共用 parser / semantic analysis
- [ ] 確認可抽出查詢涉及的 database / table dependency
- [ ] 確認 alias / join / CTE / derived table 的現況能力可直接復用
- [ ] 列出目前仍未覆蓋的 SQL case，避免 implementation 時誤判為已解

---

## Phase 1：資料庫與後端骨架

### Migration

- [ ] 新增 `query_access_grants` migration
- [ ] 欄位包含：
  - `subject_type`
  - `subject_id`
  - `connection_id`
  - `database_name`
  - `table_name`
  - `granted_via`
  - `source_ticket_id`
  - `expires_at`
  - `revoked_at`
  - `revoked_by`
  - `created_by`
  - `created_at`
  - `updated_at`
- [ ] 時間欄位統一使用 `DATETIME(6)`
- [ ] 建立 active query path 需要的 index
- [ ] 評估是否需要 unique constraint 避免完全重複 grant

### Domain / Repository

- [ ] 新增 `query_access_grants` model
- [ ] 新增 repository / store 查詢有效 grant
- [ ] 新增 create grant / revoke grant 寫入方法
- [ ] 明確定義「有效 grant」判斷：
  - `revoked_at IS NULL`
  - `expires_at IS NULL OR expires_at > now_utc`

### Query Access Service

- [ ] 新增集中式 service，例如 `internal/queryaccess`
- [ ] 封裝 grant 查詢、scope 匹配、缺權限對象整理
- [ ] 封裝 table-level / database-level 匹配優先順序
- [ ] 明確保留 instance-level grant 為未來能力，不在第一版開放 UI

### Ticket Type / Workflow

- [ ] 在 ticket domain 加入 `query_access`
- [ ] 定義 workflow：
  - `pending_review`
  - `approved`
  - `rejected`
  - `withdrawn`
  - `stopped`
- [ ] 明確 `approved` = grant 已生效
- [ ] 明確 `stopped` = grant 被提前 revoke
- [ ] 確認 status badge / status label / detail timeline 可支援此流程

---

## Phase 2：Query Access 工單主流程

### Create Ticket API

- [ ] 擴充 `POST /api/tickets` 支援 `ticket_type=query_access`
- [ ] request payload 支援：
  - `db_connection_id`
  - `scope_mode`
  - `items`
  - `approved_duration_minutes`
  - `reason`
- [ ] server 驗證：
  - 需具備 `tickets.apply`
  - 目標 connection 必須在使用者 `DB Scope` 內
  - `scope_mode` 僅允許 `database` / `table`
  - `items` 不可為空
  - `approved_duration_minutes` 必須在產品允許範圍內

### Review API

- [ ] 擴充 `tickets.review` 路徑支援審批 `query_access`
- [ ] 審批通過時建立 `query_access_grants`
- [ ] `expires_at = approved_at + duration`
- [ ] 審批拒絕時只更新 ticket 狀態，不建立 grant
- [ ] 審批事件寫 audit log

### Revoke API

- [ ] 擴充既有 revoke 行為支援 `query_access`
- [ ] 僅允許有 `tickets.review` 的 reviewer / admin / dba 執行
- [ ] revoke 時同步：
  - 更新 grant `revoked_at`
  - 更新 grant `revoked_by`
  - ticket 狀態改為 `stopped`
- [ ] revoke 事件寫 audit log

### Withdraw API

- [ ] 確認 submitter 可在 `pending_review` 時收回 `query_access`
- [ ] 被收回後不得留下有效 grant

---

## Phase 3：SQL Editor / Export 授權校驗

### POST /api/query

- [ ] 在真正執行查詢前接入 Query Access 校驗
- [ ] 不得使用 heuristic string match，必須走 parser / semantic layer
- [ ] 帶入 execution context：
  - connection
  - database
  - schema
- [ ] 對查詢涉及的所有 table 全量比對有效 grant
- [ ] 未授權時回傳明確錯誤：
  - 指出缺哪個 database / table 權限
- [ ] 確認 Explain 也走同一套 Query Access 校驗

### POST /api/exports

- [ ] 建立 export 前走與 `/api/query` 相同的 Query Access 校驗
- [ ] 缺權限時不得建立 `sql_export` ticket
- [ ] 與 SQL Editor 共用相同錯誤語義，避免兩邊口徑不同

### 邊界驗證

- [ ] 單表查詢可正確命中 table grant
- [ ] 多表 join 必須全部授權
- [ ] database-level grant 可覆蓋該 database 下所有 table
- [ ] CTE / subquery / derived table 可正確追到 base table
- [ ] 無 fully-qualified table name 時，可依當前 database 正確解析

---

## Phase 4：Frontend 落地

### Tickets > New Ticket

- [ ] ticket type 下拉新增 `Query Access`
- [ ] 依 `query_access` 顯示專屬表單，不重用 DDL/DML/Redis 表單
- [ ] 表單支援：
  - connection 下拉
  - `scope_mode` 切換
  - database / table 多選
  - duration 輸入
  - reason 輸入
- [ ] 候選項來源應走既有 metadata / database API，不讓使用者手打 object name
- [ ] 只展示目前 `DB Scope` 內的 connection

### SQL Editor 快捷入口

- [ ] 查詢缺權限時顯示清楚錯誤與 CTA
- [ ] 從當前 connection / database / table 打開 `Query Access` 申請
- [ ] 自動帶入 context，減少重填
- [ ] 若目前無法可靠推導單一 table，可至少先帶入 connection + database

### Ticket List / Detail

- [ ] ticket list 可正確顯示 `query_access`
- [ ] detail 頁可展示：
  - connection
  - scope mode
  - items
  - requested duration
  - approved until
  - revoke status
- [ ] `query_access` 不應顯示 execution 區塊
- [ ] `query_access` 在 `approved` 後可顯示「已生效」
- [ ] `query_access` 在 `stopped` 後可顯示「已回收 / 已停止」

---

## Phase 5：通知與審計

### Notification

- [ ] `query_access` 接入既有 ticket notification pipeline
- [ ] submit 時通知 reviewer，不通知 submitter 自己
- [ ] reviewer approve / reject 後通知 submitter
- [ ] reviewer revoke 後通知 submitter
- [ ] Lark 與站內信文案支援 `query_access`
- [ ] 通知內容帶：
  - 工單類型
  - 當前狀態
  - 申請範圍
  - 工單連結

### Audit Log

- [ ] `query_access` 建單、審批、拒絕、回收都要寫 audit log
- [ ] actor 顯示需正確映射實際操作人
- [ ] details 至少保留：
  - connection
  - database / table items
  - duration
  - ticket id

---

## Phase 6：QA Checklist

### 正向流程

- [ ] 使用者有 `sql_editor.query + tickets.apply + DB Scope`，但沒有 Query Access
  - 可看到 metadata
  - 不能真的查資料
- [ ] 使用者可從 `New Ticket` 建立 `query_access`
- [ ] 使用者可從 SQL Editor 缺權限提示直接建立 `query_access`
- [ ] reviewer approve 後，使用者立即可查
- [ ] reviewer revoke 後，使用者立即失去查詢能力
- [ ] 有 Query Access 但沒有 Sensitive Access 時，敏感欄位仍遮罩

### 反向流程

- [ ] 沒有 `tickets.apply`，不能建立 `query_access`
- [ ] 沒有對應 `DB Scope`，不能對該 connection 建立 `query_access`
- [ ] 沒有 `tickets.review`，不能 approve / revoke `query_access`
- [ ] `query_access` 不得走 execute action
- [ ] 無效或過期 grant 不得被視為有效

### 相容性回歸

- [ ] DDL 工單不會因缺少 Query Access 而被擋
- [ ] DML 工單不會因缺少 Query Access 而被擋
- [ ] Redis 工單不會因缺少 Query Access 而被擋
- [ ] `sql_export` 在缺少 Query Access 時會被擋
- [ ] `sensitive_query_access` 不會意外變成 query authorization 模型

---

## Phase 7：Rollout 與風險控制

### 上線前

- [ ] 準備 migration 與回滾方案
- [ ] 確認舊資料不需要 backfill grant
- [ ] 確認 feature flag 策略：
  - 是否先只開 backend reject，不開 frontend CTA
  - 或整套一起上

### 觀測點

- [ ] 觀測 `/api/query` 被 Query Access 拒絕的次數
- [ ] 觀測 `query_access` ticket submit / approve / revoke 數量
- [ ] 觀測是否出現 parser 無法解析而誤拒絕查詢
- [ ] 觀測 SQL Editor 錯誤提示是否足夠讓使用者完成申請

### 風險清單

- [ ] parser / semantic analysis 對特殊 SQL case 仍可能有盲點
- [ ] database-level grant 若 UI 表達不清，容易讓 reviewer 批太大範圍
- [ ] metadata 可見是刻意接受的取捨，需避免後續又出現互相矛盾的需求
- [ ] `query_access` 與 `sensitive_query_access` 的邊界需在 UI 文案上持續保持清楚

---

## 建議實作順序

1. Migration + domain model
2. ticket type / workflow / backend create-review-revoke
3. `/api/query` Query Access 校驗
4. `/api/exports` Query Access 校驗
5. `Tickets > New Ticket` 的 `query_access` 表單
6. SQL Editor 缺權限提示與快捷申請入口
7. notification / audit log / list-detail polish
8. QA 回歸與 rollout

---

## 驗收出口

以下條件全部成立，才算第一版完成：

- 使用者可透過 Tickets 或 SQL Editor 建立 `query_access` 工單
- reviewer 可審批並立即生效
- SQL Editor 與 Export 會正確依 Query Access 放行或拒絕
- DDL / DML / Redis 工單行為不受影響
- `Sensitive Query Access` 仍只控制 unmask
- ticket list / detail / notification / audit log 都能正確表達 `query_access`
