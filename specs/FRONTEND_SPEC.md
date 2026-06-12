# FRONTEND_SPEC.md

## 文件目的

這份文件定義 DBRE Maestro 下一階段前端實作範圍，目標是把目前只有 `Setup Wizard` 的前端，補成可登入、可看工單、可做基本治理操作的內部工具。

這不是產品願景文件，也不是設計稿彙整。它是給後續實作者直接照著落地的實作規格。

## 現況摘要

截至 2026-06-09，repo 現況如下：

- 前端技術：`React 18`、`TypeScript`、`Vite`、`React Router`、`Tailwind CSS`
- 已完成頁面：`/setup`
- 已存在後端 API：
  - `POST /setup`
  - `POST /auth/login`
  - `POST /auth/refresh`
  - `POST /auth/logout`
  - `GET /auth/me`
  - `GET /tickets`
  - `POST /tickets`
  - `GET /tickets/{id}`
  - `POST /tickets/{id}/approve`
  - `POST /tickets/{id}/reject`
  - `POST /tickets/{id}/request-execution`
  - `POST /tickets/{id}/execute`
  - `GET /db-connections`
  - `POST /db-connections`
  - `POST /db-connections/{id}/test`
  - `DELETE /db-connections/{id}`
  - `GET /audit-logs`
  - `POST /exports`
  - `GET /exports/download/{token}`
- 角色模型：
  - `developer`
  - `reviewer`
  - `dba`
  - `admin`

## 這份 Spec 的目標

前端完成後，系統至少要能支援以下流程：

1. 首次設定完成後，使用管理員帳號登入平台。
2. 使用者可查看自己的工單列表與工單詳情。
3. 使用者可建立新工單。
4. Reviewer 可審核工單。
5. DBA/Admin 可管理資料庫連線、請求執行與觸發執行。
6. DBA/Admin 可查看稽核日誌。
7. 使用者可建立匯出請求並取得下載連結。

## 非目標

這一階段不做以下內容：

- 不做行銷式 dashboard
- 不做即時 websocket / SSE 更新
- 不做複雜全域狀態管理框架
- 不做完整 SQL Editor
- 不做工單逐句執行結果視覺化
- 不做通知中心
- 不做多語系

原因：後端目前真正穩定的能力核心是認證、工單、DB 連線、稽核與匯出；前端應先把治理流程走通。

## 成功標準

前端完成本 spec 後，滿足以下條件才算完成：

1. 未登入時無法進入受保護頁面。
2. 登入後可正確初始化使用者身份與 `auth_groups`。
3. Access token 過期時可自動呼叫 `/auth/refresh` 後重試原請求。
4. Refresh 失敗時可清除前端登入態並導回 `/login`。
5. 不同角色看到的頁面入口與操作按鈕不同。
6. 所有已存在 API 都有對應可操作的前端入口。
7. 主要錯誤情境有清楚 UI 回饋，而不是靜默失敗。

## 前端架構決策

### 1. Server state 管理

本階段不引入 React Query。

理由：

- 目前頁面數量不多。
- 專案還在流程定型期，先降低依賴與抽象成本。
- 現有 API 量級用一層 `api client + feature hooks` 足以處理。

如果後續新增頁面快速成長，或列表/快取/重整需求明顯增加，再評估導入 React Query。

### 2. Client state 管理

本階段採用：

- `React Context` 管理 auth state
- feature local state 管理表單與頁面互動

不引入 Redux、Zustand、Jotai。

### 3. API 存取方式

統一使用自建 `api client`，不在頁面直接裸寫 `fetch`。

`api client` 必須負責：

- 帶入 `Authorization: Bearer <token>`
- 統一 `Content-Type`
- 401 時自動 refresh
- refresh 成功後重試一次原請求
- refresh 失敗時清空 auth state 並導回 `/login`
- 回傳統一的錯誤格式

### 4. 權限控制方式

權限控制分兩層：

- Route level：沒登入不能進
- UI level：依 `auth_groups` 控制頁面入口與操作按鈕

注意：前端權限控制只為改善 UX，不取代後端授權。

### 5. 視覺與版面

遵循 `DESIGN.md`，使用既有色彩 token、字體方向與高密度內部工具風格。

本階段 UI 要求：

- 使用同一套 App Shell
- 不做額外 hero 區塊
- 版面以「左側導覽 + 右側主工作區」為主
- 桌面優先，平板與手機可降級但不破版

## 目錄結構

前端調整後，建議採用以下結構：

```text
frontend/src/
  app/
    router/
    layout/
    providers/
  shared/
    api/
    auth/
    ui/
    lib/
    types/
  modules/
    auth/
    tickets/
    db-connections/
    audit/
    exports/
    setup/
```

約束如下：

- 共用基礎能力放 `shared`
- 業務頁面、hooks、表單型別、feature component 放各自 `modules`
- 不再把所有頁面都堆在 `pages/` 底下

