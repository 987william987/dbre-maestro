# 前端維護參考

本文件整理前端結構、route guard、API client 與新增頁面時的規則。

## Runtime

| 項目 | 值 |
|---|---|
| Framework | React 18 |
| Build tool | Vite |
| Language | TypeScript |
| Router | React Router |
| Tests | Vitest + Testing Library |
| Icons | `lucide-react` |
| SQL editor | CodeMirror |

## 主要目錄

| 路徑 | 責任 |
|---|---|
| `frontend/src/App.tsx` | route 定義與 lazy page import |
| `frontend/src/app/router` | route guard |
| `frontend/src/app/layout` | AppShell 與主導覽 |
| `frontend/src/shared/api` | API client、token refresh、SSE parser |
| `frontend/src/shared/auth` | AuthContext、permission helper |
| `frontend/src/shared/types` | 後端 response 對應的 TypeScript types |
| `frontend/src/shared/ui` | 共用 UI 元件 |
| `frontend/src/modules/*/api.ts` | 各功能模組 API wrapper |
| `frontend/src/modules/*/pages` | 各功能頁 |

## Route Guard 模型

前端 route 有兩層保護：

- `ProtectedRoute`：要求使用者已登入
- `RoleRoute`：要求使用者具備指定 permission 之一

`RoleRoute` 只控制頁面可見性與導覽體驗，不是安全邊界。後端 API 必須再次驗證 permission。

## API Client 行為

`frontend/src/shared/api/client.ts` 提供：

- `apiClient.get/post/patch/put/delete`
- `apiClient.download`
- `openEventStream`
- `ApiError`

預設 API prefix 是 `/api`。如果 request 回 `401`，client 會嘗試 refresh access token，再重試一次。refresh 失敗後呼叫 auth failure handler。

SSE 使用 `openEventStream`，目前支援：

- Authorization header
- reconnect
- `event:` / `data:` block parsing
- external abort signal

## 新增頁面流程

1. 在 `frontend/src/modules/<feature>/api.ts` 建 API wrapper。
2. 在 `frontend/src/shared/types` 補 response / payload type。
3. 在 `frontend/src/modules/<feature>/pages` 建頁面。
4. 在 `frontend/src/App.tsx` 加 lazy import 與 route。
5. 用 `RoleRoute` 指定頁面 permission。
6. 在 AppShell 導覽中加入入口。
7. 補 test，至少覆蓋權限 guard、主要互動或格式化邏輯。

## 權限顯示規則

前端可以依 permission 隱藏頁面、按鈕或欄位，但不能假設隱藏就等於安全。以下情境後端一定要擋：

- 使用者直接打 API URL
- 使用者用 DevTools 修改 request payload
- 使用者有 `users.write` 但不是 all-permissions admin
- 使用者有某頁面讀權限但沒有 DB scope
- 使用者送出敏感查詢、export、ticket execute

## 錯誤處理

後端 JSON 錯誤通常是：

```json
{
  "error": "message"
}
```

前端應優先顯示 `ApiError.message`。對安全限制類錯誤，UI 文案要能讓 DBA/Admin 判斷下一步，例如：

- 缺少 permission
- 缺少 DB scope
- 職責分離限制
- host policy violation
- masking policy blocked
- workflow resolution 缺少 approver / executor

## Build 與測試

```bash
cd frontend
npm ci
npm run build
npm test
```

repo root 也提供：

```bash
make test-frontend
```

## 新增前端功能檢查表

- route 是否在 `App.tsx` 註冊
- page 是否有正確 `RoleRoute`
- API wrapper 是否使用 `apiClient`
- TypeScript type 是否與後端 response 對齊
- loading、empty、error、forbidden 狀態是否可理解
- 長文字、長 SQL、長 instance name 是否不破壞版面
- mutation 成功後是否刷新列表或更新 local state
- 是否需要 SSE 即時更新

