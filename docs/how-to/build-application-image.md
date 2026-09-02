# How to 建立可部署的 Application Image

本文說明如何從 DBRE Maestro 原始碼建立完整的 OCI container image。產出的 image 是應用程式部署單位，可交由 Kubernetes、EKS、ECS、EC2 或其他 container runtime 執行；各平台的網路、Secret、流量入口與生命週期管理不在本文範圍內。

## Image 內容

專案根目錄的 `Dockerfile` 使用 multi-stage build，依序：

1. 執行 `npm ci` 與 `npm run build`，產生 React 靜態檔案。
2. 編譯 Linux 版 Go server。
3. 從固定 commit 編譯 `my2sql`。
4. 將 server、migrations、`my2sql` 與前端靜態檔案放入 Alpine runtime image。

最終 image 的進入點是 `/app/maestro`。Go server 預設監聽 `8080`，並從 `/app/public` 提供 React 頁面，因此不需要另外部署前端 container。

`backend/Dockerfile` 與 `frontend/Dockerfile` 是開發或個別元件建置用途，不是完整的 application image。

## 前置條件

- Docker，且支援 BuildKit；跨 CPU 架構建置需要 `docker buildx`
- 能連線到 npm registry、Go module source、GitHub 與 Alpine package repository
- 從專案根目錄執行命令

建立 image 不需要 Meta DB 或 runtime Secret；這些資源只在啟動 application container 時使用。

## 建立本機架構的 image

```bash
docker build -t dbre-maestro:local .
```

Docker 會依執行建置的主機架構產生 image，例如 Apple Silicon 通常是 `linux/arm64`，Intel/AMD 主機通常是 `linux/amd64`。

## 建立指定架構的 image

部署節點的 CPU 架構必須與 image 相容。建立可載入本機 Docker 的單一架構 image：

```bash
docker buildx build \
  --platform linux/amd64 \
  --tag dbre-maestro:local-amd64 \
  --load \
  .
```

ARM64 節點則把 platform 改為 `linux/arm64`：

```bash
docker buildx build \
  --platform linux/arm64 \
  --tag dbre-maestro:local-arm64 \
  --load \
  .
```

若要直接交付到 container registry，可將 `--load` 改為 `--push`，並使用 registry 的完整 image tag：

```bash
docker buildx build \
  --platform linux/arm64 \
  --tag <registry>/<repository>:<tag> \
  --push \
  .
```

Registry 登入、命名規則與 image 推送權限由實際部署環境負責。

## 驗證 image

確認 image 已建立，並檢查作業系統與 CPU 架構：

```bash
docker image inspect \
  --format '{{.RepoTags}} {{.Os}}/{{.Architecture}}' \
  dbre-maestro:local
```

不連接 Meta DB 也能檢查必要產物是否存在：

```bash
docker run --rm \
  --entrypoint sh \
  dbre-maestro:local \
  -c 'test -x /app/maestro && test -x /usr/local/bin/my2sql && test -d /app/migrations && test -d /app/public'
```

命令以 exit code `0` 結束，代表 image 內包含 server、`my2sql`、migrations 與前端靜態檔案。這只驗證 image 結構；應用程式健康檢查仍需啟動 container 並連上 Meta DB。

## Runtime 外部依賴

Image 不包含資料庫憑證、環境專屬設定或 MySQL server。部署平台啟動 container 前，必須準備下列資源。

### MySQL Meta DB

DBRE Maestro 使用 MySQL Meta DB 保存工單、使用者、權限、連線設定與平台設定。Runtime 必須取得有效的 `DB_DSN`，而且 container 到 MySQL 的 DNS、route、security group 或 firewall 必須允許連線。

### 環境變數或 AWS Secrets Manager

敏感設定可以直接注入 process environment，也可以設定以下變數，讓 application 在啟動時讀取 AWS Secrets Manager：

```text
AWS_SM_ENABLE=true
AWS_SM_REGION=<region>
AWS_SM_SECRET_ID=<secret-id>
```

不使用 AWS Secrets Manager 時，至少要由部署平台安全地注入：

- `DB_DSN`
- `DBRE_ENCRYPTION_KEY`
- `JWT_SECRET`
- `MIGRATION_DSN`，若 migration 使用不同帳號

使用 AWS Secrets Manager 時，container 還需要 AWS credentials 或 workload identity，以及 `secretsmanager:GetSecretValue` 權限。完整 payload 與其他設定請參考[設定與環境變數](../reference/configuration.md)。不要把正式 Secret 寫進 image、Dockerfile 或 Git。

### 外部服務網路

部署環境必須依啟用功能提供 outbound 網路、DNS 與 TLS 信任：

- 受 DBRE Maestro 管理或查詢的 MySQL、PostgreSQL 與 Redis endpoint
- 啟用 SSO/OIDC 時使用的 issuer、discovery、token 與 user-info endpoint
- 啟用 Lark 登入、通知或報表推送時使用的 Lark API
- 啟用 AWS Secrets Manager 或 DB Metadata Inventory 時使用的 AWS API

Image 可以在沒有部分選配整合的情況下啟動，但對應功能無法使用。Meta DB 則是 application health check 的必要依賴。

### Migration 權限

`RUN_MIGRATIONS_ON_STARTUP` 預設為 `true`。啟用時，server 接收流量前會使用 `MIGRATION_DSN` 執行 `/app/migrations`；若未另外設定 `MIGRATION_DSN`，會 fallback 到 `DB_DSN`。

Migration 帳號必須對 `maestro` schema 具有 migrations 所需的 DDL 與 DML 權限，包括 `CREATE`、`ALTER`、`DROP`、`INDEX`、`REFERENCES`、`SELECT`、`INSERT`、`UPDATE` 與 `DELETE`。權限不足時 container 會啟動失敗。

多副本部署不應讓每個 application container 同時執行 migration。此時應設定：

```text
RUN_MIGRATIONS_ON_STARTUP=false
```

並由部署流程以同一個 image 獨立執行：

```bash
/app/maestro -migrate-only
```

## 部署責任邊界

完成上述步驟後，產物是帶有明確 tag 與 CPU 架構的 application image。後續工作由部署平台負責，包括：

- Runtime environment 與 Secret 注入
- Meta DB 與外部服務網路
- Port mapping、Service、Ingress、Load Balancer、DNS 與 TLS
- Migration 執行時機
- Health check、log、監控、擴縮容與 rollback

部署到公司 AWS EKS 的現行流程請參考 [How to 部署到 AWS EKS](deploy-to-aws-eks.md)。
