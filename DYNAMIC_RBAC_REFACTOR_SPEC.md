# DYNAMIC_RBAC_REFACTOR_SPEC.md

## 文件目的

這份文件定義 DBRE Maestro 下一階段的 RBAC v2 重構規格。

目標是把目前「固定 auth group enum + 硬編碼路由授權 + resource group 額外分層」的模式，升級成真正可管理的動態 RBAC 系統，並把能力模型與資料庫資源授權直接收斂到：

- `User`
- `Auth Group`
- `Permission`
- `DB Connection`

這份文件是正式產品 / 後端 / 前端對齊 spec，不是局部 API patch memo。

## 背景與問題定義

截至 2026-06-10，專案已有：

- `users`
- `auth_group_memberships`
- `resource_groups`
- `resource_group_connections`
- `resource_group_users`
- `resource_group_auth_groups`

但目前授權本質上仍是硬編碼模式：

- 路由授權依賴 `RequireGroup(...)`
- 群組名稱本身被當作能力判斷依據
- `auth group` 雖然存在，但不是真正的能力管理實體
- `resource group` 又額外把資料資產範圍拆成另一套物件，導致授權模型繞遠

### 現況主要問題

1. `Auth Group` 不是完整 CRUD 管理實體
2. `Auth Group` 無法安全支援動態建立、修改、刪除
3. UI 無法展示某個 auth group 或某個 user 實際有哪些能力
4. 單一 `User` 無法做個人化能力覆蓋
5. `Resource Group` 與 `DB Connection` 之間多了一層不必要抽象
6. 前端仍需要依賴群組名稱推導能力，無法信任後端回傳的真實權限
7. setup 狀態沒有獨立 API，登入頁無法明確判斷是否該顯示 Setup Wizard

## 這次重構的核心決策

### 1. `Auth Group` 變成真實可管理實體

系統要支援：

- 建立 auth group
- 修改 auth group
- 刪除 auth group
- 綁定 permissions
- 綁定 DB connections
- 綁定 users

### 2. `Permission` 成為授權唯一真相

後端授權從：

- `RequireGroup(admin)`
- `RequireGroup(dba)`

改為：

- `RequirePermission("users.write")`
- `RequirePermission("audit_logs.read")`
- `RequirePermission("sql_editor.query")`

### 3. `User` 與 `Auth Group` 都可以直接綁定能力

有效能力來源包含兩層：

- `User` 直接綁定的 permissions
- `User` 所屬 `Auth Groups` 綁定的 permissions

最終生效權限 = 兩者聯集。

### 4. `Resource Group` 退場

本 spec 明確決定：

- `resource group` 不再是 RBAC v2 的核心授權模型
- DB 資源範圍直接綁在：
  - `user`
  - `auth group`
- 資源來源直接使用 `db_connections`

也就是說，使用者最終能碰哪些 DB connection，來自：

- `User` 直接綁定的 DB connections
- `User` 所屬 `Auth Groups` 綁定的 DB connections

### 5. bootstrap admin 是受保護帳號，但可在特殊規則下改密碼

bootstrap admin：

- 不可刪除
- 不可停用
- 不可改 username
- 不可改 email
- 不可移除最後的 admin 能力來源
- 仍可改密碼

但改 bootstrap admin 密碼必須符合：

- 只有「有效擁有 `admin` auth group」的 user 可以執行

這條規則存在的原因是：

- 避免初始密碼外洩後永久無法處理
- 同時避免非 admin user 修改最高權限帳號密碼

### 6. setup 完成後，不再顯示 Setup Wizard

一旦 bootstrap setup 完成：

- 登入頁不應再顯示 Setup Wizard
- `POST /setup` 也應維持後端硬擋

這件事不能只靠前端猜測，必須提供 setup 狀態 API。

## 產品目標

完成本 spec 後，系統應支援：

1. `Auth Group` 可動態建立、修改、刪除
2. `User` 可綁定多個 `Auth Group`
3. `Auth Group` 可綁定多個 `Permissions`
4. `User` 也可直接綁定多個 `Permissions`
5. `Auth Group` 可直接綁定多個 `DB Connections`
6. `User` 也可直接綁定多個 `DB Connections`
7. 一般 `User` 可停用而不刪除
8. 停用 user 立即失去登入、refresh、受保護 API 使用能力
9. bootstrap admin 受保護，但可由 admin user 改密碼
10. setup 完成後，登入頁不再顯示 Setup Wizard
11. 所有頁面與操作按鈕由 permission 驅動，而不是依群組名稱硬判斷

## 核心設計原則

