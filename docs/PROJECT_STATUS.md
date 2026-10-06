# 專案目前狀態

> 最後更新：2026-10-05
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
- DDL Online Execution O1 foundation 已完成：新增預設關閉的 `ddl_ghost_enabled`／`ddl_ptosc_enabled` Settings、run/event migrations、`internal/onlineddl` 狀態機與 stable errors、空 adapter interface，以及具 OCC、同 connection active lock、restart interruption、bounded event retention 的 repository；尚未註冊 tool execution API，也未改變 Native DDL 路徑。
- DDL Online Execution O2 已完成：TiDB AST 只接受單筆 MySQL `ALTER TABLE`，從 approved SQL 抽取 table identity/alter clause 並綁定原文 SHA-256；`v1` typed parameters 固定 defaults/ranges/互斥規則。原 metadata inspector 與 preflight token 已移除，compatibility 改由工具原生 dry-run／正式執行判定。
- DDL Online Execution O3 已完成：正式與本機 backend image 以 checksum 固定 gh-ost `1.1.6` 與 Percona Toolkit `3.7.0`；secret-safe adapters 使用 typed argv、0600 暫存 client config、過濾 environment、bounded/redacted output、version detection 與 process-group timeout cleanup。兩個 image 均通過真實 binary/runtime smoke；尚未註冊 API、persistent runner 或改變 Native DDL 路徑。
- DDL Online Execution O4 已完成：新增全域 concurrency 2、同 connection 串行的 persistent lifecycle runner，以及 conditional claim、heartbeat、phase/terminal persistence、startup recovery、shutdown interruption、panic/heartbeat/error handling 與 best-effort terminal audit。所有 lifecycle 由 fake executor 驗證；runner 尚未在 server 啟動，也未呼叫真實工具或資料庫 mutation。
- DDL Online Execution O5 已完成：managed executor 已串接受控 process、gh-ost socket pause/resume、pt-osc pause file、graceful/forced cancel、真實格式 progress parser與 2 秒 latest／10 秒 history persistence；所有 process exit 都必須經 read-only DDL/artifact verifier，七種 canonical 終態會原子保存 confidence 與 artifact summary。Restart discovery 保持 `interrupted` 並只回寫查核結果，不自動 resume 或 DROP。O5 階段尚未 production wiring、註冊 API 或提供 runtime tuning。
- DDL Online Execution O6 已完成：gh-ost 支援六項單欄位 typed runtime tuning，具 range/cross-field validation、per-process control serialization、OCC effective config、原子 before/after event 與持久化失敗反向補償；補償失敗回報 `outcome_unknown`。pt-osc 與 startup-only 參數明確不支援 runtime tuning；尚未 production wiring 或註冊 API。
- DDL Online Execution O7-A 已完成 handler contract：dry-run、read、pause、resume、cancel、runtime tuning routes 會重驗 ticket/execution 綁定、workflow eligibility、DB Scope、execution owner 與 OCC body，並使用 stable error mapping。
- DDL Online Execution O7-B 已完成 production coordinator 與 queue wiring：dry-run 直接執行工具原生 noop／`--dry-run`；manual statement Execute 直接攜帶 mode 與 typed parameters，external run 建立與 statement claim 同交易。Native、batch、scheduled 與 `auto_after_approval` 維持既有 Native 行為。
- DDL Online Execution O7-C 已完成 managed runner production wiring：resolver 綁定 approved SQL、readwrite credential、typed parameters與持久化 tool version；正常 exit 0 與 Native 一樣信任成功結果，cancel／失敗則以忽略 AUTO_INCREMENT counter 的執行前 DDL baseline 加 artifact inventory 分類。Terminal 會更新 execution 並依序接續 mixed batch；graceful shutdown 保存 interrupted/artifact evidence，restart 不 resume。Settings 關閉只阻止新 plan/queue，不中斷已 queued run。
- DDL Online Execution O7-D 已完成 Ticket Detail 與 Settings UI：每筆 manual DDL statement 可選 Native／gh-ost／pt-osc，external mode 具 typed parameters 與獨立 Dry run，點 Execute 時才送出 mode/parameters；active run 具 2 秒 polling、進度/outcome/artifacts、Pause／Resume／Cancel 及 gh-ost runtime tuning。Settings 顯示 pinned binary readiness/version。
- DDL Online Execution O8 本機 gate 已完成：隔離 MySQL 8 source/replica fixture 真實驗證 gh-ost／pt-osc 的 cut-over、小表／65,536-row copy、concurrent DML、Pause／Resume、Cancel/artifacts 與 credential/network failure。fixture 發現並修正 pt-osc 非互動確認與 replica discovery 問題；Aurora Testnet checklist 尚未執行，功能仍不得標為完整 Implemented。
- Online DDL process isolation 已補強：gh-ost throttle/socket/panic 與 pt-osc pause/config 使用每個 process 的獨立 work directory；現行單 replica application 以正規化 readwrite endpoint/port 串行同一 MySQL server 的 Dry run 與正式 run，避免多工單、重複 connection 或多 statement 的 tool files、replication client 與 database artifacts 互相干擾。多 application replica 前必須改為 Meta DB lease。
- DDL Online Execution 第一版不在 Lark 卡片顯示 mode、progress 或 tool status；既有收件、stage 與簡單工單狀態維持不變，後續需求已移至工程待辦。

## 已知限制與尚未接入項目

- 根目錄 `make lint` 目前只執行 Go lint，尚未納入前端 ESLint。
- application image build 目前只執行前端 build，尚未把 ESLint 作為 image build gate。
- 尚未導入瀏覽器 E2E 與視覺回歸測試；目前決定延後，不是遺漏。
- 平台目前以單副本部署為前提；多副本限制見 [工程待辦](TODOS.md)。

## 下一步

MySQL Binlog Export、Session Management S1-S8 與 Table Schema Management S1-S8 均已完成；Table Schema Management 尚待 Aurora Testnet smoke checklist 的環境驗收，現行基線見 [active spec](specs/active/20261002-table-schema-management-spec.md)。

1. 依 [Online DDL 驗收手冊](how-to/verify-online-ddl-execution.md) 執行 Aurora Testnet 分階段驗收並保存可追溯結果。
2. 在 Aurora Testnet 執行 Table Schema Management smoke checklist 並保存驗收結果。
3. 評估將前端 lint gate 接入 Makefile 與 application image build。
4. 需要更高 UI 信心時，再分階段導入 Playwright E2E 與視覺回歸。
5. 長期技術債與部署準備事項見 [工程待辦](TODOS.md)。

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
