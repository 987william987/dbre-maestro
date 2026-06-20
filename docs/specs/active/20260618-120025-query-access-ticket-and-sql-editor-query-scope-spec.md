---
status: active
---

# 查詢授權工單（Query Access Ticket）與 SQL Editor 細粒度查詢權限 Spec

## 文件目的

這份文件定義 DBRE Maestro 下一階段的「查詢授權工單 + SQL Editor 細粒度查詢權限」設計。

本 spec 要解的不是一般 RBAC 頁面權限，而是：

- 使用者可以看到哪些 DB instance
- 使用者可以對哪些 database / table 真正執行查詢
- 查詢權限如何申請、審批、生效、過期與回收
- 這套模型如何與既有 `DB Scope`、`Tickets`、`Sensitive Access` 共存

這份文件是正式產品 / 前端 / 後端對齊 spec，不是局部 patch memo。

---

## 背景與問題定義

截至 2026-06-18，系統已具備：

- permission-driven 頁面權限與工作流權限
- `DB Scope` 控制可作用的 DB connection
- SQL Editor
- DDL / DML / Redis Ticket
- SQL Export
- Sensitive Query Access

但目前模型仍有一個核心缺口：

- 只要使用者被綁到某個 DB connection，就能在 SQL Editor 與 Ticket 相關介面中看到該實例下所有 metadata
- 只要有 `sql_editor.query` 並且有 `DB Scope`，就能直接查該實例下所有庫表

這在共享實例場景會失控。單一 instance 下往往有很多個 database，而 database 下又有很多張業務表；實務上通常只應允許使用者查其中一小部分。

### 現況主要問題

1. `DB Scope` 粒度過粗，只能表達 instance 級別
2. SQL Editor 缺少真正的 database / table 細粒度查詢邊界
3. 查詢權限沒有正式工單流程，無法審批、過期、回收
4. `Sensitive Query Access` 解的是 unmask，不是查詢範圍控制
5. DDL / DML Ticket 與 SQL Editor 查詢權限若共用同一套表級授權，會把變更工單卡死

---

## 這次設計的核心決策

### 1. 採用「第三種方案」

本 spec 明確採用以下策略：

- `Ticket` 完全只吃 `DB Scope`
- `SQL Editor` 會看到擁有 `DB Scope` 的 instance metadata
- 但真正執行查詢時，後端必須額外校驗 `Query Access`
- `Export` 同樣吃 `Query Access`
- `Sensitive Access` 仍只處理 unmask，不處理 query authorization

### 2. metadata 可見，不作為主要安全邊界

本 spec 明確接受以下產品取捨：

- 只要使用者有 instance 級 `DB Scope`，就可以在 SQL Editor 與查詢授權工單介面瀏覽該 instance 的 metadata
- 系統不以隱藏 database / table metadata 作為主要安全邊界
- 真正的安全邊界是：
  - 查詢執行前的 `Query Access` 校驗
  - 查詢結果的 masking / sensitive access 控制

這是刻意的設計決策，不是遺漏。

### 3. `DB Scope` 與 `Query Access` 是兩個軸

系統的資料範圍控制拆成兩層：

- `DB Scope`
  - 表示使用者可接觸哪些 DB connection
  - 影響 SQL Editor 可選 instance、Ticket 可選 instance
- `Query Access`
  - 表示使用者可對哪些 database / table 真正執行查詢
  - 只影響 SQL Editor / Export

### 4. DDL / DML / Redis Ticket 不依賴 Query Access

本 spec 明確規定：

- 只要使用者有 `tickets.apply`
- 且對目標 instance 有 `DB Scope`
- 就可以提交 DDL / DML / Redis 工單

不要求：

- 先具備該 database / table 的 `Query Access`

原因：

- `CREATE TABLE` / `CREATE DATABASE` / rename / drop 等語句無法先以 Query Access 表示
- 變更工單的風險控制應由 review / parser / validation / reviewer / executor 處理，而不是由查詢授權模型處理

