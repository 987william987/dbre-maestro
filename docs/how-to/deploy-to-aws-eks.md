# How to 部署到 AWS EKS

本文描述 DBRE Maestro 在測試環境與正式環境部署到 AWS EKS 的建議流程。這是部署 runbook，不是 Kubernetes manifest 規格；目前 repo 尚未提供 Helm chart 或 Kustomize overlay。

## 部署目標

建議至少分成兩套環境：

| 環境 | 用途 | 建議隔離方式 |
|---|---|---|
| Test / Staging | 驗證 migration、功能與整合設定 | 獨立 AWS account 或獨立 EKS cluster |
| Production | 正式使用者流量 | 獨立 AWS account 與 EKS cluster |

正式環境不建議只用 namespace 隔離，因為資料庫、IAM、Secrets、Ingress、告警與權限邊界都應與測試環境切開。

## 目前 repo 狀態

| 項目 | 狀態 |
|---|---|
| Backend Docker image | 可用 `backend/Dockerfile` 建置 |
| Frontend Docker image | 目前是 Vite dev server image，不適合 production |
| Kubernetes manifests | 尚未提供 |
| Helm chart / Kustomize | 尚未提供 |
| Migration command | `/app/maestro -migrate-only` |
| Health check | `GET /api/health` |

正式部署 frontend 前，需先補其中一種方案：

- 建立 production frontend image，以 Nginx 或其他靜態 server 提供 Vite build output
- 或將 `frontend` build output 部署到 S3 + CloudFront，API 指向 EKS backend

在完成上述其中一種方案前，不應把現有 `frontend/Dockerfile` 直接視為正式環境部署方式。

## AWS 基礎設施

每個環境建議具備：

- EKS cluster，使用仍在 EKS standard support 的 Kubernetes 版本
- Managed Node Group 或 Fargate profile
- Amazon RDS MySQL 作為 Maestro Meta DB
- Amazon ECR 儲存 backend / frontend image
- AWS Load Balancer Controller，提供 ALB Ingress
- ACM certificate，提供 HTTPS
- Route 53 DNS record
- Secrets Manager 或 External Secrets Operator 管理 secret
- IRSA，讓 Pod 以 Kubernetes service account 綁定最小 IAM 權限
- CloudWatch Logs / Container Insights 或其他集中日誌方案

若需要 DB Metadata Inventory 掃描 AWS 資源，backend Pod 不應掛載人工 AWS profile；應使用 IRSA 授權。

## 必要環境變數

Backend Pod 至少需要：

| 變數 | 說明 |
|---|---|
| `APP_ENV` | `staging` 或 `production` |
| `PORT` | 預設 `8080` |
| `APP_BASE_URL` | 前端公開 URL，用於站內信與 Lark 工單連結，例如 `https://dbre-maestro-test.tskyrocket.xyz` |
| `AWS_SM_ENABLE` | EKS/devops 環境建議 `true`，由 app 從 AWS Secrets Manager 讀敏感值 |
| `AWS_SM_REGION` | AWS Secrets Manager region，例如 `ap-northeast-1` |
| `AWS_SM_SECRET_ID` | DBRE Maestro app secret id |
| `DB_DSN` | app user 連線 Meta DB；`AWS_SM_ENABLE=true` 時由 secret payload 提供 |
| `MIGRATION_DSN` | migration 專用連線，權限通常高於 app user；`AWS_SM_ENABLE=true` 時建議由 secret payload 提供 |
| `DBRE_ENCRYPTION_KEY` | base64 32-byte key；`AWS_SM_ENABLE=true` 時由 secret payload 提供 |
| `JWT_SECRET` | JWT 簽章 secret；`AWS_SM_ENABLE=true` 時由 secret payload 提供 |
| `RUN_MIGRATIONS_ON_STARTUP` | 是否在 Deployment Pod 啟動時執行 migration；預設 `true` |
| `MFA_ENFORCEMENT` | production 建議 `required_for_admins` |
| `REFRESH_COOKIE_SECURE` | production 必須等同 `true`；程式會強制 |
| `DB_CONNECTION_HOST_POLICY_ENFORCEMENT` | DB/Redis endpoint host policy；建議先用 `warn`，確認後改 `enforce` |
| `DB_CONNECTION_HOST_ALLOWLIST` | 允許的 host pattern，例如 `*.rds.amazonaws.com,*.cache.amazonaws.com,*.edgex.internal` |
| `DB_CONNECTION_CIDR_ALLOWLIST` | 允許的 DB / Redis subnet CIDR，需由 SRE 依環境提供 |
| `DB_CONNECTION_CIDR_DENYLIST` | 禁止連線 CIDR，至少擋 metadata / loopback，例如 `127.0.0.0/8,169.254.0.0/16,::1/128` |

