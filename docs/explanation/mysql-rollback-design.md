# MySQL Rollback 設計說明

MySQL rollback 的目標不是讓平台自動把資料改回去，而是讓 DBA 在事故後能快速拿到可審核、可追溯、可重新送審的 rollback SQL。它補的是「產出回滾候選 SQL」這段時間差，不取代人工判斷與工單治理。

## The problem

DML 工單執行成功後，後續才發現資料改錯時，傳統處理常需要 DBA 從備份或 binlog 人工提取舊值。這很慢，也容易缺少平台內的審批、通知、audit log。

直接在執行前用 AI 產生 rollback SQL 不可靠。`UPDATE` 和 `DELETE` 的 rollback 常需要舊資料值，這些值只有 DB 執行前的 row image 或 binlog before image 才能可信取得。

## The approach

平台把 rollback 拆成兩條 engine：

```text
DML ticket statement
  |
  v
prepare rollback runtime
  |
  +-- prior_backup for supported UPDATE / DELETE
  |     |
  |     +-- before original SQL:
  |     |     copy affected rows to maestro_rollback
  |     |
  |     +-- after original SQL succeeds:
  |           generate restore SQL from backup tables
  |
  +-- my2sql for INSERT or fallback
        |
        +-- before original SQL:
        |     read binlog start position
        |
        +-- after original SQL succeeds:
              read binlog end position and run my2sql async
```

`hybrid` 是預設策略。它優先使用 prior backup parser，因為 `UPDATE` / `DELETE` 的舊值可以在原 statement 執行前直接備份。若 parser 在 planning 階段判斷不支援，才 fallback 到 `my2sql`。

## Why rollback is generated after execution

rollback SQL 必須基於實際改動資料。

對 `DELETE`：

```sql
DELETE FROM accounts WHERE id = 1;
```

rollback 需要原本整列資料：

```sql
INSERT INTO accounts (...) VALUES (...);
```

對 `UPDATE`：

```sql
UPDATE accounts SET balance = 0 WHERE id = 1;
```

rollback 需要更新前的 column value：

```sql
UPDATE accounts SET balance = <old_value> WHERE id = 1;
```

這些舊值不能只靠原 SQL 推導。prior backup 在執行前保存舊 row；my2sql 從 ROW binlog 的 before image 還原。這兩種方式都比靜態生成 SQL 更可信。

## Prior backup design

prior backup parser 的核心是先把 target rows 複製到平台管理的獨立 database：

```text
maestro_rollback
```

它不把備份表放在業務 database，避免污染業務 schema。備份表由 ticket id、execution id、statement seq 命名，方便追溯與人工清理。

單表 `DELETE` 的 rollback 是：

```sql
INSERT INTO `app`.`accounts` (`id`, `status`)
SELECT `id`, `status`
FROM `maestro_rollback`.`_maestro_rb_t7_e12_s3`;
```

`UPDATE` 的 rollback 是：

```sql
INSERT INTO `app`.`accounts` (`id`, `balance`, `updated_at`)
SELECT `id`, `balance`, `updated_at`
FROM `maestro_rollback`.`_maestro_rb_t7_e11_s2`
ON DUPLICATE KEY UPDATE `balance` = VALUES(`balance`);
```

`UPDATE` 要求 target table 有 primary key，因為 rollback 需要穩定找到被更新的 row。若 update 修改 primary key，平台會拒絕 prior backup，避免 rollback target 不可定位。

## Why unsafe SQL shapes are rejected

prior backup parser 只在能清楚定位 target rows 和 target table 時工作。

| 限制 | 原因 |
|---|---|
| 無 `WHERE` 的 `UPDATE` / `DELETE` | 風險過高，可能備份與改動整張表 |
| 無 `ORDER BY` 的 `LIMIT` | row selection 不穩定，rollback 無法說清楚影響集合 |
| JOIN UPDATE 的 `SET` 欄位未加 table 或 alias | 無法可靠判斷真正 target table |
| CTE 本身作為 target | MySQL parser 和 execution 語義較複雜，v1 不承擔這個風險 |
| self-join 同一實體表多 alias 同時作為 target | 可能產生重複或互相覆蓋的 restore SQL |
| cross-database target | v1 以 ticket selected database 作為安全邊界 |

這些情況在 `hybrid` 下可 fallback 到 `my2sql`，前提是 fallback 發生在 prior backup 尚未碰目標 DB 之前，且 my2sql、rollback credential、binlog 設定都可用。

## Async my2sql generation

`my2sql` 產生可能比原 statement 本身更慢。平台不應讓下一條 statement 被 rollback generation 阻塞，所以 `my2sql` 在原 statement 成功後進入 async job。

目前限制是每張 ticket 同時最多 2 個 rollback generation job。這個限制只作用於同一張 ticket，不是全平台 queue。

prior backup 的資料備份必須在原 statement 之前完成，因此這段不能 async。它失敗時會寫入 rollback failure record、打 warning log，然後繼續執行原 statement。

## Failure model

Rollback generation 是 best effort。

| 事件 | 原 ticket execution | Rollback record |
|---|---|---|
| rollback 未啟用 | 照常執行 | `unsupported` |
| statement 不支援 | 照常執行 | `unsupported` 或 fallback 到 `my2sql` |
| prior backup DB 操作失敗 | 照常執行 | `failed` |
| 原 statement 執行失敗 | 原 statement failed | 不產生成功 rollback SQL |
| my2sql async job 失敗 | 原 statement completed | `failed` |
| rollback SQL 太大 | 原 statement completed | `failed` |

這個設計保留原 ticket execution 的主流程穩定性。rollback 功能失敗時，平台不應把原本已成功執行的 DML 再做隱式回滾。

## Trade-offs

好處：

- `UPDATE` / `DELETE` 可以在不依賴 cloud provider binlog 下載介面的情況下取得舊資料
- 備份資料存在獨立 `maestro_rollback` database，不污染業務 database
- rollback ticket 仍走既有 workflow、通知與 audit log
- `my2sql` async generation 不阻塞同一張 ticket 的後續 statement execution

代價：

- prior backup 會在原 statement 前增加一次備份 SQL，長查詢或大範圍 DML 會增加執行前成本
- prior backup 需要 readwrite user 有建立 backup database/table 的權限
- `my2sql` 仍依賴 binlog 設定、rollback credential 和工具相容性
- beta 階段仍需要 DBA 預覽 rollback SQL，不能直接自動執行

## Related

- [MySQL Rollback](../reference/mysql-rollback.md)
- [How to 設定與使用 MySQL Rollback](../how-to/configure-and-use-mysql-rollback.md)
- [Tickets](../reference/tickets.md)
- [DB Connections](../reference/db-connections.md)