## 路由規格

### 公開路由

- `/setup`
  - 首次管理員設定頁
  - 若平台已完成設定，顯示 API 錯誤並導引到 `/login`

- `/login`
  - 帳密登入頁

### 受保護路由

- `/`
  - 預設導向 `/tickets`

- `/tickets`
  - 工單列表頁

- `/tickets/new`
  - 建立工單頁

- `/tickets/:id`
  - 工單詳情頁

- `/db-connections`
  - DB 連線管理頁
  - 僅 `dba` / `admin`

- `/audit-logs`
  - 稽核日誌頁
  - 僅 `dba` / `admin`

### 路由守衛

- 未登入：
  - 造訪受保護頁面時導向 `/login`
- 已登入：
  - 造訪 `/login` 時導向 `/tickets`
- 已完成 setup：
  - `/setup` 不應是正常日常入口

## 導覽規格

### Sidebar 項目

所有登入使用者都看得到：

- `Tickets`
- `New Ticket`

只有 `dba` / `admin` 看得到：

- `DB Connections`
- `Audit Logs`

頁尾固定區域：

- 目前使用者名稱
- 角色標記
- Logout 按鈕

## Auth 規格

### 登入流程

1. `/login` 表單送出 `POST /auth/login`
2. 取得 `access_token`
3. 前端保存 access token
4. 立即呼叫 `GET /auth/me`
5. 初始化 auth context
6. 導向 `/tickets`

### 啟動流程

App 載入時：

1. 若沒有 access token，視為未登入
2. 若有 access token，先呼叫 `/auth/me`
3. 若 `/auth/me` 回 401，嘗試 `/auth/refresh`
4. refresh 成功後重試 `/auth/me`
5. 仍失敗則清空登入態並導向 `/login`

### Logout 流程

1. 呼叫 `POST /auth/logout`
2. 不論 API 是否成功，都清掉前端 access token 與 user state
3. 導回 `/login`

### Auth 資料模型

`GET /auth/me` 回傳應被前端正規化為：

```ts
type CurrentUser = {
  id: number
  username: string
  authGroups: Array<'developer' | 'reviewer' | 'dba' | 'admin'>
}
```

## 頁面規格

### 1. Login Page

#### 目的

讓已 setup 的使用者登入平台。

#### UI 元件

- username input
- password input
- submit button
- inline error message

#### 行為

- submit 時 disabled
- 錯誤時顯示 `invalid credentials` 或通用錯誤
- 成功後導向 `/tickets`

#### 不做

- 不做註冊
- 不做忘記密碼

### 2. Tickets List Page

#### 目的

顯示目前使用者可見的工單清單。

#### API

- `GET /tickets`

#### UI 元件

- page header
- status filter
- table/list
- empty state
- row click 進 detail

#### 欄位

- ticket no
- title
- ticket type
- status
- submitter id 或 submitter 自身提示
- created at

#### 權限行為

- developer：只會看到自己可見的工單
- reviewer / dba / admin：看到更多工單是後端決定，前端不自行過濾

### 3. New Ticket Page

#### 目的

讓使用者建立新工單。

#### API

- `POST /tickets`

#### 欄位

- title
- description
- ticket type
- db_connection_id
- sql_content

#### UI 行為

- 成功後導向新建立的工單詳情頁
- 錯誤顯示 inline message

#### 約束

- `ticket_type` 只有 `ddl` / `dml`
- 若後端回 422，保留使用者輸入

### 4. Ticket Detail Page

#### 目的

顯示工單內容與允許的後續操作。

#### API

- `GET /tickets/:id`
- `POST /tickets/:id/approve`
- `POST /tickets/:id/reject`
- `POST /tickets/:id/request-execution`
- `POST /tickets/:id/execute`

#### 顯示內容

- ticket no
- title
- description
- SQL content
- ticket type
- status
- submitter id
- reviewer id
- executor id
- review comment
- rejection reason
- scheduled at
- started at
- completed at
- created at
- updated at

#### 操作按鈕規則

- `reviewer` 以上：
  - 當狀態可審核時顯示 `Approve` / `Reject`
- `dba` / `admin`：
  - 當狀態為 `approved` 時顯示 `Request Execution`
  - 當狀態為 `pending_execution` 時顯示 `Execute`

#### 操作互動

- Approve：
  - 開 comment optional dialog
- Reject：
  - 開 reason required dialog
- Request Execution：
  - confirm dialog
- Execute：
  - confirm dialog

#### 錯誤處理

- 409：顯示「狀態已變更，請重新整理」
- 422：顯示後端訊息

### 5. DB Connections Page

#### 目的

讓 DBA/Admin 管理目標資料庫連線。

#### API

- `GET /db-connections`
- `POST /db-connections`
- `POST /db-connections/{id}/test`
- `DELETE /db-connections/{id}`

