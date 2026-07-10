# 登入安全與 Session

本文件描述平台登入、access token、refresh token、MFA 與 session 管理的現行行為。這些機制屬於安全邊界，不應只依賴前端路由判斷。

## Token 模型

平台使用兩種 token：

| 類型 | 保存位置 | 用途 |
|---|---|---|
| Access token | 前端 memory-only state | 呼叫一般 API，生命週期短 |
| Refresh token | `HttpOnly` cookie | 重新取得 access token 與維持瀏覽器 session |

Access token 不寫入 `localStorage` 或 `sessionStorage`。頁面重新整理時，前端會先呼叫 `POST /api/auth/refresh`，再呼叫 `GET /api/auth/me` 取得目前使用者狀態。

## Refresh Cookie

Refresh cookie 的固定行為：

- `HttpOnly`
- `SameSite=Strict`
- path 為 `/api/auth/refresh`
- production 環境強制 `Secure`

`REFRESH_COOKIE_SECURE=true` 可以在非 production 環境強制使用 `Secure`，但 production 不允許透過設定關閉 `Secure`。

## Refresh Token Rotation

每次 refresh 成功後，系統會：

1. 撤銷舊 refresh session
2. 建立新 refresh session
3. 回傳新的 access token
4. 設定新的 refresh cookie

若舊 refresh token 在短時間內被重複使用，系統會套用 grace window，避免同一使用者多個 tab 同時 refresh 時誤判。超過 grace window 的 reuse 會被視為風險事件，系統會撤銷該使用者的所有 session，要求重新登入。

## MFA Enforcement

MFA 由部署環境變數控制：

| 變數 | 可用值 | 說明 |
|---|---|---|
| `MFA_ENFORCEMENT` | `disabled` | 不強制 MFA |
| `MFA_ENFORCEMENT` | `required_for_admins` | admin user 與 admin group 成員必須使用 MFA |

預設值：

- `APP_ENV=production`：`required_for_admins`
- 其他環境：`disabled`

高權限帳號首次登入時，如果尚未啟用 MFA，登入流程會進入 MFA setup。使用者掃描 QR code 或輸入 setup key 後，提交 6 位 TOTP code；驗證成功才會建立正式 session。

每個 user 的 MFA secret 獨立存放在該 user 記錄上。把一般 user 加入 admin group 後，若該 user 尚未啟用 MFA，下次登入會產生該 user 專屬的 QR code / setup key；不會共用原始 `admin` 帳號的驗證碼。

MFA setup QR code / setup key 等同長期 TOTP secret。任何人取得同一個 QR code 或 setup key，都能在自己的 authenticator app 產生同一組 6 位驗證碼。正式環境不應把 QR code 截圖保存到共用文件、ticket、群組或 wiki。

正式環境建議：

- 初始 `admin` 帳號只用於 bootstrap
- 每位管理員建立獨立 user
- 將管理員各自加入 admin group
- 每位管理員各自完成 MFA setup
- 不共用同一個 admin 帳號、密碼或 MFA QR code

## MFA Recovery

平台提供兩種 recovery：

| 情境 | 做法 |
|---|---|
| 仍有其他管理員可登入 | 在 Users 頁對指定 user 執行 Reset MFA |
| 所有管理員都無法完成 MFA | 使用 break-glass CLI reset 指定帳號 |

Break-glass 指令：

```bash
make reset-mfa USERNAME=admin
```

或直接執行：

```bash
cd backend
go run ./cmd/server -reset-mfa-username admin
```

Reset MFA 會清除該使用者 MFA secret、停用 MFA 狀態、撤銷現有 sessions，並寫入 audit log。

擁有 `users.write` 權限的 admin 可以 reset 其他 user 的 MFA，也可以 reset 自己的 MFA。Self-reset 會撤銷自己的 sessions，因此操作後需要重新登入並重新完成 MFA setup。正式環境建議至少保留兩個獨立 admin 帳號，讓管理員可以互相協助 reset MFA；若所有 admin 都無法登入，再使用 break-glass CLI。