### 1. 後端回傳授權真相，前端只負責展示與消費

前端不可再靠：

- `group === "admin"`
- `group === "dba"`

自行推導真相。

前端應依賴後端回傳的：

- `auth_groups`
- `permissions`
- `db_connection_ids`
- `protected`
- `is_active`

### 2. 能力與資源範圍是兩個軸，但不再拆成 `resource group`

授權仍分成兩個維度：

- `Permission`：可以做什麼
- `DB Connection Scope`：可以在哪些資料庫資產上做

但資源範圍不再透過 `resource group` 間接表達，而是直接綁定在 `user` 或 `auth group`。

### 3. 停用優先於一切

若 `User.is_active = false`，則不論他還綁定哪些：

- `Auth Groups`
- `Permissions`
- `DB Connections`

都不得再登入或使用任何受保護 API。

### 4. Permission key 是系統 seed data，不開放任意新增

這次不做自訂 permission key UI。

也就是：

- `permissions` 是系統 seed data
- `auth groups` / `user direct permission` 是可管理綁定

## 目標資料模型

## 1. auth_groups

```sql
CREATE TABLE auth_groups (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  group_key    VARCHAR(64)     NOT NULL,
  name         VARCHAR(128)    NOT NULL,
  description  VARCHAR(255)    NOT NULL DEFAULT '',
  is_system    TINYINT(1)      NOT NULL DEFAULT 0,
  is_protected TINYINT(1)      NOT NULL DEFAULT 0,
  created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_auth_groups_key (group_key)
);
```

### 規則

- `group_key` 是內部穩定鍵，例如 `admin`
- `is_protected = true` 的群組不可刪除
- `is_protected = true` 的群組不可修改 `group_key`

## 2. permissions

```sql
CREATE TABLE permissions (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  permission_key  VARCHAR(128)    NOT NULL,
  name            VARCHAR(128)    NOT NULL,
  description     VARCHAR(255)    NOT NULL DEFAULT '',
  category        VARCHAR(64)     NOT NULL DEFAULT '',
  created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_permissions_key (permission_key)
);
```

## 3. auth_group_permissions

```sql
CREATE TABLE auth_group_permissions (
  auth_group_id BIGINT UNSIGNED NOT NULL,
  permission_id BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (auth_group_id, permission_id)
);
```

## 4. user_permissions

```sql
CREATE TABLE user_permissions (
  user_id        BIGINT UNSIGNED NOT NULL,
  permission_id  BIGINT UNSIGNED NOT NULL,
  granted_by     BIGINT UNSIGNED NULL,
  created_at     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, permission_id)
);
```

用途：

- 支援單一 user 的個人能力覆蓋

## 5. user_auth_groups

```sql
CREATE TABLE user_auth_groups (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id        BIGINT UNSIGNED NOT NULL,
  auth_group_id  BIGINT UNSIGNED NOT NULL,
  granted_by     BIGINT UNSIGNED NULL,
  expires_at     DATETIME NULL,
  created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_user_auth_group (user_id, auth_group_id)
);
```

### 遷移策略

現有 `auth_group_memberships.auth_group` 要對應到 `auth_groups.group_key` 後搬遷。

## 6. user_db_connections

```sql
CREATE TABLE user_db_connections (
  user_id           BIGINT UNSIGNED NOT NULL,
  db_connection_id  BIGINT UNSIGNED NOT NULL,
  granted_by        BIGINT UNSIGNED NULL,
  created_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, db_connection_id)
);
```

## 7. auth_group_db_connections

```sql
CREATE TABLE auth_group_db_connections (
  auth_group_id     BIGINT UNSIGNED NOT NULL,
  db_connection_id  BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (auth_group_id, db_connection_id)
);
```

## 8. users

沿用既有 `users.is_protected` 與 `users.is_active`。

bootstrap admin 必須：

- `is_protected = true`
- `is_active = true`

### 規則

- `is_active = false` 代表停用，不代表刪除
- 停用 user 仍保留：
  - 歷史工單
  - audit log
  - auth group 關聯
  - permission 關聯
  - db connection 關聯
- 但停用 user 不可：
  - 登入
  - refresh token
  - 呼叫任何受保護 API

## 權限鍵設計

本版先固定 seed 以下 `permission_key`：

### Users / RBAC

- `users.read`
- `users.write`

### Audit

- `audit_logs.read`
- `audit_logs.write`

### DB Connections

- `db_connections.read`
- `db_connections.write`

### Masking

- `masking_rules.read`
- `masking_rules.write`

### SQL Review

- `sql_review.read`
- `sql_review.write`

### Tickets

