# DBRE Maestro

DBRE Maestro 是資料庫治理工作台，集中管理唯讀 SQL 查詢、DDL／DML／Redis 工單、審批、RBAC、敏感資料遮罩、資料庫連線、Metadata、通知與稽核。

## 本機啟動

需要 Docker、Docker Compose 與 `make`。

```bash
cp .env.example .env
```

修改 `.env` 內的 MySQL 密碼，並產生兩組不同的 Secret：

```bash
# DBRE_ENCRYPTION_KEY：base64 編碼的 32-byte key
make gen-key

# JWT_SECRET：獨立的高熵隨機字串
openssl rand -base64 48
```

`DBRE_ENCRYPTION_KEY` 用於加密資料庫憑證與其他敏感資料；資料寫入後不可任意更換。完整用途與輪替限制見[設定與環境變數](docs/reference/configuration.md)。

啟動完整本機環境：

```bash
make dev
```

- 前端：`http://localhost:5173`
- Backend health check：`http://localhost:8080/api/health`
- 第一次啟動：前往 `/setup` 建立初始管理員

完整步驟見[本機開發教學](docs/tutorials/getting-started-local-dev.md)。

### 本機 AWS Profile

DB Metadata Inventory 可沿用主機上的 AWS CLI profile。在 `.env` 設定：

```dotenv
AWS_PROFILE=your-profile
```

使用 SSO 時先登入，再執行 `make dev`：

```bash
aws sso login --profile your-profile
```

Compose 會將 `${HOME}/.aws` 以唯讀方式掛載到 app container。若 Inventory Sync 出現 `InvalidClientTokenId`，或需要檢查 SSO 與 static credentials 衝突，請看[設定與環境變數](docs/reference/configuration.md#sso-profile-與-static-credentials-衝突)。

## 開發指令

| 指令 | 用途 |
|---|---|
| `make dev` | 啟動 MySQL、backend 與 Vite frontend |
| `make dev-backend` | 只啟動 MySQL 與 backend |
| `make dev-frontend` | 在主機啟動 Vite |
| `make db-only` | 只啟動 MySQL |
| `make test` | 執行 Go tests |
| `make test-frontend` | 執行前端 tests |
| `make lint` | 執行 Go lint |
| `docker compose down` | 停止本機 containers，保留資料 volume |

前端檢查：

```bash
cd frontend
npm ci
npm run lint
npm test
npm run build
```

## Testnet 與 Production

Testnet 與 production 的 CI/CD 都使用根目錄 multi-stage `Dockerfile` 建立單一 application image；Makefile 只作為本機開發入口，不是部署流程。

需要在本機驗證同一個 image 時，從 repository 根目錄執行：

```bash
docker build -t dbre-maestro:local .
```

Image 內包含 React production assets、Go server、database migrations 與 `my2sql`。Runtime Secret 不會寫入 image，而是由部署環境透過 Kubernetes、AWS Secrets Manager 與 IRSA 注入。

Kubernetes／ArgoCD manifests 維護在外部 GitOps repositories。本 repository 負責建立 application image；部署平台負責 image tag、runtime 設定、Secret、網路、migration 與 rollout。

- [Application image build](docs/how-to/build-application-image.md)
- [AWS EKS 部署流程](docs/how-to/deploy-to-aws-eks.md)

## 主要功能

- `Tickets`：DDL、DML、Redis、Query Access、SQL Export 與 Sensitive Access 工單
- `SQL Editor`：唯讀查詢、Explain、格式化、取消與權限申請
- `Scheduled Reports`：定期執行唯讀 SQL 並透過 Lark 推送
- `DB Connections / Metadata`：資料源連線、AWS Inventory 與物件快照
- `Masking / SQL Review`：敏感資料遮罩與 SQL 審核規則
- `Users / RBAC / Settings`：使用者權限、DB Scope、Workflow 與平台設定

## 文件

- [文件總覽](docs/README.md)
- [專案目前狀態](docs/PROJECT_STATUS.md)
- [專案導覽](docs/explanation/project-map.md)
- [架構總覽](docs/explanation/architecture-overview.md)
- [設定與環境變數](docs/reference/configuration.md)
- [安全邊界](docs/explanation/security-boundaries.md)
- [RD 使用手冊](docs/how-to/rd-user-guide.md)
- [DBA／Admin 管理手冊](docs/how-to/dba-admin-user-guide.md)
