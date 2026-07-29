# SQL Editor 查詢取消機制

SQL Editor 的 Stop Query 不是單純中斷瀏覽器 request。對 MySQL，平台會用一次顯式 cancel API，把正在 DB engine 裡執行的查詢停掉，避免使用者關閉頁面或點 Stop 後，Aurora/RDS MySQL processlist 仍殘留長查詢。

## 為什麼不能只靠 HTTP abort

瀏覽器中斷 request 只代表 client 不再等待回應，不保證資料庫端正在執行的 SQL 已經停止。若後端已經把查詢送進 MySQL，DB engine 可能繼續掃描大表，直到 SQL 自己完成、session timeout 生效，或連線被真正關閉。

這對 SQL Editor 不是可接受的行為：

- 使用者看到 Stop 後，預期 DB 查詢也被停止
- 大表 `COUNT` 或慢查詢可能持續消耗 DB 資源
- App timeout 和 proxy timeout 不一定等於 DB engine execution cancellation

因此 MySQL Stop Query 使用「顯式 cancel endpoint + MySQL thread kill」。

## 流程

```text
Frontend
  |
  | POST /api/query
  | { sql, database, schema, query_execution_id }
  v
Backend
  |
  | readonly credential
  | SELECT CONNECTION_ID()
  | register query_execution_id -> user_id + mysql_thread_id
  v
Aurora/RDS MySQL
  |
  | query running
  |
Frontend Stop
  |
  | POST /api/query/cancel
  | { query_execution_id }
  v
Backend
  |
  | user-scoped lookup
  | cancel app context
  | readwrite credential
  | CALL mysql.rds_kill_query(thread_id)
  | fallback CALL mysql.rds_kill(thread_id)
  v
Aurora/RDS MySQL
```

前端每次 Run Query 都產生一個新的 `query_execution_id`，同一次 Stop 會使用同一個 ID。後端只接受同一位 user 取消自己的 active query，因此前端不需要、也不應知道 MySQL thread id。

## Pending Cancel

快速連點 Run / Stop 時，Stop request 可能比後端完成 MySQL thread registration 更早抵達。

為了處理這個 race，後端 registry 支援 pending cancel：

1. `/api/query/cancel` 找不到 active query 時，先記錄 `query_execution_id + user_id` 為 pending。
2. `/api/query` 後續準備註冊 MySQL thread id 時，如果發現同一個 user 的 pending cancel，就立刻取消 context。
3. SQL 不會再送進 DB engine 執行。

這讓 Stop 可以處理「查詢已開始」與「查詢還沒真正送出」兩種狀態。

## Credential 邊界

SQL Editor 查詢本身仍使用 readonly credential。Stop Query 只有在需要 kill MySQL query 時，才用 readwrite credential 開一條短連線執行 RDS routine：

```sql
CALL mysql.rds_kill_query(<thread_id>);
CALL mysql.rds_kill(<thread_id>);
```

這個設計保留 readonly 查詢邊界，不需要把 kill routine 權限授予 readonly user。

Aurora/RDS MySQL 的 readwrite credential 需要：

```sql
GRANT EXECUTE ON PROCEDURE mysql.rds_kill_query TO '<readwrite_user>'@'%';
GRANT EXECUTE ON PROCEDURE mysql.rds_kill TO '<readwrite_user>'@'%';
```

若 readwrite credential 沒有上述權限，使用者可能看到 Stop request 成功送出，但 DB engine 查詢無法被 kill。這類失敗會出現在服務 log。

## Timeout 邊界

SQL Editor 查詢有自己的 app timeout 與 DB session timeout：

- App timeout：`sql_editor_app_timeout_seconds`
- MySQL session timeout：`sql_editor_mysql_max_execution_time_ms`
- PostgreSQL session timeout：`sql_editor_postgres_statement_timeout_ms`

`POST /api/query` 和 `POST /api/tickets/{id}/execute` 會豁免全域 45 秒 request timeout，避免長查詢或工單執行被全域 middleware 提前切斷。`POST /api/query/cancel` 本身不是長時間 request，因此不需要豁免。

HTTP server 的全域 write timeout 仍維持 45 秒；長時間 request 只在這些 execution route 上清除單次 response write deadline。

生產環境的 proxy、Ingress 或 ALB timeout 仍應大於 SQL Editor app timeout。否則使用者可能先看到 proxy 層 502/504，而不是平台回傳的 timeout 訊息。

## 部署限制

Active query registry 是 process-local memory。若 app 部署多個 replica，且 `/api/query` 和 `/api/query/cancel` 沒有打到同一個 pod，cancel endpoint 可能找不到原本註冊的 MySQL thread id。

可行做法：

- SQL Editor execution request 使用 sticky routing
- 或把 active query registry 移到共享狀態，並設計跨 pod kill worker

目前實作採用 process-local registry，因為它簡單、延遲低，且不把 MySQL thread id 暴露給前端。

## 相關文件

- [SQL Editor 參考](../reference/sql-editor.md)
- [How to 使用 SQL Editor](../how-to/use-sql-editor.md)
- [安全邊界說明](security-boundaries.md)
