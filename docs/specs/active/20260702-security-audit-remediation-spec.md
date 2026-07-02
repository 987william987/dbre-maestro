---
status: active
spec_issue_number:
spec_issue_url:
spec_filed_at: 2026-07-02T00:00:00+08:00
spec_branch:
spec_plan_mode: inactive
spec_executed: false
spec_worktree_path:
source_audit: /Users/william_yeh/Desktop/DBA平台安全审计.pdf
---

# DBA 平台安全審計修復規格

## 背景

安全團隊審計報告確認 DBRE Maestro 目前有 7 個需要修復的安全問題，以及 1 條長期必須遵守的資料庫唯讀配置規範。

本規格的目標不是把報告逐字搬進 backlog，而是把每個問題轉成可執行、可驗收、可測試的工程任務，並對照目前程式碼的實際結構。

## 成功標準

- S-01 到 S-07 都有對應的程式修復、測試與審計事件。
- 高危問題 S-01、S-02、S-03 優先完成，且不能只靠文檔或操作流程緩解。
- 所有涉及 token、密碼、MFA secret、DB credential 的修復都不得在 log、audit log、前端錯誤訊息中輸出敏感值。
- 權限相關修復必須 fail-closed。授權判斷不確定時拒絕，不默認放行。
- 對使用者造成行為變更的地方，前端需要有明確錯誤訊息或提示，而不是只回 `403`。

## 已驗證現況

### 架構與主要檔案

- Go backend 使用 `chi` route 與 middleware。
- 全站 API route 在 `backend/cmd/server/main.go` 註冊。
- 權限注入與權限檢查在 `backend/internal/middleware/auth.go`、`backend/internal/middleware/inject_permissions.go`。
- 使用者與 auth group 管理在 `backend/internal/handler/user.go`、`backend/internal/handler/auth_group.go`。
- ticket flow 在 `backend/internal/handler/ticket.go` 與 workflow resolution 相關 handler。
- query / masking 在 `backend/internal/handler/query.go`、`backend/internal/handler/masking_runtime.go`。
- scheduled report 在 `backend/internal/handler/scheduled_sql_report.go`。
- DB connection 與 credential resolution 在 `backend/internal/handler/db_connection.go`、`backend/internal/repository/db_connection.go`。
- export download 在 `backend/internal/handler/export.go`。

### 關鍵現況

- `backend/cmd/server/main.go` 使用 `chimw.Logger`，且 export download route 目前是公開 `GET /api/exports/download/{token}`。
- `users.write` 能呼叫使用者 session revoke、MFA reset、Patch、membership、direct permission、DB scope 等高危接口。
- `UserRepo.HasAllPermissions` 對 protected user 直接回傳 true，protected bootstrap admin 實際上是最高權限帳號。
- MFA challenge 目前是 5 分鐘 JWT，沒有 server-side challenge 狀態、失敗次數、單次使用或 revoke 機制。
- `maskingRuntime` 目前核心判斷只支援 MySQL；Redis query path 直接 return，不會進 masking。
- DB Connection PATCH 允許 endpoint 變更但不要求同步提供新密碼；後續 `ResolveCredential` 會把舊 credential 套到新 endpoint。

## 優先級與交付順序

### Phase 1: 帳號接管與權限提升阻斷

包含：
- S-01 受保護 bootstrap admin 可被接管
- S-07 `users.write` 被下放時可自我提權

原因：
- 這兩項都會讓攻擊者取得全平台權限。
- 若先修其他資料面問題，但 admin 可被接管，整體風險仍然不可接受。

### Phase 2: 流程控制與資料外洩阻斷

包含：
- S-02 工單流程職責分離
- S-06 export download token 外洩

原因：
- 工單流程是 DBA 平台的主要控制面。
- export token 進 access log 後，會把資料下載權變成 log reader 權限。

### Phase 3: 脫敏與 report fail-closed

包含：
- S-03 PostgreSQL / Redis 脫敏能力缺失

原因：
- 這項影響交互查詢與 scheduled report。
- 修復需要先定義 support matrix 與 fail-closed 行為，範圍較大。

