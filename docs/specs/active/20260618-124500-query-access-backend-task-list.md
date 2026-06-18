---
status: active
---

# Query Access Backend Task List

本文件把 Query Access 第一版需要的後端工作獨立拆出，供實作時逐項落地。

對應主文件：

- [Query Access 主 Spec](./20260618-120025-query-access-ticket-and-sql-editor-query-scope-spec.md)
- [Implementation Checklist](./20260618-123500-query-access-implementation-checklist.md)

---

## 目標

後端第一版完成後，需同時滿足：

- 支援 `query_access` 作為新的 ticket type
- 支援 reviewer approve 後建立有效 grant
- 支援 reviewer revoke 後立即失效
- `POST /api/query` 與 `POST /api/exports` 會正確做 Query Access 校驗
- DDL / DML / Redis ticket 不受 Query Access 影響

---

## A. Schema 與資料模型

### Migration

- [ ] 新增 `query_access_grants` migration
- [ ] 欄位：
  - `id`
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
- [ ] 所有時間欄位使用 `DATETIME(6)`
- [ ] 建立查詢有效 grant 所需 index
- [ ] 評估是否加入避免完全重複 grant 的 unique constraint

### Model / Enum

- [ ] 新增 `QueryAccessGrant` model
- [ ] `ticket_type` enum / 常數新增 `query_access`
- [ ] `ticket_status` / workflow 常數確認支援 `stopped`
- [ ] `redis` 名稱統一，不再保留 `redis_command` 混用

---

## B. Repository / Store

### Query Access Grants Repository

- [ ] 實作 create grant
- [ ] 實作 revoke grant
- [ ] 實作依 `subject + connection` 查有效 grant
- [ ] 實作依 `source_ticket_id` 查 grant

### 有效 grant 判斷

- [ ] 中央化有效 grant 條件：
  - `revoked_at IS NULL`
  - `expires_at IS NULL OR expires_at > now_utc`
- [ ] app 層統一使用 UTC 時間比對

---

## C. Query Access Service

### Service 邊界

- [ ] 新增集中式 service，例如 `internal/queryaccess`
- [ ] 不在 query handler / export handler 內各自複製授權邏輯

### 核心能力

- [ ] 讀取使用者有效 grant
- [ ] 比對 SQL dependency 與授權範圍
- [ ] 回傳缺權限的 object 清單
- [ ] 支援 table-level grant 命中
- [ ] 支援 database-level grant 命中
- [ ] 保留 instance-level grant 擴充點，但第一版不對外開放

### 錯誤語義

- [ ] 統一缺權限錯誤碼
- [ ] 統一錯誤訊息格式
- [ ] 錯誤需能指出缺哪個 `database / table`

---

## D. Ticket Domain 與 Workflow

### Ticket Type

- [ ] 在 ticket domain 加入 `query_access`
- [ ] 新增 `query_access` label / display name 映射

### Workflow

- [ ] 定義 `query_access` 狀態流：
  - `pending_review`
  - `approved`
  - `rejected`
  - `withdrawn`
  - `stopped`
- [ ] 明確 `query_access` 不進 execution
- [ ] 明確 `approved` 代表 grant 已生效
- [ ] 明確 `stopped` 代表 grant 已被回收

### Permission 對齊

- [ ] 建單權限走 `tickets.apply`
- [ ] 審批 / 回收走 `tickets.review`
- [ ] 不新增 `query_access.apply`
- [ ] 不新增 `query_access.review`
- [ ] 不接入 `tickets.execute`

---

## E. Create / Review / Revoke API

### 建立 Query Access Ticket

- [ ] 擴充 `POST /api/tickets`
- [ ] 支援 `ticket_type=query_access`
- [ ] request payload 支援：
  - `db_connection_id`
  - `scope_mode`
  - `items`
  - `approved_duration_minutes`
  - `reason`
- [ ] 驗證 submitter 有 `tickets.apply`
- [ ] 驗證目標 connection 在 submitter `DB Scope` 內
- [ ] 驗證 `scope_mode` 只允許 `database` / `table`
- [ ] 驗證 `items` 不為空
- [ ] 驗證 `approved_duration_minutes` 在允許範圍內