- `tickets.apply`
- `tickets.review`
- `tickets.execute`

### SQL Editor

- `sql_editor.query`
- `sql_editor.export`
- `sql_editor.sensitive_apply`
- `sql_editor.sensitive_review`
- `sql_editor.sensitive_execute`

### Global Override

- `global.sensitive`

## 頁面 / 功能對應規則

### Users 頁

- `users.read`：可看到 Users RBAC 頁面
- `users.write`：可看到並操作所有 Users RBAC 管理功能

`users.write` 包含：

- user CRUD
- auth group CRUD
- user/auth group permission binding
- user/auth group db connection binding

### Audit Logs 頁

- `audit_logs.read`：可查看 Audit logs
- `audit_logs.write`：可查看並導出 Audit logs

### DB Connections 頁

- `db_connections.read`：可查看頁面
- `db_connections.write`：可查看並修改

### Masking Rules 頁

- `masking_rules.read`
- `masking_rules.write`

### SQL Review 頁

- `sql_review.read`
- `sql_review.write`

### Tickets

- `tickets.apply`：可提交 DDL / DML 工單
- `tickets.review`：可審批 DDL / DML 工單
- `tickets.execute`：可執行 DDL / DML 工單

### SQL Editor

- `sql_editor.query`：可執行查詢
- `sql_editor.export`：可導出結果
- `sql_editor.sensitive_apply`：可申請臨時敏感資料查看
- `sql_editor.sensitive_review`：可審批敏感資料查看工單
- `sql_editor.sensitive_execute`：可執行敏感資料查看工單
- `global.sensitive`：永久可查看敏感資料，可繞過 masking rules

## 系統保留角色

系統啟動或 migration 後，必須 seed 以下 system groups：

1. `developer`
2. `reviewer`
3. `dba`
4. `admin`

### 建議預設能力

#### developer

- `tickets.apply`
- `sql_editor.query`
- `sql_editor.export`

#### reviewer

- `tickets.review`

#### dba

- `db_connections.read`
- `db_connections.write`
- `masking_rules.read`
- `masking_rules.write`
- `sql_review.read`
- `sql_review.write`
- `tickets.execute`
- `sql_editor.query`
- `sql_editor.export`
- `audit_logs.read`

#### admin

- 全部 permission

## 特殊可見性規則

以下不是單純 permission key，而是資料可見性規則：

1. 所有使用者都可查看自己提交過的工單
2. 所有審批人可查看自己審批過的工單

這些規則應在 ticket 查詢層處理，不建議硬塞成額外 permission key。

## 授權判斷規格

## 1. Route / action 層

所有後端授權都應從：

- `RequireGroup(...)`

改為：

- `RequirePermission(...)`

例如：

- `GET /users` 需要 `users.read`
- `PATCH /users/{id}` 需要 `users.write`
- `GET /audit-logs` 需要 `audit_logs.read`
- `POST /audit-logs/export` 需要 `audit_logs.write`
- `GET /db-connections` 需要 `db_connections.read`
- `POST /db-connections` 需要 `db_connections.write`

## 2. Resource scope 層

對需要資料範圍限制的功能，例如：

- SQL Editor
- metadata
- export
- sensitive data access

應同時判斷：

1. 是否具備對應 permission
2. 是否擁有目標 `db_connection` 的資源存取權

資源存取權來源為：

- direct user binding
- auth group binding

### 結論

使用者要查某個 DB，必須同時滿足：

- 有 `sql_editor.query`
- 有該 `db_connection` 的授權

## bootstrap admin / protected 規格

## 1. Protected User

bootstrap admin 不可：

- 刪除
- 停用
- 改 username
- 改 email
- 移除最後的 admin auth group
- 移除所有有效 admin 能力來源

bootstrap admin 可：

- 改密碼

但只能由「有效擁有 `admin` auth group 的 user」操作。

### 回應規則

對 protected 實體的非法操作，建議回：

- `409 protected resource cannot be modified`

## 2. Protected Auth Group

`admin` auth group 不可：

- 刪除
- 改 `group_key`
- 清空核心 permission

### 核心 permission

至少包含：

- `users.write`
- `db_connections.write`
- `audit_logs.read`
- `tickets.execute`

## Setup 流程規格

## 1. GET /setup/status

新增 setup 狀態 API：

```json
{
  "setup_completed": true
}
```

### 規則

- 若系統已有任一 user，`setup_completed = true`
- 若尚未建立任何 user，`setup_completed = false`

## 2. Login 頁行為

- `setup_completed = false`：可顯示 Setup Wizard
- `setup_completed = true`：不可顯示 Setup Wizard，只顯示登入表單

