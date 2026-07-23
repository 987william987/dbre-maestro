# SQL Editor

SQL Editor 是平台上的受控查詢工作區，用於 MySQL、PostgreSQL 與 Redis 的讀取場景。

## 功能定位

- 用於查詢資料，不是通用變更 console
- 單一 tab 代表一個獨立工作區
- 支援資產樹瀏覽、查詢、格式化、Explain、歷史、收藏、匯出申請與 Sensitive Access 申請

## 頁面入口

- Route：`/sql-editor`
- 前端 route guard：`sql_editor.read`
- 主要 API namespace：`/api/query`

`sql_editor.read` 只代表能進入 SQL Editor 頁面。實際查詢、歷史、收藏與 metadata 讀取仍需要 `sql_editor.query`。

## Tab 工作區狀態

每個 tab 都維護自己的狀態，至少包括：

- 已選資料源 `connectionId`
- 目標 `database` / `schema`
- SQL 內容與滑鼠選取片段
- 資產樹展開狀態與搜尋狀態
- 查詢結果與錯誤訊息
- Result view 模式
- Columns / Definition 詳情
- Sensitive access duration
- 分頁頁碼
- 該 tab 是否正在執行查詢

重新整理頁面或重新登入後，SQL Editor 會重置成單一預設 tab，SQL 內容回到 `SELECT 1;`。

## 支援資料庫類型

| DB Type | 支援項目 |
|---|---|
| MySQL | 查詢、metadata、Format、Explain、export、sensitive access |
| PostgreSQL | 查詢、metadata、Format、Explain、export、sensitive access |
| Redis | 查詢與部分 metadata / DB index 工作流；不使用 SQL formatter |

## 自動補全

SQL Editor 使用 CodeMirror 與 `@codemirror/lang-sql` 提供 MySQL / PostgreSQL 補全；Redis command 不走 SQL 補全。

補全來源：

- dialect 依目前 DB connection type 選擇 MySQL 或 PostgreSQL
- 資產樹已載入的 database / schema / table 名稱
- 使用者目前選中的表與其欄位
- `@codemirror/lang-sql` 內建 schema / keyword completion

為了讓日常查詢更貼近 RD 使用情境，前端有一層輕量 context adapter：

- 在 `SELECT`、`WHERE`、`GROUP BY`、`ORDER BY`、`HAVING` 等位置，若已選中表且欄位已載入，優先只提示該表欄位
- 在 `FROM`、`JOIN`、`UPDATE`、`INTO`、`DESCRIBE`、`ALTER TABLE`、`DROP TABLE`、`TRUNCATE TABLE` 等位置，優先提示表名
- 若無法判斷 context，或沒有已選表欄位，退回官方 SQL schema completion

目前補全不是完整 SQL parser，也不保證理解所有 alias、CTE 或跨多表 join 情境。若未來需要更完整的語意補全，可評估導入 Monaco 或 language-server 等方案。

## 查詢限制

### Statement 規則

- 一次只允許單一 statement
- 只允許唯讀查詢類型
- `Explain` 也只支援單一 statement
- 若使用者有選取片段，功能會優先作用在選取 SQL
- 因此整個 editor 可以保留多句草稿，但真正執行 / Explain / export / sensitive access 的是目前選取片段或唯一 statement

### 快捷鍵

- `Cmd/Ctrl + Enter`：執行目前 statement 或選取片段

### Query Timeout

SQL Editor 查詢受三層限制：

| 層級 | 來源 | 預設 |
|---|---|---|
| App timeout | `sql_editor_app_timeout_seconds` | `30s` |
| MySQL session timeout | `sql_editor_mysql_max_execution_time_ms` | `25000ms` |
| PostgreSQL session timeout | `sql_editor_postgres_statement_timeout_ms` | `25000ms` |

這三個值都由 Settings 頁面控制，而且只作用於 SQL Editor `/api/query`。

## Connection / Pool

SQL Editor 走 `query` pool profile。預設值：

| 參數 | 預設 |
|---|---|
| `MaxOpenConns` | `10` |
| `MaxIdleConns` | `5` |
| `ConnMaxLifetime` | `5m` |
| `ConnMaxIdleTime` | `2m` |

## API / Interface

### `GET /api/query/connections`

回傳目前使用者可用的 DB connections。結果已受 DB Scope 過濾。

### `GET /api/query/constraints`

回傳目前 SQL Editor 工具列需要的限制資訊，例如：

- `default_limit`
- `max_limit`
- `app_timeout_seconds`
- `mysql_max_execution_time_ms`
- `postgres_statement_timeout_ms`

