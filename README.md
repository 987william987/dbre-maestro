# DBRE Maestro

DBRE Maestro 是一個資料庫治理工作台，提供 SQL 查詢、DDL/DML 工單、敏感資料遮罩、資料庫連線治理、Metadata 掃描與 RBAC 權限控管。專案採前後端分離：

- `backend/`：Go API、排程工作、Meta DB 存取、外部資料庫連線與治理邏輯
- `frontend/`：React + Vite 管理介面
- `docs/`：產品、架構、操作與參考文件

## 功能概覽

- `Tickets`：提交與流轉 DDL / DML / SQL Export / Sensitive Access 工單
- `SQL Editor`：單 statement 查詢、格式化、Explain、匯出申請、敏感查詢權限申請
- `DB Connections`：管理 MySQL / PostgreSQL / Redis 連線與憑證角色
- `DB Metadata`：AWS Inventory 快照與資料庫物件快照
- `Masking Rules`：欄位脫敏規則與 Unmask Whitelist
- `SQL Review Rules`：DDL / DML 審核規則
- `Users / Auth Groups`：權限與 DB Scope 管理
- `Settings`：SQL Editor timeout 與 DB Metadata 掃描設定

## 文件入口

- [文件總覽](docs/README.md)
- [架構總覽](docs/explanation/architecture-overview.md)
- [權限模型說明](docs/explanation/permission-model.md)
- [本機開發教學](docs/tutorials/getting-started-local-dev.md)
- [SQL Editor 參考](docs/reference/sql-editor.md)
- [Tickets 參考](docs/reference/tickets.md)
- [Masking DSL 參考](docs/reference/masking-and-dsl.md)
- [設定與環境變數](docs/reference/configuration.md)

## 開發啟動

### 一鍵啟動整個系統

```bash
make dev
```

這會透過 Docker Compose 啟動：

- `mysql`
- `app`
- `frontend`

啟動後可使用：

- 前端：`http://localhost:5173`
- 後端 Health Check：`http://localhost:8080/api/health`

### 只啟動後端

```bash
make dev-backend
```

### 只在本機跑前端 Vite

```bash
make dev-frontend
```

此模式會把 API proxy 到本機的 `http://localhost:8080`。

## 測試

### 後端測試

```bash
make test
```

### 前端測試

```bash
make test-frontend
```

## 其他常用指令

### 只啟動 MySQL

```bash
make db-only
```

### 產生 32-byte 加密 key

```bash
make gen-key
```

## 環境變數

請先準備 `.env`，至少包含：

- `MYSQL_APP_PASSWORD`
- `MYSQL_ROOT_PASSWORD`
- `DBRE_ENCRYPTION_KEY`
- `JWT_SECRET`

後端啟動時若缺少 `DBRE_ENCRYPTION_KEY` 或 `JWT_SECRET`，服務不會成功啟動。

更完整的設定說明請看：

- [本機與部署設定](docs/reference/configuration.md)
- [平台 Settings 說明](docs/reference/settings.md)

## AWS Profile

若要在本機透過 `docker compose` 驗證 `DB Metadata inventory` 掃描，可直接沿用你本機的 AWS profile。

`docker-compose.yml` 目前會：

- 將 `${HOME}/.aws` 掛進 `app` container 的 `/root/.aws`
- 傳入 `AWS_PROFILE`
- 啟用 `AWS_SDK_LOAD_CONFIG=1`

使用方式：

```bash
export AWS_PROFILE=your-profile
docker compose up --build
```

若未指定，預設使用：

```bash
AWS_PROFILE=default
```
