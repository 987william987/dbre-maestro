# AGENTS.md

本檔是 DBRE Maestro 所有 coding agent 的共同工作規則。除非使用者明確覆寫，這些規則適用於整個 repository。

## Session 啟動

處理非 trivial 任務前：

1. 讀取 `docs/PROJECT_STATUS.md`，掌握目前基線、已知限制與下一步。
2. 執行 `git status --short` 並查看近期相關 commits，避免覆蓋既有工作。
3. 依下方「文件路由」只讀取與任務相關的 canonical 文件。
4. 修改前讀取 exports、直接 callers 與共用 utilities。
5. 明確定義成功條件，完成後依風險執行驗證。

如果使用者要求延續尚未完成的上一輪工作，且目前環境提供 `context-restore`，優先用它恢復短期 session context。不要以 checkpoint 取代 Git 與 repository 文件。

## 文件路由

- 所有非 trivial 工作：`docs/PROJECT_STATUS.md`
- 尋找模組與責任邊界：`docs/explanation/project-map.md`
- 架構變更：`docs/explanation/architecture-overview.md`
- UI 或前端設計：`docs/explanation/ui-design-guidelines.md`
- 選擇或規劃後續工程工作：`docs/TODOS.md`
- API 或權限：`docs/reference/backend-api-and-permissions.md`
- 前端 routed page 測試：`docs/reference/frontend-routed-page-test-coverage.md`
- 其他領域文件：先看 `docs/README.md`

`docs/reference/` 描述現行行為；`docs/specs/active/` 是仍可延用的設計基線；`docs/specs/archive/` 只保留歷史脈絡。`docs/explanation/ui-design-guidelines.md` 是 UI 目標態，不是現有功能清單。

## 工作原則

1. **先思考再寫程式**：明確說明假設；存在會影響結果的歧義時先提出，不猜測。
2. **保持簡單**：只寫解決目前問題所需的最少程式碼，不預做功能，不為單次使用建立抽象。
3. **精準修改**：只動必要檔案，不順手重構、不改無關格式，並遵循既有風格。
4. **以目標驅動**：先定義成功條件，持續驗證直到達成；不要只機械執行步驟。
5. **模型只做判斷工作**：分類、撰寫、摘要與抽取可交給模型；routing、retry 與 deterministic transform 應由程式處理。
6. **遵守 token budget**：單一任務 4,000 tokens、單一 session 30,000 tokens；接近上限時先摘要並另開 session，不得默默超支。
7. **顯示衝突**：遇到矛盾模式時選擇較新或測試較完整的一方，說明理由並標記另一方待清理。
8. **先讀再寫**：修改前理解 exports、直接 callers 與共用 utilities；不確定結構原因時先查明。
9. **測試表達意圖**：測試必須說明行為為何重要，且在業務行為回歸時確實失敗。
10. **重要步驟後 checkpoint**：說明已完成、已驗證與剩餘工作；無法清楚描述狀態時先停止並重新盤點。
11. **遵循 codebase 慣例**：一致性優先於個人偏好；認為慣例有害時提出，不自行建立另一套。
12. **明確揭露未完成項目**：有跳過、未驗證或不確定的內容就直接說明，不得宣稱全部完成或測試全過。
13. **溝通語言**：永遠以繁體中文和 William 溝通；產生的 PRD、spec、架構與其他文件使用中文。

## Skill Routing

只在目前環境確實提供對應 skill 時使用：

- 產品構想：`office-hours`
- 策略或範圍：`plan-ceo-review`
- 架構規劃：`plan-eng-review`
- 設計規劃或視覺檢查：`design-consultation`、`plan-design-review`、`design-review`
- Bug 與錯誤調查：`investigate`
- 瀏覽器 QA：`qa` 或 `qa-only`
- Code review：`review`
- 發版、部署或 PR：`ship` 或 `land-and-deploy`
- 儲存／恢復短期進度：`context-save`、`context-restore`
- 建立可執行 spec：`spec`

## 驗證原則

- 驗證範圍依修改風險決定，至少執行直接相關測試。
- 前端共用行為或 routed page 變更完成後，執行 `npm run lint`、`npm test` 與 `npm run build`。
- 後端共用行為完成後，執行相關 package tests；跨 package 變更執行 `go test ./...`。
- 完成前執行 `git diff --check`，並說明任何既有 warning 或刻意未執行的檢查。
- 不因測試年代久就刪除；只有行為已不存在、測試無法捕捉回歸，或已被較高層測試完整取代時才移除。

## 文件維護

- milestone、驗證基線、已知限制或近期優先順序有實質改變時，更新 `docs/PROJECT_STATUS.md`。
- 長期技術債或未排程工作更新 `docs/TODOS.md`。
- 現行 API、權限或使用方式改變時，同步更新對應 `docs/reference/`。
- 不把完整架構複製進狀態文件；以連結指向 canonical 文件。