### `POST /api/query`

請求欄位：

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `db_connection_id` | `number` | 是 | 目標資料源 |
| `sql` | `string` | 是 | 單一查詢 statement |
| `limit` | `number` | 否 | 結果限制 |
| `database` | `string` | 否 | 目標 database |
| `schema` | `string` | 否 | PostgreSQL schema |
| `redis_db_index` | `number` | 否 | Redis DB index |

回應重點：

- `columns`
- `raw_columns`
- `rows`
- `row_count`
- `duration_ms`
- `sensitive_column_indexes`

### `POST /api/query/sensitive-access`

建立 `sensitive_query_access` 工單。

請求欄位：

| 欄位 | 型別 |
|---|---|
| `db_connection_id` | `number` |
| `sql_content` | `string` |
| `database_name` | `string` |
| `schema_name` | `string` |
| `approved_duration_minutes` | `number` |

### Saved Query / History

| API | 用途 |
|---|---|
| `GET /api/query/history` | 最近查詢歷史 |
| `GET /api/query/saved-queries` | 常用 SQL 列表 |
| `POST /api/query/saved-queries` | 新增收藏 |
| `DELETE /api/query/saved-queries/{id}` | 刪除收藏 |

`GET /api/query/history` 只回傳目前登入使用者自己的最近 20 筆查詢紀錄；不會列出其他使用者的 SQL Editor history。

## Metadata Explorer

SQL Editor 左側資產樹使用：

- `GET /api/db-connections/{id}/metadata`
- `GET /api/db-connections/{id}/metadata/{schema}/{table}/columns`
- `GET /api/db-connections/{id}/metadata/{schema}/{table}/definition`

錯誤策略：

- metadata 讀取失敗時，前端只顯示暫時錯誤訊息
- 實際錯誤細節應寫入後端日誌
- 不應把原始錯誤直接灌滿整棵資產樹
- 搜尋結果與一般樹狀展開狀態都應維持在 tab 內隔離

## Format / Explain

### Format

- 前端使用 `sql-formatter`
- Dialect 依資料源決定：MySQL 用 `mysql`、PostgreSQL 用 `postgresql`
- 若有選取 SQL，優先格式化選取區塊
- Format 是前端本地處理，正常情況應為即時操作，不需呼叫後端

### Explain

- 若 SQL 已經以 `EXPLAIN` 開頭，直接沿用
- 否則自動包成 `EXPLAIN <statement>;`
- 多 statement 會被拒絕

## 匯出

匯出是透過建立 `sql_export` 工單完成，不是直接把查詢結果檔案回傳。

建立匯出需要：

- 頁面權限：`sql_editor.read`
- 查詢能力：`sql_editor.query`
- 動作權限：`sql_editor.export`
- 目標資料源必須在使用者 DB Scope 內

匯出會建立 `sql_export` 工單。若查詢結果包含敏感欄位，工單會標記為敏感導出；否則是普通導出。敏感導出永遠需要審批，普通導出是否需要審批由 Settings 的 `require_non_sensitive_export_review` 控制。即使普通導出不需審批，系統仍會建立 export ticket 作為稽核紀錄。

下載限制與回饋：

- `GET /api/exports/{id}/download` 為登入後下載，不在 URL 暴露 token
- 每個使用者對每個 export 1 分鐘內最多 5 次下載
- 前端應以頁內錯誤提示或 toast 顯示限制，不應跳轉到獨立錯誤頁

## 敏感資料

查詢結果若命中敏感欄位：

- 預設依 masking rule 套用脫敏
- 具備 `global.sensitive` 時可直接看原值
- 沒有永久權限時，可用 `sql_editor.sensitive_apply` 送 sensitive access 工單

多來源 expression 的規則：

- 若命中 2 個以上敏感來源且 mask mode 不一致，退化成 `full mask`

## 例子

### 基本查詢

```json
{
  "db_connection_id": 12,
  "sql": "SELECT id, email FROM users LIMIT 20;",
  "database": "app_db"
}
```

### PostgreSQL Explain

```json
{
  "db_connection_id": 18,
  "sql": "EXPLAIN SELECT * FROM public.orders WHERE id = 1;",
  "database": "orders",
  "schema": "public"
}
```

## 相關文件

- [How to 使用 SQL Editor](../how-to/use-sql-editor.md)
- [Tickets](tickets.md)
- [Workflow Rules](workflow-rules.md)
- [DB Connections](db-connections.md)
- [Masking 與 DSL](masking-and-dsl.md)
- [平台 Settings](settings.md)