### 5. `Sensitive Query Access` 與 `Query Access` 必須分開

兩者語義不同：

- `Query Access`
  - 決定能不能查某個 database / table
- `Sensitive Query Access`
  - 決定在已可查的範圍內，能不能暫時看到未脫敏結果

也就是：

- 沒有 `Query Access`，不能查
- 有 `Query Access` 但沒有 `Sensitive Query Access`，可以查但敏感欄位仍遮罩
- 兩者都有，才是可查且可臨時看明文

---

## 非目標

這一版明確不做：

1. column-level query authorization
2. row-level security
3. deny 規則與 allow/deny 疊加優先級
4. 自訂 permission key
5. 完全隱藏 metadata
6. 讓 Query Access 控制 DDL / DML / Redis Ticket 提交資格
7. 把 `Sensitive Query Access` 與 `Query Access` 合併成同一種工單

---

## 產品目標

完成本 spec 後，系統應支援：

1. 使用者只要有 `sql_editor.query` + instance 級 `DB Scope`，即可進入 SQL Editor 並瀏覽該 instance metadata
2. 使用者若沒有 `Query Access`，則無法真正執行 SQL 查詢
3. 使用者若沒有 `Query Access`，則無法建立 SQL Export
4. 使用者可以提交 `Query Access Ticket` 申請 database / table 級查詢授權
5. reviewer 可以審批 `Query Access Ticket`
6. `Query Access Ticket` 通過後，對應查詢範圍立即生效
7. `Query Access Ticket` 可設定時長，到期自動失效
8. reviewer / admin / dba 可提前 revoke `Query Access`
9. DDL / DML / Redis Ticket 仍只依賴 instance 級 `DB Scope`
10. `Sensitive Query Access` 仍在 `Query Access` 之上附加 unmask 能力

---

## 用戶旅程

### 1. 新用戶開戶

由 DBA / Admin 預先完成：

- 建立帳號
- 指派 auth group，例如 `developer`
- 綁定 instance 級 `DB Scope`

新用戶不需要先申請 auth group。

### 2. 新用戶首次登入

若具備：

- `sql_editor.query`
- `tickets.apply`

則可看到：

- SQL Editor
- Tickets
- 其 `DB Scope` 允許的 instance

### 3. SQL Editor 使用

新用戶進入 SQL Editor 後：

- 可以看到 instance
- 可以展開 metadata
- 可以選擇 database / table

但若對某個查詢目標沒有 `Query Access`：

- `POST /api/query` 必須被拒絕
- 前端應收到明確錯誤，並可引導建立 `Query Access Ticket`

### 4. Query Access 申請

使用者可在 SQL Editor 內，針對目前 instance / database / table 建立 `Query Access Ticket`。

通過後：

- 對應 database / table 可查
- 若再需要明文敏感值，需另外申請 `Sensitive Query Access`

### 5. Ticket 使用

使用者若對目標 instance 有 `DB Scope`，則可：

- 建立 DDL / DML / Redis 工單
- 選擇 instance 與 database
- 貼 SQL

此流程不依賴 `Query Access`。

---

## 權限模型

### 1. 平台權限（RBAC）

保留現有工作流權限：

- `sql_editor.query`
- `sql_editor.export`
- `sql_editor.sensitive_apply`
- `tickets.apply`
- `tickets.review`
- `tickets.execute`

不新增：

- `query_access.apply`
- `query_access.review`

`query_access` 復用既有 Tickets workspace，因此沿用：

- `tickets.apply`
  - 可建立 Query Access Ticket
  - 可查看自己提交的 Query Access 工單
- `tickets.review`
  - 可審批 / 提前撤銷 Query Access

理由：

- `query_access` 在產品上就是新的 ticket type，而不是新的獨立頁面模組
- 系統已經有成熟的 ticket list / detail / notification / audit log / workflow
- 若再拆新的 page permission，會讓心智與實作都變複雜