Lark App ID / Secret 建議透過平台 Settings 管理，不建議寫死在 image。

`APP_BASE_URL` 不是 secret，應放在 ArgoCD values / `deploy.envs`。如果未配置，通知內容中的工單連結會退回相對路徑，例如 `/tickets/TK-...`，站內信與 Lark 都不會帶域名。修改 `APP_BASE_URL` 後需要 rollout Pod；已經產生的舊通知內容不會自動回填。

## Secret 管理

不可把以下值寫入 Git：

- `DB_DSN`
- `MIGRATION_DSN`
- `DBRE_ENCRYPTION_KEY`
- `JWT_SECRET`
- RDS 密碼
- Lark App Secret

devops EKS 標準做法是 app 透過 IRSA 直接讀 AWS Secrets Manager。Deployment 需要注入：

```text
AWS_SM_ENABLE=true
AWS_SM_REGION=ap-northeast-1
AWS_SM_SECRET_ID=<secret-id>
```

Secrets Manager payload 至少需要：

```json
{
  "DB_DSN": "maestro_app:<password>@tcp(<host>:3306)/maestro?parseTime=true&charset=utf8mb4&loc=UTC",
  "MIGRATION_DSN": "root:<password>@tcp(<host>:3306)/maestro?parseTime=true&charset=utf8mb4&loc=UTC",
  "DBRE_ENCRYPTION_KEY": "BASE64_32_BYTE_KEY",
  "JWT_SECRET": "long-random-string"
}
```

產生 `DBRE_ENCRYPTION_KEY`：

```bash
openssl rand -base64 32
```

`DBRE_ENCRYPTION_KEY` 必須是 base64 編碼後可解出 32 bytes 的值。也可以在本 repo 用 `make gen-key` 產生同等格式。

產生 `JWT_SECRET`：

```bash
openssl rand -base64 48
```

`JWT_SECRET` 不要求固定格式，但必須是高熵隨機字串。

`MIGRATION_DSN` 可省略；未提供時程式會 fallback 到 `DB_DSN`。正式環境仍建議提供獨立 migration DSN，避免 app user 擁有 schema migration 所需的高權限。

DB pool 參數不是 secret，應繼續由 env / ConfigMap / values 管理，不放進 Secrets Manager。

IRSA policy 至少需要允許該 secret：

```text
secretsmanager:GetSecretValue
```

一般化 secret 管理流程：

1. 在 AWS Secrets Manager 建立每個環境獨立 secret
2. 透過 IRSA 讓 Pod service account 讀取該 secret
3. Deployment 注入 `AWS_SM_ENABLE`、`AWS_SM_REGION`、`AWS_SM_SECRET_ID`
4. production 與 staging 使用不同 secret 值

`DBRE_ENCRYPTION_KEY` 一旦用於加密既有資料，不可隨意更換；更換前需要設計資料重加密流程。

## ArgoCD deploy.envs 範例

devops pipeline 會更新 image tag，但通常不會自動新增 runtime env。每個環境的 `deploy.envs` 需要在 ArgoCD values 裡配置。

sre-test 初期最小建議：

