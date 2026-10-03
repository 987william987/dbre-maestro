# Table Schema Management 規格

> 狀態：Implemented；S1-S8 已完成，Aurora Testnet smoke checklist 尚待環境驗收
> UI route：`/dba-tools/table-schemas`

## 目標

提供 DBA 一個受控的 MySQL／Aurora MySQL table schema 工作區，可以一次選擇多張實體 table，直接預覽或下載結構 SQL，並把相同結構建立到另一個受 Maestro 管理的 Database。

本功能只處理 table schema，不讀取、導出或同步 table data，也不是通用 schema migration 工具。第一版不修改既有目標表，不產生 `ALTER TABLE`，不執行 `DROP TABLE`。

## 已確認決策

- 僅支援 MySQL／Aurora MySQL 實體 table；不包含 view、trigger、routine、event。
- 每次選擇一個 Source Connection、一個 Source Database 與最多 50 張 table。
- Schema Export 同步產生 preview 或 `.sql` download，不建立 job，也不保存 server-side artifact。
- Schema Sync 使用持久化 background job，提供逐表狀態、cancel、retry 與 audit。
- Source 與 Target 都必須是 Maestro 管理的 DB Connection；支援同 Connection 跨 Database 及跨 Connection。
- Source 強制使用 readonly credential；Target 強制使用 readwrite credential。
- 目標 table 保留來源 table name，不支援逐表 rename。
- Fresh sync 發現任一目標 table 已存在時整批拒絕，不自動 `ALTER`、`DROP` 或覆蓋。
- 未選取的 foreign key dependency 不會自動加入；Sync 前要求它已存在於 Target Database。
- 第一版拒絕循環 foreign key dependency，不以 `FOREIGN_KEY_CHECKS=0` 繞過。
- Sync 依 dependency order 執行，第一張失敗即停止；保留已建立 table，不自動補償刪除。
- Transformation 為整批共用設定，預設逐表跟隨來源。
- 具備專用權限的 DBA 可直接操作，不進入 Ticket Workflow。

## 不在範圍內

- table data export、data sync、row count 或 table size gate。
- database-level dump、完整 `mysqldump` 相容格式與 MySQL Shell Dump & Load。
- existing table schema diff migration、`ALTER TABLE` 規劃或 online DDL。
- table rename、逐表 transformation override、自動展開 dependency tree。
- view、trigger、procedure、function、event、user、grant。
- PostgreSQL、Redis 或其他 engine。

## 架構與資料流

```text
React Table Schemas
        |
        +-- Export preview/download (synchronous)
        |       |
        |       v
        |   Schema Service --readonly--> Source MySQL
        |       |
        |       `--> streamed .sql response (not persisted)
        |
        `-- Sync preview
                |
                +-- source DDL/dependency snapshot
                +-- target capability/existence checks
                `-- short-lived one-time token
                            |
                            v
                       Sync Job Repo
                            |
                            v
                       Bounded Worker
                  readonly | readwrite
                    Source | Target
