---
status: archived
spec_issue_number:
spec_issue_url:
spec_filed_at: 2026-06-11T16:03:46Z
spec_branch: dev-william
spec_plan_mode: inactive
spec_executed: false
spec_worktree_path:
ttfc_ms:
tthw_ms:
---

# SQL Editor 導出審批、臨時敏感查詢審批、Settings 審批人配置、站內信流轉補齊

## Context

目前系統已接近上線安全邊界，但 SQL Editor 導出與敏感資料查詢仍缺少完整審批與通知閉環。若不補齊，使用者能在缺乏可治理記錄、缺乏特殊審批人配置、缺乏狀態通知的情況下接觸敏感資料，這不符合上線要求。

本工作要把 `SQL 導出` 與 `臨時敏感查詢` 納入正式工單體系，入口仍在 SQL Editor，但審批、狀態、記錄、通知統一進 `/tickets` 工作台管理；同時新增 `Settings` 頁面，讓 admin 配置兩類特殊審批人名單。

## Current State

已驗證現況如下：

| 元件 | 現況 | Gap |
|---|---|---|
| SQL export | 已有獨立 `export_requests` 流程，可 create/list/approve/reject/download | 非敏感可直接 ready；不符合「全部先審批」 |
| export metadata | 僅存 `requester_id/sql_content/db_connection_id/status/token/expires_at` | 沒有 instance/database/table/column 明細快照，沒有敏感欄位明示 |
| tickets | 只支援 `ddl/dml` 工單 | 沒有 `sql_export`、`sensitive_query_access` |
| sensitive access | SQL Editor 只有 placeholder 按鈕與 toast | 沒有正式申請/審批/生效/撤銷流程 |
| notifications | 已有站內信 repo/handler，tickets/exports 部分節點有通知 | 沒有覆蓋所有狀態流轉與所有相關角色 |
| settings | 尚無獨立 Settings 頁面與路由 | 無法由 admin 配置特殊審批人 |

已驗證檔案：
- `backend/internal/handler/export.go`
- `backend/internal/model/export.go`
- `backend/internal/repository/export.go`
- `backend/internal/model/ticket.go`
- `backend/internal/repository/ticket.go`
- `frontend/src/modules/sql-editor/pages/SQLEditorPage.tsx`
- `frontend/src/modules/tickets/pages/TicketDetailPage.tsx`
- `frontend/src/app/layout/AppShell.tsx`

## Proposed Change

新增兩種工單型別：
- `sql_export`
- `sensitive_query_access`

入口在 SQL Editor：
- `EXPORT` 改為一鍵建立 `sql_export` 工單
- `Sensitive Access` 改為一鍵建立 `sensitive_query_access` 工單
- 申請時自動帶入使用者、SQL、connection、database/schema、以及後端實際解析出的 table/column 範圍快照，不要求使用者重新輸入大量內容

審批與記錄在 `/tickets`：
- 兩種工單都進既有 `/tickets` 工作台
- 沿用現有狀態字串，不新增新的狀態 vocabulary
- 單層審批，多審批人語義為「任一人批准即可」
- `sql_export`：
  - 一律先 `pending_review`
  - approve 後建立或啟用對應 `export_request`，狀態 ready 可下載
  - 若命中敏感欄位，工單需明示 `instance/database/table/column`
  - export 執行時不受 SQL Editor `LIMIT` 保護，自動執行原始 SQL
- `sensitive_query_access`：
  - 一律先 `pending_review`
  - approve 後在指定時效內生效，時效可選 `10/30/60` 分鐘，預設 `10`
  - 生效範圍僅限工單明細中的 `connection/database/table/column`
  - 只放開核准欄位；同次查詢中其他未核准敏感欄位仍保持遮罩
  - 支援 `admin` 或對應審批人提前撤銷；撤銷自下一次查詢起生效

新增 Settings：
- 新增獨立 `Settings` 頁面與導航入口
- 新增權限：
  - `settings.read`
  - `settings.write`
- 本期只管理兩個具名使用者清單：
  - 敏感導出審批人
  - 臨時敏感查詢審批人
- `lark/sso/2fa` 僅列入後續方向，不在本期實作

