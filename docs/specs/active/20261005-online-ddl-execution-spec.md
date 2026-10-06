# DDL Online Execution 規格

> 狀態：In progress；O1-O7 已完成，O8 尚未完成
> 適用範圍：MySQL／Aurora MySQL DDL Ticket execution

## 目標

在不改變既有審批、executor resolution、通知與 Native DDL 行為的前提下，讓 DBA 對每一筆合格的 MySQL `ALTER TABLE` statement 選擇 `Native Online DDL`、`gh-ost` 或 `pt-osc`。執行前可使用工具原生 dry-run 並保存參數；執行中提供持久化進度、Pause、Resume、Cancel，並在 gh-ost 原生支援範圍內調整節流參數。

本功能不承諾 application／tool process 重啟後續傳，也不把 process 結束等同於 DDL 未生效。所有終態必須重新檢查 database outcome。

## 已確認決策

1. Pause／Resume 只保證同一 tool process 存活期間有效；重啟後標記 `interrupted`，不自動接管殘留物件。
2. execution mode 與參數以 statement 為單位；不合格 DDL 固定使用 Native。
3. gh-ost 可調整原生 runtime allowlist；pt-osc 啟動後參數唯讀，只提供 Pause／Resume／Cancel。
4. gh-ost、pt-online-schema-change 以固定版本與 checksum 隨 application image 發布，由 Maestro 啟動受控 child process。
5. dry-run 是獨立診斷，不是正式執行的硬性條件；執行失敗由工具結果回報。
6. 只接受 typed parameter allowlist，不接受 raw CLI flags。
7. manual statement Execute 直接攜帶 external mode 與 typed parameters；batch、scheduled 與 `auto_after_approval` 維持既有 Native 行為。
8. 只保存 bounded structured progress；不持久化完整 stdout／stderr。
9. Cancel 採保守 outcome verification，不自動 DROP ownership 或 outcome 不確定的物件。
10. 同一 DB Connection 最多一個 active gh-ost／pt-osc run；Native 不占用此 lock。
11. 沿用既有 `can_execute`／`can_stop` 與 execution ownership，不新增平行 RBAC。
12. SQL 保持原審批內容 immutable；mode、initial parameters 與 runtime tuning 是 DBA execution decision，不重新送審，但必須完整 audit。
13. binary 隨 image 發布；`ddl_ghost_enabled`、`ddl_ptosc_enabled` 保存於平台 Settings 且預設 `false`。關閉只阻止新 run，不中斷 active run。
14. 第一版 Lark 卡片維持現有的工單簡單狀態，不增加 Online DDL mode、progress 或 tool status；後續另案評估。

## 模式能力

| 能力 | Native Online DDL | gh-ost | pt-osc |
|---|---|---|---|
| 適用 DDL | 現行支援的 DDL | 合格的單表 MySQL `ALTER TABLE` | 合格的單表 MySQL `ALTER TABLE` |
| 大表 copy 進度 | 無統一進度 | 支援 | 支援 |
| Pause／Resume | 不新增 | control socket throttle | pause file |
| Runtime tuning | 不新增 | 受控 allowlist | 不支援 |
| Cancel outcome | 沿用現行 stop/recovery | graceful request + database verification | graceful request + database verification |
| 主要限制 | 依 MySQL algorithm/lock | binlog、topology、FK 等限制 | trigger、FK、load 等限制 |

Native 必須執行已核准 SQL 原文，不自動加入或改寫 `ALGORITHM`／`LOCK`。外部工具 adapter 只能從已核准 AST 抽取 database、table 與 alter clause，禁止接受另一份自由文字 SQL。

## 架構與資料流

```text
Approved DDL ticket
        |
        v
statement execution plan
        |
        +-- mode comparison / typed parameters
        +-- permission + DB scope + SQL hash
        +-- optional tool-native dry-run
        |
        v
persistent online DDL run + per-connection lock
        |
        +-- gh-ost adapter ---- control socket
        `-- pt-osc adapter ---- pause file
        |
        v