### Phase 4: MFA 與 DB Connection SSRF / credential 外發

包含：
- S-04 修改 DB Connection 地址時復用已存 credential
- S-05 MFA verify 缺少速率限制與鎖定

原因：
- 兩項都是中危，但都涉及資料模型或行為變更。
- 可在 Phase 1 到 3 完成後獨立交付。

### Phase 5: 安全配置規範落地

包含：
- C-01 Readonly Host 必須指向真正只讀資料庫連線

原因：
- 這不是單一漏洞，但會影響 SQL Editor、Export、Scheduled Report 的長期安全邊界。

## S-01: Protected Bootstrap Admin 可被接管

### 目前問題

`users.write` 持有者可對 protected admin 做 password-only PATCH，也可呼叫 MFA reset。攻擊者可以重設 protected admin 密碼、清空 MFA，再以新密碼登入並綁定自己的 TOTP。

### 修復策略

新增 protected user 高危操作守門函式：

- `requireProtectedUserAdmin(ctx, users, actorID, targetUser) error`
- 若 target 不是 protected user，直接通過。
- 若 target 是 protected user，actor 必須是 all-permissions admin。
- actor 不能只靠普通 `users.write` 修改 protected user 的認證材料。

適用操作：

- `PATCH /api/users/{id}` 中的 password、username、email、lark recipient、active 狀態、auth group、direct permission、DB scope。
- `POST /api/users/{id}/mfa/reset`。
- `DELETE /api/users/{id}/sessions` 與 `DELETE /api/users/{id}/sessions/{sessionID}`。
- direct permission / membership / DB connection scope 修改。

保守決策：

- protected user 的 MFA reset 不開普通 admin override。若未來要 break-glass，另開獨立 CLI 或一次性流程，不混在 `users.write` API 裡。

### 需要修改的檔案

- `backend/internal/handler/user.go`
- `backend/internal/repository/user.go` 若需要補 actor helper
- `backend/internal/handler/user_test.go` 或新增對應 handler test

### 驗收條件

- 普通 `users.write` 使用者無法修改 protected admin 密碼。
- 普通 `users.write` 使用者無法 reset protected admin MFA。
- all-permissions admin 可以執行必要 protected user 管理操作。
- 所有拒絕操作都回 `403`，且 audit log 記錄 actor、target user、action、reason，不記錄密碼或 MFA secret。
- protected user session revoke 也受到同一守門函式保護。

## S-07: `users.write` 可自我提權為 Admin

### 目前問題

使用者只要取得 `users.write`，就能把自己加入 admin group，或修改 auth group permission，間接取得 all-permissions。

### 修復策略

新增權限授予上界：

- 非 all-permissions actor 不得授予 admin group。
- 非 all-permissions actor 不得授予 all-permissions auth group。
- 非 all-permissions actor 不得修改 protected auth group。
- 非 all-permissions actor 只能授予自己已擁有、且允許委派的 permission。

新增 helper：

- `actorHasAllPermissions(ctx, users, actorID) (bool, error)`
- `canGrantPermission(ctx, users, actorID, permissionKey) (bool, error)`
- `canModifyAuthGroup(ctx, users, actorID, group) error`
- `canGrantAuthGroup(ctx, users, actorID, group) error`

### 需要修改的檔案

- `backend/internal/handler/user.go`
- `backend/internal/handler/auth_group.go`
- `backend/internal/repository/user.go`
- `backend/internal/repository/auth_group.go`

### 驗收條件

- 只有 `users.write` 但不是 all-permissions 的使用者，不能把自己或他人加入 admin group。
- 只有 `users.write` 但不是 all-permissions 的使用者，不能替 auth group 加上自己沒有的 permission。
- protected auth group 的 permission、DB scope、member 修改都需要 all-permissions admin。
- 所有拒絕都要有 audit event，且 reason 可被 DBA/Admin 排查。

## S-02: 工單流程未強制職責分離

### 目前問題

目前 `canReviewTicket` 只檢查 review permission 與 workflow candidate，沒有排除 submitter。`canExecuteTicket` 也沒有排除 submitter。

### 修復策略

強制職責分離：