## Implementation Details

### Data Model

`tickets` 擴充：
- `ticket_type` 允許：`ddl | dml | sql_export | sensitive_query_access`
- `title/description/sql_content/db_connection_id` 仍保留
- 新增工單明細表，1:N 掛在 ticket 上，保存申請當下的結構化範圍快照：
  - `ticket_id`
  - `connection_id`
  - `database_name`
  - `schema_name`
  - `table_name`
  - `column_name`
  - `is_sensitive`
  - `source_kind` 或等價欄位，標識此列是 export 或 sensitive-access 命中的來源欄位
- `sensitive_query_access` 主表需保存：
  - `approved_duration_minutes`
  - `approved_until`
  - `revoked_at`
  - `revoked_by`
- `sql_export` 與 `export_requests` 保持 1:1 關聯
  - `export_request.ticket_id -> tickets.id`
  - `tickets.ticket_type = sql_export`

`export_requests` 擴充：
- 保留現有 download token / expires / downloaded_at 模型
- 建立時機改成審批後 ready，或先建 pending 再在 approve 時轉 ready，但對外語義必須等價
- 必須能回看其所屬 `ticket_id`

`platform_settings` 或等價設定表新增：
- `sensitive_export_reviewer_user_ids`
- `sensitive_query_access_reviewer_user_ids`

### API / Permission

新/改 API：
- `POST /exports` 改為建立 `sql_export` ticket，而不是直接建立 ready export
- `POST /query/sensitive-access` 或等價路徑建立 `sensitive_query_access` ticket
- `/tickets` list/get/detail/approve/reject 擴充支援兩種新 `ticket_type`
- 新增 `POST /tickets/{id}/revoke` 僅適用 `sensitive_query_access`
- 新增 `GET/PATCH /settings` 或分段 settings API

權限：
- `settings.read`
- `settings.write`
- `sql_export` 審批人不由 `tickets.review` 推導，而是由 Settings 具名名單決定
- `sensitive_query_access` 審批人不由 `tickets.review` 推導，而是由 Settings 具名名單決定
- 後端 API 必須做權限與名單檢查，不能只靠前端隱藏按鈕

### SQL / Metadata Resolution

建單範圍必須以前端當下選中的樹節點為輔、以後端實際解析或執行得到的來源欄位為準：
- 使用者若手寫 join / alias SQL，工單明細仍應保存實際來源 `instance/database/table/column`
- `sql_export` 與 `sensitive_query_access` 均可包含多筆欄位範圍
- 敏感欄位判定沿用後端既有 masking/runtime 判定結果，不在前端重算

### Notifications

所有狀態流轉都發站內信，至少覆蓋：
- submitter
- approver
- executor 或處理人
- admin 或審批人撤銷時的申請人通知

通知內容最小集合：
- 工單編號
- 工單類型
- 狀態變更前後
- 相關角色
- connection 名稱
- 若為 `sql_export`：是否含敏感欄位
- 若為 `sensitive_query_access`：有效期與核准範圍
- 不包含 SQL 原文

## Acceptance Criteria

1. SQL Editor 的 `EXPORT` 只能建立 `sql_export` 工單，不能再直接下載。
2. `sql_export` 工單建立時自動帶入 submitter、SQL、connection 與結構化明細範圍；使用者不需重填這些內容。
3. 所有 `sql_export` 工單都先進 `pending_review`。
4. 若 `sql_export` 命中敏感欄位，工單詳情可明確列出 `instance/database/table/column`。
5. `sql_export` 審批通過後，對應 export 變為 `ready` 可下載。
6. export 執行時不得自動補 `LIMIT`，必須執行原始 SQL。
7. SQL Editor 的 `Sensitive Access` 可建立 `sensitive_query_access` 工單，時效選項僅有 `10/30/60` 分鐘，預設 `10`。
8. `sensitive_query_access` 審批通過後，只對核准的 `connection/database/table/column` 範圍生效。
9. 同次查詢若包含已核准欄位與未核准敏感欄位，只放開已核准欄位，其他欄位仍遮罩。
10. `sensitive_query_access` 支援 admin 或對應審批人提前撤銷；撤銷自使用者下一次查詢起生效。
11. `/tickets` 列表與詳情頁能正確顯示 `sql_export` 與 `sensitive_query_access`，且操作按鈕依工單類型與角色正確收斂。
12. 新增 `Settings` 頁面，admin 可配置兩類具名審批人名單。
13. 無 `settings.read/settings.write` 的使用者看不到 `Settings` 導航，且後端 API 會拒絕存取。
14. 工單所有狀態流轉都會建立站內信，覆蓋 submitter、approver、executor 或處理人。
15. 通知內容不得帶 SQL 原文。
16. 所有新流程都有後端權限檢查與 audit log，不能透過直接呼叫 API 繞過。
17. 既有 `ddl/dml` tickets、既有 SQL Editor 查詢、既有 masking/export download 能力不得退化。