structured progress / audit / SSE hint
        |
        v
database outcome and artifact verification
```

新增 `internal/onlineddl` domain，負責 parsing boundary、typed config、CLI argument building、dry-run、process lifecycle、progress parsing、control 與 outcome verification。既有 Native SQL execution 保留在原路徑，不搬進 adapter；ticket handler 只負責 authorization、plan/run orchestration 與 response mapping。

## 資料模型與狀態

`ticket_online_ddl_runs` 每個 `ticket_execution` 最多一筆，保存 mode、status、phase、initial/effective parameters、SQL/preflight hash、tool/version、progress、lag、load、ETA、heartbeat、pause/cancel flags、executor、outcome confidence、artifact summary、stable error code、OCC version 與 timestamps。

`ticket_online_ddl_events` 保存 bounded progress samples 與 control event：actor、action、before/after typed values、result、timestamp。不得保存 credential、完整 DSN、完整 command line、preview token、完整 SQL 或未 redacted raw output。

```text
queued -> running <-> paused
                    |
                    +-> cancel_requested
                    +-> completed
                    +-> failed
                    +-> interrupted
                    +-> completed_after_cancel
                    +-> cancelled
                    +-> cancelled_with_artifacts
                    `-> outcome_unknown
```

`queued`、`running`、`paused`、`cancel_requested` 占用 `(connection_id)` active unique lock。Application 啟動時只把遺留 active process 標記 `interrupted` 並執行 read-only artifact/outcome discovery，不自動 resume、DROP 或重新執行。

在現行單 application replica 邊界內，Dry run 與正式 run 另以正規化 `readwrite endpoint:port` 使用 process-local target gate：同一 MySQL server 一次只啟動一個 gh-ost／pt-osc process，不同 server 可並行。正式 run 等待前一個 process 結束，Dry run 遇到忙碌立即回 conflict。gh-ost socket、panic、throttle flag 與 pt-osc pause file、client config 均位於每次 process 的 `0700` work directory。未來若支援多 application replicas，必須先將 target gate 升級為 Meta DB lease。

## 參數與控制

第一版 gh-ost allowlist：`max-load Threads_running`、`critical-load Threads_running`、`chunk-size`、`dml-batch-size`、`nice-ratio`、`max-lag-millis`、`cut-over-lock-timeout-seconds`。除 cut-over timeout 外，只有工具版本證明支援 socket command 的欄位可 runtime update。

第一版 pt-osc allowlist：`chunk-size` 或 `chunk-time`、`max-load Threads_running`、`critical-load Threads_running`、`max-lag`、`check-interval`，以及經 capability 驗證的 `alter-foreign-keys-method` enum。啟動後皆唯讀。

後端必須驗證型別、上下限、互斥欄位與 `critical-load > max-load`。Host、port、database、table、credential、socket、work directory、cleanup、cut-over、recursion、SSL 與 topology flags 由平台產生，不能由 request 覆寫。

O2 固定 `v1` parameter schema：gh-ost defaults 為 max/critical load `10/20`、chunk size `1000`、DML batch `50`、nice ratio `0.2`、max lag `1500ms`、cut-over lock timeout `3s`；範圍依序為 `1-10000`、`2-100000`、`100-100000`、`1-100`、`0-100`、`100-60000ms`、`1-60s`。pt-osc defaults 為 chunk size `1000`、max/critical load `25/50`、max lag/check interval `1s/1s`、foreign key method `none`；chunk size `100-100000` 與 chunk time `0.1-10s` 必須二選一，load 範圍同上，lag/interval 為 `1-60s`，foreign key method 只接受 `none/auto/rebuild_constraints/drop_swap`。

工具 compatibility 由 gh-ost／pt-osc 原生 dry-run 與正式執行結果判定；平台不再另做 metadata capability preflight。受支援 binary version仍由 O3 pinned adapter 固定。

## Dry-run 與安全邊界

