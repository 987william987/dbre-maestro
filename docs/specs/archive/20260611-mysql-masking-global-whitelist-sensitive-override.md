---
status: archived
spec_issue_number:
spec_issue_url:
spec_filed_at: 2026-06-11T00:00:00Z
spec_branch: dev-william
spec_plan_mode: inactive
spec_executed: false
spec_worktree_path:
ttfc_ms:
tthw_ms:
---

# MySQL 遮敏模型重構：Global Column Masking + Object-level Unmask Whitelist + `global.sensitive` Override

## Context

目前脫敏功能已進入實際配置階段，且屬於上線前必須完成的核心治理能力。最重要的目標不是 DBA 配置體驗，而是一般 SQL Editor 使用者在查詢與匯出結果時，能被正確且一致地脫敏。

現況的主要問題是產品語義與實作能力不一致。系統目前對 masking rule 呈現出精準匹配的外觀，但實際上 MySQL 與 PostgreSQL 的匹配能力不同，容易誤導管理者配置錯誤規則。這次必須把模型收斂為「只支援 MySQL」，並改成更符合實際使用的兩層模型：`global column masking` 與 `object-level unmask whitelist`，同時正式接上既有但尚未落實於執行期的 `global.sensitive` 永久繞過能力。

## Why This Matters

- 對一般 SQL Editor 使用者：查詢結果與匯出結果必須穩定被脫敏，避免敏感欄位明文外洩。
- 對擁有高權限的治理人員：`global.sensitive` 必須有明確且可預測的效果，不能只存在 RBAC 定義中卻不生效。
- 對管理者：規則必須可理解、可預測，不能讓 UI 暗示系統支援它其實做不到的精準匹配。
- 對上線風險：若脫敏語義不清，上線後會同時出現漏脫敏、誤殺、權限失效三種問題。

## Current State

驗證時間：2026-06-11

### Query 路徑

- `backend/internal/handler/query.go`
  - `/query` 會載入 `masking_rules`
  - 逐條 rule 檢查 whitelist exemption
  - 然後對結果集套用 masking engine
- 問題：
  - 目前沒有接入 `global.sensitive`
  - whitelist 判斷目前只吃 `connection + table + column`，沒有 `database`
  - MySQL / PostgreSQL 行為不一致

### Export 路徑

- `backend/internal/handler/export.go`
  - `/exports` 建立申請時，只用「該 connection 是否存在 masking rules」判斷是否敏感
  - `/exports/download/{token}` 直接執行 SQL 並輸出 CSV
  - 目前不會重跑 masking / whitelist 邏輯
  - 目前也沒有接入 `global.sensitive`
- 問題：
  - SQL Editor 與 export 的最終資料安全語義不一致

### Masking Rules 模型

- `backend/internal/handler/masking_rule.go`
- `backend/internal/model/masking_rule.go`
- 現況：
  - rule 已被擴成 `database/schema/table/column`
  - scope 仍可綁特定 connection
- 問題：
  - 與本次需求衝突
  - 新需求要求 `masking rule` 只能是 truly global：`column_name + mask_mode`

### Masking Whitelist 模型

- `backend/internal/handler/masking_whitelist.go`
- 現況：
  - whitelist 仍是舊模型
  - 只支援 `connection + table + column`
  - 還有 `user_id / auth_group` 型豁免語義
- 問題：
  - 與新需求衝突
  - 新需求要的是「資料物件級解除脫敏」，不是針對某些人看明文

### `global.sensitive` 權限

- `backend/migrations/015_rbac_v2_foundation.up.sql`
  - `global.sensitive` 已存在於 permission 定義
- `frontend/src/modules/users/pages/UsersPage.tsx`
  - Users 權限頁也有列出此權限
- 問題：
  - 目前查詢與匯出執行期尚未接線
  - 使用者即使擁有 `global.sensitive`，目前也不保證能永久繞過遮罩

### 前端權限與頁面入口