- reviewer 不能等於 submitter。
- executor 不能等於 submitter。
- workflow resolution 在計算候選人時應排除 submitter。
- preview / simulate 要顯示被排除的人員與原因。

本期採用「提交人分離」作為必做安全邊界：

- 即使 submitter 是 admin，也不能審批或執行自己提交的工單。
- 若 admin William 提交工單，可以由另一位 admin 審批。
- 若該另一位 admin 同時具備 execute 權限與 workflow executor 候選資格，本期允許同一位另一位 admin 執行。
- 也就是說，本期禁止的是「自己處理自己的單」，不是強制 reviewer 與 executor 必須是不同人。

保守決策：

- admin 不默認豁免職責分離。
- 若未來業務真的需要緊急豁免，另新增 `tickets.sod_override` 權限與強 audit，不在本次修復中加入。
- 若安全團隊要求完整三方分離，後續可升級為 `executor != reviewer`，但這會要求每張敏感工單至少有三個不同角色的人參與。

### 需要修改的檔案

- `backend/internal/handler/ticket.go`
- `backend/internal/handler/workflow_resolution.go`
- `backend/internal/handler/settings.go` 中 workflow preview / simulate 回應
- `frontend` ticket review / execute 錯誤提示

### 驗收條件

- submitter 呼叫 approve 類 API 時回 `403`，錯誤訊息明確為職責分離限制。
- submitter 呼叫 execute 類 API 時回 `403`。
- 另一位具備權限且符合 workflow resolution 的 admin 可以 approve submitter 的工單。
- 另一位具備權限且符合 workflow resolution 的 admin 可以 execute submitter 的工單，即使他也是 reviewer。
- workflow preview / simulate 能看到候選人被排除的原因。
- 測試涵蓋 DDL、DML、Redis、query access、sensitive query access、export ticket。

## S-06: Export Download Token 被寫入 Access Log

### 目前問題

download token 放在 URL path，`chimw.Logger` 會記錄完整 URL。下載接口目前只靠 token 取得 export request，不要求登入。

### 修復策略

短期必做：

- download route 加上 auth middleware。
- download handler 驗證 current user 必須是 export requester，或具備 `sql_editor.export_review` / all-permissions。
- 自訂 HTTP logger 或 wrapper，對 `/api/exports/download/{token}` 做 redaction。
- token 下載成功後標記為 used，或把有效期縮短到可接受範圍。

後續可選：

- 改成 `POST /api/exports/{id}/download-token` 取得一次性短 token，再下載。
- 避免 URL path 作為唯一 bearer credential。

### 需要修改的檔案

- `backend/cmd/server/main.go`
- `backend/internal/handler/export.go`
- `backend/internal/repository/export.go`
- `backend/internal/model/export.go`
- 可能新增 migration：為 export download token 增加 `used_at` 或 `downloaded_at`

### 驗收條件

- 未登入使用者不能下載 export。
- 非 requester 且沒有 review 權限的使用者不能下載 export。
- access log 不出現完整 download token。
- 同一 token 若設計為一次性，第二次下載回 `410 Gone` 或明確錯誤。
- audit log 能記錄下載者 user id、export id、成功/失敗原因。

## S-03: PostgreSQL / Redis 脫敏能力缺失

### 目前問題

masking rule 語義上是平台全局規則，但實作主要只覆蓋 MySQL。PostgreSQL 查詢可能繞過有效脫敏，Redis path 直接回傳，Scheduled SQL Report 只做敏感欄位檢測且 PG 檢測結果可能為空。

### 支援矩陣

| DB 類型 | Interactive Query | Scheduled Report | 本次策略 |
|---|---|---|---|
| MySQL | 支援脫敏 | 必須實際 apply masking | 保持並補測試 |
| PostgreSQL | 補欄位名與 origin based masking | 必須實際 apply masking | Phase 3 必做 |
| Redis | 不做 SQL column masking | 禁止 scheduled report | interactive 先採命令和值層級保守策略 |

### 修復策略

PostgreSQL：