### 2. DB Scope

`DB Scope` 繼續保留 instance 級控制，作用：

- SQL Editor connection 清單
- Ticket connection 清單
- Query Access Ticket 可申請的 instance 候選清單

### 3. Query Access

`Query Access` 是新的資料範圍授權層，作用：

- `POST /api/query`
- `POST /api/exports`
- 與查詢結果相關的後端執行路徑

不作用於：

- DDL / DML / Redis Ticket 提交資格
- `POST /api/tickets/review`
- `POST /api/tickets`

### 4. Sensitive Access

`Sensitive Access` 生效前提：

- 使用者已具備對應資料範圍的 `Query Access`

它只影響：

- 查詢結果是否顯示未脫敏欄位

---

## 權限語義總表

| 層級 | 控制內容 | 來源 |
|---|---|---|
| 平台頁面與動作 | 能不能進 SQL Editor / Tickets / 審批 / 執行 | RBAC permission |
| instance 候選範圍 | 能不能看到與選擇某個 DB connection | DB Scope |
| database/table 查詢權限 | 能不能真正查詢或導出某個 database/table | Query Access |
| 敏感明文可見性 | 已可查資料中，能不能暫時看未脫敏結果 | Sensitive Access |

---

## Query Access 資料模型

### 1. query_access_rules

```sql
CREATE TABLE query_access_rules (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  subject_type     VARCHAR(16)     NOT NULL,
  subject_id       BIGINT UNSIGNED NOT NULL,
  effect           VARCHAR(16)     NOT NULL DEFAULT 'allow',
  connection_id    BIGINT UNSIGNED NOT NULL,
  database_pattern VARCHAR(255)    NOT NULL DEFAULT '*',
  table_pattern    VARCHAR(255)    NOT NULL DEFAULT '*',
  granted_via      VARCHAR(32)     NOT NULL DEFAULT 'ticket',
  source_ticket_id BIGINT UNSIGNED NULL,
  expires_at       DATETIME(6)     NULL,
  revoked_at       DATETIME(6)     NULL,
  revoked_by       BIGINT UNSIGNED NULL,
  created_by       BIGINT UNSIGNED NULL,
  updated_by       BIGINT UNSIGNED NULL,
  created_at       DATETIME(6)     NOT NULL,
  updated_at       DATETIME(6)     NOT NULL,
  PRIMARY KEY (id),
  KEY idx_qar_subject (subject_type, subject_id),
  KEY idx_qar_connection (connection_id),
  KEY idx_qar_ticket (source_ticket_id),
  KEY idx_qar_expiry (expires_at),
  KEY idx_qar_active (revoked_at)
);
```

### 欄位語義

- `subject_type`
  - `user`
  - `auth_group`
- `effect`
  - `allow`：授權命中範圍可查
  - `deny`：排除命中範圍，且優先於 allow
- `connection_id`
  - 必填
- `database_pattern`
  - `*` 表示所有 database
  - 其他值表示指定 database
- `table_pattern`
  - `*` 表示所有 table
  - 其他值表示指定 table

### 粒度語義

- `connection_id + database_pattern='*' + table_pattern='*'`
  - 代表整個 instance 可查
- `connection_id + database_pattern='A' + table_pattern='*'`
  - 代表整個 database 可查
- `connection_id + database_pattern='A' + table_pattern='aaa'`
  - 代表指定 table 可查

### Rule-based 授權

一般使用者透過 Query Access Ticket 申請自己的 user rules。後台 fallback 可由 admin / DBA 對 user 或 auth group 建立 / 修改 / revoke rules。

反向授權以 `deny` 表達，例如測試環境開放全部但排除少數敏感庫：

```text
allow a1 * *
deny  a1 secret_db *
deny  a1 test_db user_token
```

---