- `frontend/src/App.tsx`
  - `/masking-rules` 目前被包在一個混合多種 permission 的外層 route 裡
- `frontend/src/app/layout/AppShell.tsx`
  - 側邊欄對 `Masking Rules` 的顯示條件為 `masking_rules.read || masking_rules.write`
- 問題：
  - 側邊欄方向正確
  - route gate 不夠精準，無法保證「沒有 masking_rules 權限的用戶完全看不到該頁」

## Proposed Change

將脫敏治理模型重構為兩區分離，且本次只支援 MySQL。

### 1. Global Masking Rules

用途：定義「哪些欄位名稱通常應被脫敏」。

規則模型固定為：

- `column_name`
- `mask_mode`

限制：

- 只允許 truly global
- 不允許綁定 connection
- 不允許綁定 database / schema / table
- 只支援 MySQL 生效
- PostgreSQL / Redis 不納入本次模型

生效語義：

- 只要 MySQL 查詢結果中的欄位名稱 match `column_name`
- 即視為應套用該 `mask_mode`
- 此規則同時作用於：
  - SQL Editor `/query`
  - Export `/exports/download`

### 2. Unmask Whitelist

用途：修正 global rule 的誤殺。

規則模型固定為：

- `db_connection_id`
- `database_name`
- `table_name`
- `column_name`

限制：

- 只支援 MySQL connection
- 不支援 PostgreSQL / Redis
- 不支援 `user_id`
- 不支援 `auth_group`
- 是資料物件級白名單，不是人員級白名單

生效語義：

- 若 query/export 結果欄位先命中 global masking rule
- 再檢查 whitelist 是否命中：
  - `connection -> database -> table -> column`
- 命中則解除脫敏

### 3. `global.sensitive` 永久繞過

用途：允許特定高權限使用者永久查看原始敏感資料。

語義：

- 若使用者透過 user permission 或 auth group 擁有 `global.sensitive`
- 則不受以下兩套模型限制：
  - `global masking rules`
  - `object-level unmask whitelist`
- 對這類使用者：
  - SQL Editor `/query` 返回原始未脫敏資料
  - Export `/exports/download` 返回原始未脫敏資料

限制：

- `global.sensitive` 不繞過 sensitive export approval
- 也就是：
  - 仍可保留 `/exports` 的敏感匯出審批流程
  - 只是審批通過後，下載結果可為未脫敏資料

UI 要求：

- 若目前使用者擁有 `global.sensitive`
- SQL Editor 與 export 相關頁面需明示 `Sensitive override active`
- 避免使用者誤以為看到的仍是一般遮罩結果

### 4. 最終優先順序

執行優先序固定為：

1. `global.sensitive`
2. `whitelist`
3. `global masking rule`

### 5. UI 結構調整

將頁面拆為兩個治理區：

- `Global Masking Rules`
  - 只管理 global column rules
  - 只顯示 `column_name + mask_mode`
- `Unmask Whitelist`
  - 管理 object-level unmask 清單
  - 建立流程使用 lazy load

Whitelist 的 lazy load 流程：

1. 選 MySQL connection
2. 載入 database
3. 選 database 後載入 table
4. 選 table 後載入 column

限制：

- 下拉只顯示 MySQL connections
- 不顯示 PostgreSQL / Redis 作為可選目標
- 上層選擇變更時，下層選項與值必須清空

### 6. 權限治理

#### Read

- `masking_rules.read`
  - 可查看 Masking Rules / Masking Whitelist 頁面與列表
  - 不可新增、修改、刪除

#### Write

- `masking_rules.write`
  - 可新增、修改、刪除 Global Masking Rules
  - 可新增、修改、刪除 Whitelist

#### No Permission

- 沒有 `masking_rules.read` 且沒有 `masking_rules.write`
  - 側邊欄不顯示該頁入口
  - 直接輸入 URL 也必須被前端 route gate 與後端 API 拒絕

## Root Cause

目前問題不是單一 bug，而是產品語義與實作能力長期偏移造成的：

