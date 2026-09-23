# DBRE Maestro

DBRE Maestro 是資料庫治理工作台，集中管理唯讀 SQL 查詢、DDL／DML／Redis 工單、審批、權限、敏感資料遮罩、資料庫連線、Metadata、通知與稽核。

- `backend/`：Go API、排程工作、Meta DB 與外部資料源整合
- `frontend/`：React、Vite、TypeScript 管理介面
- `docs/`：操作、架構、設定與功能參考

## 快速啟動

需要 Docker、Docker Compose 與 `make`。

1. 建立本機環境檔：

   ```bash
   cp .env.example .env
   ```

2. 修改 `.env` 內的 MySQL 密碼，並依下一節產生 `DBRE_ENCRYPTION_KEY` 與 `JWT_SECRET`。

3. 啟動完整開發環境：

   ```bash
   make dev
   ```

   這會啟動 MySQL、Go backend 與 Vite frontend。

4. 開啟：

   - 前端：`http://localhost:5173`
   - Backend health check：`http://localhost:8080/api/health`

第一次啟動時，前往 `/setup` 建立初始管理員。完整操作步驟見[本機開發教學](docs/tutorials/getting-started-local-dev.md)。

## 本機 Secret

`.env` 至少需要：

```dotenv
MYSQL_APP_PASSWORD=<本機 app user 密碼>
MYSQL_ROOT_PASSWORD=<本機 root 密碼>
DBRE_ENCRYPTION_KEY=<base64 編碼的 32-byte key>
JWT_SECRET=<高熵隨機字串>
```

### DBRE_ENCRYPTION_KEY

執行：

```bash
make gen-key
```

將輸出填入 `.env` 的 `DBRE_ENCRYPTION_KEY`。這個 key 用於加密資料庫憑證、MFA secret、rollback SQL、Lark／OIDC secret，並衍生 masking hash pepper。

此值一旦用來加密資料就必須保持不變。遺失或直接更換會讓既有密文無法解密；正式環境如需輪替，必須先設計資料重加密流程。

### JWT_SECRET

另外產生一組獨立值：

```bash
openssl rand -base64 48
```

將輸出填入 `.env` 的 `JWT_SECRET`。它用於 JWT 與其他簽章 token，不應和 `DBRE_ENCRYPTION_KEY` 共用。更換後，既有簽章 token 會失效。

如果沒有 OpenSSL，也可以再執行一次 `make gen-key`，將第二次產生的獨立值用作 `JWT_SECRET`。

`.env` 已被 Git ignore，只供本機使用。不要把任何真實 secret 提交到 Git。

## AWS Profile

DB Metadata Inventory 在本機可以沿用主機上的 AWS CLI profile。`docker-compose.yml` 會把 `${HOME}/.aws` 以唯讀方式掛載到 app container，並啟用 AWS shared config。

先確認 profile 已存在且可用：

```bash
aws sts get-caller-identity --profile your-profile
```

需要 SSO 時，先登入：

```bash
aws sso login --profile your-profile
```

長期使用同一個 profile 時，直接寫進根目錄 `.env`：

```dotenv
AWS_PROFILE=your-profile
```

單次覆寫則使用 shell environment；shell 值的優先序高於 `.env`：

```bash
AWS_PROFILE=your-profile make dev
```

未設定時預設使用 `default`。不要把 `AWS_ACCESS_KEY_ID` 或 `AWS_SECRET_ACCESS_KEY` 寫進 `.env`；本機使用 named profile，EKS 使用 IRSA。

## 常用指令

| 指令 | 用途 |
|---|---|
| `make dev` | 以 Docker Compose 啟動完整本機環境 |
| `make dev-backend` | 只啟動 MySQL 與 backend |
| `make dev-frontend` | 在主機啟動 Vite，API proxy 到 `localhost:8080` |
| `make db-only` | 只啟動 MySQL |
| `make test` | 執行完整 Go tests |
| `make test-frontend` | 執行完整前端 tests |
| `make lint` | 執行 Go lint；目前尚未包含前端 ESLint |
| `make build` | 只編譯本機 backend binary，不建立部署 image |
| `docker compose down` | 停止本機 containers；保留 MySQL volume |

直接在主機執行 backend test／build 需要 Go 1.25；`make lint` 另外需要 `golangci-lint`。執行 frontend 指令前先安裝 Node.js 20 dependencies：

```bash
cd frontend
npm ci
```

接著可執行前端 lint 與 production build：

```bash
npm run lint
npm run build
```

## 建立部署 Image

測試與 production 使用根目錄的 multi-stage `Dockerfile`，不是 `make build`。從 repository 根目錄執行：

```bash
docker build -t dbre-maestro:local .
```

產出的單一 application image 包含：

- React production assets
- Go server
- Database migrations
- `my2sql`

Image 不包含環境專屬 secret。啟動 container 時必須由部署平台注入 Meta DB 連線與必要設定；EKS 建議使用 AWS Secrets Manager 與 IRSA。Kubernetes／ArgoCD manifests 維護在外部 GitOps repositories，不在本 repository。

詳細流程：

- [建立可部署的 Application Image](docs/how-to/build-application-image.md)
- [部署到 AWS EKS](docs/how-to/deploy-to-aws-eks.md)
- [設定與環境變數](docs/reference/configuration.md)

## 主要功能

- `Tickets`：DDL、DML、Redis、Query Access、SQL Export 與 Sensitive Access 工單
- `SQL Editor`：唯讀查詢、Explain、格式化、查詢取消與匯出／權限申請
- `Scheduled Reports`：定期執行唯讀 SQL，產生 CSV 並透過 Lark 推送
- `DB Connections`：MySQL、PostgreSQL、Redis 連線與讀寫憑證角色
- `DB Metadata`：AWS Inventory 與資料庫物件快照
- `Masking Rules`：欄位遮罩規則與 Unmask Whitelist
- `SQL Review Rules`：依資料庫類型套用 SQL 審核規則
- `Users / RBAC`：使用者、Auth Groups、Permissions、Resources 與 DB Scope
- `Settings`：Workflow、查詢執行、整合與 Metadata 掃描設定

## 文件入口

- [文件總覽](docs/README.md)
- [專案目前狀態](docs/PROJECT_STATUS.md)
- [本機開發教學](docs/tutorials/getting-started-local-dev.md)
- [專案導覽](docs/explanation/project-map.md)
- [架構總覽](docs/explanation/architecture-overview.md)
- [設定與環境變數](docs/reference/configuration.md)
- [安全邊界](docs/explanation/security-boundaries.md)
- [RD 使用手冊](docs/how-to/rd-user-guide.md)
- [DBA／Admin 管理手冊](docs/how-to/dba-admin-user-guide.md)
