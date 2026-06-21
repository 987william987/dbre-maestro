# 權限模型

DBRE Maestro 的權限設計，不是單純把每個按鈕綁一個 permission，而是把「導航頁面可見性」與「跨頁工作流動作」分層處理。

## 問題

如果只用粗粒度角色，例如 `admin` / `developer` / `dba`，很快就會碰到幾個問題：

- 能不能看到頁面，和能不能操作頁面，常常不是同一件事
- SQL Editor / Tickets 這類跨資料源功能，還需要額外限制可作用的 DB 範圍
- 同一個 Tickets 頁面裡，DDL / DML / Redis / Query Access / Export / Sensitive Access 的 reviewer 與 executor 角色並不相同

如果把這些規則全部寫死在前端，會很快失控，而且很難稽核。

## 核心做法

平台採三個軸線控制：

```text
Permission 決定：
  1. 看不看得到功能頁，也就是 page entry permission
  2. 能不能觸發某類動作，也就是 operation permission

DB Scope 決定：
  3. 這個動作可以作用在哪些 DB connection

Approval Policy 決定：
  4. 某類工單送出後，路由給哪些有效審批人
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

## 原則二：工作台入口與工作流動作分開

`SQL Editor` 與 `Tickets` 不是 CRUD 頁面，但仍需要清楚分開「能進頁面」與「能做動作」。

頁面入口：

- `tickets.read`：可進入 Tickets workspace，並查看自己被允許看到的工單
- `sql_editor.read`：可進入 SQL Editor workspace

工作流與操作能力：

- `sql_editor.query`
- `sql_editor.export`
- `sql_editor.export_review`
- `sql_editor.sensitive_apply`
- `sql_editor.sensitive_review`
- `tickets.apply`
- `tickets.review`
- `tickets.execute`

其中：

- 有 `sql_editor.read` 才能進入 SQL Editor；有 `sql_editor.query` 才能實際查詢
- 有 `tickets.read` 才能進入 Tickets workspace；有 `tickets.apply` 才能建立一般工單
- 但可作用的連線清單，仍由使用者的 DB Scope 決定

也就是「能做這類事」與「能對哪個 DB 做這件事」是兩個獨立軸線。

## Tickets 的入口與建單權限

Tickets workspace 入口使用 `tickets.read`，建單使用 `tickets.apply`。

`tickets.apply` 目前只代表能建立以下一般工單：

- `ddl`
- `dml`
- `redis`
- `query_access`

同樣地，`query_access` 雖然是新的工單類型，但不是新的獨立頁面模組：

- 不新增 `query_access.apply`
- 不新增 `query_access.review`
- 建單沿用 `tickets.apply`
- 審批 / 提前回收沿用 `tickets.review`

這樣可以直接復用既有的 ticket list、ticket detail、notification、audit log 與 workflow。

`sql_export` 與 `sensitive_query_access` 是 SQL Editor 情境工單，不應從 `POST /api/tickets` 直接建立：

- `sql_export` 由 SQL Editor 的 export 流程建立，操作權限是 `sql_editor.export`
- `sensitive_query_access` 由 SQL Editor 的敏感查詢申請流程建立，操作權限是 `sql_editor.sensitive_apply`

## Approval Policy 是路由，不是授權

Approval Policy Center 決定每一種 workflow 的候選審批人，例如 user 清單或 auth group 清單。它不取代 permission。

有效審批人必須同時滿足：

- 被 Approval Policy 指定為候選人，或屬於被指定的 auth group
- 使用者仍為 active
- 具備該 workflow 需要的 review permission

目前 review permission 對應如下：

| Workflow | 必要審批權限 |
|---|---|
| DDL / DML / Redis / Query Access | `tickets.review` |
| Normal SQL Export / Sensitive SQL Export | `sql_editor.export_review` |
| Sensitive Query Access | `sql_editor.sensitive_review` |

因此 DBA 即使有 `tickets.review`，如果沒有被 Approval Policy 指定為某 workflow 的 reviewer，也不會成為該 workflow 的有效審批人。反過來說，只被 policy 指定但沒有對應 review permission，也不會成為有效審批人。

Settings 儲存 Approval Policy 時會檢查所有啟用 workflow；若某個 workflow 沒有有效審批人，會拒絕儲存並提示管理者修正。建立 ticket 時不會因 policy 暫時無有效審批人而被擋，避免把設定治理錯誤轉嫁到申請人身上。

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

## Admin 與 All Permissions 規則

平台有兩種「全權限」來源：

- `users.is_protected = true` 的 admin user
- `auth_groups.is_all_permissions = true` 的 admin auth group

這兩種身分在產品語意上都代表「永有所有權限」。因此新增任何功能時，不應在 handler 或 service 裡自行手寫 `username == admin`、`group_key == admin` 或類似判斷，而必須走統一 helper：

- 頁面 / API permission：使用 `GetEffectivePermissionKeys()`
- DB Scope：使用 `GetEffectiveDBConnectionIDs()`
- 額外 grant 類能力，例如 query access：在檢查 grant 前先使用 `HasAllPermissions()`

這條規則的目的，是避免新功能新增了額外授權表後，admin user 或 admin auth group 反而被細粒度 grant 擋住。

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
- Approval Policy 可以彈性路由不同 workflow 的審批人
- RBAC 不需要為每個資料庫硬切角色

代價是：

- permission 名稱數量會增加
- 新增功能時，需要同步更新前端導航、route guard、後端 middleware、Approval Policy 與說明文件

## 相關文件

- [後端 API 與權限對照](../reference/backend-api-and-permissions.md)
- [Users / RBAC](../reference/users-and-rbac.md)
- [Tickets](../reference/tickets.md)
- [SQL Editor](../reference/sql-editor.md)
