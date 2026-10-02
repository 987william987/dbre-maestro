# MySQL Binlog Export

MySQL Binlog Export 是 DBA 使用的人工分析工具。它從 ROW / FULL binlog 產生兩份獨立 artifact：Forward SQL 與 Rollback SQL。這些內容是依 binlog row event 重建的 SQL，不宣稱是原始 query。

前端入口是 `/dba-tools/binlog-export`。具備 read permission 的使用者可查看 inventory、jobs 與下載 artifacts；建立、取消及重試控制項另由 execute permission 控制。

## 權限與資料源

- `binlog_exports.read`：查看 connections、binlogs、jobs 與下載 artifacts。
- `binlog_exports.execute`：建立、取消與重試 jobs。
- 所有操作都疊加使用者 DB Scope。
- 連線固定使用 DB Connection 的 `Binlog / Rollback Credential`。
- 預設只有 DBA auth group 取得這兩個 permissions；all-permissions 身分透過既有 effective permission helper 取得。

## Range 與 filters

支援兩種互斥模式：

- `time`：`start_time`、`end_time` 與 IANA `timezone`。
- `position`：start/end binlog file 與 position。

Database 最多一個，table 可多選；DML types 支援 `insert`、`update`、`delete`，空陣列代表三種全部。table name 保留大小寫，避免在 case-sensitive MySQL 環境選錯表。

有 database/table filter 的 time range 最長 24 小時。完全未過濾時必須送出 `acknowledge_unfiltered: true`，time range 最長 15 分鐘，position range 必須在同一個 binlog file 內。

## Binlog inventory

inventory 使用 `SHOW BINARY LOGS` 與 binary log status，取得 file name、size、active file 與 current position，結果快取 30 秒。檔案時間不由 `SHOW BINARY LOGS` 推測；使用者按下 `Probe times` 後，前端會逐檔串行請求，後端使用 Go MySQL replication client 從 position 4 讀取第一個具有 timestamp 的 event 後立即關閉連線。每檔有獨立 10 秒 timeout，結果依 connection、file 與 file size 快取 5 分鐘。下一支檔案的開始時間是上一支的結束邊界；active file 的結束時間為未定。

Time mode 建立 job 時會固定當下 active file/position，並把同一硬終點交給 Forward 與 Rollback；`end_time` 不可晚於建立時間。Position mode 在排隊前驗證檔案仍存在、position 未超出檔案，且一次最多跨越 20 支 binlog。

## Job 與 artifact

worker 全域最多同時執行兩個 jobs，同一 connection 最多一個。Forward 與 Rollback 使用相同 range 依序生成；Forward 依事件時間正序輸出，Rollback 依事件時間反序輸出。每個 row event 對應的 SQL 群組前保留 my2sql 提供的 event datetime、database、table、binlog file 與 position，協助人工核對；同一 event 產生多筆 SQL 時共用一組 metadata。取消 running job 會終止目前的 `my2sql` process。

ROW binlog 與 my2sql extra info 不提供原始連線的 DB user，因此 artifact 不顯示或推測執行帳號。若需要把 SQL 歸因到 DB user，必須由資料庫端另行提供 MySQL Audit Log 或 General Log，並建立獨立的關聯與保留政策。

artifact 保留 7 天，gzip 後使用應用程式 AES key 加密；到期後 background worker 清除密文。Job metadata 保留 90 天。Preview 與下載都重新驗證 DB Scope、expiry、解壓後大小與 SHA-256，response 使用 `Cache-Control: no-store`。

worker 對 stdout、stderr、artifact memory 與 temporary directory 設有硬上限。每個 terminal job 會輸出分段 timing、artifact bytes、filter 數量、terminal status 與穩定 error code；log 不包含 SQL、密碼、table name 或資料值。

## 整合驗證

本機 Docker 環境可執行 `make test-binlog-export-integration`，以真實 MySQL ROW/FULL binlog 與 image 內的 `my2sql` 驗證 Position／Time snapshot 的 Forward／Rollback SQL。fixture 會額外寫入 snapshot 後事件並確認它不會混入 artifact；測試結束後自動清理隔離資料，不包含在一般 unit test gate。
