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

實作上會先用 OAuth code 換取 access token，再呼叫 `GET /open-apis/authen/v1/user_info` 取得身份資料。平台只接受 `user_info.enterprise_email` 作為 `users.email` 匹配依據；`user_info.email` 可能是 personal email，不會寫入 `users.email`，也不會拿來匹配既有使用者。

部署控制：

| 變數 | 預設 | 說明 |
|---|---|---|
| `LARK_OAUTH_SCOPES` | `directory:employee.base.enterprise_email:read` | OAuth authorize URL 要求的 scopes，逗號分隔 |
| `LARK_OAUTH_REQUIRE_ENTERPRISE_EMAIL` | `true` | 是否要求 Lark 回傳 `enterprise_email` |
| `LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS` | `example.com` | 允許的企業信箱 domain，逗號分隔 |

`LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS=example.com` 時，`<user>@example.com` 與 `<user>@team.example.com` 允許登入，其他 domain 會被拒絕。

`LARK_OAUTH_SCOPES` 只控制 OAuth 授權頁要求的 scope。若授權頁已顯示企業郵箱權限但登入仍出現 `lark user info missing enterprise_email` 或 `lark enterprise_email is required`，通常代表 `GET /open-apis/authen/v1/user_info` 沒回 `enterprise_email`；此時應優先檢查 Lark app 的「查看員工工作郵箱」欄位權限、審核發布狀態與使用者授權結果。

Protected admin 不允許透過 Lark email 自動綁定。這是 bootstrap admin 的安全邊界，避免有人用同 email 的 Lark identity 接管初始管理員。

若環境啟用了 MFA policy，Lark OAuth 成功後仍會套用既有 MFA 要求。也就是說，高權限帳號不會因為改用 Lark 登入而繞過 MFA。

## CLI Bearer 登入（IdP 簽發的 OIDC Token）

除了 session access token，`Authorization: Bearer` 也可以直接帶 Authentik 簽發的 OIDC token（id_token 或 access_token）。用途是 CLI 與自動化工具：使用者在自己的瀏覽器完成 loopback OIDC 登入拿到 token，之後呼叫 API 不需要瀏覽器 session、refresh cookie 或 `/api/auth/refresh`。

規則：

- 只在 `SSO_OIDC_BEARER_ISSUER_URL` 與 `SSO_OIDC_BEARER_AUDIENCES` 都設定時啟用，預設關閉。issuer 必須是 https URL。
- 驗證項目：簽章（只接受 RS256，金鑰來自 issuer discovery 的 JWKS，金鑰輪換時最多每 30 秒重新拉取一次，偽造簽章不會讓伺服器反覆打 IdP）、`iss` 必須與設定完全相同、`exp`、`aud` 必須包含允許清單中的 client id。超過 16 KiB 的 token、issuer 不符、`aud` 不符、過期或沒有 `exp` 的 token 在驗簽前就被拒絕，不產生網路請求。`aud` 檢查不能省：Authentik 所有 provider 共用同一把簽章金鑰，沒有 `aud` 限制時任何 app 的 token 都能登入。
- 對應使用者：只用 `external_identity_source='oidc'` 加 token `sub` 找瀏覽器 SSO 登入時綁定的使用者。不比對 email、不建立、不綁定：新使用者仍須先用瀏覽器 SSO 登入一次。因此簽發 CLI token 的 Authentik provider 必須與 Maestro 的 provider 使用相同的 Subject mode（預設「Based on the User's hashed ID」），否則 `sub` 對不上，bearer 登入一律失敗（fail closed）。
- Protected 使用者（bootstrap admin）不能用 bearer token，與瀏覽器 SSO 不自動綁定 protected 使用者的規則一致。
- MFA：bearer 流程沒有 TOTP 步驟。落在 MFA policy 內的使用者（`MFA_ENFORCEMENT=required_for_admins` 下的 admin）只有在 SSO「信任 IdP MFA」設定生效時才能通過，判斷時機是每次請求，來源與瀏覽器 SSO 相同（環境變數 `SSO_OIDC_TRUST_MFA`，Settings 有設定時以 Settings 為準）。設定關閉時這些使用者的 bearer 請求回 401，一般使用者不受影響。
- 之後的檢查與一般登入相同：active user、RBAC、DB scope 都照常套用。
- 沒有 session row：`/api/auth/me` 的 `auth_method` 為空，logout 與 session 撤銷對這種 token 無效。要撤銷只能在 Authentik 撤銷 token，或停用使用者。
- Audit：目前沒有逐請求的 audit，bearer 請求只會在後續動作（工單、查詢）留下一般 audit 紀錄。拒絕的 token 記在 log（discovery 失敗為 warn，其餘 debug；有效 token 但無綁定使用者為 info）。
- Authentik 端：允許清單裡的 client 其存取政策必須與 Maestro app 相同（或直接用 Maestro 專屬的 CLI client）。Authentik 只在簽發時檢查該 client 的政策，使用者若被移出 Maestro app 但仍在 CLI client 的政策內，只要 Maestro 帳號還是 active 就仍能呼叫 API。

| 變數 | 預設 | 說明 |
|---|---|---|
| `SSO_OIDC_BEARER_ISSUER_URL` | 無 | 簽發 token 的 Authentik provider issuer，https，須與 discovery document 的 `issuer` 完全相同（含結尾 `/`） |
| `SSO_OIDC_BEARER_AUDIENCES` | 無 | 允許的 client id 清單，逗號分隔 |

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
