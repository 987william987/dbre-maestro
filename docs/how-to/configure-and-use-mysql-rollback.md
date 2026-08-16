# How to 設定與使用 MySQL Rollback

這份指南說明如何啟用 MySQL rollback beta、測試 connection 能力、執行 DML ticket，並從已產生的 rollback SQL 建立新的 rollback ticket。

## Prerequisites

- 你有 `settings.write`，可以調整平台 Settings
- 你有 `db_connections.write`，可以配置 DB Connection credential
- 你有 `tickets.apply`，可以建立 rollback ticket
- 目標 connection 是 MySQL
- 原工單是 DML ticket
- 若使用 `my2sql` 或 `hybrid` fallback，目標 MySQL 必須啟用：
  - `log_bin = ON`
  - `binlog_format = ROW`
  - `binlog_row_image = FULL`

## Steps

1. 確認 backend image 內有 `my2sql`。

   目前 backend Dockerfile 會把 `my2sql` 放到：

   ```text
   /usr/local/bin/my2sql
   ```

   如果部署環境使用自建 image，請進 pod 驗證：

   ```bash
   which my2sql
   my2sql --help
   ```

2. 到 `Settings > MySQL Rollback` 啟用 rollback。

   建議先使用預設 engine：

   ```text
   Hybrid: prior backup + my2sql fallback
   ```

   Settings 填法：

   | 欄位 | 建議值 |
   |---|---|
   | Enable MySQL rollback generation | 開啟 |
   | Rollback Engine | `hybrid` |
   | my2sql path | `/usr/local/bin/my2sql` |
   | Generation timeout (seconds) | `30` |
   | Max rollback SQL bytes | `5242880` |

3. 到 `DB Connections` 配置 rollback credential。

   打開 MySQL connection 詳情，填入：

   - `Rollback Username`
   - `Rollback Password`

   若 engine 是 `prior_backup`，Test Rollback 不會要求 rollback credential；但如果使用 `hybrid`，parser 不支援時會 fallback 到 `my2sql`，因此仍建議配置。

4. 確認目標 MySQL 權限。

   `my2sql` 路徑需要 rollback user 能讀 binlog position 與 binlog event。

   prior backup 路徑需要 ticket execution 的 readwrite user 能做以下操作：

   ```sql
   CREATE DATABASE IF NOT EXISTS maestro_rollback;
   CREATE TABLE maestro_rollback._maestro_rb_t... LIKE app.target_table;
   INSERT INTO maestro_rollback._maestro_rb_t... SELECT ... FROM app.target_table WHERE ...;
   ```

   DBA 應依公司權限模型授予最小必要權限。

5. 點 `DB Connections > Test Rollback`。

   成功時，前端 toast 會顯示 rollback capability test passed。若走 `my2sql` 或 `hybrid`，成功訊息會帶 binlog file 與 position。

   `prior_backup` engine 的測試只代表 Settings 與 MySQL 類型正確。實際 SQL 是否支援，會在 ticket execution 時檢查。

6. 建立並執行 DML ticket。

   支援範例：

   ```sql
   UPDATE accounts
      SET status = 'disabled'
    WHERE user_id = 1001;
   ```

   prior backup 會在這條 statement 送進 DB 前先備份 affected rows。原 statement 成功後，Statement Results 的 Rollback 欄位會變成 `Generated` 或 `Generation Failed`。

7. 在 Ticket Detail 點右上角 `Rollback`。

   前端會載入所有已產生的 rollback SQL，並列出：

   - 原 statement
   - rollback SQL
   - 可選 checkbox

   你可以選全部 statement，也可以只選其中幾句。

8. 點 `Create Ticket`。

   系統會建立一張新的 DML ticket。這張 ticket 會重新走 Workflow Rules，因此 reviewer / executor 仍可再次審核 rollback SQL 是否安全。

## Verification

成功啟用與使用後，你應該能看到：

- `DB Connections > Test Rollback` 成功
- 原 DML ticket 的 Statement Results 出現 Rollback 欄位
- 成功 statement 的 rollback 狀態為 `Generated`
- 點 `Rollback` 後可以預覽 rollback SQL
- 建立 rollback ticket 後，原 rollback record 狀態變成 `Ticket Created`

## Troubleshooting

### `mysql rollback is disabled`

Settings 的 `Enable MySQL rollback generation` 尚未開啟。

### `my2sql binary was not found in PATH`

`mysql_rollback_my2sql_path` 指向的 binary 不存在。若部署使用 backend Dockerfile，請填：

```text
/usr/local/bin/my2sql
```

### `rollback credential is not configured`

connection 沒有配置 `rollback` credential。這會影響 `my2sql` 與 `hybrid` fallback。

### `mysql binlog is not enabled`

目標 MySQL 沒有啟用 binlog。`my2sql` 需要 binlog 才能產生 rollback SQL。

### `mysql binlog_format is not ROW`

`my2sql` 需要 ROW 格式 binlog。statement 或 mixed format 不適合用來產生精準 rollback SQL。

### `mysql binlog_row_image is not FULL`

`UPDATE` / `DELETE` rollback 需要完整 before image。若不是 FULL，binlog 可能缺少還原資料。

### `prior backup requires UPDATE to have a WHERE clause`

prior backup parser 拒絕無條件 `UPDATE`。請改成明確指定 `WHERE`，或在 `hybrid` engine 下讓平台 fallback 到 `my2sql`。

### `prior backup requires ORDER BY when UPDATE/DELETE uses LIMIT`

`LIMIT` 沒有 `ORDER BY` 時，MySQL 可能選到不穩定的 row。prior backup parser 會拒絕這種 SQL。

### `prior backup joined UPDATE requires SET columns to be table-qualified`

JOIN UPDATE 必須明確寫 table 或 alias：

```sql
UPDATE accounts a
JOIN users u ON u.id = a.user_id
   SET a.status = 'disabled'
 WHERE u.disabled = 1;
```

不要寫成：

```sql
UPDATE accounts a
JOIN users u ON u.id = a.user_id
   SET status = 'disabled'
 WHERE u.disabled = 1;
```

### rollback SQL 產生失敗，但原工單仍完成

這是預期行為。rollback 是 beta 輔助能力，不是 ticket execution 的必要條件。DBA 應根據 `failure_message` 和 audit log 判斷是否需要人工補救。

## 相關文件

- [MySQL Rollback](../reference/mysql-rollback.md)
- [MySQL Rollback 設計說明](../explanation/mysql-rollback-design.md)
- [How to 建立與執行 Tickets](create-and-execute-tickets.md)
- [DB Connections](../reference/db-connections.md)
- [平台 Settings](../reference/settings.md)