#### UI 組成

- connection list
- create form 或 drawer/modal
- test button
- delete button

#### Create 欄位

- name
- db_type
- host
- port
- database_name
- username
- password
- ssl_mode

#### 行為

- `Test` 顯示 success / fail feedback
- `Delete` 需 confirm
- 不回填密碼

### 6. Audit Logs Page

#### 目的

讓 DBA/Admin 查詢平台稽核事件。

#### API

- `GET /audit-logs`

#### Filters

- action_type
- actor_id
- resource_type
- resource_id
- from
- to
- limit
- offset

#### 顯示欄位

- created at
- actor name
- action type
- resource type
- resource id
- ip address
- details

#### 行為

- 支援 filter submit
- 支援 pagination
- `details` 先用可讀 JSON block 顯示，不做進階 viewer

### 7. Export Entry

#### 目的

讓使用者建立匯出請求並下載結果。

#### API

- `POST /exports`
- `GET /exports/download/{token}`

#### UI 形式

本階段不做獨立 `/exports` 頁面。

建議先以以下兩種其中一種呈現：

- 方案 A：在 ticket detail 內提供 export action
- 方案 B：在獨立 modal / drawer 提供 export request form

本 spec 採用方案 B。

#### 欄位

- db_connection_id
- sql_content

#### 行為

- 建立成功後顯示 `download_url`
- 使用者可點擊下載
- 不保證保存歷史匯出列表

## 共用元件清單

本階段應建立以下共用元件：

- `AppShell`
- `Sidebar`
- `TopBar`
- `PageHeader`
- `DataTable`
- `StatusBadge`
- `EmptyState`
- `FormField`
- `TextInput`
- `Textarea`
- `Select`
- `Button`
- `Dialog`
- `InlineError`
- `LoadingBlock`

約束：

- 只抽真正重複的 primitive
- 不提早抽象複雜業務元件工廠

## 型別規格

需要建立明確的 API response/request 型別，至少包含：

- `CurrentUser`
- `Ticket`
- `TicketStatus`
- `TicketType`
- `DBConnection`
- `AuditLog`
- `ExportRequestPayload`

禁止直接在頁面內散落匿名物件型別。

## API 對接規格

### 錯誤處理規則

若後端回傳：

- `400`：顯示請求格式錯誤
- `401`：走 refresh 流程
- `403`：顯示無權限
- `404`：顯示資源不存在
- `409`：顯示資源狀態已變更
- `422`：顯示表單或業務規則錯誤
- `500`：顯示通用錯誤

### 日期格式

前端統一顯示 local time。

格式先採：

- 列表：`YYYY-MM-DD HH:mm`
- 詳情：`YYYY-MM-DD HH:mm:ss`

## 實作分批

### Phase 1 — Auth 基礎層

範圍：

- `api client`
- auth context
- route guard
- `/login`
- app shell

完成標準：

- 可登入
- 可登出
- 可刷新 token
- 可用 `/auth/me` 初始化身份

### Phase 2 — Ticket 主流程

範圍：

- `/tickets`
- `/tickets/new`
- `/tickets/:id`
- approve / reject / request execution / execute

完成標準：

- 使用者可從登入一路走到提交工單與查看詳情
- reviewer / dba 可做對應操作

### Phase 3 — DBA 管理頁

範圍：

- `/db-connections`
- `/audit-logs`

完成標準：

- DBA/Admin 可管理連線與查 audit log

### Phase 4 — Export 入口與收尾

範圍：

- export modal
- download flow
- 錯誤訊息與空狀態補齊

完成標準：

- 使用者可提交匯出請求並下載結果

## 測試規格

本階段至少要補：

- auth client 單元測試
  - 401 後 refresh 成功會重試
  - refresh 失敗會清狀態
- route guard 測試
- login form 行為測試
- ticket action 基本互動測試

若時間有限，優先保護：

1. auth refresh
2. route guard
3. role-based action visibility

## 已知風險

### 1. 缺少 submitter/reviewer/executor 的完整展示資訊

目前 API 主要回 user id，不一定有完整名稱。前端第一版可先顯示 id，不自行腦補名稱。

### 2. 工單 execute 還不是完整執行引擎

前端 `Execute` 的文案必須保守，避免暗示有完整 statement-level execution viewer。

### 3. Export 沒有歷史列表

第一版只做「建立後立即拿下載連結」，不保證使用者能回頭找舊匯出。

## 待實作前確認事項

開始做前端前，先確認以下假設仍成立：

1. `/auth/me` 保持穩定，回傳 `id`、`username`、`auth_groups`
2. `/tickets` 與 `/tickets/:id` response shape 不會在近期大改
3. 不會在這一輪同時引入新的 dashboard / SQL Editor 範圍

若以上任一改變，應先更新這份 spec 再實作。