## 3. POST /setup

即使前端已隱藏 Setup Wizard，後端仍必須繼續硬擋：

- 若已有 user，回 `409 setup already completed`

## API 規格

## Auth

### GET /auth/me

應回傳：

```json
{
  "id": 1,
  "username": "admin",
  "protected": true,
  "is_active": true,
  "auth_groups": [
    {
      "id": 1,
      "group_key": "admin",
      "name": "Admin",
      "is_system": true,
      "is_protected": true
    }
  ],
  "permissions": [
    "users.read",
    "users.write",
    "db_connections.read",
    "db_connections.write"
  ],
  "db_connection_ids": [3, 8, 12]
}
```

### POST /auth/login

在帳密驗證正確後，仍需檢查：

- `user.is_active == true`

否則回：

- `403 user is disabled`

### POST /auth/refresh

refresh token 驗證通過後，仍需檢查：

- 對應 user 存在
- `user.is_active == true`

否則回：

- `401 user is disabled`

並撤銷該 user 的有效 sessions。

## Permissions

### GET /permissions

回傳所有系統 seed permissions，供前端做 RBAC 工作台編輯。

### 非目標

本階段不開放動態新增 / 刪除 permission key。

## Auth Groups

### GET /auth-groups

回傳 auth group 摘要：

```json
{
  "auth_groups": [
    {
      "id": 1,
      "group_key": "admin",
      "name": "Admin",
      "description": "Full platform administrator",
      "is_system": true,
      "is_protected": true,
      "user_count": 1,
      "permission_count": 19,
      "db_connection_count": 4
    }
  ]
}
```

### POST /auth-groups

```json
{
  "group_key": "data-analyst",
  "name": "Data Analyst",
  "description": "Analytics reader",
  "permission_keys": ["sql_editor.query"],
  "db_connection_ids": [3, 8]
}
```

### GET /auth-groups/{id}

```json
{
  "id": 8,
  "group_key": "data-analyst",
  "name": "Data Analyst",
  "description": "Analytics reader",
  "is_system": false,
  "is_protected": false,
  "permissions": ["sql_editor.query"],
  "db_connections": [
    { "id": 3, "name": "analytics-readonly" }
  ],
  "users": [
    { "id": 21, "username": "alice" }
  ]
}
```

### PATCH /auth-groups/{id}

允許修改：

- `name`
- `description`
- `permission_keys`
- `db_connection_ids`

若為 protected auth group：

- 不允許改 `group_key`
- 不允許移除核心 permission

### DELETE /auth-groups/{id}

規則：

- `is_protected = true` 不可刪
- 若仍有 users 綁定，先拒絕刪除 `409`

## Users

### GET /users

回傳：

- `protected`
- `is_active`
- `auth_groups`
- `direct_permissions`
- `effective_permissions`
- `direct_db_connections`
- `effective_db_connections`

### GET /users/{id}

回傳完整 detail，供 RBAC 工作台管理。

### PATCH /users/{id}

允許修改：

- `username`
- `email`
- `password`
- `is_active`

規則：

- protected bootstrap admin 不可停用
- protected bootstrap admin 不可改 username / email
- protected bootstrap admin 可改密碼，但操作者必須具備 `admin` auth group
- user 從 `active -> inactive` 時：
  - 撤銷所有有效 session

### DELETE /users/{id}

保留刪除能力，但產品層仍建議優先停用。

對 protected bootstrap admin：

- 不可刪除

### POST /users/{id}/auth-groups

```json
{
  "auth_group_id": 8
}
```

### DELETE /users/{id}/auth-groups/{authGroupID}

對 protected bootstrap admin：

- 不可移除最後的 admin auth group

### POST /users/{id}/permissions

```json
{
  "permission_key": "audit_logs.read"
}
```

### DELETE /users/{id}/permissions/{permissionKey}

移除 user 直接綁定的 permission。

### POST /users/{id}/db-connections

```json
{
  "db_connection_id": 3
}
```

### DELETE /users/{id}/db-connections/{connID}

移除 user 直接綁定的 DB connection。

## Auth Group Bindings

### POST /auth-groups/{id}/permissions

```json
{
  "permission_key": "sql_editor.query"
}
```

### DELETE /auth-groups/{id}/permissions/{permissionKey}

### POST /auth-groups/{id}/db-connections

```json
{
  "db_connection_id": 3
}
```

### DELETE /auth-groups/{id}/db-connections/{connID}

## Resource Groups 退場策略

這次重構後：

- `resource_groups`
- `resource_group_connections`
- `resource_group_users`
- `resource_group_auth_groups`

