# 通知與工單更新架構：REST 初始化 + SSE 即時更新

本文整理目前專案內「工單狀態更新 / 站內通知 / Lark 通知」的實際架構。

最後更新日期：`2026-06-18`

## 1. 現況總覽

系統目前採混合模型：

- REST：初始化資料、重新整理頁面、斷線後補資料
- SSE：通知與工單狀態的即時推送
- Meta DB：通知、工單、審批與執行結果的持久化來源
- Lark：工單狀態變更的外部即時通知通道

也就是說，現在不是「純輪詢」，也不是「完全靠前端 reload」。

## 2. 事件流

### 2.1 後端事件來源

當下列事件發生時，後端除了寫入資料表，還會發送即時事件：

- 建立站內通知
- 工單提交、收回、審批、駁回
- 工單進入待執行
- 工單執行成功或失敗

主要元件：

- `backend/internal/handler/realtime_events.go`
- `backend/internal/handler/event_stream.go`
- `backend/internal/realtime/broker.go`

### 2.2 SSE Stream

目前單一 SSE 入口為：

- `GET /api/events/stream`

連線建立後：

- 會先送一個 `ready` 事件
- 每 `25s` 送一次 heartbeat
- 後續按使用者身分推送事件

目前已使用的事件：

- `notification.created`
- `ticket.updated`

## 3. SSE 與 Timeout

SSE 是長連線，不適合直接套用一般 REST request 的 timeout 模型。這次實作的最終方案是：

- `GET /api/events/stream` 不走一般 `chi` 的 `Timeout(45s)`
- 主 HTTP server 仍保留 `WriteTimeout = 45s`
- 只有 SSE handler 內，才用 `http.NewResponseController(w).SetWriteDeadline(time.Time{})` 清掉該條 stream 的 write deadline

對應程式碼位置：

- `backend/cmd/server/main.go`
- `backend/internal/handler/event_stream.go`

這代表目前不是：

- 直接移除全域 `WriteTimeout`
- 額外再起一台獨立 SSE server

而是「同一台 server，只有 `/api/events/stream` 這條 route 做 per-request timeout 特例」。

### 3.1 為什麼不能直接沿用一般 Timeout

若 SSE 直接套用一般 request timeout：

- 長連線在 timeout 到點後會被中斷
- 前端會出現事件流反覆重連
- 工單狀態與通知的即時更新會不穩定，甚至看起來像偶發失效

若 SSE 直接套用一般 write timeout：

- heartbeat 或後續事件推送時，可能因 write deadline 到期而斷線
- 某些角色頁面會出現「有的人能收到更新、有的人收不到」的表面症狀

### 3.2 為什麼不直接拿掉全域 WriteTimeout

如果把全域 `WriteTimeout` 拿掉，雖然 SSE 會自然恢復穩定，但一般 REST API 也會失去這層保護，風險包括：

- 慢客戶端長時間占住 response write
- 某些異常連線更難被及時回收
- 一般 API 與 SSE 的連線特性被混在一起，缺少邊界

因此目前方案的取捨是：

- 一般 REST：保留 `requestTimeout = 45s` 與 `WriteTimeout = 45s`
- SSE：只對 `/api/events/stream` 做例外處理

這樣能同時兼顧：

- SSE 長連線正確性
- 一般 API 的保護性
- 架構簡單度，不必額外拆第二台 server

## 4. 前端使用方式

### 4.1 App Shell

`frontend/src/app/layout/AppShell.tsx` 會：

1. 先以 `GET /api/notifications` 取得通知清單與未讀數
2. 再呼叫 `openEventStream('/events/stream')`
3. 收到 `notification.created` 後刷新通知清單、更新 badge、依情境顯示 toast

### 4.2 Tickets 頁與 Detail 頁

- `frontend/src/modules/tickets/pages/TicketsPage.tsx`
- `frontend/src/modules/tickets/pages/TicketDetailPage.tsx`

兩頁都會監聽前端自訂事件 `maestro:realtime`。

當 SSE 收到 `ticket.updated` 時：

- Tickets List 會重新載入目前篩選頁
- Ticket Detail 會重新抓單張工單資料

因此工單狀態切換不需要手動 F5。

## 5. 站內通知與 Lark 的分工

### 5.1 站內通知

站內通知仍以 `notifications` 表為持久化來源。

SSE 只是讓前端更快得知「有新通知」，不是取代資料庫保存。

這樣的好處是：

- 使用者重整頁面仍可看到歷史通知
- SSE 中斷後，前端可再用 REST 補資料
- Audit 與通知記錄不依賴記憶體事件本身

### 5.2 Lark 通知

Lark 與站內通知是並行通道：

- 站內通知：平台內可追蹤、可回看
- Lark：讓 submitter、reviewer、executor 及時收到外部通知

Lark 收件人目前使用 `users.lark_recipient`，值需為可投遞的 `open_id`。

## 6. 目前架構的優點

- 即時性比輪詢好很多
- 仍保留 REST 作為初始化與補償機制
- 實作成本低於 WebSocket
- 對目前通知量與工單型產品很合適
- 不必為 SSE 額外拆一台 server
- 一般 REST timeout 保護不會因 SSE 被一併移除

## 7. 目前限制

目前 broker 是 app process 內記憶體實作，代表：

- 單機或單副本時行為最直接
- 若未來是多副本部署，不同 app instance 之間不會自動共享事件
- 若要真正水平擴展，需補 Redis Pub/Sub、NATS、outbox dispatcher 等跨節點事件層

另外，前端收到事件後目前多數仍採「重新拉 REST 資料」，而不是直接把 SSE payload 當成唯一真相來源。這是刻意的保守設計，可避免前後端局部狀態漂移。

## 8. 與舊文件的差異

若你看過較舊版本文件，最大的差異是：

- 舊版描述是「REST + 輪詢為主，SSE 是未來規劃」
- 現況已經是「REST 初始化 + SSE 即時更新」

因此後續討論通知問題時，應以本文件為準，不再把 SSE 當成尚未落地的藍圖。