## 新增 Ticket 類型

### query_access

新增 ticket type：

```text
query_access
```

用途：

- 申請 SQL Editor / Export 查詢資料的 database / table 權限

### workflow

`query_access` 沿用「審批即生效」模型：

```text
pending_review
  -> approved
  -> rejected
  -> withdrawn
  -> stopped (revoke 後)
```

說明：

- `approved`：grant 生效
- `stopped`：grant 被提前 revoke，或有效權限被回收

它不需要 execution 階段。

---

## Query Access Ticket 表單

### 入口

`query_access` 復用既有 Tickets workspace，但入口採雙入口設計：

- `Tickets > New Ticket`
  - 正式入口
  - 適合主動申請、預先申請
- `SQL Editor`
  - 情境化快捷入口
  - 適合在查詢被拒絕時，直接從當前 connection / database / table 發起

這個雙入口是刻意設計，不表示 `query_access` 是獨立頁面模組。

### 表單欄位

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `connection_id` | `number` | 是 | 目標 instance |
| `scope_mode` | `database` / `table` | 是 | 申請粒度 |
| `items` | array | 是 | 一筆或多筆申請項 |
| `approved_duration_minutes` | `number` | 是 | 時長 |
| `reason` | `string` | 是 | 申請原因 |

### items 結構

#### database 模式

```json
{
  "database_name": "nacos"
}
```

#### table 模式

```json
{
  "database_name": "nacos",
  "table_name": "users"
}
```

### 表單來源

使用者不需手打 instance / database / table 名稱。

第一版採：

- instance：下拉
- database/table：從既有 metadata explorer 選取

這是本 spec 刻意接受的 metadata 可見取捨。

---

## SQL Editor 行為調整

### 1. 可見性

只要有：

- `sql_editor.query`
- 該 instance 的 `DB Scope`

即：

- 可以看到該 connection
- 可以展開 metadata explorer

### 2. 真正查詢執行

`POST /api/query` 前後端規則：

- 前端仍可正常讓使用者送出
- 後端必須解析 SQL
- 抽出涉及的 database / table
- 與有效 `Query Access` 比對

若任一對象未被授權：

- 回傳 `403` 或 `422`（實作時需統一）
- message 應清楚指出缺少哪個查詢權限

建議錯誤訊息：

- `You do not have query access to nacos.users`

### 3. Explain

Explain 屬於查詢類操作，應與 `POST /api/query` 一樣做 Query Access 校驗。

### 4. Saved Query / History

Saved Query 與 History 不需因 Query Access 另建模型；但再次執行時，仍需重新校驗當下是否有權限。

---

## Export 行為調整

`sql_export` 本質上也是查詢資料導出，因此：

- 建立 export request 前
- 必須走與 SQL Editor 查詢一致的 Query Access 校驗

若未命中有效 Query Access：

- 不允許建立 export ticket

---

## Ticket 行為調整

### 1. DDL / DML / Redis Ticket

仍只要求：

- `tickets.apply`
- instance 級 `DB Scope`

不要求：

- 先有 Query Access

### 2. Ticket database selector

這份 spec 明確接受：

- Ticket 表單可以繼續展示 database 候選
- 也就是 metadata 仍可見

原因：

- 系統整體已接受 metadata 可見不是主要安全邊界
- 若強行收斂 Ticket database selector，反而會讓產品不一致且使用體驗更差

### 3. SQL Review / Validation

DDL / DML / Redis Ticket 的風險控制仍然由現有流程承擔：

- parser
- SQL review
- validation
- shadow validation
- reviewer / executor

不由 Query Access 承擔。

---

## Query Access 校驗規則

### 1. 基本規則

查詢涉及的所有資料來源都必須命中有效 rule：

- rule 未過期
- rule 未 revoke
- subject 為目前 user，或使用者有效 auth group

### 2. rule 匹配優先級

後端匹配規則：