不再作為新 RBAC 模型的核心依賴。

### 實作建議

先採兩階段：

1. 新模型上線後，前端停止新增與維護 resource group
2. 完成資料搬遷後，再進一步移除舊頁面與舊 API

## 後端實作 Phase

## Phase 1：基礎模型與 setup 狀態

1. 新增 `GET /setup/status`
2. 確認 setup 完成後登入頁不再顯示 Setup Wizard
3. 建立 / 補強：
   - `auth_groups`
   - `permissions`
   - `auth_group_permissions`
   - `user_permissions`
   - `user_auth_groups`
   - `user_db_connections`
   - `auth_group_db_connections`
4. seed system groups 與 permissions

## Phase 2：讀取相容層

1. `GET /auth/me` 回傳：
   - `permissions`
   - `db_connection_ids`
   - `protected`
   - `is_active`
2. `GET /users` / `GET /users/{id}` 回傳 direct / effective 權限與 DB connections
3. `GET /auth-groups` / `GET /auth-groups/{id}` 正式改吃資料表

## Phase 3：授權中介層改造

1. 新增 `RequirePermission(...)`
2. 建立 effective permission / effective db scope 聚合邏輯
3. 將 `RequireGroup(...)` 逐步替換成 `RequirePermission(...)`
4. 保留 `is_active` 強制檢查

## Phase 4：管理 API 補齊

1. auth group CRUD
2. user direct permission CRUD
3. auth group permission CRUD
4. user db connection CRUD
5. auth group db connection CRUD
6. protected bootstrap admin 密碼更新特殊規則

## Phase 5：前端 RBAC 工作台升級

1. Login 頁改成依 `GET /setup/status` 控制 Setup Wizard
2. Users 頁升級成真正 RBAC 工作台：
   - user 視角
   - auth group 視角
   - permission / db resource 綁定視角
3. 導覽與頁面可見性改由 `permissions` 驅動

## Phase 6：Resource Group 退場

1. 前端移除 resource group 建立 / 編輯入口
2. 舊資料搬遷到新模型
3. 舊 API 與頁面標記 deprecated
4. 確認無流量依賴後再移除

## 驗收標準

以下全部成立，才算 RBAC v2 完成：

1. `auth group` 是真實資料表實體，可動態 CRUD
2. 路由授權不再依賴硬編碼 group 名稱，而是依 permission
3. `user` 可直接綁定 permission
4. `auth group` 可直接綁定 permission
5. `user` 可直接綁定 DB connection
6. `auth group` 可直接綁定 DB connection
7. 前端可顯示 direct / effective 權限與 direct / effective DB scope
8. 一般 user 可被停用，且停用後不可登入、不可 refresh、不可呼叫受保護 API
9. setup 完成後，登入頁不再顯示 Setup Wizard
10. bootstrap admin 不可刪除、不可停用、不可改 username / email
11. bootstrap admin 可改密碼，但只有 admin user 可以操作
12. `resource group` 不再是新授權模型的核心依賴

## 測試需求

## 後端

- migration 測試
- setup status 測試
- permission 聚合測試
- db connection scope 聚合測試
- protected auth group 測試
- protected bootstrap admin 測試
- user disable / enable 測試
- route 授權測試
- `permission + db scope` 雙條件測試

## 前端

- setup wizard 顯示 / 隱藏測試
- auth group CRUD 測試
- direct permission 綁定 UI 測試
- direct db connection 綁定 UI 測試
- protected user / protected group UI 禁制測試
- permission 驅動導覽與按鈕顯示測試

## 明確非目標

這次 spec 不做：

- ABAC
- row-level security policy engine
- LDAP / SSO group sync
- 多租戶權限隔離
- 自訂 permission key UI 建立流程

## 結論

這份 spec 的核心收斂是：

1. `Auth Group` 正式成為可管理實體
2. `Permission` 成為授權唯一真相
3. `User` 與 `Auth Group` 都可直接綁定能力
4. `User` 與 `Auth Group` 都可直接綁定 DB connection 資源
5. `Resource Group` 從核心授權模型退場
6. `bootstrap admin` 仍受保護，但保留 admin 可改密碼的營運出口
7. Setup Wizard 只在未初始化系統時出現

這樣才能完整滿足你目前的產品目標：

- auth group 可動態調整
- 不同 auth group 與不同 user 可擁有不同能力
- 資源授權直接對應 db connection
- 初始 admin 不可被破壞，但仍可安全輪替密碼
- 前端之後可以真正依後端授權真相去建頁面，而不是靠群組名稱猜行為