```yaml
deploy:
  envs:
    AWS_SM_ENABLE: "true"
    AWS_SM_REGION: "ap-northeast-1"
    AWS_SM_SECRET_ID: "/testnet/dbre-maestro/default"
    APP_ENV: "sre-test"
    APP_BASE_URL: "https://dbre-maestro-test.tskyrocket.xyz"
    MFA_ENFORCEMENT: "disabled"
    RUN_MIGRATIONS_ON_STARTUP: "true"
    DB_CONNECTION_HOST_POLICY_ENFORCEMENT: "warn"
    DB_CONNECTION_HOST_ALLOWLIST: "*.rds.amazonaws.com,*.cache.amazonaws.com,*.edgex.internal"
    DB_CONNECTION_CIDR_ALLOWLIST: "10.183.0.0/16,10.222.38.0/24"
    DB_CONNECTION_CIDR_DENYLIST: "127.0.0.0/8,169.254.0.0/16,::1/128"
```

上述設定假設 test 仍使用單副本，並由 Deployment Pod 啟動時執行 migration。若 test 已建立 migration Job，建議改成：

```yaml
    RUN_MIGRATIONS_ON_STARTUP: "false"
```

production 建議：

```yaml
deploy:
  envs:
    AWS_SM_ENABLE: "true"
    AWS_SM_REGION: "ap-northeast-1"
    AWS_SM_SECRET_ID: "/prod/dbre-maestro/default"
    APP_ENV: "production"
    APP_BASE_URL: "https://dbre-maestro.<prod-domain>"
    MFA_ENFORCEMENT: "required_for_admins"
    REFRESH_COOKIE_SECURE: "true"
    RUN_MIGRATIONS_ON_STARTUP: "false"
    DB_CONNECTION_HOST_POLICY_ENFORCEMENT: "enforce"
    DB_CONNECTION_HOST_ALLOWLIST: "*.rds.amazonaws.com,*.cache.amazonaws.com,*.edgex.internal"
    DB_CONNECTION_CIDR_ALLOWLIST: "<prod-db-and-redis-subnet-cidrs>"
    DB_CONNECTION_CIDR_DENYLIST: "127.0.0.0/8,169.254.0.0/16,::1/128"
```

Host policy 建議先在 test 使用 `warn` 模式觀察 backend log 與 audit log，確認既有 DB / Redis endpoint 沒有誤傷後再於 production 使用 `enforce`。這是第一階段連線前檢查，會檢查 DB Connection 新增 / 修改，以及 SQL Editor、metadata、export、scheduled report、ticket execute、metadata sync 等 runtime 連線前的 resolved endpoint；目前尚未接管 driver custom dialer。

production 必須先由 migration Job 執行：

```bash
/app/maestro -migrate-only
```

DB pool 參數已有保守預設，通常不需要一開始配置。若需要調整連線數，可在同一個 `deploy.envs` 補 `DB_POOL_*`；修改後需要 rollout Pod 才會生效。

## Image 建置與推送

Backend image：

```bash
docker build -t <account_id>.dkr.ecr.<region>.amazonaws.com/dbre-maestro-backend:<git_sha> backend
docker push <account_id>.dkr.ecr.<region>.amazonaws.com/dbre-maestro-backend:<git_sha>
```

Frontend 若改成 production image，應使用 Vite build output，不應使用 dev server：

```bash
cd frontend
npm ci
npm run build
```

## Migration 流程

backend 支援兩種 migration 執行方式：

```text
RUN_MIGRATIONS_ON_STARTUP=true
```

Pod 啟動時先用 `MIGRATION_DSN` 執行 migration，再啟動 server。這是預設值，適合本機開發與單副本測試環境。

```bash
/app/maestro -migrate-only
```

只執行 migration，成功後退出。這適合 Kubernetes Job 或一次性維運命令。

### 模式 A：單副本簡化部署

適用於：

