---
status: archived
---

# BACKEND_RBAC_API_GAP_SPEC.md

## 文件目的

這份文件定義 DBRE Maestro 目前 RBAC 管理工作台尚缺的後端 API 與規則補齊項目。

目標不是重做整套權限模型，而是在現有資料結構與路由風格上，補足前端要把 `Users / Auth Groups / Resource Groups` 做成完整管理台所需要的最小後端能力。

這份 spec 以 2026-06-10 的 repo 現況為準。

## 現況摘要

目前後端已經有以下 RBAC 相關能力：

- `GET /users`
- `POST /users`
- `GET /users/{id}`
- `PATCH /users/{id}`
- `POST /users/{id}/memberships`
- `DELETE /users/{id}/memberships/{group}`
- `GET /resource-groups`
- `POST /resource-groups`
- `GET /resource-groups/{id}`
- `DELETE /resource-groups/{id}`
- `POST /resource-groups/{id}/connections`
- `DELETE /resource-groups/{id}/connections/{connID}`
- `POST /resource-groups/{id}/users`
- `DELETE /resource-groups/{id}/users/{userID}`
- `POST /resource-groups/{id}/auth-groups`
- `DELETE /resource-groups/{id}/auth-groups/{group}`

目前 `auth group` 是固定 enum，定義於 [backend/internal/model/user.go](../../../backend/internal/model/user.go)：

- `developer`
- `reviewer`
- `dba`
- `admin`

目前前端已能做出三視角 RBAC 工作台，但仍有以下後端落差：

- 無法刪除 user
- 無法修改 resource group 基本資料
- 無法獨立列出 auth group 視角的綁定摘要
- 無法管理 auth group 本身的描述或顯示 metadata
- bootstrap `admin` 只在前端被保護，後端尚未強制禁止修改

## 這份 Spec 的目標

完成本 spec 後，後端應支援以下能力：

1. Admin 可安全刪除一般 user。
2. DBA / Admin 可修改 resource group 名稱與描述。
3. 前端可用專門 API 取得 auth group 視角資料，而不是自己從多個列表拼裝。
4. 後端明確保護 bootstrap `admin`，避免被改名、刪除、降權、解除關聯。
5. 前端能用一致的資料格式完成三視角管理與後續擴充。

## 非目標

本階段不做以下內容：

- 不把 auth group 改成完全自訂 RBAC policy engine
- 不做 permission matrix / action-level policy DSL
- 不做 nested group / hierarchy group
- 不做批次匯入匯出 user / group
- 不做 SCIM / LDAP / SSO 同步

原因：目前產品真正要先補的是管理面 API 完整度，而不是重構權限模型。

## 設計原則

### 1. 先補齊現有模型，而不是重做模型

目前系統的核心 RBAC 實體已經很明確：

- User
- Auth Group
- Resource Group

Auth group 仍維持固定系統群組，避免在這一階段引入 migration 與權限推導的大改。

### 2. 前端不要自己推導保護規則

像 bootstrap `admin` 是否可編輯、可刪除、可變更 auth group，應由後端明確定義與拒絕，而不是只靠前端禁用按鈕。

### 3. 回傳格式要支援工作台視角

API 應該直接回傳前端需要的工作台資料，而不是只提供最原始資料列，讓前端再拼裝過多關聯。

## 名詞定義

### Bootstrap Admin

指首次 `/setup` 建立的初始管理者帳號。這個帳號在本 spec 中視為系統保護帳號。

建議辨識規則：

- 先新增後端明確欄位，例如 `users.is_system_admin` 或 `users.is_protected`
- 在該欄位尚未存在前，不應只靠 `id = 1 AND username = 'admin'`

本 spec 建議最終採用資料庫欄位標記，而不是魔術條件。

### Auth Group

系統固定角色集合，目前為：

- `developer`
- `reviewer`
- `dba`
- `admin`

本階段允許有「auth group 管理 API」，但只管理顯示資訊與綁定視角，不允許新增自訂 group 值。

## API 缺口總表

本階段建議新增或補強以下 API：

1. `DELETE /users/{id}`
2. `PATCH /resource-groups/{id}`
3. `GET /auth-groups`
4. `GET /auth-groups/{group}`
5. 保護規則套用到既有 API：
   - `PATCH /users/{id}`
   - `DELETE /users/{id}`
   - `POST /users/{id}/memberships`
   - `DELETE /users/{id}/memberships/{group}`
   - `DELETE /resource-groups/{id}/users/{userID}`
   - `DELETE /resource-groups/{id}/auth-groups/{group}`

## 詳細 API 規格

## 1. DELETE /users/{id}

### 目的

刪除一般使用者帳號，支援 Users 頁面的完整 CRUD。

### 權限

- 僅 `admin`

### 行為規則

- 若 user 不存在，回 `404`
- 若目標 user 為 bootstrap admin，回 `409`
- 刪除 user 時，應一併清理其關聯資料：
  - auth memberships
  - resource group direct memberships
  - sessions
