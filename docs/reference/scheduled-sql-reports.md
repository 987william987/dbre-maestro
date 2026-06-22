# Scheduled SQL Reports

Scheduled SQL Reports 是 Workbench 的定期查詢報表功能。它讓具備權限的使用者設定 DB connection、database / schema、唯讀 SQL、cron expression、timezone 與 Lark recipients；排程到期後，後端執行查詢、產生 CSV，並透過 Lark App 模式把檔案推送給指定使用者。

## 頁面入口

| 項目 | 值 |
|---|---|
| Route | `/scheduled-sql-reports` |
| Navigation | Workbench / Scheduled Reports |
| API namespace | `/api/scheduled-sql-reports` |

## 權限

| 權限 | 用途 |
|---|---|
| `scheduled_sql_reports.read` | 進入頁面、查看 report definitions 與 run history |
| `scheduled_sql_reports.write` | 建立、更新、啟用、停用、刪除 reports |

新增權限會自動授予 admin user 與 admin auth group。

建立與更新 report 時，仍會檢查建立者對目標 DB connection 的 DB Scope 與 Query Access。

## 支援範圍

| 項目 | 行為 |
|---|---|
| DB 類型 | MySQL、PostgreSQL |
| SQL 類型 | 只允許 `SELECT`、`WITH`、`SHOW` 開頭，並通過 read-only 檢查 |
| 敏感資料 | 禁止建立包含敏感欄位的 report |
| 輸出格式 | CSV |
| 發送方式 | Lark App 檔案推送 |
| 執行帳號 | DB connection 的 readonly credential |
| 執行 endpoint | DB connection resolved readonly endpoint |

執行時還會再次檢查 query access 與敏感欄位；若規則在建立後被收緊，後續 run 會失敗並記錄原因。

## Cron 與 Timezone

`cron_expression` 使用五欄 crontab 格式，例如：

```text
0 9 * * *
```

`timezone` 使用 IANA timezone，例如：

```text
Asia/Taipei
```

系統會依 report 的 timezone 計算下一次執行時間，並以 UTC 寫入資料庫。

## Scheduler

Scheduled SQL Reports 使用後端 scheduler 掃描 due reports。到期 report 會先被 claim，避免同一筆 report 被重複執行。

執行流程：

1. 找出已啟用且 `next_run_at` 到期的 reports
2. claim report
3. 使用 readonly credential 執行查詢
4. 確認結果不包含敏感欄位
5. 產生 CSV
6. 使用 Lark App 將檔案推送給 recipients
7. 寫入 run history 與 audit log
8. 計算下一次 `next_run_at`

## Lark 前置條件

要成功推送檔案，需要：

- Settings 已設定 Lark App ID / Secret
- 收件使用者已設定可投遞的 `lark_recipient`
- report 至少指定一位 recipient

Webhook fallback 不適合此功能，因為報表需要定向檔案推送。

## 資料表

### `scheduled_sql_reports`

| 欄位 | 說明 |
|---|---|
| `name` | 報表名稱 |
| `description` | 說明 |
| `db_connection_id` | 目標 DB connection |
| `database_name` | database |
| `schema_name` | PostgreSQL schema |
| `sql_content` | 報表 SQL |
| `cron_expression` | 五欄 cron |
| `timezone` | IANA timezone |
| `recipient_user_ids` | Lark recipients |
| `is_active` | 是否啟用 |
| `next_run_at` | 下一次執行時間 |
| `last_run_at` | 最近一次執行時間 |
| `last_status` | 最近一次執行狀態 |
| `last_error` | 最近一次錯誤 |
| `created_by` | 建立者 |
| `updated_by` | 最後更新者 |

### `scheduled_sql_report_runs`

| 欄位 | 說明 |
|---|---|
| `report_id` | 對應 report |
| `status` | `success` 或 `failed` |
| `row_count` | CSV rows 數量 |
| `file_name` | 產生的檔名 |
| `error_message` | 失敗原因 |
| `started_at` | 開始時間 |
| `finished_at` | 結束時間 |

## API

| API | 用途 |
|---|---|
| `GET /api/scheduled-sql-reports` | 列表 |
| `GET /api/scheduled-sql-reports/{id}` | 詳情與 run history |
| `GET /api/scheduled-sql-reports/connections` | 可用 DB connections |
| `GET /api/scheduled-sql-reports/recipients` | 可選 Lark recipients |
| `POST /api/scheduled-sql-reports` | 建立 report |
| `PATCH /api/scheduled-sql-reports/{id}` | 更新 report |
| `DELETE /api/scheduled-sql-reports/{id}` | 刪除 report |

## Audit Log

主要 audit action：

- `scheduled_sql_report_create`
- `scheduled_sql_report_update`
- `scheduled_sql_report_delete`
- `scheduled_sql_report_run`
- `scheduled_sql_report_run_failed`
- `scheduled_sql_report_delivery_failed`

## 相關文件

- [How to 建立 Scheduled SQL Report](../how-to/create-scheduled-sql-report.md)
- [SQL Editor](sql-editor.md)
- [DB Connections](db-connections.md)
- [Lark Open ID 綁定](../how-to/bind-lark-open-id.md)