- sre-test 初期
- production 初期希望先降低流程複雜度
- 可接受單副本短暫不可用
- 尚未建立 migration Job 流程

配置：

```yaml
replicaCount: 1

deploy:
  envs:
    RUN_MIGRATIONS_ON_STARTUP: "true"
```

流程：

1. 建立或更新 AWS Secrets Manager secret
2. 確認 `MIGRATION_DSN` 可用且權限足夠
3. rollout 單副本 Deployment
4. Pod 啟動時自動執行 migration
5. migration 成功後 server 啟動
6. 檢查 `/api/health`
7. 檢查 `schema_migrations`

正常重啟時，程式仍會進入 migration check；若 `schema_migrations` 已是最新且 `dirty=false`，`golang-migrate` 會回報 no change，不會重跑所有 SQL。若新 image 帶了新的 migration，Pod 啟動時會自動套用尚未執行的 migration。

限制：

- 單副本沒有 HA，node drain 或 Pod 重啟會有短暫不可用
- Deployment Pod 長期持有 `MIGRATION_DSN`
- 未來擴多副本前必須關閉 startup migration
- 若新版本 migration 失敗，Pod 會啟動失敗，需要先處理 migration dirty state

### 模式 B：Job 標準部署

適用於：

- production 穩定流程
- 多副本 Deployment
- 需要避免多 Pod 同時執行 migration
- 需要將 migration 與 app rollout 分開審核

流程：

1. 先建立或更新 ConfigMap / Secret
2. 用同一個 backend image 啟動 Kubernetes Job 執行 `/app/maestro -migrate-only`
3. backend Deployment 設定 `RUN_MIGRATIONS_ON_STARTUP=false`
4. migration 成功後再 rollout backend Deployment
5. rollout 完成後檢查 `/api/health`

Job command：

```bash
/app/maestro -migrate-only
```

Deployment 配置：

```yaml
deploy:
  envs:
    RUN_MIGRATIONS_ON_STARTUP: "false"
```

Job 成功後再 rollout Deployment。若 Job 失敗，不應 rollout app，應先查看 Job logs 並處理 DB、secret、權限或 dirty state 問題。

### 從單副本切換到多副本

可以先用單副本簡化部署完成第一次初始化，再切到多副本：

1. 初次部署：

```text
replicaCount=1
RUN_MIGRATIONS_ON_STARTUP=true
```

2. 確認 migration 完成：

```sql
SELECT version, dirty FROM schema_migrations;
```

`dirty` 必須為 `false`。

3. 切換 Deployment：

```text
replicaCount=2
RUN_MIGRATIONS_ON_STARTUP=false
```

4. ArgoCD sync / rollout。

之後每次新版本包含 migration 時，應先用 migration Job 跑 `/app/maestro -migrate-only`，再 rollout 多副本 Deployment。

不要只把 `replicaCount` 從 1 改成 2+ 而保留 `RUN_MIGRATIONS_ON_STARTUP=true`。即使多數情況會 no-op，遇到新 migration 或 rolling update 時仍有多 Pod 同時搶 migration 的風險。

### 不建議人工貼 SQL

不建議把 `backend/migrations/*.up.sql` 手工貼到 MySQL 當常規流程。原因：

- `golang-migrate` 會維護 `schema_migrations` 的 version / dirty state
- 手工執行容易漏跑、順序錯或失敗後狀態不一致
- 部分 migration 不是完全 idempotent
- 本專案 migration 包含 `GRANT` / `REVOKE`，仍需要 migration admin 權限

若 emergency 情境必須由 DBA 手工處理，需同步維護 `schema_migrations`，並確認最新 version 與 `dirty=false`。常規流程應使用 app image 的 `/app/maestro -migrate-only` 或單副本 startup migration。

## Backend Deployment 要點

Backend Deployment 應包含：

- `readinessProbe`: `GET /api/health`
- `livenessProbe`: `GET /api/health`
- resource requests / limits
- service account 綁定 IRSA
- envFrom Secret / ConfigMap
- rolling update strategy
- PodDisruptionBudget