- 若系統中有其他業務資料引用 user，例如 ticket / audit log，這些資料不應被硬刪除
- 建議採用：
  - user 主體刪除
  - 關聯 membership / session 清除
  - audit / ticket 保留歷史 reference

### Request

無 body。

### Response

- `204 No Content`

### 錯誤

- `404 user not found`
- `409 protected system user cannot be deleted`

## 2. PATCH /resource-groups/{id}

### 目的

允許修改 resource group 的名稱與描述，補齊 Resource Group 基本 CRUD。

### 權限

- `dba` / `admin`

### Request

```json
{
  "name": "analytics-readers",
  "description": "readonly analytics scope"
}
```

欄位皆可選填，但至少要有一個欄位存在。

### 行為規則

- 若 resource group 不存在，回 `404`
- `name` 不可為空字串
- 若名稱需唯一且撞名，回 `409`
- 更新後回傳最新資料

### Response

```json
{
  "id": 11,
  "name": "analytics-readers",
  "description": "readonly analytics scope",
  "created_by": 1,
  "created_at": "2026-06-10T00:00:00Z"
}
```

### 錯誤

- `404 resource group not found`
- `409 resource group name already exists`
- `422 at least one mutable field is required`

## 3. GET /auth-groups

### 目的

提供 Auth Group 視角總覽，讓前端不用自己從 users 與 resource groups 兩端做聚合。

### 權限

- `admin`

### Response

```json
{
  "auth_groups": [
    {
      "name": "developer",
      "label": "Developer",
      "description": "Can create tickets and use SQL editor within granted resources.",
      "system_defined": true,
      "user_count": 12,
      "resource_group_count": 3
    }
  ]
}
```

### 行為規則

- 即使目前沒有任何 user 綁定，也要回傳所有固定 auth groups
- 排序固定為：
  - `developer`
  - `reviewer`
  - `dba`
  - `admin`

### 備註

`label` 與 `description` 可以先寫死在後端常數，不一定要先建資料表。

## 4. GET /auth-groups/{group}

### 目的

提供 Auth Group 單一視角明細，支援前端點入某個 group 後看到綁定 users 與 resource groups。

### 權限

- `admin`

### Response

```json
{
  "name": "dba",
  "label": "DBA",
  "description": "Can manage database connections, query execution, and governance settings.",
  "system_defined": true,
  "users": [
    {
      "id": 7,
      "username": "alice",
      "email": "alice@example.com",
      "created_at": "2026-06-10T00:00:00Z",
      "updated_at": "2026-06-10T00:00:00Z"
    }
  ],
  "resource_groups": [
    {
      "id": 11,
      "name": "analytics-readers",
      "description": "readonly analytics scope",
      "created_by": 1,
      "created_at": "2026-06-10T00:00:00Z"
    }
  ]
}
```

### 行為規則

- 若 `group` 不是合法固定值，回 `404`
- `users` 為直接持有該 auth group 的使用者
- `resource_groups` 為綁定該 auth group 的 resource groups

## 5. Bootstrap Admin 保護規則

這不是單一路由，而是要套用到既有多條 API 的共通規則。

### 5.1 保護目標

bootstrap admin 不可被：

- 刪除
- 改名
- 改 email
- 改密碼
- 移除 `admin` auth group
- 額外解除任何被視為必要的保護性關聯

### 5.2 必須攔截的 API

- `PATCH /users/{id}`
- `DELETE /users/{id}`
- `DELETE /users/{id}/memberships/{group}`

### 5.3 建議延伸保護

如果 bootstrap admin 被 direct 綁到某些 resource groups，是否允許解除，需明定。

本 spec 建議：

- bootstrap admin 不需要依賴 resource group 才能擁有全權限
- 因此 resource group 關聯可允許存在或不存在
- 但如果產品要讓「Users 頁面永遠顯示 admin 擁有全部資源」，應由權限推導實作，不應靠人工綁定 resource groups

### 5.4 錯誤碼

建議使用：

- `409 protected system user cannot be modified`

不要回 `403`，因為操作者本身有權限，失敗原因是目標資源受保護。

## 6. 對既有 API 的回傳補強建議

以下不是絕對缺口，但若補上會讓前端更穩定。

### 6.1 GET /users/{id}

目前已有：

- `id`
- `username`
- `email`
- `created_at`
- `memberships`

建議補上：

- `updated_at`
- `protected`
- `resource_groups`

建議 response：

```json
{
  "id": 1,
  "username": "admin",
  "email": "admin@example.com",
  "created_at": "2026-06-10T00:00:00Z",
  "updated_at": "2026-06-10T00:00:00Z",
  "protected": true,
  "memberships": [
    {
      "id": 9,
      "user_id": 1,
      "auth_group": "admin",
      "granted_by": null,
      "expires_at": null,
      "created_at": "2026-06-10T00:00:00Z"
    }
  ],
  "resource_groups": [
    {
      "id": 11,
      "name": "analytics-readers"
    }
  ]
}
```

理由：

- 前端不必再另查所有 resource groups 才能知道 user 綁定
- `protected` 可直接驅動 UI

### 6.2 GET /users