1. admin user / all-permissions auth group 直接 bypass
2. 彙總 user direct rules 與 auth group rules
3. 任一 `deny` 命中目標 object，拒絕
4. 任一 `allow` 命中目標 object，允許
5. 沒有命中 allow，拒絕

`deny` 跨 subject 生效，因此 user direct deny 可以排除 auth group broad allow，auth group deny 也不能被 user direct allow 覆蓋。

### 3. 多表查詢

只要 SQL 涉及多張表，必須全部命中 Query Access。

例：

- `SELECT * FROM users u JOIN orders o ...`

必須同時具備：

- `users`
- `orders`

否則拒絕執行。

### 4. alias / join / CTE / subquery

本專案既有 SQL parser / semantic layer 已開始處理：

- alias
- join
- CTE
- derived table

這一層應直接沿用現有分析能力，不重新發明 heuristic。

---

## Query Access Ticket Approval 後的生效規則

### approved

審批通過後：

- 立即寫入 `query_access_grants`
- `expires_at = approved_at + duration`

### revoked / stopped

若 reviewer / admin / dba 主動回收：

- 寫 `revoked_at`
- `revoked_by`
- 對應 ticket 進入 `stopped`

### expired

grant 到期後：

- 不一定要立即改 ticket status
- 但查詢校驗時必須視為無效

第二階段可補：

- 定時 job 將已過期 grant 對應 ticket 標記為自然結束狀態

---

## API 設計

### 1. 建立 Query Access Ticket

```text
POST /api/query-access
```

或也可掛在 tickets namespace：

```text
POST /api/tickets/query-access
```

建議最終仍統一走 `/api/tickets`，以維持 ticket 模型一致性。

#### 建議請求

```json
{
  "connection_id": 12,
  "scope_mode": "table",
  "items": [
    { "database_name": "nacos", "table_name": "users" },
    { "database_name": "nacos", "table_name": "roles" }
  ],
  "approved_duration_minutes": 1440,
  "reason": "debug production permission issue"
}
```

### 2. 查詢權限校驗

不需要額外暴露獨立 API 給前端。  
由以下路徑在 server 內部統一校驗：

- `POST /api/query`
- `POST /api/exports`

### 3. revoke

```text
POST /api/tickets/{id}/revoke
```

`query_access` 與 `sensitive_query_access` 都可共用 `revoke` 動作，但後端行為需依 ticket type 分支。

---

## 後端實作要點

### 1. parser 優先

Query Access 校驗不能靠字串包含判斷，必須沿用現有 parser / semantic analysis。

### 2. Query Execution Context

校驗時需要帶入：

- connection
- database
- schema（若適用）

以正確解析未 fully-qualified 的 table 來源。

### 3. 中央化授權判斷

建議新增單一 service，例如：

```text
internal/queryaccess
```

用途：

- 讀有效 grant
- 比對 SQL 分析結果
- 回傳缺權限對象

避免：

- query handler
- export handler
- 其他 read path

各自複製授權邏輯。

### 4. 稽核

建立 / 審批 / 回收 Query Access 時，必須寫 audit log。

建議 action type：

- `query_access_submit`
- `query_access_approve`
- `query_access_reject`
- `query_access_revoke`

若不想新增過多 action type，也可暫沿用 ticket action，但 details 內需標明 `ticket_type=query_access`。

---

## 前端實作要點

### 1. SQL Editor

新增：

- 缺少 Query Access 時的明確錯誤提示
- 從當前 database/table 快速建立 Query Access Ticket 的入口

入口顯示條件：

- 使用者有 `tickets.apply`
- 且對當前 instance 有 `DB Scope`

### 2. Ticket New / Detail

新增：

- `query_access` ticket type
- 審批即生效型 detail 呈現
- revoke 行為

### 3. Ticket List

需正確顯示：

- `query_access`
- `approved`
- `stopped`
- 到期 / revoke 後的細節