- 僅 MySQL／Aurora MySQL writer endpoint、單表 `ALTER TABLE`。
- 重新驗證 authentication、active user、`can_execute`、DB Scope、execution ownership 與 statement pending 狀態。
- 重新解析 SQL 並綁定 approved SQL SHA-256，不接受 request 提供 alter clause。
- 執行前再次檢查 Settings；關閉模式只阻止新 run，active run 的控制 API 保持可用。
- Dry-run 直接使用 approved SQL、readwrite credential 與頁面 typed parameters 執行工具原生 noop／`--dry-run`；結果不限制 Execute。
- Execute 重新驗證 approved SQL identity、typed parameters、Settings mode 與 pinned tool version，並原子保存 queued run；工具當下的 compatibility error 直接成為 run failure，不 fallback Native。
- Password 不得出現在 args/env/log；使用 tool-compatible、`0600`、短生命週期 config，結束後 best-effort shred/delete。
- 使用 `exec.CommandContext`/argv，不經 shell；stdout/stderr 有 byte limit 與 redaction。

## 權限與控制語意

- Plan、Start、Resume、Tune：`can_execute` 且為 execution owner；protected admin 依現行規則介入。
- Pause、Cancel：沿用 `can_stop`，供既有 emergency stop 角色使用。
- Viewer 可看 mode、參數、progress 與 outcome，但不能控制。
- 所有 mutation 使用 status + OCC version，重複／stale command 回 stable `409`。
- 不改 reviewer/executor resolution、Lark 收件人、卡片 stage 或既有簡單狀態內容。

## Progress 與 Cancel

最新 progress 持久化；active run 最多每 2 秒刷新，history 最多每 10 秒一筆並設 retention/cap。欄位包含 phase、copied rows、estimated percent、ETA、replication lag、Threads_running、throttle reason、pause state、tool version 與 heartbeat。SSE 只作刷新提示，GET response/Meta DB 才是真實來源。

Cancel 先要求工具 graceful stop，bounded timeout 後才 termination。Process 結束後回讀原表 DDL，並盤點 ghost/old table、trigger 與 tool metadata：已套用目標 DDL為 `completed_after_cancel`；原表未變且無殘留為 `cancelled`；有殘留為 `cancelled_with_artifacts`；無法證明則 `outcome_unknown`。第一版不提供自動 cleanup mutation。

## API 規劃

```text
POST  /api/tickets/{ref}/online-ddl/dry-run
POST  /api/tickets/{ref}/executions/{executionID}/execute
GET   /api/tickets/{ref}/executions/{executionID}/online-ddl
POST  /api/tickets/{ref}/executions/{executionID}/online-ddl/pause
POST  /api/tickets/{ref}/executions/{executionID}/online-ddl/resume
POST  /api/tickets/{ref}/executions/{executionID}/online-ddl/cancel
PATCH /api/tickets/{ref}/executions/{executionID}/online-ddl/runtime-parameters
```

Manual statement Execute request 可攜帶 external mode/parameters；未提供時保持 Native。Batch 與 scheduled execution 維持 Native。API 使用 stable codes，例如 `mode_disabled`、`unsupported_statement`、`preflight_changed`、`tool_unavailable`、`online_ddl_conflict`、`invalid_tool_parameters`、`control_not_supported`、`stale_control_version`、`tool_process_failed`、`cancelled_with_artifacts`、`outcome_unknown`。

## UI

Ticket execution row 對每個 manual statement 顯示三種模式；Native 預設選取。External mode modal 顯示 typed parameters 與獨立 Dry run，參數只在點 Execute 時送出。Batch 維持 Native。

執行中顯示 phase、進度、ETA、lag、load、throttle/pause 原因、initial/effective parameters、最後 heartbeat 與 artifacts。gh-ost 顯示可修改欄位；pt-osc 顯示唯讀參數。Pause、Resume、Cancel 與 Tune 各有 loading、stale、permission denied、tool unavailable、heartbeat lost 與 outcome unknown states。