## Testing Plan

| Layer | What | Count |
|---|---|---|
| Unit | 特殊審批人名單判定、ticket type 狀態轉移、sensitive access 生效或撤銷判定 | +8 |
| Integration | 建立 `sql_export` 工單、審批後 ready download、原始 SQL 導出不補 limit | +4 |
| Integration | 建立 `sensitive_query_access` 工單、審批後放開指定欄位、未核准欄位仍遮罩、撤銷後失效 | +5 |
| Integration | Settings 讀寫與權限拒絕 | +3 |
| Integration | 站內信在 submit/approve/reject/revoke/ready 節點建立 | +5 |
| Frontend | SQL Editor 一鍵建單、Tickets 詳情顯示新類型、Settings 配置頁 | +6 |

## Rollback Plan

- 若新 ticket types 上線後出現流程問題，可先關閉 SQL Editor 中的 `EXPORT` 與 `Sensitive Access` 入口。
- 保留既有 `export_requests` 表與下載 token 模型，避免資料不可回讀。
- migration 需採 additive 方式，不要破壞既有 `ddl/dml` tickets。
- 如需回退，先停用新 route，再回退前端入口，最後保留資料表但不使用。

## Effort Estimate

- 後端 tickets / export / settings model & migration：~8h
- 後端 handler / permission / notification / audit：~10h
- 前端 SQL Editor / Tickets / Settings：~10h
- 測試補齊：~6h

總計：約 `34h`

## Files Reference

| File | Change |
|---|---|
| `backend/internal/model/ticket.go` | 擴充 ticket_type 與敏感查詢生命周期欄位 |
| `backend/internal/repository/ticket.go` | tickets CRUD / list / status / revoke 擴充 |
| `backend/internal/handler/ticket.go` | 新 ticket types 的建立/審批/撤銷/顯示 |
| `backend/internal/model/export.go` | export 與 ticket 關聯 |
| `backend/internal/repository/export.go` | export ready/download 邏輯調整 |
| `backend/internal/handler/export.go` | `/exports` 語義改為 ticket 驅動或兼容封裝 |
| `backend/internal/handler/query.go` | sensitive access 生效判定接入 |
| `backend/internal/handler/masking_runtime.go` | 動態放開核准欄位與撤銷判定 |
| `backend/internal/repository/notification.go` | 流轉通知覆蓋擴充 |
| `frontend/src/modules/sql-editor/pages/SQLEditorPage.tsx` | EXPORT / Sensitive Access 改為一鍵建單 |
| `frontend/src/modules/tickets/pages/TicketsPage.tsx` | 新 ticket types 列表顯示 |
| `frontend/src/modules/tickets/pages/TicketDetailPage.tsx` | 新 ticket types 詳情與操作 |
| `frontend/src/app/layout/AppShell.tsx` | Settings 導航入口 |
| `frontend/src/modules/settings/*` | 新增 Settings 頁面與 API |
| `backend/internal/handler/settings.go` | 新增 Settings API |
| `backend/internal/repository/settings.go` | 新增 settings repo |

## Out of Scope

- Lark 設定頁完整配置
- SSO
- 2FA
- 多層審批
- 將 SQL 原文寫入站內信
- 將所有既有 `export_requests` 全量遷移成新 ticket 歷史資料的批量轉換工具
