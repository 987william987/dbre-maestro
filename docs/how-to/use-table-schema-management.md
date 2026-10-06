# 使用 Table Schema Management

Table Schema Management 用於匯出 MySQL／Aurora MySQL 實體資料表結構，或把結構建立到另一個 Maestro 管理的 database。功能不讀取或同步資料，也不會修改、覆蓋或刪除既有目標表。

## 權限與連線條件

- 匯出需要 `table_schemas.read`；同步需要 `table_schemas.sync`。
- Source 使用 readonly credential，Target 使用 readwrite credential。
- 使用者必須同時具備 Source 與 Target DB Connection scope。
- 每次可選 1 至 50 張 table；不支援 view、trigger、routine 或 event。

## 匯出結構

1. 開啟 `/dba-tools/table-schemas`，停留在 `Export`。
2. 選擇 Source Connection、Database 與 tables。
3. 視需要設定 AUTO_INCREMENT、engine、charset、collation 與 row format；預設沿用來源。
4. 執行 Preview，檢查建立順序、外部 foreign key dependency、options 與 SQL。
5. 下載 `.sql`。下載內容不會保存在 Maestro。

## 同步結構

1. 切換到 `Sync`，完成 Source 選擇後指定 Target Connection 與 Database。
2. 執行 Preview。Target 任一同名 table 已存在、外部 dependency 不存在、foreign key 有 cycle，或 capability 不相容時，整批不會寫入。
3. 確認 preview 後建立 background job。Preview token 有效 60 秒且只能使用一次；來源或目標狀態變更時需重新 preview。
4. 在 Sync Jobs 查看逐表狀態與耗時。Cancel 是 cooperative，已送到 MySQL 的 DDL 仍可能完成。
5. failed、cancelled 或 interrupted job 可 Retry；先前成功的 table 必須仍存在且 hash 相符，否則拒絕 retry。

## 本機整合驗證

執行 `make test-table-schema-integration`。測試會建立隔離的 MySQL 5.7 與兩個 MySQL 8.0 containers，驗證同 instance／跨 instance preflight、mixed options、generated columns、indexes、foreign keys、partition、dependency order、partial failure、cancel 與 timeout。預設 ports 為 `13317`、`13318`、`13319`，可用 `TABLE_SCHEMA_IT_MYSQL_PORT`、`TABLE_SCHEMA_IT_MYSQL80_TARGET_PORT`、`TABLE_SCHEMA_IT_MYSQL57_PORT` 覆寫。ARM 主機上的 MySQL 5.7 fixture 使用 amd64 模擬。

## Aurora Testnet smoke checklist

Aurora 必須在 Testnet 以實際 DB Connection 驗收，不可用本機 MySQL 結果代替：

1. 建立僅涵蓋測試 database 的 Source readonly 與 Target readwrite scope。
2. 對含 generated column、secondary/unique index、foreign key 與 partition 的 tables 執行 Export Preview，確認 SQL 不含資料。
3. 對空白 Target Database 執行 Sync Preview 與 job，確認 dependency order、逐表 hash 與 completed 狀態。
4. 建立同名 target table 後重跑 preview，確認以 `target_table_exists` 拒絕且沒有額外 write。
5. 修改已同步 table 後嘗試 retry，確認以 `retry_target_drifted` 拒絕。
6. 檢查 audit 與 structured logs 不含 credential、preview token 或完整 DDL。

目前自動化 fixture 不包含 Aurora；每次發版的 Aurora 結果應記錄環境、Aurora MySQL major version、時間與驗收人。