## Lark OAuth 登入

平台支援 Lark OAuth 作為登入入口。OAuth 只負責身份識別與 Lark `open_id` 綁定，不會自動授予 DBA / admin / DB scope 權限。

相關 API：

| API | 用途 |
|---|---|
| `GET /api/auth/lark/login/start` | 產生 Lark OAuth 授權 URL |
| `GET /api/auth/lark/login/callback` | 接收 Lark callback，交換 access token 並取得使用者資訊 |
| `POST /api/auth/lark/login/result/consume` | 前端消費一次性 login result，建立平台 session |

身份匹配順序：

1. 優先用 `lark_login_open_id` 或 `lark_login_union_id` 找既有 user。
2. 若尚未綁定，使用 Lark 回傳的 `enterprise_email` 匹配 `users.email`。
3. 若匹配到既有 user，系統會自動綁定 Lark identity 後登入。
4. 若找不到既有 user，系統會建立普通 user，加入 developer auth group，並停用密碼登入。

平台不使用 Lark personal email 建立或匹配 `users.email`。若 Lark user 沒有 `enterprise_email`，或 enterprise email domain 不符合 deploy env allowlist，登入會失敗。

部署控制：

| 變數 | 預設 | 說明 |
|---|---|---|
| `LARK_OAUTH_REQUIRE_ENTERPRISE_EMAIL` | `true` | 是否要求 Lark 回傳 `enterprise_email` |
| `LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS` | `edgex.exchange` | 允許的企業信箱 domain，逗號分隔 |

`LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS=edgex.exchange` 時，`<user>@edgex.exchange` 與 `<user>@team.edgex.exchange` 允許登入，其他 domain 會被拒絕。

Protected admin 不允許透過 Lark email 自動綁定。這是 bootstrap admin 的安全邊界，避免有人用同 email 的 Lark identity 接管初始管理員。

若環境啟用了 MFA policy，Lark OAuth 成功後仍會套用既有 MFA 要求。也就是說，高權限帳號不會因為改用 Lark 登入而繞過 MFA。

## Session 管理

使用者可以在 `/account/sessions` 查看自己的 active refresh sessions，並撤銷不認識的 session。

Admin 可以在 Users 頁查看與撤銷指定 user 的 sessions。停用 user 時，後端也會撤銷該 user 的 sessions。

API：

| API | 用途 |
|---|---|
| `GET /api/auth/sessions` | 查看自己的 sessions |
| `DELETE /api/auth/sessions/{id}` | 撤銷自己的指定 session |
| `DELETE /api/auth/sessions` | 撤銷自己的所有 sessions |
| `GET /api/users/{id}/sessions` | Admin 查看指定 user sessions |
| `DELETE /api/users/{id}/sessions/{sessionID}` | Admin 撤銷指定 user 的單一 session |
| `DELETE /api/users/{id}/sessions` | Admin 撤銷指定 user 的所有 sessions |
| `POST /api/users/{id}/mfa/reset` | Admin reset 指定 user MFA |

## Audit Log

登入安全相關 audit 至少包含：

- login success
- login failed
- disabled user login attempt
- MFA failed
- MFA enabled
- MFA reset
- break-glass MFA reset
- Lark OAuth login success / failed
- refresh token reuse detection

Audit log 用於事後追蹤安全事件，不取代即時 rate limit 或後端授權。

## Security Headers

後端會加上基本安全 header：

- `Content-Security-Policy`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy`
- `Permissions-Policy`
- production 環境加上 `Strict-Transport-Security`

目前 CSP 仍允許 `style-src 'unsafe-inline'`，主要是為了相容前端現有 inline style 與部分 UI runtime 行為；後續若要收緊 CSP，需要先移除或替換這些 inline style 來源。

## 相關文件

- [設定與環境變數](configuration.md)
- [Users / RBAC](users-and-rbac.md)
- [後端 API 與權限對照](backend-api-and-permissions.md)