### 審批 Query Access Ticket

- [ ] 擴充既有 review handler / service 支援 `query_access`
- [ ] approve 時建立 grant
- [ ] `expires_at = approved_at + duration`
- [ ] reject 時只更新 ticket 狀態
- [ ] 審批權限走 `tickets.review`

### 回收 Query Access Ticket

- [ ] 擴充 revoke handler / service 支援 `query_access`
- [ ] revoke 時更新 grant：
  - `revoked_at`
  - `revoked_by`
- [ ] ticket 狀態同步更新為 `stopped`
- [ ] revoke 後查詢應立即失效

### 收回 Query Access Ticket

- [ ] 確認 submitter 可在 `pending_review` 時 withdraw
- [ ] withdraw 後不可遺留有效 grant

---

## F. Query / Export 授權校驗

### Query Handler

- [ ] 在 `/api/query` 真正執行前做 Query Access 校驗
- [ ] Explain 也走同一套校驗
- [ ] 帶入 context：
  - connection
  - database
  - schema

### Export Handler

- [ ] 在 `/api/exports` 建立前做 Query Access 校驗
- [ ] 與 `/api/query` 共用相同 service
- [ ] 缺權限時拒絕建立 export ticket

### 相容性要求

- [ ] 不影響 DDL ticket 建單
- [ ] 不影響 DML ticket 建單
- [ ] 不影響 Redis ticket 建單
- [ ] 不改變 Sensitive Access 的授權語義

---

## G. Parser / Semantic Integration

### 依賴抽取

- [ ] 復用既有 parser / semantic analysis
- [ ] 確認能抽出 base table dependency
- [ ] 不可退回 string contains heuristic

### 第一版必須覆蓋

- [ ] 單表查詢
- [ ] join
- [ ] alias
- [ ] CTE
- [ ] subquery
- [ ] derived table
- [ ] 未 fully-qualified table name

### 已知風險盤點

- [ ] 列出 parser 暫未覆蓋的 case
- [ ] 決定遇到無法分析時是 fail closed 還是回特定錯誤

---

## H. Notification / Audit / Timeline

### Notification

- [ ] `query_access` 接入既有通知管線
- [ ] submit 時通知 reviewer
- [ ] approve / reject 時通知 submitter
- [ ] revoke 時通知 submitter
- [ ] Lark 與站內信模板支援 `query_access`

### Audit Log

- [ ] 建單寫 audit log
- [ ] approve 寫 audit log
- [ ] reject 寫 audit log
- [ ] revoke 寫 audit log
- [ ] actor 顯示正確

### Ticket Timeline Data

- [ ] detail timeline 可區分：
  - Submitted
  - Approved
  - Rejected
  - Withdrawn
  - Revoked / Stopped
- [ ] 附帶 reviewer 輸入意見與時間

---

## I. 測試

### Unit Test

- [ ] grant scope 匹配測試
- [ ] database-level / table-level 命中優先順序測試
- [ ] revoke / expiry 生效測試
- [ ] 缺權限 object 聚合測試

### Integration Test

- [ ] 建立 `query_access` ticket -> approve -> query 成功
- [ ] approve 後 revoke -> query 立即失敗
- [ ] 沒有 Query Access -> export 建立失敗
- [ ] 有 Query Access 但沒有 Sensitive Access -> 查詢成功但仍遮罩
- [ ] DDL / DML / Redis 工單不受影響

### Regression Test

- [ ] ticket notification 不被破壞
- [ ] ticket detail status / timeline 不被破壞
- [ ] SQL Editor query path 不引入 parser regression

---

## J. 建議實作順序

1. migration / model / enum
2. repository / queryaccess service
3. `query_access` ticket workflow
4. `/api/query` 授權校驗
5. `/api/exports` 授權校驗
6. notification / audit / tests

---

## Backend 完成定義

以下全部成立才算 backend 第一版完成：

- `query_access` 可被建立、審批、拒絕、收回、回收
- approve 後 grant 立即生效
- revoke 後 grant 立即失效
- `/api/query` 與 `/api/exports` 會正確依 Query Access 放行 / 拒絕
- DDL / DML / Redis ticket 行為不變
