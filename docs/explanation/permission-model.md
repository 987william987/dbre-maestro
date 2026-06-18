# 權限模型

DBRE Maestro 的權限設計，不是單純把每個按鈕綁一個 permission，而是把「導航頁面可見性」與「跨頁工作流動作」分層處理。

## 問題

如果只用粗粒度角色，例如 `admin` / `developer` / `dba`，很快就會碰到幾個問題：

- 能不能看到頁面，和能不能操作頁面，常常不是同一件事
- SQL Editor / Tickets 這類跨資料源功能，還需要額外限制可作用的 DB 範圍
- 同一個 Tickets 頁面裡，DDL / DML / Redis / Query Access / Export / Sensitive Access 的 reviewer 與 executor 角色並不相同

如果把這些規則全部寫死在前端，會很快失控，而且很難稽核。

## 核心做法

平台採兩層控制：

```text
Permission 決定：
  1. 看不看得到功能頁
  2. 能不能觸發某類動作

DB Scope 決定：
  3. 這個動作可以作用在哪些 DB connection
```

## 原則一：導航頁對應頁面可查看 / 可編輯

大多數治理頁面遵循 `*.read` / `*.write` 模式：

- `db_connections.read`：可進入 DB Connections 頁，看列表與詳情
- `db_connections.write`：可新增、修改、刪除、測試
- `masking_rules.read` / `masking_rules.write`
- `sql_review.read` / `sql_review.write`
- `settings.read` / `settings.write`
- `users.read` / `users.write`
- `audit_logs.read` / `audit_logs.write`

這讓頁面可見性與寫入能力對齊，容易理解也容易維護。

同一個導航群組底下的子頁，也沿用同一組權限。例如：

- `Users`
- `Auth Groups`
- `Resources`

這三個子頁都屬於同一個 RBAC workspace，因此共用 `users.read` / `users.write`。

## 原則二：工作台能力用功能 permission，資料範圍用 DB Scope

`SQL Editor` 與 `Tickets` 不適合單純用 `read/write` 表達，因為它們不是 CRUD 頁面，而是工作流入口。

因此平台改用動作型 permission：

- `sql_editor.query`
- `sql_editor.export`
- `sql_editor.export_review`
- `sql_editor.sensitive_apply`
- `sql_editor.sensitive_review`
- `tickets.apply`
- `tickets.review`
- `tickets.execute`

其中：

- 只要使用者有 `sql_editor.query`，就可以進入 SQL Editor
- 只要使用者有 `tickets.apply`，就可以看到 Tickets 工作台並建立 DDL / DML / Redis / Query Access 工單
- 但可作用的連線清單，仍由使用者的 DB Scope 決定

也就是「能做這類事」與「能對哪個 DB 做這件事」是兩個獨立軸線。

## 為什麼 `tickets.apply` 包含 read

目前 `tickets.apply` 被視為工單工作台的最小可用權限，理由很直接：

- 不存在只看工單但完全不能送工單的常見場景
- 建單人需要回頭看自己提交的工單、審核結果與執行狀態

所以 Tickets 頁的 route guard 不是 `tickets.read`，而是整個 ticket workspace 權限集合之一。

同樣地，`query_access` 雖然是新的工單類型，但不是新的獨立頁面模組：

- 不新增 `query_access.apply`
- 不新增 `query_access.review`
- 建單沿用 `tickets.apply`
- 審批 / 提前回收沿用 `tickets.review`

這樣可以直接復用既有的 ticket list、ticket detail、notification、audit log 與 workflow。

## Resources 子頁的角色

`/users/resources` 不是新的權限模組，而是 RBAC 的「資源反查視角」。

它回答的是：

- 某個 DB Connection 目前直接綁了哪些 user
- 哪些 auth group 綁到這個資源
- 綜合計算後，哪些 user 最終有效可用

這個頁面讓管理者可以從 resource 反向檢查 DB Scope，而不是只能從 user / auth group 正向查看。

## 前後端雙重驗證

前端會先用 route guard 控制頁面可見性，減少無效操作；但真正的安全邊界在後端。

後端每條 API 都會依序做：

1. 驗證 JWT
2. 驗證 user 是否 active
3. 注入 permission
4. 驗證對應 permission
5. 在需要時驗證 DB Scope、Ticket access 或 ticket type-specific workflow

這樣就算前端被繞過，也不能直接呼叫不該用的 API。

## 特殊權限

有幾個 permission 不對應單一導航頁：

- `global.sensitive`：永久繞過 masking
- `sql_editor.export_review`：審核匯出工單
- `sql_editor.sensitive_review`：審核或撤銷 sensitive query access

這些權限是 workflow 權限，而不是頁面權限。

## 工單入口模型

Tickets 系統允許不同 ticket type 擁有不同入口，但最後都收斂到同一套工單管理模型。

### 通用工單

以下工單可脫離 SQL Editor 單獨建立，因此入口放在 `Tickets > New Ticket`：

- `ddl`
- `dml`
- `redis`
- `query_access`

### SQL Editor 情境工單

以下工單依賴當前查詢上下文，因此入口放在 SQL Editor：

- `sql_export`
- `sensitive_query_access`

### 雙入口工單

`query_access` 同時支援兩種入口：

- `Tickets > New Ticket` 作為正式建立入口
- `SQL Editor` 作為缺權限時的快捷申請入口

入口不同，不代表資料模型不同；它們最後仍回到同一套 ticket list / detail / review / notification / audit log。

## Trade-offs

這套模型的優點是：

- 頁面權限與工作流權限分工清楚
- SQL Editor / Tickets 能共用 DB Scope 機制
- RBAC 不需要為每個資料庫硬切角色

代價是：

- permission 名稱數量會增加
- 新增功能時，需要同步更新前端導航、route guard、後端 middleware 與說明文件

## 相關文件

- [後端 API 與權限對照](../reference/backend-api-and-permissions.md)
- [Users / RBAC](../reference/users-and-rbac.md)
- [Tickets](../reference/tickets.md)
- [SQL Editor](../reference/sql-editor.md)