Settings 頁面提供 `Enable gh-ost DDL execution`、`Enable pt-osc DDL execution` toggles，預設關閉，並顯示 image 內 binary/version readiness。關閉 active mode 時必須提示「只阻止新執行，不會終止目前工作」。

## 分階段實作與獨立 Gate

### O1：Domain、Settings 與持久化 foundation

狀態：已完成（2026-10-05）。

新增兩個預設關閉的 Settings、run/event migrations、model/repository 狀態機、active connection unique lock、stable errors 與空 adapter interface；不註冊可啟動 tool 的 API。

Gate：migration up/down；Settings read/write permission；repository atomic create、OCC、double claim、lock release、restart interruption、event cap/retention；`go test ./...`、migration smoke、`git diff --check`。

### O2：Statement parser 與 typed parameters

狀態：已完成（2026-10-05）。

從 approved SQL AST 解析單表 ALTER identity/clause，建立三模式 capability matrix 與 versioned defaults/ranges。原 O2 metadata inspector 與短效 token 已由後續 dry-run 決策移除。

Gate：quoted identifiers、explicit database、multi-table/非 ALTER 拒絕、SQL substitution，以及 typed parameter bounds／mutual exclusion。

### O3：Pinned binaries 與 secret-safe adapters

狀態：已完成（2026-10-05）。

Dockerfile 以固定 version/checksum 建置 gh-ost 與 pt-online-schema-change；實作 argv builder、0600 temp config、version detection、bounded/redacted output、process group cleanup 與 fake-binary tests。Native 路徑不改。

Image 固定 gh-ost `1.1.6`（amd64 SHA-256 `5d15547f207e72591fd3a55c9cbea275396880a65a290742287a1ac84d0f4977`；arm64 SHA-256 `12f9d91a77774e85073fdea6bfb26f457424bf65b12043cb330e288231aa3465`）與 Percona Toolkit `3.7.0`（source tarball SHA-256 `cda1058177ad5de4e2c9e8848f3745a911675589599814547f43c4f58a42c464`）。兩個 Dockerfile 使用相同版本與 runtime dependencies。

Adapter 只接受 typed parameters 並直接傳遞 argv，不經 shell。Password 只寫入 `0700` 暫存目錄中的 `0600` client config；args、過濾後的 environment 與回傳 error 不含 password。stdout/stderr 分別硬性限制為 1 MiB 並在回傳前遮罩，timeout/cancel 會終止整個 process group，process 結束後刪除暫存目錄。O3 不註冊 API、不啟動 persistent runner，也不改變 Native execution。

Gate：linux target image build；binary/version health；args/env/log 不含 password；shell metacharacter 無法注入；timeout、oversized output、missing binary、temp cleanup、child process cleanup；image smoke 執行 `--version`。

### O4：Persistent runner 與 lifecycle

狀態：已完成（2026-10-05）。

實作 bounded dispatcher、per-connection serialization、claim/heartbeat、phase/status transition、shutdown/restart interruption與 terminal audit。先只允許 fake adapter 完整跑完，不接真實 database mutation。

Runner 的全域 concurrency 預設為 `2`，同一 connection 同時只執行一筆；repository claim、heartbeat 與 terminal update 都以預期 status 條件更新。啟動時將遺留 active runs 標為 `interrupted`，執行中的 shutdown、executor panic、heartbeat loss 與 executor failure 分別保存 stable terminal status/error code並釋放 connection lock。Terminal audit 採 best effort，失敗不反轉已保存終態。

O4 runner 只依賴抽象 executor 並以 fake executor 驗證，尚未在 server 啟動、未解析真實 credential/SQL、未呼叫 O3 command adapter，也未註冊 API。真實工具 process、progress/control 與 outcome verification 由 O5 接入。

Gate：double executor、double dispatch、different-connection bounded parallelism、same-connection serialization、settings disabled、shutdown、panic、lost heartbeat、audit failure；狀態與 lock 在所有終態一致。

### O5：Progress、Pause／Resume／Cancel 與 outcome verification

狀態：已完成（2026-10-05）。

