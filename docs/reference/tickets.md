# Tickets

Tickets 模組負責 DDL / DML / Redis 變更工單、Query Access 查詢授權工單，以及 SQL Export、Sensitive Query Access 兩類特殊工單的審批流轉。

## 功能範圍

| Ticket Type | 用途 |
|---|---|
| `ddl` | 結構變更 |
| `dml` | 資料變更 |
| `redis` | 受控 Redis 命令變更 |
| `query_access` | 申請 database / table 級查詢授權 |
| `sql_export` | 從 SQL Editor 導出資料 |
| `sensitive_query_access` | 申請敏感欄位臨時查詢權限 |

## 頁面入口

| 頁面 | Route |
|---|---|
| Ticket List | `/tickets` |
| New Ticket | `/tickets/new` |
| Ticket Detail | `/tickets/:ticket_no` |

## 權限模型

| 權限 | 意義 |
|---|---|
| `tickets.read` | 進入 Tickets workspace，查看自己被允許看到的工單 |
| `tickets.apply` | 建立 DDL / DML / Redis / Query Access 工單 |
| `tickets.review` | 審核 DDL / DML / Redis / Query Access 工單 |
| `tickets.execute` | 執行 DDL / DML / Redis 工單 |
| `sql_editor.export` | 從 SQL Editor 建立 `sql_export` 工單 |
| `sql_editor.export_review` | 審核 `sql_export` |
| `sql_editor.sensitive_apply` | 從 SQL Editor 建立 `sensitive_query_access` 工單 |
| `sql_editor.sensitive_review` | 審核 / 撤銷 `sensitive_query_access` |

實際上：

- Tickets workspace 的頁面入口是 `tickets.read`
- `tickets.apply` 只代表能建立一般工單，不代表能審批或執行
- 可建立的連線清單仍受 DB Scope 過濾
- 工單列表與詳情仍會依 ticket access 控制可見範圍

`query_access` 復用既有 Tickets workspace：

- 不新增獨立的 `query_access.apply`
- 不新增獨立的 `query_access.review`
- 建單沿用 `tickets.apply`
- 審批 / 提前回收沿用 `tickets.review`

## 入口模型

並非所有 ticket type 都有相同入口。入口是否存在，取決於該工單是否依賴當前查詢上下文。

### `Tickets > New Ticket`

以下工單可獨立建立：

- `ddl`
- `dml`
- `redis`
- `query_access`

### `SQL Editor`

以下工單依賴當前查詢上下文，因此入口位於 SQL Editor：

- `sql_export`
- `sensitive_query_access`

另外，`query_access` 也提供 SQL Editor 快捷入口，方便在查詢被拒絕時直接發起申請。

`POST /api/tickets` 只接受 `ddl`、`dml`、`redis`、`query_access`。`sql_export` 與 `sensitive_query_access` 必須走 SQL Editor 專用流程建立。

## 建單前檢測

DDL / DML / Redis 工單在提交前，應先經過 `POST /api/tickets/review`。

檢測內容包括：

- statement parser 拆分
- syntax / parser 檢查
- ticket type policy
- SQL review rule 檢測
- validation 檢測
- 每一句 statement 的 review result

前端應要求檢測通過後才能提交。

## New Ticket API / Interface

### `GET /api/tickets/connections`

回傳建單可用 DB connections，已受 DB Scope 過濾。

### `GET /api/tickets/connections/{id}/databases`

回傳目標實例可選資料庫清單。前端應顯示為下拉選單，而不是自由輸入。

- MySQL / PostgreSQL：目標 database 名稱
- Redis：目標 database index

### `POST /api/tickets/review`

請求欄位：

| 欄位 | 型別 |
|---|---|
| `sql_content` | `string` |
| `ticket_type` | `ddl`、`dml` 或 `redis` |
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
| `ticket_type` | `ddl \| dml \| redis \| query_access` | 是 |
| `db_connection_id` | `number \| null` | 是 |
| `database_name` | `string \| null` | 是 |

