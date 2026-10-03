# 專案目前狀態

> 最後更新：2026-10-03
> 本文件提供新 session 的快速上下文，不取代程式碼、reference 文件或 Git history。

## 專案目的

DBRE Maestro 是資料庫治理平台，集中管理 SQL 查詢、DDL／DML／Redis 工單、權限與審批、敏感資料遮罩、資料庫連線、Metadata、通知與稽核。前端是 React + Vite + TypeScript，後端是 Go；正式交付由根目錄 `Dockerfile` 產生單一 application image。

## 目前基線

- 前端 ESLint 已導入 type-aware 設定，涵蓋 `tsconfig.app.json` 與 `tsconfig.node.json`。
- `npm run lint`：0 errors、0 warnings。
- `npm test`：36 test files、334 tests 全部通過。
- `npm run build`：通過；Vite 仍有既有的單一 chunk 超過 500 kB warning。
- 所有 30 個 render routes 已達到目前功能階段對應的 routed page 最低整合測試標準。
- `/`、`/settings`、`/sql-review-rules` 與 catch-all redirect 已有 route coverage。
- `AppErrorBoundary` 測試會刻意將 `NotFoundError` stack trace 寫到 stderr；suite 通過時不是測試失敗。

上述數字是 2026-10-03 在 Table Schema Management S8 完成時驗證的基線。新增或刪除測試後必須更新，不應永久假設數字不變。

## 最近完成

