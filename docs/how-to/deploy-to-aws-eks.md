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
| Migration command | `./maestro -migrate-only` |
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
| `APP_BASE_URL` | 前端公開 URL，用於通知內連結 |
| `AWS_SM_ENABLE` | EKS/devops 環境建議 `true`，由 app 從 AWS Secrets Manager 讀敏感值 |
| `AWS_SM_REGION` | AWS Secrets Manager region，例如 `ap-northeast-1` |
| `AWS_SM_SECRET_ID` | DBRE Maestro app secret id |
| `DB_DSN` | app user 連線 Meta DB；`AWS_SM_ENABLE=true` 時由 secret payload 提供 |
| `MIGRATION_DSN` | migration 專用連線，權限通常高於 app user；`AWS_SM_ENABLE=true` 時建議由 secret payload 提供 |
| `DBRE_ENCRYPTION_KEY` | base64 32-byte key；`AWS_SM_ENABLE=true` 時由 secret payload 提供 |
| `JWT_SECRET` | JWT 簽章 secret；`AWS_SM_ENABLE=true` 時由 secret payload 提供 |
| `MFA_ENFORCEMENT` | production 建議 `required_for_admins` |
| `REFRESH_COOKIE_SECURE` | production 必須等同 `true`；程式會強制 |

Lark App ID / Secret 建議透過平台 Settings 管理，不建議寫死在 image。

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

目前 backend 啟動時會自動執行 migrations，且也支援：

```bash
./maestro -migrate-only
```

EKS 建議流程：

1. 先建立或更新 ConfigMap / Secret
2. 用同一個 backend image 啟動 Kubernetes Job 執行 `./maestro -migrate-only`
3. migration 成功後再 rollout backend Deployment
4. rollout 完成後檢查 `/api/health`

注意：目前 app 啟動仍會執行 migration。若 backend replicas 大於 1，rolling update 期間可能有多個 Pod 同時嘗試 migration。正式環境建議後續補一個開關，讓 Deployment 可關閉 startup migration，只由 migration Job 負責。

在該開關完成前，正式 rollout 應保守處理：

- migration Job 先跑完
- backend Deployment 初期使用 `replicas: 1`
- 確認 migration 無 dirty state
- 再擴 replicas

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