Service 使用 ClusterIP，Ingress 由 ALB 對外暴露。

## Ingress 與 HTTPS

建議用 AWS Load Balancer Controller 管理 ALB Ingress：

- ACM certificate 綁定正式網域
- HTTP 自動 redirect HTTPS
- backend target 指向 backend Service
- 若 frontend 在 EKS 內，前端與 API 可由同一 ALB 依 path 或 host routing
- 若 frontend 在 CloudFront，ALB 只暴露 API

正式環境必須使用 HTTPS，否則 refresh cookie 的 Secure 行為會導致瀏覽器不接受 cookie。

## Test 部署流程

1. 建立或更新 test EKS / RDS / ECR / Secrets
2. 建置 backend image 並推送到 test ECR
3. 部署或更新 ConfigMap / Secret
4. 執行 migration Job
5. rollout backend Deployment
6. 部署 frontend production artifact
7. 驗證 `/api/health`
8. 驗證登入、MFA 設定、SQL Editor、Tickets、Scheduled SQL Reports、Lark 通知
9. 檢查 audit log 與 backend logs

Test 環境可設定：

```text
APP_ENV=staging
APP_BASE_URL=https://dbre-maestro-test.tskyrocket.xyz
MFA_ENFORCEMENT=disabled
```

若 test 也需要演練高權限登入安全，可暫時設為：

```text
MFA_ENFORCEMENT=required_for_admins
```

## Production 部署流程

1. 確認 test 環境已用同一 image tag 驗證通過
2. 確認 RDS snapshot / backup 可用
3. 確認 production Secrets 已更新且未誤用 test secret
4. 建置或 promote 已驗證 image 到 production ECR
5. 執行 production migration Job
6. rollout backend Deployment
7. 部署 frontend production artifact
8. 檢查 ALB target health 與 `/api/health`
9. 驗證登入、refresh、MFA、Tickets、SQL Editor、Lark 通知
10. 觀察 error logs、5xx、latency 與 DB connection 數

Production 建議設定：

```text
APP_ENV=production
APP_BASE_URL=https://dbre-maestro.<prod-domain>
MFA_ENFORCEMENT=required_for_admins
```

## Rollback

Rollback 需要分成 image rollback 與 database rollback。

建議原則：

- 優先使用向前修復，避免直接 rollback DB schema
- migration 前先建立 RDS snapshot
- 若 migration 已改變資料結構，回滾 image 前要確認舊版本是否相容新 schema
- 若需要 DB rollback，應先停止 backend Deployment，還原 RDS snapshot，再部署對應 image

目前 migrations 並非全部保證可無痛 down migration；production rollback plan 不應只依賴 `down.sql`。

## 驗收清單

部署後至少檢查：

- `/api/health` 回 200
- login / refresh / logout 正常
- production refresh cookie 有 Secure
- admin MFA 行為符合 `MFA_ENFORCEMENT`
- Tickets list / detail / action API 授權正常
- 站內信與 Lark 的工單連結帶完整 `APP_BASE_URL` 域名
- SQL Editor query access 正常
- Scheduled SQL Reports 可建立且 run history 正常
- Lark App 通知可定向送達
- Audit logs 有登入失敗、工單、報表與設定變更紀錄

## 後續建議補齊

目前部署文件可以指導手動或 CI/CD 落地，但 repo 還缺：

- production frontend Dockerfile 或 S3/CloudFront 部署文件
- Helm chart 或 Kustomize overlays
- migration Job manifest
- staging / production values 範本
- startup migration 開關，避免多 Pod 同時 migration
- CI/CD pipeline 文件

## 相關文件

- [設定與環境變數](../reference/configuration.md)
- [登入安全與 Session](../reference/auth-and-sessions.md)
- [Scheduled SQL Reports](../reference/scheduled-sql-reports.md)
- [本機開發教學](../tutorials/getting-started-local-dev.md)
