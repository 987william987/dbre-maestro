# How to 建立與執行 Tickets

這份指南說明如何建立 DDL / DML / Redis 工單、先做檢測，再完成審核與執行流程。

## Prerequisites

- 你已登入系統
- 建單者需要 `tickets.read` 與 `tickets.apply`
- Reviewer 需要 `tickets.review`
- Executor / DBA 需要 `tickets.execute`
- 目標 DB connection 必須在你的 DB Scope 內

## Steps

1. 打開 `New Ticket` 頁面。

   路徑是 `/tickets/new`。若你看不到，通常代表沒有 `tickets.apply`。若整個 Tickets 頁都看不到，先確認是否有 `tickets.read`。

2. 填寫標題、描述、工單類型與目標實例。

   `Target Instance` 與 `Target Database` 都應從下拉選單選擇，不應手動輸入。

   - MySQL / PostgreSQL：選 database 名稱
   - Redis：選 database index

3. 輸入 SQL 或 Redis command，必要時先點 `Format`。

   SQL 類型範例：

   ```sql
   ALTER TABLE orders ADD COLUMN archived_at DATETIME NULL;
   ```

   Redis 範例：

   ```text
   SET my:key "value"
   EXPIRE my:key 60
   ```

4. 點擊 `Review SQL` 做檢測。

   系統會回傳每一句 statement / command 的：

   - 內容
   - 掃描 / 影響行數
   - 審核狀態
   - 訊息

5. 確認 `Review SQL` 通過後，再提交工單。

   如果檢測未通過，請先修正內容；前端不應允許跳過檢測直接提交。

6. 到 `Ticket Detail` 查看詳細結果。

   詳情頁會保留每一句 statement 的 review result，供 reviewer 與 DBA 重新確認。

7. Reviewer 審核工單。

   Reviewer 除了要有對應 review permission，也要被 Workflow Rules 指定為該 workflow 的有效審批人。

   - 通過：`Approve`
   - 不通過：`Reject`
   - 若提交人改變需求且尚未開始審核：`Withdraw`

8. 若是 DDL / DML / Redis，Executor / DBA 再進行執行。

   流程通常是：

   - `Execute`
   - 若在待執行階段發現問題：可於 `approved` 或 `pending_execution` 階段 `Reject`

## 通知行為

Ticket 流程中的通知規則如下：

- 提交後通知審批人，不通知提交人自己
- 收回後通知審批人
- 審批拒絕後通知提交人
- 審批通過後：
  - `DDL / DML / Redis` 通知執行人
  - `SQL Export / Sensitive Query Access` 通知提交人
- 執行階段拒絕後通知提交人
- 執行成功後通知提交人
- 執行失敗後通知提交人與執行人

## Verification

可用以下方式確認流程成功：

- `/tickets` 列表看到新工單
- `/tickets/:ticket_no` 詳情頁可看到 review results
- 工單狀態從 `pending_review` 轉到下一階段
- 執行後有 `started_at`、`completed_at` 或失敗狀態
- 若是 MySQL DML 且已啟用 rollback，成功 statement 會顯示 rollback generation 狀態

## Troubleshooting

### 檢測明明有兩句 SQL，卻被當成一條

目前系統依 parser 拆 statement，不靠純字串 heuristic。若缺少分號或語法不完整，review 應回報 parser / syntax 問題，而不是誤判通過。

### 建單時看不到目標資料庫

先確認：

- 該 connection 是否在你的 DB Scope
- 目標實例是否可正常連線
- `/api/tickets/connections/{id}/databases` 是否成功回傳

### 工單執行卡住或中斷

後端在重啟時會把正在執行中的工單標記成 interrupted，避免假性長跑狀態殘留。

## 相關文件

- [Tickets 參考](../reference/tickets.md)
- [How to 設定與使用 MySQL Rollback](configure-and-use-mysql-rollback.md)
- [How to 設定 Workflow Rules](configure-workflow-rules.md)
- [後端 API 與權限對照](../reference/backend-api-and-permissions.md)