若 `ticket_type` 是 `sql_export` 或 `sensitive_query_access`，後端會拒絕請求；這兩種工單需要透過 SQL Editor 的 export 或 sensitive access API 建立。

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

### Redis

Redis 工單沿用 DDL / DML 的 reviewer / executor workflow：

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
  -> failed
  -> completed
```

### Query Access

`query_access` 採審批即生效，不進 execution：

```text
pending_review
  -> approved
  -> rejected
  -> withdrawn
  -> stopped
```

說明：

- `approved`：對應 Query Access rules 生效
- `stopped`：已生效 rules 被 reviewer / admin / dba 提前回收，或權限被手動停止

Query Access 使用 rule-based 授權：

| 欄位 | 語義 |
|---|---|
| `subject_type` | `user` 或 `auth_group` |
| `effect` | `allow` 或 `deny` |
| `connection_id` | 目標 DB connection |
| `database_pattern` | `*` 或指定 database |
| `table_pattern` | `*` 或指定 table |

查詢校驗時會彙總使用者 direct rules 與其有效 auth group rules。`deny` 永遠優先於 `allow`，因此可用 `allow a1.*.*` 搭配 `deny a1.secret_db.*` 表達測試環境的反向授權。

### SQL Export

SQL Export 有普通導出與敏感導出兩種語義，使用同一個 `sql_export` ticket type，並用 `contains_sensitive` 區分：

| 欄位 | 語義 |
|---|---|
| `contains_sensitive = false` 或 `null` | 普通數據導出 |
| `contains_sensitive = true` | 敏感數據導出 |

敏感導出永遠需要審批。普通導出是否需要審批，由 Settings 的 `require_non_sensitive_export_review` 控制；關閉時，普通導出會自動成為可下載狀態，但仍建立一張 export ticket 作為稽核紀錄。

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

SQL Export 通知會補充工單類型語義，讓收件人能分辨普通數據導出與敏感數據導出。

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
  - `ddl` / `dml` / `redis`：通知執行人
  - `sql_export` / `sensitive_query_access` / `query_access`：通知提交人

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

- 工單基本資訊與審批流程
- DB connection / database
- submitter / reviewer / executor
- SQL 或 Redis command 內容
- statement-level review results
- execution statement results
- activity timeline 與操作紀錄
- scope / export request / sensitive access details（依類型顯示）
- query access scope / duration / revoke 狀態（依類型顯示）

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

對 DDL 工單，validation 還可能包含 shadow validation 結果。

## MySQL DDL Shadow Validation

MySQL DDL review 目前會額外做 shadow validation，目的不是替代正式執行，而是盡量提前抓出：

- 目標 database / table 不存在
- 重複名稱
- statement 在複製出的結構環境中無法執行

目前範圍：

- 以 MySQL 為主
- 聚焦 database / table 等常見 DDL
- 不處理 stored procedure、trigger、event 等複雜物件

若平台 validation database 權限未配置，review result 會回報 shadow validation unavailable，而不是直接把底層錯誤細節暴露到前端。

## Ticket Number

Ticket number 不使用單純 auto increment 流水號，而改成較不易碰撞、也較不易被猜測的格式。

目前策略是：

- 時間戳
- 加上隨機 6 位尾碼

## Export Download

匯出下載目前是透過 `GET /api/exports/{id}/download` 完成，並帶有：

- authenticated access，限 requester、approver 或 `sql_editor.export_review`
- 過期時間
- 每分鐘最多 3 次下載限制
- 每次下載成功與失敗原因都會寫入 ticket activity

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
- [Workflow Rules](workflow-rules.md)
- [How to 設定 Workflow Rules](../how-to/configure-workflow-rules.md)
- [後端 API 與權限對照](backend-api-and-permissions.md)
- [SQL Editor](sql-editor.md)
- [DB Connections](db-connections.md)