```

建立獨立 `internal/tableschema` domain，重用既有 DB Connection repository、credential resolution、host policy、DB scope、audit 與 background worker 慣例，但不把功能塞入 `binlogexport`。Schema Export 與 Sync 共用 DDL snapshot、dependency analysis 與 transformation engine。

## Schema 取得與轉換

來源結構使用 `SHOW CREATE TABLE` 取得。Foreign key dependency 使用 `information_schema.KEY_COLUMN_USAGE` 讀取，不從 DDL 文字猜測。

DDL transformation 必須使用能辨識 quoted identifier、string、comment、括號層級與 table option boundary 的 scanner。禁止以全域字串取代或一般 regex 改寫 database/table/options，以免誤改欄位、comment、generated expression 或 constraint。

整批 transformation 設定如下：

| 設定 | 預設 | Override |
|---|---|---|
| AUTO_INCREMENT | 保留來源值 | reset：移除目前數值，不硬寫 `AUTO_INCREMENT=1` |
| Engine | 跟隨來源 | Target 實際支援的 engine |
| Charset | 跟隨來源 | 支援的 character set |
| Collation | 跟隨來源 | 與選定 charset 相容的 collation |
| Row format | 跟隨來源 | DEFAULT、DYNAMIC、COMPACT、COMPRESSED、REDUNDANT |

Sync preview 必須從 Target 查詢 `SHOW ENGINES`、`information_schema.CHARACTER_SETS` 與 `information_schema.COLLATIONS`，並在後端驗證組合。Export 沒有 Target，override 依 Source server capability 驗證；下載檔不保證可在不同版本的任意 MySQL server 執行。

Preview 顯示每張 table 的來源值、輸出值、external dependencies、建立順序與 warning。完整 DDL 只在當次 preview response 與 export response 中出現，不寫入 structured log、audit 或 Meta DB。

## Dependency 規則

選取範圍內的 foreign key 建立有向圖並做拓撲排序：

```text
parent table --> child table
      |
      +-- DAG：依排序建立
      +-- cycle：preview 失敗，執行前不產生任何 write
      `-- external dependency：Target 已存在才允許執行
```

Export 可輸出含 external dependency 的選取結果，但必須顯示 warning。Sync 的任一 external dependency 在 Target 不存在時整批拒絕。來源跨 schema foreign key 第一版視為 external dependency，Target 必須存在相同被引用 schema/table；不自動改寫跨 schema reference。

## Preview Token 與 Drift 防護

Sync preview token 有效 60 秒，只能使用一次，並綁定：

- actor ID
- Source/Target Connection ID 與 Database
- sorted table names
- 每張 table 的來源 DDL SHA-256
- dependency snapshot
- transformation config
- Target capability snapshot

建立 job 前重新取得來源 DDL、dependency、Target capability 與 table existence。任一 hash 或 capability 改變即拒絕並要求重新 preview。Token store 必須有 TTL、容量上限與 consume-once 語意；第一版遵循平台單副本部署基線。

## Sync Job 狀態與失敗語意

```text
queued -> running -> completed
                  -> failed
                  -> cancelled
                  -> interrupted
```

Item 狀態為 `pending`、`created`、`failed`、`not_started`。Worker 按 dependency order 串行執行；第一個錯誤後停止，剩餘項目標為 `not_started`。MySQL DDL 會 implicit commit，因此不得宣稱整批 atomic，也不得在失敗後自動 drop 已建立 table。

Cancel 是 cooperative：尚未開始的 table 不再執行；已送出的 `CREATE TABLE` 使用 request context 取消，返回後仍須查詢 Target 確認 table 是否實際建立，再寫入 item 狀態。

Fresh sync 對任一既有目標 table 都整批拒絕。每張 table 建立成功後必須立即回讀 Target 的 `SHOW CREATE TABLE` 並保存 hash，避免跨 MySQL 版本的 server-side DDL normalization 造成誤判。Retry 建立新 job 並引用原 job；只有原 job 已記錄為 `created`，且目前 Target DDL 仍符合建立後回讀 hash 的 table 可視為已完成並跳過。若已建立 table 被修改、消失，或 process 在建立成功後、回讀 hash 前中斷而無法證明歸屬，retry 必須拒絕並要求 DBA 重新 preview。

Server 啟動時把遺留 `running` job 標記為 `interrupted`，不自動恢復。第一版只允許一個 worker instance；多副本前需要集中式 claim/lease。

## 資料模型

`table_schema_sync_jobs` 保存：

- requester、source/target connection 與 database
- transformation config JSON
- status、cancel request、retry source
- table count、created/failed/not-started count
- phase timings、stable error code、created/started/finished timestamps

`table_schema_sync_job_items` 保存：

- job ID、source table name、dependency order
- source DDL hash、建立後回讀的 target DDL hash
- status、stable error code、duration
- created/started/finished timestamps

不保存 credential 或完整 DDL。Job 與 item 必須使用唯一鍵避免同一 job 重複執行同一 table。對同一 Target Connection/Database 的重疊 active jobs 在建立時拒絕，避免互相競爭 namespace。

