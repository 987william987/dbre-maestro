# DBRE Maestro

DBRE Maestro 是一個資料庫治理工作台，當前 repo 採前後端分離開發：

- backend：Go API + MySQL meta DB
- frontend：React + Vite

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
- 後端 Health Check：`http://localhost:8080/health`

### 只啟動後端

```bash
make dev-backend
```

### 只在本機跑前端 Vite

```bash
make dev-frontend
```

這適合只專注修改前端 UI 的情境。此模式預設會把 API proxy 到本機的 `http://localhost:8080`。

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