接入 gh-ost socket、pt-osc pause file、structured parsers、bounded samples、graceful/forced termination、read-only artifact discovery與七種 canonical 終態判定；仍不提供 runtime tuning。原 gate 的「九種終態」與本文件狀態圖及程式 constants 不一致，已以狀態圖實際定義的七種 terminal statuses 為準，不新增無語意來源的狀態。

Managed executor 將 O3 process adapter 接到 O4 lifecycle runner，但仍由未來 O7 注入 approved SQL／credential resolver，尚未 production wiring。執行中 output snapshot 經結構化 parser 擷取 phase、rows、percent、ETA、lag、Threads_running 與 throttle reason；latest update 最快每 2 秒、history 最快每 10 秒，且沿用 1,000 events／30 天上限。

Pause／Resume 對 gh-ost 使用 control socket `throttle`／`no-throttle`，pt-osc 使用 `0600` pause file；Cancel 先走 gh-ost panic flag或 pt-osc SIGTERM，grace timeout 後終止 process group。正式執行成功時由工具本身清除 old table、new table 與 triggers；失敗、取消或中斷時保留 artifacts 供 DBA 人工確認，平台不主動執行 DROP。所有完成與取消都必須經 read-only verifier；artifact inventory 只查 `information_schema`，保存 confidence 與 bounded summary。Restart recovery 先取得 active snapshots，再標記 `interrupted`／釋放 lock，最後只回寫 discovery confidence 與 artifacts。

Gate：progress malformed/truncated、pause/resume idempotency、cancel copy/cut-over race、process exits before/after signal、completed-after-cancel、artifacts、unknown outcome、restart discovery；禁止自動 DROP。

### O6：gh-ost Runtime tuning

狀態：已完成（2026-10-05）。

已實作六項 typed socket command allowlist、range/cross-field validation、effective config OCC update與 before/after audit：`max_load_threads_running`、`critical_load_threads_running`、`chunk_size`、`dml_batch_size`、`nice_ratio`、`max_lag_millis`。`cut_over_lock_timeout_seconds` 維持 startup-only，pt-osc 明確回 `control_not_supported`。

每次 request 只能調整一個欄位，避免 gh-ost 非交易式 socket command 發生部分套用。running／paused 可調整；queued、cancel requested、terminal、inactive process 與 stale version 都在送出命令前拒絕。同一 process 的 control/tuning 以 mutex 串行。socket 成功後以同一交易更新 effective parameters、增加 OCC version 並寫入 bounded `runtime_tune` event；交易或 OCC 失敗時送出反向 typed command，反向命令也失敗則回 `outcome_unknown`。Audit 只接收 typed before/after parameters 且採 best effort，不保存 raw socket output、SQL、DSN 或 credential。

Gate：每個可調欄位 success/boundary、stale version、paused/running/terminal states、socket timeout、工具拒絕時不更新 effective config、concurrent tune、audit redaction。

### O7：Ticket API、workflow 與完整 UI

狀態：已完成（2026-10-05）。

O7-B 將 Settings mode gate、pinned binary version detection 與 run repository 接入 server。Manual statement Execute 攜帶 external mode/parameters，重新確認 SQL／parameters／mode／tool version 後，在同一交易建立 queued run 並將 statement `pending -> running`。Batch、scheduled 與 `auto_after_approval` 維持 Native。

O7-C 將 managed runner 綁定 server lifecycle，resolver 只從持久化 run 反查 approved execution SQL、readwrite credential、typed parameters 與 pinned tool version，並在所有 process path 關閉驗證 DB。正常 exit 0 採與 Native DDL 一致的信任模型，視為 completed；cancel／tool failure 才比較執行前記憶體 `SHOW CREATE TABLE` baseline 與 bounded artifact inventory。Baseline hash 忽略會被正常 DML 推進的 table-level `AUTO_INCREMENT` counter，其餘 DDL 保持參與比對。Application restart 維持 interrupted，只盤點 artifacts，不宣稱成功或自動 resume。Terminal callback 更新 statement，batch 再由既有 execution loop 依序接續下一筆；manual statement 更新 aggregate。Settings toggle 只阻止新 plan/queue，已 queued run 不被中斷。

