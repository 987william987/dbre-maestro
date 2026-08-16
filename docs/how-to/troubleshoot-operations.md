# How to 排查線上與部署問題

本文整理 DBRE Maestro 在 EKS / ArgoCD / DB / Lark 整合時最常見的排查流程。

## Prerequisites

你需要：

- 目標環境的 ArgoCD 或 Kubernetes read 權限
- backend Pod log 權限
- AWS Secrets Manager secret id
- 目標 Meta DB 與外部 DB 的連線資訊
- 若排查 inventory，還需要確認 Pod 使用的 IAM role

## 1. Pod 啟動失敗

先看 backend log。常見錯誤：

```text
config error: DB_DSN is required
```

處理方式：

1. 確認 Deployment env 有：

   ```yaml
   AWS_SM_ENABLE: "true"
   AWS_SM_REGION: "ap-northeast-1"
   AWS_SM_SECRET_ID: "<secret-id>"
   ```

2. 確認 Secrets Manager payload 至少有：

   ```json
   {
     "DB_DSN": "<app-dsn>",
     "DBRE_ENCRYPTION_KEY": "<base64-32-byte-key>",
     "JWT_SECRET": "<random-secret>"
   }
   ```

3. 確認 Pod service account 的 IRSA policy 允許：

   ```text
   secretsmanager:GetSecretValue
   ```

## 2. Migration 失敗

常見錯誤：

```text
migration failed
dirty migration state at version <n>
```

處理方式：

1. 先停止 app 或把 Deployment replica 暫時降到 0，避免多個 Pod 同時重試。
2. 到 Meta DB 檢查 `schema_migrations`。
3. 確認失敗的 migration 是否部分執行。
4. 由 DBA 判斷是補 SQL、回復 partial change，還是修正 dirty state。
5. 再用單副本或 migration Job 重跑。

正式環境建議：

```yaml
RUN_MIGRATIONS_ON_STARTUP: "false"
```

並用 Job 執行：

```bash
/app/maestro -migrate-only
```

## 3. DB Connection 測試或查詢 timeout

排查順序：

1. 確認 app Pod 到 DB endpoint 的網路是否通。
2. 確認 DNS 解析結果是否符合預期 subnet。
3. 確認 security group / NACL / route table。
4. 確認 DB Connection host policy 是否阻擋。
5. 確認 DB 帳號權限與 database name。
6. 確認 SQL Editor timeout 設定。

相關設定：

- `sql_editor_app_timeout_seconds`
- `sql_editor_mysql_max_execution_time_ms`
- `sql_editor_postgres_statement_timeout_ms`
- `DB_CONNECTION_HOST_POLICY_ENFORCEMENT`
- `DB_CONNECTION_CIDR_ALLOWLIST`
- `DB_CONNECTION_CIDR_DENYLIST`

## 4. Host Policy 阻擋 DB / Redis endpoint

如果 log 或 audit 出現 host policy violation：

1. 確認 host 是否符合 `DB_CONNECTION_HOST_ALLOWLIST`。
2. 在 Pod 內解析 endpoint，確認 IP 是否落在 `DB_CONNECTION_CIDR_ALLOWLIST`。
3. 確認 IP 沒有命中 `DB_CONNECTION_CIDR_DENYLIST`。
4. test 環境可先用 `warn` 觀察。
5. production 若誤傷，優先修 allowlist/CIDR，不要直接關閉 policy。

範例：

```yaml
DB_CONNECTION_HOST_POLICY_ENFORCEMENT: "warn"
DB_CONNECTION_HOST_ALLOWLIST: "*.rds.amazonaws.com,*.cache.amazonaws.com,*.db.example.com"
DB_CONNECTION_CIDR_ALLOWLIST: "10.183.0.0/16,10.222.38.0/24"
DB_CONNECTION_CIDR_DENYLIST: "127.0.0.0/8,169.254.0.0/16,::1/128"
```

## 5. Metadata inventory 沒資料

先看 backend log：

```text
db metadata inventory: run failed
AccessDenied: ... rds:DescribeDBClusters
```

處理方式：

1. 確認 Settings 裡 `db_metadata_inventory_enabled=true`。
2. 確認 scan region 包含目標 region。
3. 確認 Pod IAM role 有必要 AWS API 權限，例如 RDS / ElastiCache describe。
4. 確認 CloudWatch / app log 不是只顯示上一輪錯誤。

若 object scan 某 connection 沒資料：

1. 看 log 的 `connection_id` 與 `connection_name`。
2. 確認 DB Connection readonly credential 有 metadata read 權限。
3. PostgreSQL 常見錯誤是 schema permission denied。
4. MySQL 寫入 snapshot 若 deadlock，確認是否有多輪 job 重疊或同輪寫入競爭。

## 6. Lark 或站內信連結缺域名

通知連結依賴：

```yaml
APP_BASE_URL: "https://maestro.example.com"
```

如果未設定，通知可能只出現相對路徑，例如 `/tickets/TK-...`。

處理方式：

1. 在 ArgoCD values / `deploy.envs` 加上 `APP_BASE_URL`。
2. rollout Pod。
3. 新產生的通知會帶完整 domain。

已產生的舊通知不會自動回填。

## 7. Export 下載失敗

檢查：

- 使用者是否登入
- 使用者是否為 export requester，或具備 `sql_editor.export_review`
- download token 是否已經使用過
- export request 是否 ready
- token 是否過期

Export token 是一次性下載。第二次下載應被拒絕。

## 8. SQL Editor 403

常見原因：

- 使用者沒有 `sql_editor.query`
- 使用者沒有該 DB Connection scope
- query access policy 未覆蓋目標 database/table
- SQL 命中 masking sensitive policy
- Redis 命中 sensitive key prefix value 查詢
- WAF 在 Ingress 前攔截 SQL-looking request

排查順序：

1. 看 Network response body 的 `error`。
2. 查 audit log 是否有 blocked query。
3. 查使用者 effective permission。
4. 查使用者 DB scope。
5. 查 query access rule。
6. 若 response 是 HTML 攔截頁，先找 SRE 查 WAF event id。

## 9. MFA 問題

使用者 MFA 綁定遺失時，正常流程是由另一位 all-permissions admin reset MFA。

若所有 admin 都無法登入，可使用 break-glass：

```bash
make reset-mfa USERNAME=<username>
```

或在容器內執行：

```bash
/app/maestro -reset-mfa-username <username>
```

執行後該使用者所有 session 會被撤銷，下一次登入會重新進入 MFA setup。

## Verification

排查完成後至少確認：

- Pod 不再重啟
- `/api/health` 回 200
- ArgoCD application healthy/synced
- backend log 沒有重複出現同一錯誤
- audit log 可看到關鍵阻擋或成功事件
- 使用者能重跑原本失敗的操作