1. rule 模型逐步擴成精準匹配，但底層不同 DB driver 能力不同
2. UI 暗示管理者可以做精準治理，但執行期不能在所有 DB 類型上穩定兌現
3. whitelist 還停留在較舊 exemption 模型，沒有對齊新的誤殺修正場景
4. export 與 query 沒共用同一套最終脫敏結果管線
5. `global.sensitive` 已存在 RBAC 定義，但沒有接入執行期資料路徑

## Implementation Details

### Backend

#### A. 收斂 MaskingRule 模型

受影響檔案：

- `backend/internal/model/masking_rule.go`
- `backend/internal/repository/masking_rule.go`
- `backend/internal/handler/masking_rule.go`

要求：

- create/patch payload 只接受 `column_name`, `mask_mode`
- 不再接受 connection/database/schema/table 維度作為 rule 輸入
- list response 也以新語義輸出
- 舊資料不承諾自動兼容
- 規格中明寫：上線前需人工整理既有資料

#### B. 將 Whitelist 改為 object-level unmask

受影響檔案：

- `backend/internal/handler/masking_whitelist.go`
- `backend/internal/model/masking_whitelist.go`
- `backend/internal/repository/masking_whitelist.go`

要求：

- whitelist payload 只接受：
  - `db_connection_id`
  - `database_name`
  - `table_name`
  - `column_name`
- 移除 `user_id` / `auth_group` 為主要產品語義
- 寫入時驗證該 connection 必須是 MySQL
- list API 回傳完整 object-level 欄位
- uniqueness 至少需防止相同 `(connection, database, table, column)` 重複建立

#### C. 重構 Query 脫敏執行順序

受影響檔案：

- `backend/internal/handler/query.go`
- `backend/internal/masking/engine.go`
- `backend/internal/masking/rules.go`

新邏輯：

1. 先檢查當前使用者是否有 `global.sensitive`
2. 若有，直接返回原始資料，並在 response metadata 中標示 override 狀態
3. 若無，僅對 MySQL connection 使用新模型
4. 讀取 global masking rules
5. 對結果欄位名做 column-level match
6. 若命中，檢查 whitelist 是否命中 `(connection, database, table, column)`
7. whitelist 命中則跳過 masking
8. 否則套用 `mask_mode`

#### D. 讓 Export 與 Query 使用一致的最終資料語義

受影響檔案：

- `backend/internal/handler/export.go`

要求：

- `/exports/download/{token}` 在流式輸出前，需套用與 `/query` 一致的資料決策邏輯
- 若使用者有 `global.sensitive`：
  - 可下載未脫敏資料
  - 但仍不可跳過 sensitive export approval
- 若使用者無 `global.sensitive`：
  - 套用 MySQL masking + whitelist override
- 匯出內容必須與最終權限語義一致

#### E. 權限治理

受影響檔案：

- `backend/cmd/server/main.go`

要求：

- `GET /masking-rules`、`GET /masking-whitelist` 需 `masking_rules.read` 或 `masking_rules.write`
- `POST/PATCH/DELETE` 需 `masking_rules.write`
- API 層不可只靠前端遮按鈕

### Frontend

#### A. Route Gate 收斂

受影響檔案：

- `frontend/src/App.tsx`

要求：

- `/masking-rules` 應有獨立 route gate
- 僅允許 `masking_rules.read` 或 `masking_rules.write`
- 不可再混在 `sql_editor.query`、`db_connections.*`、`sql_review.*` 的大集合 route 裡

#### B. 導覽顯示

受影響檔案：

- `frontend/src/app/layout/AppShell.tsx`

要求：

- 保持目前邏輯：
  - 只有 `masking_rules.read || masking_rules.write` 才顯示入口
- 沒有 permission 時，不顯示入口

#### C. 頁面分區

受影響檔案：

- `frontend/src/modules/masking-rules/pages/MaskingRulesPage.tsx`

要求：

