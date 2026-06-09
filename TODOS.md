# TODOS

## P1 — 核心安全/正確性

- [x] **T8** — GET /health endpoint（Meta DB 連線狀態 + 平台版本），docker-compose healthcheck 用
  - Files: `backend/internal/handler/health.go`
  - Verify: DB 斷線後 /health 返回 503；docker-compose healthcheck 通過

- [x] **T1** — SQL Editor 白名單強制（parser 層 whitelist，非 blacklist）
  - Files: `backend/internal/sqlreview/readonly_check.go`
  - Verify: `SET @x = (SELECT ...)` 被拒絕；`SELECT 1` 通過

- [x] **T2** — Ticket 狀態機補 `stopped` + `interrupted` 兩個狀態
  - Files: `backend/internal/ticket/state.go`
  - Verify: invalid transition 測試、`interrupted` startup 掃描邏輯

- [x] **T3** — Lark 通知 retry 2-3x + failure log
  - Files: `backend/internal/notification/lark.go`
  - Verify: Mock 返回 500，確認重試 3 次後寫入 audit_logs.notification_failure

- [x] **T4** — 脫敏 Fail Closed（parse 失敗返回 422，不 Fail Open）
  - Files: `backend/internal/masking/engine.go`
  - Verify: 不支援語法返回 422 with 明確錯誤訊息

- [x] **T5** — Ticket API IDOR 保護（角色 + ownership 二層驗證）
  - Files: `backend/internal/middleware/ticket_ownership.go`
  - Verify: User A 無法透過 API 讀取 User B 的工單

- [x] **T6** — `db_connections` 表加入 `encryption_key_version` 欄位
  - Files: `migrations/add_encryption_key_version.sql`
  - Verify: schema 正確；version=1 預設值

- [x] **T7** — Connection Pool 分離：`exec_pool` (MaxOpenConns=3) + `query_pool` (MaxOpenConns=10)
  - Files: `backend/internal/pool/manager.go`
  - Verify: 工單執行中 SQL Editor 查詢不受影響

- [x] **T9** — Ticket 執行 OCC（`WHERE status='pending_execution'`，affected=0 返回 409）
  - Files: `backend/internal/ticket/execute.go`
  - Verify: 並發 Execute 請求只有一個成功

- [x] **TE1** — sessions 表 + Refresh Token 可撤銷（hash 存 meta DB，logout 刪除，換 token 驗 DB）
  - Files: `backend/internal/auth/sessions.go`, `migrations/sessions.sql`
  - Verify: logout 後舊 refresh token 無法換取新 access token

- [x] **TE2** — 脫敏規則 cache TTL（5 分鐘）+ 主動 invalidate（DBA 儲存規則時觸發 cache clear）
  - Files: `backend/internal/masking/cache.go`
  - Verify: DBA 修改規則後，下一次查詢立即用新規則（不需重啟）

- [x] **TE3** — 工單狀態機中心化 Transition Table（`map[TicketStatus][]TicketStatus`，所有 handler 走這一層）
  - Files: `backend/internal/ticket/state_machine.go`
  - Verify: invalid transition（如 rejected→executing）返回 422

- [x] **TE5** — 脫敏 Hash 改用 HMAC-SHA256 + pepper（pepper 從 DBRE_ENCRYPTION_KEY 派生）
  - Files: `backend/internal/masking/rules.go`
  - Verify: 相同 value + 不同 pepper → 不同 hash；有 pepper 時字典攻擊無效

- [x] **TE6** — export_requests 表 + 安全下載連結（32 bytes crypto/rand token，含 expires_at + downloaded_at）
  - Files: `migrations/export_requests.sql`, `backend/internal/export/token.go`
  - Verify: 過期 token 返回 403；token 可正確下載 CSV

- [x] **TE8** — audit_logs MySQL REVOKE（app_user 無 UPDATE/DELETE 權限）
  - Files: `migrations/revoke_audit_logs.sql`
  - Verify: 以 app_user 連線嘗試 DELETE audit_logs 返回 ERROR 1142

## P2 — 功能增強

- [x] **T10** — E1 稽核日誌 Dashboard API（GET /audit-logs，DBA/Admin only，篩選 + 分頁）
  - Files: `backend/internal/model/audit_log.go`, `backend/internal/repository/audit.go`, `backend/internal/handler/audit.go`
  - Filters: action_type, actor_id, resource_type, resource_id, from/to (RFC3339), limit/offset

- [x] **T11** — E2 工單審核意見欄（拒絕原因必填 + 審核意見選填 + Lark 通知含意見）
  - Files: `backend/internal/handler/ticket.go`, `backend/internal/config/config.go`
  - Approve: comment 寫入 audit_logs details + notifyLark 含 comment
  - Reject: reason 寫入 audit_logs details + notifyLark 含 reason
  - LARK_WEBHOOK_URL 留空 = 靜默跳過；失敗自動寫 notification_failure

## P1 — UI/UX 設計（/plan-design-review 2026-06-09）

- [x] **TD3** — Setup Wizard UX（4 步驟 + 進度條 + 每步驗證 + 完成後導引）
  - Files: `frontend/src/pages/setup/SetupWizard.tsx`
  - Steps: 歡迎 → 管理員帳號（密碼強度即時反饋 + API 呼叫）→ Lark 通知說明 → 完成

- [x] **TE4** — SQL Review 加入 EXPLAIN 層（複用 query_pool，偵測全表掃描 + 預估影響行數）
  - Files: `backend/internal/sqlreview/explain_check.go`
  - Verify: 沒有 index 的全表掃描 SQL 觸發 Warning；有 WHERE + index 的 SQL 通過

- [x] **TE7** — Lark 4xx（401/403/404）不重試，直接記錄 notification_failure + unit test
  - Files: `backend/internal/notification/lark.go`, `backend/internal/notification/lark_test.go`
  - Verify: Mock 401/403/404 各自 1 次即失敗不重試；Err 含 status code 供 caller 記 audit_logs

- [ ] **TE9** — audit_logs 長期清理策略（6-12 個月後評估 MySQL PARTITION BY RANGE）
  - Files: 設計文件記錄已知限制

## P2 — UI/UX 細化

- [x] **TD1** — DESIGN.md 已完成（色彩 tokens、字體系統、間距、元件語氣）
  - Files: `DESIGN.md`

- [x] **TD2** — 工單佇列空狀態元件（三種 variant: queue / history / search）
  - Files: `frontend/src/components/tickets/EmptyState.tsx`
  - queue variant: 插畫 + 「所有工單已處理完畢」+ 「查看歷史工單」按鈕
  - history / search: 對應情境文案，無 action button