O7-D 在 Ticket Detail 以 manual statement 為單位提供 Native／gh-ost／pt-osc 模式說明、Settings disabled reason、typed parameters 與獨立 dry-run；mode/parameters 只在點 Execute 時送出。Active run 每 2 秒從 canonical GET 刷新，顯示 phase、progress、ETA、lag、load、throttle、heartbeat、outcome 與 artifacts；Pause／Resume／Cancel 使用最新 OCC version，gh-ost 提供單欄位 runtime tuning，pt-osc 啟動後保持唯讀。

gh-ost／pt-osc 僅支援 `db_type=mysql`。Ticket Detail 對其他 connection type 不提供這兩個模式，後端 dry-run、queue 與 worker resolve 皆重新驗證 connection type；PostgreSQL／Redis 等其他類型維持既有 Native 或各自執行路徑。

註冊 dry-run/control APIs，延伸 manual statement Execute，保持 batch、scheduled 與 `auto_after_approval` Native 行為；完成 statement mode selector、參數、dry-run output、progress、controls 與 Settings toggles。Lark 卡片不在本階段增加 Online DDL 摘要。

Gate：handler permission/ownership/DB scope/direct API bypass；Native regression；manual statement mode/parameters；auto workflow unchanged；前端 dry-run success/failure、direct Execute、disabled/error/stale/poll cleanup/permission/outcome states；`npm run lint`、`npm test`、`npm run build`、`go test ./...`。

### O8：真實工具驗收、故障注入與文件同步

狀態：本機 gate 已完成（2026-10-05）；Aurora Testnet 尚待依驗收手冊執行，因此整體規格仍為 In progress。

建立隔離 MySQL source/replica fixtures，真實執行兩工具的 copy、throttle、pause/resume、cancel、cut-over、artifact discovery；Testnet Aurora 分階段開啟 Settings。同步 API/permissions、Settings、Tickets how-to、Docker image、PROJECT_STATUS 與 runbook。

本機 fixture 已驗證固定版本、ROW/FULL async replica、cut-over、小表／65,536-row copy、concurrent DML、pause/resume、cancel/artifact，以及 credential/network failure。真實測試發現 pt-osc 需要平台固定 `--force` 與 `--recursion-method=hosts`，避免非互動 stdin 失敗及 processlist 回傳 `host:socket` 造成 replica lag polling crash；兩項均由 adapter unit test 鎖定，不開放 raw CLI flag。

Gate：小/大表、concurrent DML、replica lag、metadata lock、FK/trigger、tool crash、app shutdown、network loss、credential rotation、disk/process limits；完整 Go/frontend/image gates。Aurora checklist 必須記錄 engine/version/topology、時間、操作者與結果；未跑不得宣稱功能完成。

## NOT in scope

- 跨 application/tool process 的 checkpoint resume：兩工具沒有可通用證明安全的接管協議。
- PostgreSQL／Redis online schema change。
- 系統自動依 table size 選 mode；mode 必須由 DBA 明確決定。
- 任意 CLI flags、shell command 或使用者指定 credential/target/socket/path。
- 自動清理 outcome/ownership 不確定的 ghost/old table 或 trigger。
- 改變既有 SQL approval、executor resolution、通知收件人或 `auto_after_approval` Native 行為。
- Lark 卡片顯示 Online DDL mode、progress 或 tool status；第一版維持既有簡單工單狀態。
- 第一版多副本 active/active runner；平台仍遵循單副本基線，多副本前需 distributed lease。

## 完成條件

O1-O8 gates 全部通過，Testnet Aurora smoke 有可追溯結果，且 Native DDL、manual statement execution、batch execution、scheduled execution、auto-after-approval、stop/recovery 與 Lark 通知均無回歸，才可將狀態改為 Implemented。