## API 規劃

```text
GET  /api/dba-tools/table-schemas/connections
GET  /api/dba-tools/table-schemas/connections/{id}/databases
GET  /api/dba-tools/table-schemas/connections/{id}/tables

POST /api/dba-tools/table-schemas/export/preview
POST /api/dba-tools/table-schemas/export

POST /api/dba-tools/table-schemas/sync/preview
POST /api/dba-tools/table-schemas/sync/jobs
GET  /api/dba-tools/table-schemas/sync/jobs
GET  /api/dba-tools/table-schemas/sync/jobs/{id}
POST /api/dba-tools/table-schemas/sync/jobs/{id}/cancel
POST /api/dba-tools/table-schemas/sync/jobs/{id}/retry
```

所有 endpoint 要求 authentication、active user、permission 與 DB scope。`table_schemas.read` 控制 metadata、preview 與 export；`table_schemas.sync` 控制 sync preview、建立、cancel 與 retry。Job read 同時要求 read permission 與 Source/Target scope，不因知道 ID 即可存取。

Request body 有固定大小限制；table names 必須去重、排序並限制 1-50 張。Database/table identifier 只能來自後端重新查詢的 metadata，執行時仍使用 identifier quoting，不直接拼接未驗證輸入。

主要 stable error codes：

- `unsupported_engine`
- `invalid_table_selection`
- `source_scope_denied` / `target_scope_denied`
- `source_ddl_changed`
- `target_capability_changed`
- `target_table_exists`
- `external_dependency_missing`
- `foreign_key_cycle`
- `invalid_transformation`
- `preview_expired` / `preview_replayed`
- `sync_conflict`
- `create_table_failed`
- `invalid_retry_state` / `retry_target_drifted`

## UI

`/dba-tools/table-schemas` 位於 DBA Tools 導航下，使用 `Export` 與 `Sync` tabs。兩者共用可搜尋的 Source Connection、Database、table multi-select 與整批 transformation controls。

Export 顯示 dependency warning、逐表 option 摘要、SQL preview 與 Download。Sync 額外選 Target Connection/Database，顯示 preflight 結果、建立順序、external dependency、來源與輸出 option 差異，再由明確 confirmation 建立 job。

Job history 使用 server-side pagination，detail 顯示每張 table 的 `created`、`failed`、`not_started`、duration 與可處理的錯誤訊息。Cancel、retry、loading、empty、permission denied、source drift、target conflict、cycle 與 partial failure 都需要獨立 UI state。

## Audit 與日誌

Audit 保存 actor、source/target connection/database、sorted table names、transformation config、action、job ID 與結果，不保存完整 DDL。Export preview、download、sync preview、job create、cancel、retry、completed/failed 都需有對應事件。

Structured log 至少包含 request/job ID、table count、dependency scan、source DDL load、target preflight、DDL transform、每張 create 與總耗時；不得記錄 password、preview token 或完整 DDL。

## 實作階段

### S1：規格、權限與 domain foundation

- 新增 `table_schemas.read`、`table_schemas.sync` migrations 與 RBAC seed。
- 建立 `internal/tableschema` package 的 request/value types、limits、stable errors。
- 註冊 backend route skeleton 與 frontend lazy route/navigation/permission guard。

驗證：migration up/down、permission middleware、route access 與 routed-page smoke tests；`go test ./...`、前端 lint/test/build。

### S2：DDL snapshot、scanner 與 transformation engine

- 使用 readonly credential 取得 `SHOW CREATE TABLE`。
- 實作 SQL-aware identifier/table-option scanner。
- 實作五類 inherit/override 與 capability validation。
- 保證原始輸入 immutable，輸出 deterministic。

驗證：以 MySQL 5.7/8.0 DDL fixtures 覆蓋 quoted identifier、comment、generated column、partition、foreign key 與所有 table options；scanner fuzz test 不得 panic 或改寫 literal/comment。

