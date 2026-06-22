# How to 建立 Scheduled SQL Report

本文說明如何建立定期 SQL 報表，讓系統定期查詢資料、產生 CSV，並透過 Lark App 推送給指定使用者。

## 前置條件

你需要具備：

- `scheduled_sql_reports.read`
- `scheduled_sql_reports.write`
- 目標 DB connection 的 DB Scope
- 對查詢目標的 Query Access

平台也需要先完成：

- Settings 已配置 Lark App ID / Secret
- 收件人 user 已設定 `lark_recipient`
- 目標 DB connection 已配置 readonly endpoint / credential

## 1. 進入 Scheduled Reports

開啟 `/scheduled-sql-reports`。

若看不到入口，先確認是否具備 `scheduled_sql_reports.read`。

## 2. 選擇 DB Connection

選擇目標 DB connection 後，再選 database / schema。

報表只支援 MySQL 與 PostgreSQL。Redis 不支援 Scheduled SQL Reports。

## 3. 填寫 SQL

SQL 必須是唯讀查詢：

- `SELECT`
- `WITH`
- `SHOW`

建立與更新時，後端會檢查：

- SQL 是否為 read-only
- 建立者是否有 query access
- 查詢是否涉及敏感欄位

如果查詢命中敏感欄位，系統會拒絕建立 report。Scheduled SQL Reports 不提供敏感欄位報表。

## 4. 設定 Cron 與 Timezone

Cron 使用五欄格式：

```text
0 9 * * *
```

Timezone 建議使用：

```text
Asia/Taipei
```

上述設定代表依 Asia/Taipei 每天 09:00 執行。

## 5. 選擇 Lark Recipients

至少要選一位收件人。收件人必須已在 User 資料上設定可投遞的 Lark Open ID。

報表執行成功後，系統會把 CSV 檔案直接推送到收件人的 Lark。

## 6. 查看 Run History

儲存後可以在右側 run history 查看：

- run status
- row count
- file name
- error message
- started / finished time

若 Lark 未配置、recipient 無法投遞、query access 被收緊或結果包含敏感欄位，run 會失敗並保留錯誤原因。

## 相關文件

- [Scheduled SQL Reports](../reference/scheduled-sql-reports.md)
- [Lark Open ID 綁定](bind-lark-open-id.md)
- [SQL Editor](../reference/sql-editor.md)
