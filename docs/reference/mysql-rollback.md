# MySQL Rollback

MySQL Rollback 是 DML ticket 的 beta 輔助功能。當 MySQL DML statement 執行成功後，平台會嘗試產生對應 rollback SQL，讓使用者或 DBA 可以先預覽，再建立一張新的 DML rollback ticket。

這不是自動回滾。rollback SQL 只會被保存與展示；真正回滾仍要由使用者建立 rollback ticket，並依 Workflow Rules 重新審批與執行。

## 支援範圍

| 項目 | 行為 |
|---|---|
| DB type | 只支援 MySQL connection |
| Ticket type | 只支援 DML statement rollback |
| DDL | 不產生 rollback SQL |
| Redis | 不產生 rollback SQL |
| PostgreSQL | v1 不支援 |
| 產生時機 | 原 statement 執行成功後 |
| 產生失敗 | 不阻塞原 ticket execution |

## Rollback Engine

Settings 的 `mysql_rollback_engine` 決定產生策略。

| Engine | 適用 | 行為 |
|---|---|---|
| `hybrid` | 預設值 | `UPDATE` / `DELETE` 先用 prior backup parser；parser 不支援時 fallback 到 `my2sql` |
| `prior_backup` | 想避免依賴 binlog 工具時 | 只處理 parser 支援的 `UPDATE` / `DELETE`；不支援 `INSERT` |
| `my2sql` | 想完全使用 binlog 反解析時 | 所有可支援 DML 都走 `my2sql` |

`hybrid` 只會在 prior backup 還沒有碰目標 DB 之前 fallback。若 prior backup 已開始建立備份表但 DB 操作失敗，平台會把該 statement 的 rollback generation 標成 failed，並繼續執行原 statement。

## Settings

| 欄位 | Key | 預設值 | 說明 |
|---|---|---|---|
| Enable MySQL rollback generation | `mysql_rollback_enabled` | `false` | 是否啟用 MySQL rollback SQL 產生 |
| Rollback Engine | `mysql_rollback_engine` | `hybrid` | `hybrid`、`prior_backup` 或 `my2sql` |
| my2sql path | `mysql_rollback_my2sql_path` | `my2sql` | `my2sql` binary 路徑或 PATH 中的指令名 |
| Generation timeout (seconds) | `mysql_rollback_generation_timeout_seconds` | `30` | `my2sql` 產生流程 timeout |
| Max rollback SQL bytes | `mysql_rollback_max_sql_bytes` | `5242880` | 保存 rollback SQL 的大小上限 |

## DB Connection Credential

MySQL connection 可以配置 `rollback` credential role。

| Role | 用途 |
|---|---|
| `readonly` | SQL Editor、metadata、export |
| `readwrite` | ticket execution |
| `rollback` | MySQL rollback generation |

`my2sql` / `hybrid` 的 binlog fallback 會使用 `rollback` credential 連 writer endpoint 讀取 binlog position 與產生 rollback SQL。

`prior_backup` 在 ticket execution 的同一條 readwrite connection 上建立備份表，因此不要求 `rollback` credential 或 binlog 設定。但管理者仍應給 readwrite execution user 足夠權限，至少能建立 `maestro_rollback` database、建立表、讀取目標表資料並寫入備份表。

## Prior Backup Parser

prior backup parser 會在原 statement 送進 DB 前，把即將被影響的 row 複製到獨立 database：

```text
maestro_rollback._maestro_rb_t{ticket_id}_e{execution_id}_s{seq}
```

多 target table 時會加上 target suffix，例如：

```text
maestro_rollback._maestro_rb_t7_e12_s3_01_a
maestro_rollback._maestro_rb_t7_e12_s3_02_u
```

支援：

| SQL 類型 | 支援 |
|---|---|
| 單表 `UPDATE ... WHERE ...` | 是 |
| 單表 `DELETE ... WHERE ...` | 是 |
| `ORDER BY ... LIMIT ...` | 是，必須同時有 `ORDER BY` |
| JOIN with single target table | 是 |
| multi-table `UPDATE` / `DELETE` | 是 |
| CTE 作為來源或 join reference | 是 |

限制：

