# 通知與工單更新架構：REST/輪詢 vs SSE

本文整理目前專案內「工單狀態更新 / 站內信通知」的實際架構，並給出未來若升級到 `SSE` 的建議藍圖。

最後更新日期：`2026-06-12`

## 1. 目前架構：REST + 輪詢

### 1.1 核心特性

目前系統不是事件驅動。

無論是：

- 工單列表
- 工單詳情
- 右上角鈴鐺通知
- 待審批 / 待執行 toast

本質上都還是靠「前端重新發 HTTP API 取最新資料」。

### 1.2 工單狀態更新流程

```text
[使用者操作]
    |
    v
POST /api/tickets/{id}/approve
POST /api/tickets/{id}/reject
POST /api/tickets/{id}/request-execution
POST /api/tickets/{id}/execute
POST /api/tickets/{id}/revoke
    |
    v
[Backend Handler]
    |
    +--> 更新 tickets / executions / export_requests
    |
    +--> 寫入 audit_logs
    |
    +--> 寫入 notifications
    |
    v
[MySQL]
    |
    v
[前端不會被主動通知]
    |
    +--> TicketDetail 頁在操作成功後主動 reload
    |
    +--> Tickets 頁需再次 GET /api/tickets 才會看到新狀態
```

### 1.3 站內信通知流程

```text
[某個業務事件發生]
例如：
- 建單成功
- reviewer 待審批
- approve / reject
- 待執行
- 執行完成 / 失敗
    |
    v
[Backend Handler]
    |
    +--> INSERT INTO notifications
    |
    v
[MySQL.notifications]
    |
    v
[前端通知中心]
    |
    +--> 初次載入：GET /api/notifications
    |
    +--> 之後每 30s 輪詢一次 GET /api/notifications
    |
    +--> 發現新通知後更新鈴鐺未讀數
    |
    +--> 若 type = ticket_pending_review / ticket_pending_execution
         額外顯示 toast
```

### 1.4 目前前後端元件位置

#### 後端

- `backend/internal/handler/ticket.go`
- `backend/internal/handler/query.go`
- `backend/internal/handler/export.go`
- `backend/internal/handler/notification.go`
- `backend/internal/repository/notification.go`

#### 前端

- `frontend/src/app/layout/AppShell.tsx`
- `frontend/src/modules/notifications/api.ts`
- `frontend/src/shared/types/notification.ts`
- `frontend/src/shared/ui/ToastContext.tsx`

### 1.5 優點

- 架構簡單，直接沿用既有 REST API。
- 不需要新增長連線基礎設施。
- 不需要處理多機事件廣播。
- 對目前工單型產品來說足夠可用。

### 1.6 限制

- 通知最多延遲一個輪詢週期，目前是約 `30s`。
- Tickets 列表與詳情不是即時同步。
- 前端要反覆打 API 才能知道資料更新。
- 未來若通知密度提高，輪詢效率不佳。

## 2. 未來架構：SSE 事件驅動

### 2.1 適用情境

若未來希望做到：

- 工單狀態幾乎即時更新
- 鈴鐺通知不靠輪詢
- 審批/待執行 toast 幾乎立即出現
- 工單詳情頁執行進度持續推送

則 `SSE` 會是比 WebSocket 更輕量、也更適合目前場景的做法。

### 2.2 建議事件流

```text
[某個業務事件發生]
例如：
- ticket created
- ticket approved
- ticket rejected
- ticket pending_execution
- ticket completed
- notification created
    |
    v
[Backend Handler / Service]
    |
    +--> 寫入業務資料表（tickets / executions / exports）
    |
    +--> 寫入 notifications
    |
    +--> publish event
           例如：
           - notification.created
           - ticket.updated
    |
    v
[Event Bus / Dispatcher]
    |
    +--> 單機版：process memory hub
    |
    +--> 多機版：Redis Pub/Sub / NATS / Kafka / Outbox Dispatcher
    |
    v
[SSE Stream Endpoint]
GET /api/notifications/stream
GET /api/tickets/stream   (可選)
    |
    v
[Browser EventSource]
    |
    +--> 更新鈴鐺未讀數
    +--> 插入新的 notification item
    +--> 顯示 toast
    +--> 若需要，也可更新 tickets list / detail
```

### 2.3 前端視角

```text
App 啟動
  |
  +--> 先 GET /api/notifications          （初始化列表）
  |
  +--> 再建立 SSE：/api/notifications/stream
           |
           +--> 收到 notification.created
           |      - 更新通知下拉
           |      - 更新未讀 badge
           |      - 依 type 顯示 toast
           |
           +--> 斷線時自動重連
           |
           +--> 重連成功後再補一次 GET /api/notifications
                  避免漏事件
```

### 2.4 後端視角

```text
HTTP Handler
  |
  +--> 驗證 auth
  +--> 驗證權限
  +--> 執行業務更新
  +--> 寫 DB
  +--> 發 event

SSE Hub
  |
  +--> 記錄目前有哪些 user 正在線上
  +--> 哪些連線訂閱哪些事件
  +--> 有 event 時推到對應 user 的連線
  +--> 斷線時清理 client
```

## 3. 目前 vs 未來：對照簡表

| 面向 | 現在 REST/輪詢 | 未來 SSE |
|---|---|---|
| 資料取得方式 | 前端定期 GET | 後端主動 push |
| 通知延遲 | 最多約 30s | 幾乎即時 |
| 前端複雜度 | 低 | 中 |
| 後端複雜度 | 低 | 中 |
| 基礎設施需求 | 幾乎無新增 | 需長連線與事件分發 |
| 多機擴展 | 容易 | 需 event bus / pubsub |
| 適合場景 | 工單、後台、低頻通知 | 即時通知、進度推送、狀態同步 |

## 4. 若未來要做 SSE，建議分階段

### Phase 1：只做通知 SSE

目的：

- 保留現有 notifications table
- 只把鈴鐺通知從輪詢改成即時推送

建議：

- 新增 `GET /api/notifications/stream`
- 後端在建立通知時額外發 `notification.created`
- 前端保留 `GET /api/notifications` 作初始化與重連補償

### Phase 2：工單詳情 SSE

目的：

- 工單詳情頁不必手動 refresh
- 執行中工單的狀態、execution rows、completed/failed 可即時更新

建議：

- 新增 `GET /api/tickets/{id}/stream`
- 發送 `ticket.updated`、`ticket.execution.updated`

### Phase 3：多機事件分發

目的：

- 支援多台 app server / 多副本部署

建議：

- 引入 Redis Pub/Sub、NATS、Kafka 或 Outbox Dispatcher
- 不再依賴單機記憶體 hub
