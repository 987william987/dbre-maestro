# 本機開發教學

這份教學會帶你從零啟動 DBRE Maestro，本機打開 UI，並完成一次最基本的驗證流程。

## 你會完成什麼

你會把整個系統在本機跑起來，進入前端，並確認 backend / frontend / Meta DB 都正常工作。

## What you'll need

- Docker 與 Docker Compose
- `make`
- 一份可用的 `.env`

## Step 1: 準備環境變數

把 `.env.example` 複製成 `.env`，至少補齊以下值：

```dotenv
MYSQL_APP_PASSWORD=changeme_app
MYSQL_ROOT_PASSWORD=changeme_root
DBRE_ENCRYPTION_KEY=BASE64_32_BYTE_KEY
JWT_SECRET=long-random-string
```

這一步完成後，app 才能正常解密 DB 連線設定並簽發 JWT。

## Step 2: 啟動整個系統

執行：

```bash
make dev
```

這一步會啟動：

- MySQL
- Go backend
- Vite frontend

做到這裡，你就已經有一個可打開的系統了。

## Step 3: 驗證服務是否正常

打開：

- 前端：`http://localhost:5173`
- 健康檢查：`http://localhost:8080/api/health`

如果健康檢查正常，你會看到 API 回應；如果前端正常，你會看到登入或 setup 畫面。

## Step 4: 完成初始化或登入

若是第一次啟動，先走 `/setup` 建立初始管理員帳號；若資料已存在，直接登入。

登入後可先確認：

- 預設導頁是否正常
- 左側導航是否依權限顯示

## Step 5: 做第一個工作流驗證

至少選一個最短路徑驗證：

### 選項 A：看 Settings

如果你有 `settings.read`：

1. 打開 `/settings`
2. 確認 SQL Editor timeout 與 metadata scan 欄位可顯示

### 選項 B：看 SQL Editor

如果你有 `sql_editor.query`：

1. 打開 `/sql-editor`
2. 確認頁面初始化成單一 tab，預設 SQL 是：

   ```sql
   SELECT 1;
   ```

3. 若已有 DB connection，可執行一次查詢

### 選項 C：看 Tickets

如果你有 ticket workspace 權限：

1. 打開 `/tickets`
2. 確認列表可正常載入

## What you built

你現在已經在本機跑起：

- React 前端
- Go API
- MySQL Meta DB
- 受控的治理工作台骨架

下一步建議依你要做的工作閱讀：

- [架構總覽](../explanation/architecture-overview.md)
- [SQL Editor 參考](../reference/sql-editor.md)
- [Tickets 參考](../reference/tickets.md)
- [設定與環境變數](../reference/configuration.md)
