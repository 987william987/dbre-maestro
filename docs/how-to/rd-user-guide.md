# How to RD 使用 DBRE Maestro

本文給一般 RD / 研發使用者，說明如何在 DBRE Maestro 內提工單、申請查詢權限、執行查詢、申請資料導出，以及申請敏感資料臨時查看權限。

## 前置條件

- 你已取得平台帳號並可登入
- 你的帳號狀態是 active
- 你至少被授權一個 DB connection scope
- 你具備對應功能權限

常見權限如下：

| 需求 | 需要的權限 |
|---|---|
| 進入 Tickets | `tickets.read` |
| 建立 DDL / DML / Redis / Query Access 工單 | `tickets.apply` |
| 進入 SQL Editor | `sql_editor.read` |
| 執行查詢、讀取 metadata | `sql_editor.query` |
| 建立 SQL Export 工單 | `sql_editor.export` |
| 建立 Sensitive Query Access 工單 | `sql_editor.sensitive_apply` |

如果你看不到某個頁面，通常是缺少頁面入口權限。如果你看得到頁面但看不到資料源，通常是缺少 DB Scope 或查詢動作權限。

## 1. 登入與第一次檢查

1. 開啟平台網址。
2. 使用帳號密碼登入。
3. 如果系統要求 MFA，依畫面掃描 QR code 或輸入 setup key，完成 6 位 TOTP 驗證碼設定。
4. 登入後確認你能看到自己的主要工作頁：
   - `Tickets`
   - `SQL Editor`
   - `Notifications`

正式環境不要共用帳號、密碼或 MFA QR code。每個使用者應使用自己的帳號和自己的 MFA secret。

平台目前沒有使用者自助改密碼或忘記密碼的流程，密碼只能由 admin 重設。如果需要變更密碼，請直接聯絡 admin 協助處理。

## 2. 提 DDL / DML / Redis 工單

DDL / DML / Redis 工單從 `Tickets > New Ticket` 建立。

1. 打開 `Tickets`。
2. 點擊 `New Ticket`。
3. 選擇工單類型：
   - `DDL`：結構變更，例如 `ALTER TABLE`
   - `DML`：資料變更，例如 `UPDATE` / `DELETE`
   - `Redis`：受控 Redis command
4. 選擇目標 DB connection 與 database。請使用下拉選單，不要手動猜名稱。
5. 填寫標題與描述。描述應包含：
   - 變更目的
   - 影響範圍
   - 回滾方式或風險說明
6. 輸入 SQL 或 Redis command。
7. 點擊 `Review SQL`。
8. 確認每一句 statement / command 都通過檢測。
9. 提交工單。

提交後，工單會進入審批流程。審批通過後，DDL / DML / Redis 工單還需要 executor / DBA 執行。

驗證方式：

- `Tickets` 列表可看到新工單
- Ticket Detail 內有 review result
- 狀態從 `pending_review` 進入下一階段

## 3. 申請 Query Access 查詢權限

Query Access 用來申請 database / table 級查詢授權。它不是直接執行 SQL，而是讓你在審批通過後可以查指定範圍。

你可以從兩個地方申請：

- `Tickets > New Ticket` 建立 `Query Access`
- `SQL Editor` 內查詢被拒絕時，使用快捷入口申請

申請時應填清楚：

- 目標 DB connection
- database / table 範圍
- 申請原因
- 需要多久

Query Access 審批通過後會立即生效，不需要 DBA 執行。若權限被提前回收，工單狀態可能變成 `stopped`。

驗證方式：

- 工單狀態為 `approved`
- 回到 SQL Editor 後，對應 scope 的查詢不再被拒絕

## 4. 使用 SQL Editor 查詢

SQL Editor 是受控查詢工作區，用於查詢資料、看 metadata、Explain、收藏 SQL、建立導出或敏感查看申請。

1. 打開 `SQL Editor`。
2. 在左側資產樹選擇 DB connection、database、schema 或 table。
3. 在 editor 輸入查詢。

   ```sql
   SELECT id, email
   FROM users
   LIMIT 20;
   ```

