# How to 使用 SQL Editor

這份指南示範如何在 SQL Editor 中選資料源、執行查詢、格式化 SQL、看 Explain，並在需要時送出 export 或 sensitive access 工單。

## Prerequisites

- 你已登入系統
- 你擁有 `sql_editor.query`
- 你的帳號已被授權至少一個 DB connection scope
- 若要送 export / sensitive access，還需要對應動作 permission

## Steps

1. 打開 `SQL Editor` 頁面。

   頁面路徑是 `/sql-editor`。如果你看不到這個頁面，先確認是否有 `sql_editor.query`。

2. 在左側資產樹選擇資料源與目標物件。

   選定後，當前 tab 會記住自己的 connection、database、schema 與資產樹展開狀態。

3. 在編輯器輸入單一查詢 statement。

   ```sql
   SELECT id, email
   FROM users
   LIMIT 20;
   ```

   SQL Editor 目前只接受單一 statement。若貼入多句 SQL，查詢與 Explain 都會被拒絕。

4. 點擊 `Format` 美化 SQL。

   這會依資料源類型套用對應 SQL formatter。若你有反白選取 SQL，系統只會格式化選取片段。

5. 點擊 `Run Query` 執行查詢。

   成功後可在下方看到：

   - 結果表格
   - 垂直檢視
   - Object metadata
   - History
   - Saved queries

6. 若要看執行計畫，點擊 `Explain`。

   系統會把當前 statement 包成 `EXPLAIN ...;` 後送出。若原本已是 `EXPLAIN`，就直接沿用。

7. 若查詢結果需要匯出，點擊匯出相關操作並建立 export 工單。

   你需要 `sql_editor.export`。匯出不是立即下載，而是走工單審核流程。

8. 若查詢命中敏感欄位且需要看原值，建立 sensitive access 工單。

   你需要 `sql_editor.sensitive_apply`。審核通過後，批准時間內可對應 scope 看未遮罩結果。

## Verification

你可以用以下方式確認操作成功：

- `Run Query` 後，結果區顯示 `row_count` 與 `duration`
- `Explain` 後，結果區出現執行計畫資料
- `Saved queries` 可看到新收藏
- 工單送出後，可在 `/tickets` 看到對應工單

## Troubleshooting

### 看不到任何資料源

可能原因：

- 沒有 `sql_editor.query`
- 有權限，但沒有任何 DB Scope

### Query timeout

如果查詢被系統終止，先確認：

- SQL Editor app timeout
- MySQL `max_execution_time`
- PostgreSQL `statement_timeout`

這三個值由 Settings 頁面控制。

### Metadata 錯誤

若左側資產樹出現暫時錯誤訊息，代表 metadata 讀取失敗。前端只顯示簡短錯誤，實際細節會寫在後端日誌。

### 匯出下載被限流

目前下載限制是 1 分鐘最多 3 次。超過時前端會提示暫時錯誤訊息。

## 相關文件

- [SQL Editor 參考](../reference/sql-editor.md)
- [Tickets](../reference/tickets.md)
- [Masking 與 DSL](../reference/masking-and-dsl.md)