- 使用既有 query origin resolution 能力，將 result column 對回 schema/table/column。
- 若 origin resolution 不完整，至少基於欄位名、alias、常見敏感名稱做保守脫敏。
- 若查詢結果命中 masking rule 但無法可靠處理，fail-closed。

Redis：

- Redis interactive query 不套 SQL column masking。
- Redis 應有獨立 sensitive key / command 策略。
- 對已知高危命令保持禁止。
- 對返回值若無法判斷欄位語義，不能假裝已脫敏。
- Scheduled SQL Report 不允許 Redis connection。

Scheduled Report：

- 不再只做 `analyzeSensitiveColumns` 後直接輸出 CSV。
- 統一呼叫 `masking.applyResult` 或同等流程，確保輸出到 CSV 的資料已脫敏。
- 若 DB 類型不支援可靠脫敏但存在 masking rule 或敏感欄位可能性，拒絕建立或執行 report。

### 需要修改的檔案

- `backend/internal/handler/masking_runtime.go`
- `backend/internal/handler/query.go`
- `backend/internal/handler/scheduled_sql_report.go`
- `backend/internal/handler/redis_command_policy.go` 或現有 Redis policy 檔案
- `frontend` masking rule / scheduled report / DB connection UI 的支援矩陣提示

### 驗收條件

- PostgreSQL 查詢敏感欄位時，結果不會明文返回。
- PostgreSQL scheduled report 輸出的 CSV 不會明文包含敏感欄位。
- Redis 不允許建立 scheduled report。
- Redis interactive query 若遇到高風險或無法保證安全的敏感輸出場景，回明確錯誤，不回明文。
- 前端顯示 masking support matrix，避免 admin 誤以為所有 DB 類型都等效支援。

## S-04: DB Connection Endpoint 變更時復用已存 Credential

### 目前問題

DB Connection 可修改 host / port / role endpoint，但若 request 未傳 password，舊 credential 仍會被套用到新 endpoint。攻擊者可讓後端把舊 DB credential 發往新 host。

### 修復策略

短期必做：

- 偵測 endpoint 變更，包括 default host/port、readonly host/port、readwrite host/port。
- endpoint 變更時，要求 affected role 同步提供 password。
- 若不提供 password，拒絕 PATCH，不能保留舊 credential。
- 連線測試錯誤對前端脫敏，不回傳原始 driver error 中可能包含的 host、username、network detail。

中期加強：

- 支援 DB host allowlist 或 CIDR allowlist，由 env 或 settings 管理。
- 禁止連向 metadata service、localhost、link-local、未允許內網段。

### 需要修改的檔案

- `backend/internal/handler/db_connection.go`
- `backend/internal/repository/db_connection.go`
- `backend/internal/db/pool` 或 connection test error wrapping
- `frontend` DB Connection edit form

### 驗收條件

- 修改 host 但不提供 password 時回 `422`。
- 修改 readonly endpoint 但不提供 readonly credential 時回 `422`。
- 修改 readwrite endpoint 但不提供 readwrite credential 時回 `422`。
- 未修改 endpoint 時仍可只更新 name、description、tags 等非敏感欄位。
- connection test 的前端錯誤不顯示敏感 credential 或過細 network detail。

## S-05: MFA Verify 缺少速率限制與 Challenge 鎖定

### 目前問題

`POST /api/auth/mfa/verify` 只驗 JWT challenge 與 TOTP code。失敗後不鎖定、不作廢、不限制 per-token 嘗試次數。

### 修復策略

新增 server-side MFA challenge state。

資料模型建議：

- `mfa_challenges.id`
- `mfa_challenges.token_id`
- `mfa_challenges.user_id`
- `mfa_challenges.setup`
- `mfa_challenges.expires_at`
- `mfa_challenges.attempt_count`
- `mfa_challenges.used_at`
- `mfa_challenges.revoked_at`
- `mfa_challenges.created_ip`
- `mfa_challenges.created_at`

行為：

- login 產生 MFA token 時，同步建立 challenge record。
- MFA token claims 加入 `jti` 或 challenge id。
- verify 前先查 challenge 狀態。
- 每次錯誤 TOTP 增加 attempt_count。
- attempt_count 達 5 次後 revoke challenge。
- 成功後設定 used_at，token 不可再次使用。
- 增加 per-user、per-token、per-IP rate limiter。

