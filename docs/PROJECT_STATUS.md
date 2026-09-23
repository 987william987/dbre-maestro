# 專案目前狀態

> 最後更新：2026-09-23  
> 本文件提供新 session 的快速上下文，不取代程式碼、reference 文件或 Git history。

## 專案目的

DBRE Maestro 是資料庫治理平台，集中管理 SQL 查詢、DDL／DML／Redis 工單、權限與審批、敏感資料遮罩、資料庫連線、Metadata、通知與稽核。前端是 React + Vite + TypeScript，後端是 Go；正式交付由根目錄 `Dockerfile` 產生單一 application image。

## 目前基線

- 前端 ESLint 已導入 type-aware 設定，涵蓋 `tsconfig.app.json` 與 `tsconfig.node.json`。
- `npm run lint`：0 errors、0 warnings。
- `npm test`：32 test files、268 tests 全部通過。
- `npm run build`：通過；Vite 仍有既有的單一 chunk 超過 500 kB warning。
- 所有 28 個 render routes 已達到 routed page 最低整合測試標準。
- `/`、`/settings`、`/sql-review-rules` 與 catch-all redirect 已有 route coverage。
- `AppErrorBoundary` 測試會刻意將 `NotFoundError` stack trace 寫到 stderr；suite 通過時不是測試失敗。

上述數字是 2026-09-23 在 commit `07d1b006` 驗證的基線。新增或刪除測試後必須更新，不應永久假設數字不變。

## 最近完成

- 移除未掛 route 的舊 Export approve／reject handlers，縮小誤用面。
- 同步 backend API 與 permission reference，使其符合實際 routes。
- 導入前端 ESLint，修正 conditional hooks、floating promises 與 exhaustive dependencies。
- 修正 SQL Editor 離開頁面時的 render loop，以及 Filter Columns 無反應問題。
- 補齊 routed page 的 success、error、mutation、redirect 與已知回歸 coverage。
- 清查舊前端測試；沒有刪除仍具獨立意圖的案例，只合併重複的 AppShell route fixtures。

## 已知限制與尚未接入項目

- 根目錄 `make lint` 目前只執行 Go lint，尚未納入前端 ESLint。
- application image build 目前只執行前端 build，尚未把 ESLint 作為 image build gate。
- 尚未導入瀏覽器 E2E 與視覺回歸測試；目前決定延後，不是遺漏。
- 平台目前以單副本部署為前提；多副本限制見 [工程待辦](TODOS.md)。

## 下一步

目前沒有進行中的功能修改。後續工作依需求選擇：

1. 評估將前端 lint gate 接入 Makefile 與 application image build。
2. 需要更高 UI 信心時，再分階段導入 Playwright E2E 與視覺回歸。
3. 長期技術債與部署準備事項見 [工程待辦](TODOS.md)。

## Canonical 文件

- 模組位置與修改入口：[專案導覽](explanation/project-map.md)
- 系統邊界與資料流：[架構總覽](explanation/architecture-overview.md)
- UI 目標態：[UI 設計規範](explanation/ui-design-guidelines.md)
- 現行 API 與權限：[後端 API 與權限對照](reference/backend-api-and-permissions.md)
- 前端測試標準：[前端 Routed Page 測試覆蓋](reference/frontend-routed-page-test-coverage.md)
- 文件分類與完整索引：[文件總覽](README.md)
- 尚未排程的工程工作：[工程待辦](TODOS.md)

## Session 交接

- 已提交的事實以 Git history 與 canonical 文件為準。
- 尚未完成的短期工作可在 session 結束時使用 `context-save` 保存，新 session 使用 `context-restore` 恢復。
- checkpoint 是本機交接資料，不可取代 repository 內的狀態與 reference 文件。