| SQL 形態 | 結果 |
|---|---|
| `UPDATE` / `DELETE` 沒有 `WHERE` | 不支援 |
| `LIMIT` 但沒有 `ORDER BY` | 不支援 |
| JOIN UPDATE 的 `SET` 欄位未加 table 或 alias 前綴 | 不支援 |
| CTE 本身作為 UPDATE / DELETE target | 不支援 |
| self-join 同一實體表多 alias 同時作為 target | 不支援 |
| cross-database target table | 不支援 |
| partition-qualified table reference | 不支援 |
| `UPDATE` 修改 primary key | 不支援 |
| `UPDATE` target table 沒有 primary key | 不支援 |

## Rollback Status

每個 ticket execution 最多有一筆 rollback record。

| Status | 說明 |
|---|---|
| `unsupported` | 此 statement 不支援 rollback generation，或設定未啟用 |
| `generating` | rollback SQL 正在產生中，通常是 `my2sql` async job |
| `generated` | rollback SQL 已產生，可以預覽並建立 rollback ticket |
| `failed` | rollback generation 失敗，原 statement execution 不會因此回滾 |
| `submitted` | 使用者已用這筆 rollback SQL 建立 rollback ticket |

Ticket Detail 的 Statement Results 只顯示 rollback 狀態。詳細原因會放在 tooltip 與 rollback preview 中。

## API

### `POST /api/db-connections/{id}/test-rollback`

Gate：`db_connections.write`

用途：讓 DBA 驗證指定 MySQL connection 的 rollback capability。

回應：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `ok` | `boolean` | 整體測試是否通過 |
| `message` | `string` | 第一個失敗原因或成功訊息 |
| `checks` | `array` | 逐項檢查結果 |
| `binlog` | `object` | `my2sql` / `hybrid` 成功讀到的 binlog file / position |

在 `prior_backup` engine 下，這個 API 不檢查 my2sql binary、rollback credential 或 binlog。它只確認 connection 是 MySQL、Settings 已啟用，並提示 parser 能力會在 ticket execution 時檢查。

### `GET /api/tickets/{id}/rollbacks/preview`

Gate：`tickets.apply`

用途：載入目前 ticket 所有 `generated` rollback SQL，供前端預覽。

回應：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `items[].rollback` | `TicketExecutionRollback` | rollback metadata |
| `items[].original_sql` | `string` | 原 statement |
| `items[].rollback_sql` | `string` | 解密後 rollback SQL |

### `POST /api/tickets/{id}/rollbacks/create-ticket`

Gate：`tickets.apply`

用途：用選定的 rollback SQL 建立新的 DML ticket。

請求：

```json
{
  "rollback_ids": [101, 102]
}
```

後端會把選定 rollback SQL 合併成一張新的 DML ticket，title 會使用來源工單號，description 會標記來源 statement 序號。

### `POST /api/tickets/{id}/rollbacks/{rollbackID}/create-ticket`

Gate：`tickets.apply`

用途：legacy 單筆 rollback ticket 建立 API。新前端使用 selection API。

## 資料模型

Rollback metadata 存在 Meta DB 的 `ticket_execution_rollbacks`。

| 欄位 | 說明 |
|---|---|
| `ticket_id` / `execution_id` / `seq` | 來源 ticket statement |
| `status` | rollback generation 狀態 |
| `unsupported_reason` | 不支援原因 |
| `failure_message` | 產生失敗原因 |
| `generator` / `generator_version` | `my2sql` 或 `prior_backup` |
| `binlog_start_file` / `binlog_start_pos` | `my2sql` 起點 |
| `binlog_end_file` / `binlog_end_pos` | `my2sql` 終點 |
| `rollback_sql_encrypted` | 加密後 rollback SQL |
| `rollback_sql_sha256` | rollback SQL checksum |
| `rollback_sql_bytes` | rollback SQL bytes |
| `statement_count` | 產生的 rollback statement 數量 |
| `confidence` | 產生器信心標記 |
| `warning_message` | 需要 DBA 注意的風險或備份表資訊 |
| `rollback_ticket_id` | 已建立的 rollback ticket |

## 相關文件

- [How to 設定與使用 MySQL Rollback](../how-to/configure-and-use-mysql-rollback.md)
- [MySQL Rollback 設計說明](../explanation/mysql-rollback-design.md)
- [Tickets](tickets.md)
- [DB Connections](db-connections.md)
- [平台 Settings](settings.md)