4. 點擊 `Format` 整理 SQL。
5. 點擊 `Run Query`，或使用 `Cmd/Ctrl + Enter`。
6. 在結果區查看表格、垂直檢視、metadata、history 或 saved queries。

注意事項：

- 真正執行時只接受單一 statement。
- 如果你反白選取 SQL，系統會優先執行選取片段。
- SQL Editor 用於唯讀查詢，不是一般變更 console。
- 查詢 timeout 由平台 Settings 控制。

## 5. 查看 Explain

Explain 用來看 SQL 執行計畫。

1. 在 SQL Editor 選定目標 DB connection 與 database。
2. 輸入單一查詢 statement。
3. 點擊 `Explain`。

如果 SQL 已經以 `EXPLAIN` 開頭，系統會直接沿用；否則會自動包成 `EXPLAIN <statement>`。

## 6. 建立 SQL Export 導出工單

SQL Export 必須從 SQL Editor 建立，不能從 `Tickets > New Ticket` 手動建立。

1. 在 SQL Editor 選擇資料源。
2. 輸入要導出的單一查詢 statement。
3. 確認查詢條件足夠收斂，避免導出不必要資料。
4. 點擊 export 相關操作並提交導出申請。
5. 等待審批或系統自動通過。
6. 工單 ready 後，從工單詳情下載導出檔案。

普通導出是否需要審批由 Settings 控制。敏感導出永遠需要審批。即使普通導出不需審批，系統仍會建立 export ticket 作為稽核紀錄。

下載限制：

- 下載連結是 token-based
- 1 分鐘最多下載 3 次
- 過期後需要重新從工單頁取得可用下載入口

## 7. 申請 Sensitive Query Access 臨時查看敏感資料

當查詢結果命中敏感欄位時，平台會依 masking rule 顯示脫敏值。若你有業務理由需要看原值，可以申請 Sensitive Query Access。

1. 在 SQL Editor 執行或準備好目標查詢。
2. 選擇 sensitive access 申請操作。
3. 填寫申請原因與需要的有效時間。
4. 提交工單。
5. 審批通過後，在批准時間內重新查詢對應 scope。

注意事項：

- Sensitive Query Access 是臨時授權。
- 超過批准時間後會失效。
- Reviewer / Admin / DBA 可提前 revoke。
- 如果只是需要匯出資料，應走 SQL Export，而不是用臨時查看權限繞過導出流程。

## 8. 查看通知與工單狀態

平台會在工單流程中發送站內通知與 Lark 通知。

常見通知：

- 工單提交後通知審批人
- 審批拒絕後通知提交人
- DDL / DML / Redis 審批通過後通知執行人
- SQL Export / Sensitive Query Access 審批通過後通知提交人
- 執行成功或失敗後通知提交人

你可以在 `Tickets` 列表或 Ticket Detail 追蹤狀態、審批紀錄、執行結果與 activity timeline。

## 常見問題

### 看不到 SQL Editor

通常是缺少 `sql_editor.read`。如果看得到 SQL Editor 但不能查詢，通常是缺少 `sql_editor.query`。

### 看不到任何 DB connection

請確認 DBA/Admin 是否已把 DB connection scope 綁到你的 user 或 auth group。

### 查詢被拒絕

常見原因：

- 沒有 Query Access
- Query Access scope 不包含該 database / table
- 有 deny rule 命中，deny 會優先於 allow
- 查詢不是唯讀 statement

### 工單一直沒有人審批

可能是 Workflow Rules 沒有解析到有效 reviewer。請把工單號提供給 DBA/Admin 檢查 rule、auth group 與通知設定。

### 導出工單通過但不能下載

可能是下載 token 過期，或 1 分鐘下載超過 3 次。回到工單詳情重新操作，或稍後再試。

## 相關文件

- [How to 使用 SQL Editor](use-sql-editor.md)
- [How to 建立與執行 Tickets](create-and-execute-tickets.md)
- [Tickets 參考](../reference/tickets.md)
- [SQL Editor 參考](../reference/sql-editor.md)
- [Masking 與 DSL](../reference/masking-and-dsl.md)
