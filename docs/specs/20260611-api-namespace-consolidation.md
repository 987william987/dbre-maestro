# API Namespace 收斂到 `/api/*`

## 背景

目前前端 SPA 頁面路由與後端 API 共用根路徑，例如：

- 頁面：`/db-connections`、`/audit-logs`、`/tickets`
- API：`/db-connections`、`/audit-logs`、`/tickets`

在本地開發與未來部署中，這會造成瀏覽器直接打頁面、刷新、返回上一頁時，有機會命中後端 JSON API，而不是前端頁面。這不只是 UX 問題，也會讓資料直接暴露在瀏覽器畫面上，安全邊界不清楚，不能上線。

## 目標

把所有後端 HTTP API 全面收斂到 `/api/*`，前端頁面路由維持原樣。

完成後應滿足：

- 瀏覽器直接打任何前端頁面路由，只會得到 SPA 頁面，不會得到後端 JSON。
- 前端所有 API 呼叫統一經由 `/api/*`。
- 後端不再暴露舊的根路徑 API。

## 範圍

包含：

- 後端 router 全量遷移到 `/api/*`
- 前端 API client 與直接 `fetch(...)` 呼叫改為 `/api/*`
- Vite dev proxy 改為只代理 `/api`
- 相關測試更新

不包含：

- 正式環境反向代理 / CDN 設定
- 舊 API 路徑相容層
- 其他頁面展示或資訊文案調整

## 實作要點

1. 後端 `chi` router 以 `/api` 為 namespace 掛載所有既有 API。
2. refresh cookie path 改為 `/api/auth/refresh`，避免登入刷新流程失效。
3. 所有後端回傳的可下載 URL 改為 `/api/...`。
4. 前端 API client 提供統一的 `/api` path 前綴，避免各模組各自拼接。
5. Vite proxy 僅代理 `/api`，頁面路由不再被 proxy 攔截。

## 驗收標準

- 直接打 `/db-connections`、`/tickets`、`/audit-logs` 等頁面路徑時，不會返回 JSON API。
- `/api/db-connections`、`/api/query`、`/api/auth/me` 等 API 正常工作。
- 登入、refresh、logout、setup、audit export、SQL export 仍可正常運作。
- 前後端測試與 build 通過。

## 風險

- 若漏改 refresh cookie path，登入後 token refresh 會失敗。
- 若漏改 download URL，匯出下載會 404。
- 若有前端直接 `fetch('/xxx')` 未納入 API client，也可能殘留舊 namespace。
