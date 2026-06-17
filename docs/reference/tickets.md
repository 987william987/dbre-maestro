# Tickets

Tickets 模組負責 DDL / DML 變更工單，以及 SQL Export、Sensitive Query Access 兩類特殊工單的審批流轉。

## 功能範圍

| Ticket Type | 用途 |
|---|---|
| `ddl` | 結構變更 |
| `dml` | 資料變更 |
| `sql_export` | 從 SQL Editor 導出資料 |
| `sensitive_query_access` | 申請敏感欄位臨時查詢權限 |

## 頁面入口

| 頁面 | Route |
|---|---|
| Ticket List | `/tickets` |
| New Ticket | `/tickets/new` |
| Ticket Detail | `/tickets/:id` |

## 權限模型

| 權限 | 意義 |
|---|---|
| `tickets.apply` | 建立 DDL / DML 工單，且可進入 ticket workspace |
| `tickets.review` | 審核 DDL / DML 工單 |
| `tickets.execute` | 執行 DDL / DML 工單 |
| `sql_editor.export_review` | 審核 `sql_export` |
| `sql_editor.sensitive_review` | 審核 / 撤銷 `sensitive_query_access` |

## 建單前檢測

DDL / DML 工單在提交前，應先經過 `POST /api/tickets/review`。

檢測內容包括：

- statement parser 拆分
- syntax / parser 檢查
- ticket type policy
- SQL review rule 檢測
- 每一句 statement 的 review result

前端應要求檢測通過後才能提交。

## New Ticket API / Interface

### `GET /api/tickets/connections`

回傳建單可用 DB connections，已受 DB Scope 過濾。

### `GET /api/tickets/connections/{id}/databases`

回傳目標實例可選資料庫清單。前端應顯示為下拉選單，而不是自由輸入。

### `POST /api/tickets/review`

請求欄位：

| 欄位 | 型別 |
|---|---|
| `sql_content` | `string` |
| `ticket_type` | `ddl` 或 `dml` |
| `db_connection_id` | `number` |
| `database_name` | `string` |

回應欄位：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `passed` | `boolean` | 是否通過整體檢測 |
| `results` | `TicketReviewResult[]` | 每一句 statement 的檢測結果 |

`results` 至少包含：

- `seq`
- `sql_stmt`
- `scan_rows`
- `status`
- `message`

### `POST /api/tickets`

請求欄位：

| 欄位 | 型別 | 必填 |
|---|---|---|
| `title` | `string` | 是 |
| `description` | `string \| null` | 否 |
| `sql_content` | `string` | 是 |
| `ticket_type` | `ddl \| dml` | 是 |
| `db_connection_id` | `number \| null` | 是 |
| `database_name` | `string \| null` | 是 |

## Workflow

### DDL / DML

```text
pending_review
  -> approved
  -> rejected
  -> withdrawn

approved
  -> pending_execution
  -> rejected

pending_execution
  -> rejected
  -> executing

executing
  -> interrupted
  -> failed
  -> completed
```

### SQL Export

```text
pending_review
  -> approved (export ready)
  -> rejected
  -> withdrawn
```

### Sensitive Query Access

```text
pending_review
  -> approved (scope 生效直到 approved_until)
  -> rejected
  -> withdrawn
  -> revoked
```

## 通知規則

目前 Ticket 通知同時會走站內通知與 Lark；是否派送給自己，依事件策略決定。

### 提交與收回

- 提交人送出工單後：
  - 不通知提交人自己
  - 通知對應審批人
- 提交人收回工單後：
  - 通知對應審批人

### 審批階段

- 審批人拒絕後：
  - 通知提交人
- 審批人同意後：
  - `ddl` / `dml`：通知執行人
  - `sql_export` / `sensitive_query_access`：通知提交人

### 執行階段

- 執行人於 `approved` / `pending_execution` 階段拒絕後：
  - 通知提交人
- 執行成功後：
  - 通知提交人
- 執行失敗後：
  - 通知提交人
  - 額外通知執行人

### 邊界規則

- 若提交人與執行人是同一人：
  - 仍需符合上述通知規則
  - 例如執行成功時，提交人本人仍應收到成功通知

## Ticket Detail

Ticket Detail 頁會展示：

- 工單基本資訊
- DB connection / database
- submitter / reviewer / executor
- SQL content
- scopes
- review results
- export request（若適用）
- execution records

Review results 應以 statement 粒度呈現，而不是只顯示一個整體結論。

## Review Results

review result 是多 statement 工單的重要輸出。使用場景：

- 建單前檢測
- 工單詳情重看每一句的審核結果
- DBA / Reviewer 對照哪些語句被拒絕

重要欄位：

| 欄位 | 說明 |
|---|---|
| `seq` | 第幾句 statement |
| `sql_stmt` | 原始 statement |
| `scan_rows` | 掃描 / 影響行數 |
| `status` | 審核狀態 |
| `message` | 錯誤或說明 |

## Ticket Number

Ticket number 不使用單純 auto increment 流水號，而改成較不易碰撞、也較不易被猜測的格式。

目前策略是：

- 時間戳
- 加上隨機 6 位尾碼

## Export Download

匯出下載目前是透過 `GET /api/exports/download/{token}` 完成，並帶有：

- token-based access
- 過期時間
- 每分鐘最多 3 次下載限制

## 範例

### Review SQL

```json
{
  "sql_content": "ALTER TABLE orders ADD COLUMN archived_at DATETIME NULL;",
  "ticket_type": "ddl",
  "db_connection_id": 3,
  "database_name": "order_db"
}
```

### Create Ticket

```json
{
  "title": "Add archived_at to orders",
  "description": "Needed by retention workflow",
  "sql_content": "ALTER TABLE orders ADD COLUMN archived_at DATETIME NULL;",
  "ticket_type": "ddl",
  "db_connection_id": 3,
  "database_name": "order_db"
}
```

## 相關文件

- [How to 建立與執行 Tickets](../how-to/create-and-execute-tickets.md)
- [後端 API 與權限對照](backend-api-and-permissions.md)
- [SQL Editor](sql-editor.md)