### S3：Dependency analyzer 與 metadata APIs

- 讀取 databases、實體 tables、source options 與 foreign key dependencies。
- 實作 DAG ordering、external dependency 與 cycle detection。
- 完成 scoped connections/databases/tables endpoints。

驗證：unit tests 覆蓋空集合、單表、diamond dependency、external dependency、self/cross-table cycle、跨 schema reference；handler tests 覆蓋 permission、scope、deleted connection 與非 MySQL connection。

### S4：Schema Export

- 完成 export preview 與直接 `.sql` download。
- 支援 1-50 tables、deterministic order、transformation 與 warning。
- response streaming 設定 bounded buffer/timeout，不保存 artifact。

驗證：輸出可在隔離 MySQL fixture 建立等價 table；測試 client disconnect、來源 timeout、DDL 過大、特殊檔名、無權限與 audit redaction。

### S5：Sync preview 與安全邊界

- 完成 Target capability、table existence、external dependency preflight。
- 實作 60 秒 bounded one-time token 與 source/target drift revalidation。
- Fresh sync 任一 conflict 必須在 write 前整批拒絕。

驗證：token expiry/replay、actor/resource/config substitution、來源 DDL race、目標 table race、capability drift、missing dependency 與 cycle 都不能產生 Target write。

### S6：持久化 Sync Job 與 worker

- 新增 jobs/items repository、bounded worker、狀態機與 per-target conflict guard。
- 依 dependency order 執行、fail-fast、cooperative cancel、structured timing。
- server restart 將 running job 標為 interrupted。

驗證：repository atomicity、double claim、partial success、cancel during DDL、context timeout、audit failure、restart 與同 target job conflict；跨 package 變更執行 `go test ./...`。

### S7：Retry 與完整 UI

- Retry 驗證 prior-created table ownership/hash，只接續安全的 missing items。
- 完成 Export/Sync tabs、searchable multi-select、transform controls、preview、confirmation、job pagination/detail/cancel/retry。
- Request cleanup，避免切換 connection/database/tab 後舊 response 覆蓋新狀態。

驗證：routed-page integration tests 覆蓋 success、validation、permission、empty/error、stale response、cycle、conflict、partial failure、cancel 與 retry drift；執行 `npm run lint`、`npm test`、`npm run build`。

### S8：真實 MySQL 驗收與文件同步

- 建立隔離 MySQL 5.7、MySQL 8.0 integration fixtures；Aurora 以 Testnet smoke checklist 驗收。
- 驗證 mixed options、generated columns、indexes、foreign keys、partition 與同/跨 connection sync。
- 同步 API/permission、configuration、project map、PROJECT_STATUS 與使用說明。
- 執行最終 lint/test/build、`go test ./...` 與 `git diff --check`。

完成條件：所有 S1-S8 gate 通過；任何 skipped fixture、環境限制或 Aurora 未驗證項目必須明確記錄，不得以 unit test 通過宣稱全功能完成。

## 測試覆蓋圖

```text
Export
  selection -> scope -> snapshot -> dependency -> transform -> preview/download
       |         |         |             |            |             |
     limits    denied    timeout       cycle warn   invalid       disconnect

Sync
  preview -> token -> revalidate -> persist -> claim -> ordered CREATE
     |         |          |             |        |           |
  conflict  replay     source drift   DB error  race      success/fail/cancel
                                                               |
                                                       item status + audit

Retry
  prior job -> verify created hashes -> missing items -> new immutable job
      |                  |                    |
 invalid state      target drift          safe resume
```

每條 error path 必須同時驗證 stable API code、無非預期 Target write、無 credential/DDL log leakage，以及使用者可理解且可恢復的 UI 狀態。

## 已知部署邊界

平台目前以單副本為基線。Preview token store、job claim 與 worker 遵循此限制；多副本部署前必須將 token consume 與 job lease 移至共享狀態。MySQL DDL 可能等待 metadata lock，部署時需設定 bounded query timeout，並理解 cancel 不等於 server 一定未完成 DDL。
