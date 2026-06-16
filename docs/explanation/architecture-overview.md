# 架構總覽

DBRE Maestro 的核心目標，是把資料庫治理拆成一個獨立的控制平面：使用者在平台上查詢資料、送審工單、套用脫敏規則與管理權限；平台再以受控方式連到外部 MySQL、PostgreSQL 與 Redis。

## 系統解決的問題

如果直接把正式資料庫帳密交給所有工程師，常見問題會集中在三類：

- 查詢權限與操作權限混在一起，難以區分誰可以看、誰可以改
- 敏感欄位缺少統一遮罩策略，查詢結果容易外洩
- 變更流程分散在聊天、腳本與人工操作，缺少可追溯的審批與執行紀錄

DBRE Maestro 把這些能力集中在同一個工作台裡，並用 RBAC、DB Scope、Ticket Workflow 與 Audit Log 做閉環。

## 整體組成

```text
+------------------+       HTTP        +----------------------+
| React Frontend   | <---------------> | Go API Server        |
| Vite             |                   | chi + handlers       |
+------------------+                   +----------+-----------+
                                                 |
                                                 | sqlx
                                                 v
                                      +----------------------+
                                      | Meta DB (MySQL)      |
                                      | users / tickets /    |
                                      | settings / metadata  |
                                      +----------------------+
                                                 |
                           +---------------------+----------------------+
                           |                     |                      |
                           v                     v                      v
                 +----------------+   +--------------------+   +----------------+
                 | MySQL targets  |   | PostgreSQL targets |   | Redis targets  |
                 +----------------+   +--------------------+   +----------------+
```

## 前端責任

前端主要負責：

- route guard 與導航可見性控制
- SQL Editor 的 tab 工作區狀態隔離
- Ticket / Masking / Settings 等頁面操作
- 暫時錯誤提示與結果展示

前端不是權限最終來源。所有關鍵動作仍由後端再次驗證 permission 與 DB Scope。

## 後端責任

後端主要負責：

- JWT 驗證、使用者狀態驗證、permission injection
- 各功能頁對應 API 與動作 permission gate
- Ticket workflow、審核、執行與通知
- SQL Editor 查詢、timeout 注入、脫敏分析與結果遮罩
- Metadata 掃描與快照持久化
- 設定、RBAC、DB Scope 與 Audit Log 管理

路由集中定義在 `backend/cmd/server/main.go`，這是理解整個產品 surface 的最佳入口。

## Meta DB 與外部 DB 的分工

平台自己的 MySQL Meta DB 儲存：

- 使用者、Auth Group、Permissions、DB Scope
- Tickets、Ticket scopes、Review results、Execution logs
- Masking rules、Whitelist、SQL review rules
- Query history、Saved queries、Export artifacts
- Platform settings
- AWS inventory / object metadata snapshots

外部資料庫則只用於：

- SQL Editor 查詢
- Ticket review 前的 SQL 檢測與 scope 分析
- DDL / DML 工單執行
- Metadata 讀取

也就是說，頁面展示大多數治理資料時，查的是平台自己的 Meta DB；只有實際查詢、執行、metadata 掃描時才連外部 DB。

## Connection Pool Profile

平台不只維護一種連線池，而是按用途拆成不同 profile：

- `query`：SQL Editor 查詢與其他 read path
- `exec`：Ticket execute
- `metadata`：background metadata scan
- `scoped_pg_query`：PostgreSQL 跨 database 臨時查詢

這樣做的目的是避免長查詢、工單執行與背景掃描互相搶同一組連線。

## SQL Editor 的限制與定位

SQL Editor 目前是「受控讀取介面」，不是通用 SQL console。核心限制包括：

- 只允許單一 statement
- 只允許唯讀查詢類型
- MySQL 與 PostgreSQL 會在 session 層注入 timeout
- 匯出、敏感權限申請與查詢結果，都是以目前 tab 的資料源上下文為基礎

這些限制是刻意設計，用來避免把查詢介面誤用成變更通道。

## Ticket 與 SQL Editor 的分工

平台把資料庫互動分成兩條路徑：

- `SQL Editor`：讀取資料、Explain、Export 申請、Sensitive Access 申請
- `Tickets`：DDL / DML 變更工單與執行流轉

這個切分的目的，是讓查詢與變更在權限、流程與審計上清楚分開。

## Metadata 掃描模型

Metadata 目前有兩條背景掃描：

- `Inventory scan`：從 AWS API 拉 RDS / ElastiCache 等雲端資產快照
- `Object scan`：實際連進選定 DB connections，抓資料庫物件快照

展示時讀的是快照，不是每次進頁面都即時直連外部資料庫。這可降低 UI 請求延遲，也讓掃描範圍與頻率可控。

## 遮罩與語法分析

系統在幾個路徑都盡量優先使用 parser，而不是單純 heuristic：

- SQL Editor statement 限制與查詢分析
- Ticket review 與 statement 拆分
- Sensitive column lineage 分析
- SQL review rule 的部分 AST 檢查

Masking rule 本身則不是 SQL parser 問題，而是 DSL / 規則引擎問題：它根據欄位匹配與查詢分析結果決定套用哪一種 mask mode。

## 取捨

這個架構的主要取捨是：

- 好處：權限清楚、審計完整、資料遮罩集中、行為較可控
- 代價：不是直接連 DB 的萬用工具；部分能力必須經過工單或審批，操作步驟較多

對 DBRE / DBA 場景來說，這是有意識的取捨。平台優先解的是治理與風險控制，不是提供最大自由度。

## 相關文件

- [權限模型](permission-model.md)
- [後端 API 與權限對照](../reference/backend-api-and-permissions.md)
- [SQL Editor](../reference/sql-editor.md)
- [Tickets](../reference/tickets.md)
- [DB Metadata](../reference/db-metadata.md)