目前已有：

- `id`
- `username`
- `email`
- `auth_groups`
- `created_at`
- `updated_at`

建議補上：

- `protected`

這可以避免前端用 `id === 1 && username === 'admin'` 這種不可靠推導。

### 6.3 GET /resource-groups/{id}

目前已有：

- `id`
- `name`
- `description`
- `created_by`
- `created_at`
- `connections`
- `user_members`
- `auth_groups`

建議補上：

- `updated_at`
- `protected`（若未來有系統保留群組）

## 資料模型建議

## 1. users 新增保護欄位

建議新增：

```sql
ALTER TABLE users ADD COLUMN is_protected BOOLEAN NOT NULL DEFAULT FALSE;
```

`/setup` 建立第一個 admin 時，應將該欄位設為 `TRUE`。

### 原因

- 避免 magic condition
- 測試更明確
- 未來若有其他系統保留帳號可沿用

## 2. resource_groups 可考慮補 updated_at

若要支援編輯與審計，建議新增：

```sql
ALTER TABLE resource_groups
  ADD COLUMN updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;
```

這不是強制，但若要做完整後台管理，應該有更新時間。

## 權限矩陣

### User 管理

- `GET /users`: `admin`
- `POST /users`: `admin`
- `GET /users/{id}`: `admin`
- `PATCH /users/{id}`: `admin`
- `DELETE /users/{id}`: `admin`
- `POST /users/{id}/memberships`: `admin`
- `DELETE /users/{id}/memberships/{group}`: `admin`

### Auth Group 視角

- `GET /auth-groups`: `admin`
- `GET /auth-groups/{group}`: `admin`

### Resource Group 管理

- `GET /resource-groups`: `dba` / `admin`
- `POST /resource-groups`: `dba` / `admin`
- `GET /resource-groups/{id}`: `dba` / `admin`
- `PATCH /resource-groups/{id}`: `dba` / `admin`
- `DELETE /resource-groups/{id}`: `admin`
- `POST /resource-groups/{id}/connections`: `dba` / `admin`
- `DELETE /resource-groups/{id}/connections/{connID}`: `dba` / `admin`
- `POST /resource-groups/{id}/users`: `dba` / `admin`
- `DELETE /resource-groups/{id}/users/{userID}`: `dba` / `admin`
- `POST /resource-groups/{id}/auth-groups`: `dba` / `admin`
- `DELETE /resource-groups/{id}/auth-groups/{group}`: `dba` / `admin`

## 驗收標準

以下全部成立，才算本 spec 完成：

1. 前端不再需要用魔術條件判斷 protected admin。
2. 一般 user 可被刪除，且刪除後不殘留 membership / session。
3. bootstrap admin 無法被刪除、改名、改密碼、移除 admin membership。
4. resource group 可修改名稱與描述。
5. 前端可透過 `GET /auth-groups` 與 `GET /auth-groups/{group}` 直接渲染 auth group 視角。
6. 所有新增/補強 API 都有 audit log。
7. 新增行為有 handler / repository / migration / test 對應覆蓋。

## 測試需求

## 1. Handler 測試

至少補以下情境：

- `DELETE /users/{id}` 成功
- `DELETE /users/{id}` 刪除 protected user 失敗
- `PATCH /resource-groups/{id}` 成功
- `GET /auth-groups` 回傳固定四組且順序正確
- `GET /auth-groups/{group}` 回傳 user 與 resource group 綁定
- `DELETE /users/{id}/memberships/{group}` 對 protected admin 回 `409`

## 2. Repository 測試

至少補以下情境：

- 刪除 user 時關聯 membership / session 清理是否正確
- resource group 更新是否只改 mutable fields
- auth group summary 查詢是否正確計數

## 3. Migration 驗證

若新增 `users.is_protected` 或 `resource_groups.updated_at`，需驗證：

- 新 migration 可在空資料庫執行
- 舊資料升級可執行
- `/setup` 建立初始 admin 時欄位值正確

## 建議開發順序

### Phase 1

- 新增 `users.is_protected`
- `/setup` 寫入 protected admin
- 在既有 user 修改 / membership API 補保護規則

### Phase 2

- 新增 `DELETE /users/{id}`
- 補 user 刪除清理邏輯與測試

### Phase 3

- 新增 `PATCH /resource-groups/{id}`
- 若需要，補 `resource_groups.updated_at`

### Phase 4

- 新增 `GET /auth-groups`
- 新增 `GET /auth-groups/{group}`

## 與前端對接結論

前端 Users 頁面若要達到完整產品化管理體驗，後端最少需要：

1. `DELETE /users/{id}`
2. `PATCH /resource-groups/{id}`
3. `GET /auth-groups`
4. `GET /auth-groups/{group}`
5. `protected` 欄位與 bootstrap admin 後端保護規則

若只完成其中一部分，前端仍能運作，但會持續存在以下問題：

- 需要前端自行推導保護帳號
- Auth Group 視角資料需前端自行聚合
- Resource Group CRUD 仍不完整
- User CRUD 仍不完整
