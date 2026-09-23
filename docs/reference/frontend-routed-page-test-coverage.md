# 前端 Routed Page 測試覆蓋

本文件是 `frontend/src/App.tsx` 與頁面整合測試的對照表。新增、移除或改變 route 時，必須同步更新本表與對應測試。

## 最低測試標準

頁面整合測試必須 render 真實 page component，並 mock API 或外部邊界。只在 `AppShell` route 中放 placeholder element，不算頁面覆蓋。

| 等級 | 適用頁面 | 最低要求 |
|---|---|---|
| S：靜態 | 不載入 API 的說明頁 | 可 render、關鍵內容、主要導覽或互動 |
| R：唯讀非同步 | 列表、dashboard、詳情頁 | loading 完成後的成功或 empty 狀態、API 失敗狀態、至少一個核心篩選／分頁／導覽互動 |
| W：可寫入 | 表單、管理頁 | R 全部要求、主要 mutation 成功、mutation 失敗、read-only 或 permission 行為 |
| X：狀態密集 | SQL Editor、工單流程等 | W 全部要求，加上跨 tab／route／非同步競態、取消或重試，以及已發生過的回歸案例 |

測試應驗證使用者可觀察的結果與送出的 API payload，不依賴 CSS class、內部 state 或 implementation-only function。每個已修復的頁面回歸都必須留下可在回歸時失敗的測試。

## Route 覆蓋盤點

狀態說明：`達標` 表示現有測試已涵蓋該等級的核心風險；`補強` 表示已有頁面測試但仍缺最低案例；`缺少` 表示沒有真實 page component 整合測試。

| Route | Page component | 等級 | 現有測試 | 狀態／缺口 |
|---|---|---:|---|---|
| `/login` | `LoginPage` | W | `LoginPage.test.tsx` | 達標 |
| `/setup` | `SetupWizard` | W | 無 | 缺少：setup status、validation、建立成功與 API 失敗 |
| `/dashboard` | `DashboardPage` | R | 僅 API／chart component test | 缺少：頁面成功、失敗與 personal/platform 分支 |
| `/account/access-scopes` | `AccessScopesPage` | R | 無 | 缺少：成功、失敗、搜尋／renew 導覽 |
| `/account/sessions` | `SessionsPage` | W | 無 | 缺少：成功、失敗、單筆與全部 revoke |
| `/tickets` | `TicketsPage` | R | `TicketsPage.test.tsx` | 達標 |
| `/tickets/:id` | `TicketDetailPage` | X | `TicketDetailPage.test.tsx` | 達標 |
| `/tickets/new` | `NewTicketPage` | X | `NewTicketPage.test.tsx` | 達標 |
| `/users` | `UsersPage` | W | `UsersPage.test.tsx` | 達標 |
| `/users/groups` | `UsersPage` | W | `UsersPage.test.tsx` | 達標 |
| `/users/resources` | `UsersPage` | R | `UsersPage.test.tsx` | 達標 |
| `/users/query-access` | `UsersPage` | W | `UsersPage.test.tsx` | 補強：mutation 失敗案例 |
| `/sql-editor` | `SQLEditorPage` | X | `SQLEditorPage.test.tsx` | 達標；涵蓋 AppShell route 切換、navigation render-loop 與 Filter Columns 回歸 |
| `/scheduled-sql-reports` | `ScheduledSQLReportsPage` | W | 無 | 缺少：成功、失敗、建立與 read-only 行為 |
| `/db-connections` | `DBConnectionsPage` | W | `DBConnectionsPage.test.tsx` | 達標 |
| `/db-connections/:id/overview` | `DBConnectionDetailPage` | R | `DBConnectionDetailPage.test.tsx` | 達標 |
| `/db-connections/:id/databases` | `DBConnectionDetailPage` | R | `DBConnectionDetailPage.test.tsx` | 補強：API 失敗狀態 |
| `/db-connections/:id/accounts` | `DBConnectionDetailPage` | R | `DBConnectionDetailPage.test.tsx` | 補強：API 失敗狀態 |
| `/db-metadata/inventory` | `DBMetadataInventoryPage` | R | `DBMetadataPages.test.tsx` | 補強：API 失敗狀態 |
| `/db-metadata/objects` | `DBMetadataObjectsPage` | R | `DBMetadataPages.test.tsx` | 補強：API 失敗狀態 |
| `/masking-rules` | `MaskingRulesPage` | W | `MaskingRulesPage.test.tsx` | 補強：載入與 mutation 失敗案例 |
| `/masking-rules/dsl-guide` | `MaskingDSLGuidePage` | S | `MaskingDSLGuidePage.test.tsx` | 達標 |
| `/sql-review-rules/:engine` | `SQLReviewRulesPage` | W | `SQLReviewRulesPage.test.tsx` | 補強：載入與 mutation 失敗案例 |
| `/audit-logs` | `AuditLogsPage` | R | `AuditLogsPage.test.tsx` | 補強：API 失敗與 export 行為 |
| `/settings/workflow` | `SettingsPage` | W | `SettingsPage.test.tsx` | 補強：儲存成功與失敗 |
| `/settings/scans` | `SettingsPage` | W | `SettingsPage.test.tsx` | 補強：儲存成功與失敗 |
| `/settings/query-execution` | `SettingsPage` | W | `SettingsPage.test.tsx` | 補強：儲存成功與失敗 |
| `/settings/integrations` | `SettingsPage` | W | `SettingsPage.test.tsx` | 補強：儲存成功與失敗 |

## Redirect 與 Guard 覆蓋

`/`、`/sql-review-rules`、`/settings` 與 catch-all route 是導向行為，不套用 page component 標準，但仍須各有 route integration test。目前 `app/router/guards.test.tsx` 已覆蓋登入與權限 guard；上述四個 redirect 尚待補測。`AppShell.test.tsx` 驗證導覽呈現與 route 切換，不取代上表的頁面測試。

## 維護規則

1. 新增 route 時，先在本表指定等級，再新增對應整合測試。
2. 修正頁面回歸時，先建立可重現失敗的測試，再修正實作。
3. 測試只有在意圖重複、被較高層測試完整取代或對應功能已刪除時才能移除。
4. 不以測試檔案年齡作為刪除理由；舊測試仍能保護有效行為時必須保留。
5. 清理後必須執行 `npm run lint`、`npm test` 與 `npm run build`。
