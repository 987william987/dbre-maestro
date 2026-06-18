---
status: active
---

# Query Access Frontend Task List

本文件把 Query Access 第一版需要的前端工作獨立拆出，供實作時逐項落地。

對應主文件：

- [Query Access 主 Spec](./20260618-120025-query-access-ticket-and-sql-editor-query-scope-spec.md)
- [Implementation Checklist](./20260618-123500-query-access-implementation-checklist.md)

---

## 目標

前端第一版完成後，需同時滿足：

- 使用者可從 `Tickets > New Ticket` 建立 `query_access`
- 使用者可從 SQL Editor 缺權限提示快速發起 `query_access`
- ticket list / detail 能正確顯示 `query_access`
- UI 能清楚區分 Query Access 與 Sensitive Access

---

## A. Ticket Type 與共用展示

### Ticket Type 映射

- [ ] 前端 ticket type 常數加入 `query_access`
- [ ] ticket type label / badge / 顏色支援 `query_access`
- [ ] 篩選器、列表 tag、詳情標題支援 `query_access`
- [ ] `redis` 名稱統一，不再顯示 `redis_command`

### Ticket Status 展示

- [ ] `query_access` 支援：
  - `pending_review`
  - `approved`
  - `rejected`
  - `withdrawn`
  - `stopped`
- [ ] `stopped` 文案、顏色、badge 與其他 ticket type 一致

---

## B. Tickets > New Ticket

### 入口

- [ ] `New Ticket` type selector 新增 `Query Access`
- [ ] 權限仍由 `tickets.apply` 控制，不新增新頁面權限

### 表單

- [ ] `query_access` 使用獨立表單區塊
- [ ] 不重用 DDL / DML / Redis 的 SQL 編輯表單
- [ ] 表單欄位：
  - connection selector
  - `scope_mode` selector
  - database / table selector
  - duration input
  - reason textarea

### 候選項

- [ ] connection 候選只顯示 `DB Scope` 內可用 connection
- [ ] database / table 候選走既有 metadata / database API
- [ ] 不允許手打 object name
- [ ] `scope_mode=database` 時可多選 database
- [ ] `scope_mode=table` 時可多選 table

### UX

- [ ] 切換 connection 時清空不相容的 database / table 選擇
- [ ] 切換 `scope_mode` 時清空不相容的 items
- [ ] duration 有清楚的單位提示
- [ ] submit 前做基本表單驗證

---

## C. SQL Editor 快捷入口

### 缺權限提示

- [ ] `/api/query` 因 Query Access 被拒絕時，顯示專屬錯誤，不混成 generic query failed
- [ ] 錯誤訊息指出缺少哪個 database / table 權限
- [ ] 提供清楚 CTA，例如 `Apply Query Access`

### 快捷申請流程

- [ ] 從 SQL Editor 直接打開 `query_access` 建單流程
- [ ] 自動帶入：
  - connection
  - database
  - table（若可可靠推導）
- [ ] 若無法可靠推導 table，至少帶入 connection + database
- [ ] 建單完成後導向對應 ticket detail 或 ticket list

### 與現有按鈕邏輯的邊界

- [ ] 不與 `Sensitive Access` 按鈕混淆
- [ ] 不與 `Export` 按鈕混淆
- [ ] Query Access CTA 應出現在缺權限場景，而不是固定常駐干擾主流程

---

## D. Ticket List

### List 展示

- [ ] list 頁可正確顯示 `query_access`
- [ ] ticket type filter 可篩選 `query_access`
- [ ] status filter 可處理 `stopped`
- [ ] list row 摘要可顯示 scope 概要

### 即時更新

- [ ] `query_access` 狀態變更可沿用既有 SSE / 刷新機制
- [ ] approve / reject / revoke 後列表應即時更新

---

## E. Ticket Detail

### Detail 版型

- [ ] `query_access` detail 使用既有 ticket detail 框架
- [ ] 不顯示 execution 區塊
- [ ] 不顯示 statement execution result 區塊

### 關鍵欄位

- [ ] 顯示 connection
- [ ] 顯示 `scope_mode`
- [ ] 顯示申請 items
- [ ] 顯示 requested duration
- [ ] 顯示 approved until
- [ ] 顯示 revoke / stopped 狀態

### Timeline / Other Details

- [ ] Submitted / Approved / Rejected / Withdrawn / Revoked 都可視化
- [ ] reviewer 意見可展示
- [ ] revoke 原因可展示

### Actions

- [ ] submitter 在 `pending_review` 可 withdraw
- [ ] reviewer 在 `pending_review` 可 approve / reject
- [ ] reviewer 在 `approved` 可 revoke
- [ ] `query_access` detail 不出現 execute / request execution

---

## F. 文案與心智模型

### 用語

- [ ] 文案明確使用 `Query Access`
- [ ] 文案明確區分：
  - `Query Access` = 能不能查
  - `Sensitive Access` = 能不能看未脫敏值
- [ ] 避免讓使用者誤以為申請 Sensitive Access 就等於取得查詢權限

### 錯誤提示

- [ ] 缺 Query Access 的錯誤提示要可行動
- [ ] 不暴露不必要的後端技術細節
- [ ] 但要保留足夠上下文，讓使用者知道該申請哪個範圍

---

## G. QA 測項

### 建單

- [ ] 可從 `New Ticket` 建立 `query_access`
- [ ] connection / scope / items / duration / reason 都能正常提交
- [ ] 表單驗證錯誤顯示正確

### SQL Editor 捷徑

- [ ] 查詢缺權限時可看到正確 CTA
- [ ] CTA 建單時 context 帶入正確
- [ ] 建單後可回到 ticket detail

### Detail / List

- [ ] list 與 detail 都能正確顯示 `query_access`
- [ ] `approved` 與 `stopped` 顯示清楚
- [ ] `query_access` 不出現 execution 相關 UI

### 回歸

- [ ] DDL / DML / Redis 建單 UI 不受影響
- [ ] SQL Export UI 不受影響
- [ ] Sensitive Access UI 不受影響
- [ ] 既有 ticket list / detail 不被破壞

---

## H. 建議實作順序

1. ticket type / status 共用映射
2. `New Ticket` 的 `query_access` 表單
3. ticket list / detail 顯示
4. SQL Editor 缺權限提示與 CTA
5. SSE / 即時更新回歸
6. 文案 polish 與 QA

---

## Frontend 完成定義

以下全部成立才算 frontend 第一版完成：

- `query_access` 可從 `New Ticket` 建立
- SQL Editor 缺權限時可快速發起 `query_access`
- ticket list / detail 可正確展示 `query_access`
- UI 不把 Query Access 與 Sensitive Access 混為一談