- 分為兩區：
  - `Global Masking Rules`
  - `Unmask Whitelist`
- Global rule 表單只顯示：
  - `column_name`
  - `mask_mode`
- Whitelist 表單使用 lazy load：
  - connection(MySQL only)
  - database
  - table
  - column

#### D. `global.sensitive` UI 提示

受影響檔案：

- `frontend/src/modules/sql-editor/pages/SQLEditorPage.tsx`
- export 相關頁面與結果區

要求：

- 若目前使用者有 `global.sensitive`
- 頁面需明示 override 狀態
- 提示內容需清楚表達：
  - 使用者目前查看的是原始敏感資料
  - 這不是一般遮罩結果

## Acceptance Criteria

1. 系統明確定義本次脫敏模型只支援 MySQL，PostgreSQL 與 Redis 不納入本模型。
2. Global masking rule 只能建立 `column_name + mask_mode` 規則，不可綁定 connection/database/schema/table。
3. Query 結果在 MySQL connection 下，凡欄位名稱命中 global masking rule，即套用脫敏。
4. 若同一欄位同時命中 whitelist `(connection,database,table,column)`，則 whitelist 優先，該欄位不脫敏。
5. 若使用者擁有 `global.sensitive`，則 query 結果直接返回未脫敏資料，不受 global masking 與 whitelist 限制。
6. Export 下載內容必須與 SQL Editor 查詢結果套用同一套最終資料語義。
7. `global.sensitive` 不可繞過 sensitive export approval；審批流程仍存在。
8. Whitelist UI 建立流程必須採 lazy load：選 MySQL connection 後才載入 database，再載入 table，再載入 column。
9. Whitelist 下拉中不可顯示 PostgreSQL / Redis connections 作為可選目標。
10. 有 `masking_rules.read` 但無 `masking_rules.write` 的使用者只能查看頁面，不能建立、修改、刪除。
11. 無 `masking_rules.read` 且無 `masking_rules.write` 的使用者不會看到頁面入口，直接輸入 URL 也無法存取。
12. 擁有 `global.sensitive` 的使用者在 UI 上會看到明確 override 提示。
13. Spec 明確標註：既有歷史規則不承諾自動遷移，需人工整理後再上線。
14. 所有新增或修改測試通過，且不造成既有 SQL Editor / export 基本功能回歸。

## Testing Plan

| Layer | What | Count |
|------|------|------|
| Backend unit | global masking 命中欄位名後套遮罩 | +3 |
| Backend unit | whitelist 命中 `(connection,database,table,column)` 後解除遮罩 | +3 |
| Backend unit | `global.sensitive` 直接返回原始資料 | +3 |
| Backend unit | export download 套用同一套資料決策邏輯 | +2 |
| Backend unit | `global.sensitive` 不繞過 export 審批 | +2 |
| Backend unit | 非 MySQL connection 不走本模型 | +2 |
| Frontend unit | `read only` / `write` / `no permission` 三種頁面與 action 呈現 | +3 |
| Frontend unit | whitelist lazy load 流程：connection -> database -> table -> column | +3 |
| Frontend unit | `global.sensitive` 提示顯示 | +2 |
| E2E / manual | SQL Editor 查詢與 export 同一 SQL 的最終資料語義一致 | +1 |

## Effort Estimate

- 後端 masking rule 模型收斂：3-4h
- whitelist 模型與 API 重做：4-6h
- query/export 共用資料決策邏輯：5-7h
- `global.sensitive` 執行期接線：2-3h
- 前端頁面拆區與 lazy load：4-6h
- 權限 route / action 收斂：1-2h
- UI override 提示：1-2h
- 測試補齊與回歸驗證：3-4h

總計：約 23-34h

## Out of Scope

- PostgreSQL 脫敏模型與 whitelist
- Redis 脫敏模型與 whitelist
- 歷史規則自動遷移或批量轉換工具
- 以 user/auth_group 為中心的個人級 whitelist 豁免模型