- 移除未掛 route 的舊 Export approve／reject handlers，縮小誤用面。
- 同步 backend API 與 permission reference，使其符合實際 routes。
- 導入前端 ESLint，修正 conditional hooks、floating promises 與 exhaustive dependencies。
- 修正 SQL Editor 離開頁面時的 render loop，以及 Filter Columns 無反應問題。
- 補齊 routed page 的 success、error、mutation、redirect 與已知回歸 coverage。
- 清查舊前端測試；沒有刪除仍具獨立意圖的案例，只合併重複的 AppShell route fixtures。
- MySQL DDL shadow validation 已具備 credential-aware readonly connection pool；功能旗標 `DB_SHADOW_READONLY_POOL_ENABLED` 預設關閉，需先在 Testnet 啟用並觀察 timing 後再推至 Production。
- 前端已支援 Light／Dark／System，並以 semantic tokens 統一路由頁面、共用 UI、圖表與一般 CodeMirror editor；Admin Query Console 刻意維持固定 one-dark。
- 色彩主題已支援 Default／Ocean／Forest／Amber／Amethyst，與 Light／Dark／System 獨立保存並可任意組合。
- 已修正 theme migration 期間的 light sidebar 樣式回歸，以及 Dashboard 行動版 KPI 卡片被內容最小寬度裁切的問題。
- MySQL Binlog Export 已完成 T1-T7：共用 `my2sql` adapter、job/artifact schema、range snapshot、受控 worker、scoped API、含 preview／expiry／timestamp probe 的前端頁面，以及 runner、worker、repository、handler security 與 routed-page 自動測試。真實 MySQL integration fixture 屬 T8 驗收資產，目前已提前完成 Position／Time snapshot 驗證。
- Binlog timestamp probe 已改為前端逐檔串行、後端以 Go MySQL replication client 只讀取第一個 event 時間；不依賴 `mysqlbinlog` 或 MariaDB runtime。實測 Aurora 單檔約 0.84–1.01 秒，單檔 timeout 不會因 inventory 檔案數增加而擴大。
- Session Management S1 foundation 已完成：目標 route 為 `/dba-tools/sessions-management`；已加入獨立 operations credential、DB Connection UI/API 支援，以及 `read`／`kill`／`loop_kill` 三級權限。
- Session Management S2 已完成：後端會依 DB Connection engine 與 Settings 內的 AWS regions 即時呼叫 RDS／ElastiCache API；cluster 與 topology 回應必須由 connection 的 readonly/readwrite endpoint 證明 ownership。自訂 CNAME 或非 AWS target 不回退 inventory snapshot，後續 session 操作必須走每次套用 host/CIDR policy 的 manual target validation。
- Session Management S3 已完成：MySQL processlist、PostgreSQL `pg_stat_activity` 與 Redis `CLIENT LIST` 皆透過獨立 operations credential 讀取；AWS physical node 由 Session Management topology snapshot 解析，manual target 每次重驗 host policy。`/dba-tools/sessions-management` 已提供 target selection、手動 refresh、filter、truncation 與 protected session 顯示。
- Session Management S4 已完成：MySQL／PostgreSQL 支援單筆 Cancel Query 與 Terminate Session，Redis 支援 Disconnect Client。Destructive action 使用 AWS topology snapshot 或重新套用 manual host policy，再讀取 session 比對 identity；PostgreSQL 額外比對 `backend_start`。protected session 一律拒絕，結果寫入不含原始 SQL 的 audit。
- Session Management S5 已完成：MySQL／PostgreSQL 支援 database、SQL prefix 與 minimum age 限定的 preview／一次性批次 Cancel Query。60 秒 token 綁定 actor、connection、target 與 preview identity snapshot；執行時從 topology snapshot 解析 target 並重新驗證每筆 session，preview 後新出現或 identity 改變的 session 不會被取消。Token store 有數量上限且不保留 raw SQL。
- Session Management S6 已完成：loop-kill job 持久化於 Meta DB，以 `db_sessions.loop_kill` 控制建立／停止；具 interval、duration、max kills、同 physical node 單一 active job 與連續三次錯誤停止限制。Worker 每輪重驗 connection、operations credential，並從 topology snapshot 解析 target；原始 prefix 僅加密保留到終態，server restart 會把 running job 標記 interrupted 而不恢復。
- Session Management S7 已完成：頁面提供手動／2／5／10／30 秒刷新、hidden tab／離頁／target 切換時的 timer 與 request cleanup、session filters 與 client-side pagination、prefix preview／一次性 cancel、loop job 建立／列表／停止，以及 Redis／read-only／protected／truncated／empty／error states。
- Session Management S8 已完成：新增隔離 MySQL 8／PostgreSQL 16／Redis 7 integration fixtures，實際驗證 session list 與 cancel／terminate／disconnect，且只 signal 測試自行記錄 ID 的 session；安全 unit tests、Go／前端完整 gate 與 canonical 文件均已同步。
- Session Management 只在頁面首次載入或使用者手動 Refresh clusters 時掃描 AWS inventory；選 cluster、讀取 sessions 與 destructive action 共用該 snapshot。MySQL／PostgreSQL 的 session identity revalidation 與 signal 共用受控 operations credential pool，並輸出 target、session、signal、audit 與總耗時的 structured log；signal 前的 session identity 防誤殺檢查維持不變。
- Lark 工單互動卡片會保存成功投遞的 message ID，並依 review／execution stage 非同步同步原卡片狀態；原有 reviewer／executor recipient resolution 與重疊角色收到兩張卡的邏輯不變，卡片更新失敗不影響工單交易。
- Table Schema Management S1 foundation 已完成：新增 `table_schemas.read`／`table_schemas.sync` 權限、`internal/tableschema` limits/types/stable errors、受權限保護的 API skeleton，以及 `/dba-tools/table-schemas` route、導航與靜態頁面測試。
- Table Schema Management S2 已完成：以 readonly credential 與 metadata pool 取得 deterministic `SHOW CREATE TABLE` snapshot；SQL-aware lexer/scanner 只改寫 table identifier 與 AUTO_INCREMENT／engine／charset／collation／row format；capability discovery 由 MySQL server 實際回報。MySQL 5.7／8.0、generated expression、quoted content、partition executable comment 與 fuzz coverage 已加入。
- Table Schema Management S3 已完成：啟用 scoped MySQL connection、database 與 base-table metadata APIs，回傳來源 table options 與 foreign key dependencies；dependency analyzer 支援 deterministic DAG order、external dependency、跨 schema reference 與精準 cycle detection。
- Table Schema Management S4 已完成：Export preview 與直接 `.sql` download 共用 scoped readonly pipeline，依 dependency order 產生 deterministic DDL，支援整批 transformation 與 external dependency warning；request、timeout、單表／整批輸出大小、下載檔名與 audit redaction 均有界。Sync routes 在 S4 尚未啟用，現已由 S5-S6 接通；真實 MySQL 5.7／8.0 建表驗收已於 S8 完成。
- Table Schema Management S5 已完成：Sync preview 強制 Source readonly、Target readwrite 與雙邊 DB Scope，執行 target capability、fresh-table conflict 與 external dependency preflight；60 秒 bounded token 綁定 actor、完整 request、source DDL、dependency 與 target snapshot，僅能消耗一次。S6 建立 job 前必須透過 `RevalidateSyncPreview` 重跑完整 preflight，target conflict、missing dependency、source/capability drift 與 target scope/readwrite boundary 均有專項測試。
- Table Schema Management S6 已完成：新增 `090_table_schema_sync_jobs` migration、job/item model 與 repository 狀態機，具同 Target Connection/Database active unique lock、原子 job/items 建立、conditional claim、queued/running cancel、fail-fast item transitions、terminal lock release 與 restart interrupt。bounded worker 依 dependency order 執行、重驗 source DDL、CREATE 後回讀 target hash，並提供 cooperative cancel、持久化終態 structured timing 與 best-effort terminal audit；create/list/detail/cancel lifecycle API 具雙邊 scope 與 ID disclosure 防護。獨立 MySQL 8 fixture 已驗證 dependency order、partial success、preview 後 target race、cancel during blocked DDL 與 parent timeout；fixture 曾抓出 `CREATE` 失敗但因既有 table 可回讀而誤標 created 的 ownership bug，現已限制只有成功執行或 context 結果不確定時才採用回讀判定。
- Table Schema Management S7 已完成：retry API 只接受 failed／cancelled／interrupted job，並重新驗證 source DDL、dependency、external dependency、target capability、transformation 與 prior-created table ownership/hash；安全時只建立剩餘 items 的 linked job。Job list 具雙邊 connection scope、server-side count/pagination；UI 已完成 Export/Sync、searchable selection、transformation、preview/download、sync confirmation、job history/detail/cancel/retry、active polling 與 stale request cleanup。整合測試涵蓋 success、permission、empty、partial failure、cancel、retry drift、stale response、target conflict 與 dependency cycle。
- Table Schema Management S8 已完成本機驗收與文件同步：隔離 MySQL 5.7／8.0 fixture 驗證 mixed options、generated columns、indexes、foreign keys、partition，以及同 instance／跨 instance sync preflight；Aurora Testnet 尚未在本機驗證，操作 checklist 已列入使用手冊。

## 已知限制與尚未接入項目

- 根目錄 `make lint` 目前只執行 Go lint，尚未納入前端 ESLint。
- application image build 目前只執行前端 build，尚未把 ESLint 作為 image build gate。
- 尚未導入瀏覽器 E2E 與視覺回歸測試；目前決定延後，不是遺漏。
- 平台目前以單副本部署為前提；多副本限制見 [工程待辦](TODOS.md)。

## 下一步

MySQL Binlog Export、Session Management S1-S8 與 Table Schema Management S1-S8 均已完成；Table Schema Management 尚待 Aurora Testnet smoke checklist 的環境驗收，現行基線見 [active spec](specs/active/20261002-table-schema-management-spec.md)。

1. 在 Aurora Testnet 執行 Table Schema Management smoke checklist 並保存驗收結果。
2. 評估將前端 lint gate 接入 Makefile 與 application image build。
3. 需要更高 UI 信心時，再分階段導入 Playwright E2E 與視覺回歸。
4. 長期技術債與部署準備事項見 [工程待辦](TODOS.md)。

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
