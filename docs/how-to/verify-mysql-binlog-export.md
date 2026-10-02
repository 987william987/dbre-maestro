# How to 驗證 MySQL Binlog Export

這項驗證會使用本機 Docker Compose 的 MySQL 與 app image 內的真實 `my2sql`，確認 position range 可以產生正確的 Forward SQL 與 Rollback SQL。

## 前置條件

- `docker compose up` 已啟動 `mysql` 與 `app`。
- MySQL 必須是 `log_bin=ON`、`binlog_format=ROW`、`binlog_row_image=FULL`。
- app image 內必須存在 `my2sql`；timestamp probe 由應用程式的 Go MySQL replication client 處理，不依賴額外 CLI。

## 執行

```bash
make test-binlog-export-integration
```

測試會建立唯一命名的 schema 與最小權限 replication user，寫入 INSERT、UPDATE、DELETE，再以同一段 binlog position 驗證：

- Forward SQL 保留 INSERT、UPDATE、DELETE 的原執行順序與新值。
- Rollback SQL 以相反順序還原 DELETE、UPDATE、INSERT，並使用正確的 before image。

成功或失敗後都會清除測試 schema、user 與 app container 內的暫存 artifacts。這是 opt-in integration test，不會隨一般 `make test` 執行。