### 需要修改的檔案

- `backend/internal/auth/jwt.go`
- `backend/internal/handler/auth.go`
- `backend/internal/repository` 新增 `mfa_challenge.go`
- `backend/internal/model` 新增 MFA challenge model
- `backend/migrations` 新增 table

### 驗收條件

- 同一 MFA challenge 連續錯 5 次後不能再使用。
- 成功驗證後，同一 challenge 第二次使用失敗。
- MFA verify 被 rate limit 時回 `429`。
- setup flow 與正常 MFA flow 都使用同一 challenge state。
- audit log 記錄 MFA fail、lock、success，不記錄 TOTP code。

## C-01: Readonly Host 必須是真正只讀連線

### 要求

Readonly credential 必須只具備業務查詢所需的最小 SELECT 權限，不可具備 DML、DDL、FILE、superuser、UDF、危險 extension 或 server program execution 類權限。

### 程式側落地

- SQL Editor、Export、Scheduled Report 必須使用 readonly role credential。
- 不允許 readonly credential 缺失時 fallback 到 readwrite credential。
- DB Connection UI 要清楚標示 readonly credential 是安全邊界，不是 optional label。
- connection test 要能分角色測試 readonly / readwrite。

### 文檔側落地

- 補到部署文檔與 DBA/Admin 使用手冊。
- 說明 MySQL 與 PostgreSQL readonly user 最小權限範例。
- 說明每次修改 DB Connection、DB user grant、readonly endpoint 都必須重新複查。

### 驗收條件

- readonly credential 未配置時，SQL Editor / Export / Scheduled Report 不 fallback 到 readwrite。
- DBA/Admin 文檔明確列出 MySQL 與 PostgreSQL 禁止權限。
- DB Connection 頁面對 readonly role 顯示安全提示。

## 測試計畫

### Backend unit / handler tests

- protected user password / MFA / session / membership / permission / DB scope guard。
- auth group protected / all-permissions guard。
- ticket approve / execute SOD guard。
- export download auth guard 與 token redaction。
- MFA challenge attempt / lock / used_at。
- DB connection endpoint change password requirement。

### Integration tests

- 建立 submitter、reviewer、executor 三種使用者，驗證工單流程不可同人閉環。
- 建立 PostgreSQL 測試表與敏感欄位，驗證 query 與 scheduled report 脫敏。
- 建立 export request，驗證非 requester 不能下載。
- 修改 DB connection endpoint，驗證不提供 password 時不能保存。

### Manual QA

- Admin 管理 protected user 時，只有 all-permissions admin 能成功。
- 普通 DBA/Admin 在 workflow preview 看到被排除候選人原因。
- Scheduled Report 建立 Redis report 時被阻擋。
- DB Connection edit form 在 endpoint 改動時要求重新輸入 password。

## Rollback

- Phase 1 / Phase 2 屬於安全阻斷，不建議 rollback。若影響操作，應用 hotfix 調整錯誤訊息或補合法授權，不應恢復漏洞行為。
- Phase 3 若 PostgreSQL masking 導致誤殺，可先以 feature flag 關閉 PG scheduled report，interactive query 保持 fail-closed。
- Phase 4 的 endpoint password requirement 若影響既有編輯流程，可短期只允許非 endpoint 欄位更新，不回退 credential reuse。
- MFA challenge table 可保留，rollback 時舊 JWT flow 仍可讀 token，但不建議回退。

## 待確認決策

以下決策目前採保守值，實作前可再由 William / SRE / 安全團隊確認：

- S-02 是否允許緊急 admin override。本規格預設不允許。
- S-03 Redis interactive query 是否做值層級 pattern masking，或遇到敏感風險一律拒絕。本規格預設保守拒絕高風險與不可靠場景。
- S-06 export download token 是否立即改成一次性。本規格建議一次性，但若前端下載重試需求明確，可改為短 TTL 加 authenticated download。
- S-04 host allowlist 是否本次一起做。本規格把 endpoint password requirement 列必做，host allowlist 列中期加強。