---

## 安全與產品取捨

### 1. 本方案的安全邊界

真正安全邊界是：

- 查詢執行前的 Query Access 校驗
- 匯出前的 Query Access 校驗
- masking / sensitive access 對結果值的控制

### 2. 本方案明確接受的取捨

本方案不試圖隱藏 metadata。  
使用者若有 instance 級 `DB Scope`，可看到該 instance 下的 metadata，這是刻意接受的產品取捨。

### 3. 為什麼接受這個取捨

因為若要讓使用者方便地申請查詢權限：

- 最終仍需要提供 database / table 選擇能力

既然 metadata 終究可見，則不應再為了假隱藏而：

- 讓 SQL Editor 看不到
- 但申請頁又看得到

那只會讓產品心智混亂。

---

## 工單入口規則

本 spec 明確定義：不是所有 ticket type 都必須同時提供 `New Ticket` 與 `SQL Editor` 兩種入口。

入口是否存在，取決於該工單是否依賴「當前查詢上下文」。

### 1. 通用工單

以下工單可脫離 SQL Editor 單獨建立，因此放在 `Tickets > New Ticket`：

- `ddl`
- `dml`
- `redis`
- `query_access`

### 2. SQL Editor 情境工單

以下工單依賴當前查詢內容、當前 statement 或當前查詢上下文，因此入口放在 SQL Editor：

- `sql_export`
- `sensitive_query_access`

### 3. 雙入口工單

`query_access` 同時具備兩種需求，因此採雙入口：

- `Tickets > New Ticket` 提供正式建立入口
- `SQL Editor` 提供缺權限時的快捷申請入口

### 4. 統一管理層

無論入口來自哪裡，所有工單最後都仍回到同一套 ticket 系統：

- ticket list
- ticket detail
- review workflow
- notification
- audit log

也就是：

- 入口可以不同
- 但資料模型、狀態流、通知與審計是一套

---

## 遷移與分期落地

### Phase 1

1. 新增 `query_access` ticket type
2. 新增 `query_access_grants`
3. Query Access Ticket 審批與 revoke
4. `POST /api/query` 做 Query Access 校驗
5. `POST /api/exports` 做 Query Access 校驗
6. `Tickets > New Ticket` 新增 `query_access` 類型
7. `SQL Editor` 新增 Query Access 快捷申請入口

### Phase 2

1. SQL Editor 補更明確的 Query Access 引導
2. Ticket Detail / List 補更完整的 `query_access` 工作流展示
3. grant expiry 對應 ticket 狀態的背景同步

### Phase 3

1. 視需要支援 auth group 級 Query Access grant
2. 視需要支援 database-level 快速批量申請
3. 視需要支援更細的 review policy

---

## 成功標準

以下條件全部滿足，才算完成：

1. 新用戶拿到帳號後，DBA/Admin 只需配置 auth group + instance 級 DB Scope
2. 使用者可看到對應 instance metadata
3. 使用者若沒有 Query Access，SQL Editor 查詢會被正確拒絕
4. 使用者若沒有 Query Access，Export 會被正確拒絕
5. 使用者可提交 Query Access Ticket
6. reviewer 可審批並立即生效
7. grant 到期或 revoke 後，查詢立即失效
8. DDL / DML / Redis Ticket 不會因缺少 Query Access 而被卡住
9. Sensitive Access 仍必須建立在 Query Access 之上

---

## 與現有文件的關係

這份 spec 建立在以下現有設計之上：

- [權限模型](../../explanation/permission-model.md)
- [SQL Editor](../../reference/sql-editor.md)
- [Tickets](../../reference/tickets.md)
- [Dynamic RBAC Refactor Spec](./DYNAMIC_RBAC_REFACTOR_SPEC.md)

若後續 implementation 與這些 reference 文件不一致，應以實作完成後更新 reference 文件，而不是反向修改本 spec 的核心決策。
