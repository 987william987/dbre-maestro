# How to 驗證安全審計修復

本文用來驗證安全審計報告中的 S-01 到 S-07 是否在目標環境生效。它是部署後驗收清單，不取代程式測試。

## Prerequisites

你需要：

- 一個 all-permissions admin
- 一個只有部分管理權限的 DBA 測試帳號
- 一個普通 RD 測試帳號
- 至少一個 MySQL 或 PostgreSQL DB Connection
- 至少一個 Redis DB Connection，如果要驗證 Redis sensitive prefix
- 可查看 backend log 與 audit log

## S-01 Protected admin

1. 用非 all-permissions、但具備 `users.write` 的測試帳號登入。
2. 嘗試修改 protected admin 的 password、MFA、active 狀態、auth group、direct permission、DB scope。
3. 預期全部被拒絕。

驗證：

- API 回 `403`
- audit log 有拒絕事件
- protected admin 可正常登入，權限未被修改

## S-02 工單職責分離

1. 用使用者 A 建立 DDL / DML / Redis 或 export 工單。
2. 用同一個使用者 A 嘗試 approve。
3. 用同一個使用者 A 嘗試 execute。
4. 用使用者 B approve。
5. 用使用者 C execute，或依環境使用另一個合格 executor。

驗證：

- A approve / execute 自己的單會被拒絕
- reviewer 不能 execute 同一張工單
- 合格的其他使用者可以完成流程

## S-03 PostgreSQL / Redis 敏感資料策略

PostgreSQL：

1. 建立 masking rule，命中 PostgreSQL 查詢欄位。
2. 用 SQL Editor 查詢該欄位。
3. 建立 scheduled report 查詢該欄位。

驗證：

- SQL Editor 結果被遮罩，或在無法安全處理時 fail-closed
- Scheduled Report 不會明文外發敏感欄位
- audit log 可看到被阻擋或敏感策略相關事件

Redis：

1. 在 Masking Rules 的 Redis sensitive prefix 頁面新增 prefix。
2. 執行 `SCAN`，確認可看到 key name。
3. 執行 `TYPE`、`TTL`、`EXISTS`，確認允許。
4. 對命中 prefix 的 key 執行讀 value 類命令，例如 `GET`。

驗證：

- key discovery 與 metadata 命令可用
- value/content 查詢被拒絕
- query history 與 audit log 都能看到 Redis 查詢或阻擋紀錄

## S-04 DB Connection endpoint / host policy

1. 修改既有 DB Connection 的 readonly/readwrite host 或 port，但不提供對應 password。
2. 預期 API 拒絕。
3. 配置 host policy 為 `warn`，使用非 allowlist endpoint 建立連線。
4. 配置 host policy 為 `enforce`，重試相同操作。

驗證：

- endpoint 變更時必須重新輸入對應 credential
- `warn` 只記錄 violation
- `enforce` 會拒絕建立、修改或 runtime 連線
- 前端錯誤不洩漏 DB password 或過度詳細連線資訊

## S-05 MFA 限速與一次性 challenge

1. 對啟用 MFA 的帳號輸入正確密碼，取得 MFA challenge。
2. 多次輸入錯誤 TOTP。
3. 使用已成功驗證過的 challenge token 再次提交。

驗證：

- 多次錯誤會觸發限速或 challenge revoke
- challenge 成功後不可重複使用
- audit log 有 MFA failure 記錄，但不包含 TOTP code 或 secret

## S-06 Export download token

1. 建立 export request 並取得 download link。
2. 未登入狀態嘗試下載。
3. 用非 requester 且無 review 權限的使用者嘗試下載。
4. 用 requester 下載一次。
5. 重複使用同一 token 下載第二次。
6. 查看 access log。

驗證：

- 未登入不可下載
- 非 requester / 非 reviewer 不可下載
- 第一次下載成功
- 第二次下載被拒絕
- access log 不包含完整 download token

## S-07 users.write 自我提權

1. 用只有 `users.write` 但不是 all-permissions 的測試帳號登入。
2. 嘗試把自己加入 admin group。
3. 嘗試把其他人加入 admin group。
4. 嘗試修改 protected auth group。
5. 嘗試授予自己沒有的 permission 或 DB scope。

驗證：

- 以上高危操作都被拒絕
- all-permissions admin 仍可執行合法管理操作
- audit log 有拒絕原因

## C-01 Readonly Host

這不是單一代碼漏洞，而是 DB 端配置要求。

驗證：

1. DBA 檢查 DB Connection readonly credential 的 grants。
2. 確認 MySQL readonly 帳號沒有 DML、DDL、FILE、SUPER、UDF/plugin 管理等權限。
3. 確認 PostgreSQL readonly 帳號不是 superuser，沒有 server file、dangerous extension、sequence write 等權限。
4. 在平台上用 SQL Editor 執行只讀查詢。
5. 嘗試透過 SQL Editor 執行變更語句，確認被拒絕。

## 相關文件

- [安全邊界說明](../explanation/security-boundaries.md)
- [部署到 AWS EKS](deploy-to-aws-eks.md)
- [排查線上與部署問題](troubleshoot-operations.md)
- [Users / RBAC](../reference/users-and-rbac.md)
- [Masking 與 DSL](../reference/masking-and-dsl.md)

