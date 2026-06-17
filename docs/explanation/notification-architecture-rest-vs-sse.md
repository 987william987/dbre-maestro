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

## 3. 前端使用方式

### 3.1 App Shell

`frontend/src/app/layout/AppShell.tsx` 會：

1. 先以 `GET /api/notifications` 取得通知清單與未讀數
2. 再呼叫 `openEventStream('/events/stream')`
3. 收到 `notification.created` 後刷新通知清單、更新 badge、依情境顯示 toast

### 3.2 Tickets 頁與 Detail 頁

- `frontend/src/modules/tickets/pages/TicketsPage.tsx`
- `frontend/src/modules/tickets/pages/TicketDetailPage.tsx`

兩頁都會監聽前端自訂事件 `maestro:realtime`。

當 SSE 收到 `ticket.updated` 時：

- Tickets List 會重新載入目前篩選頁
- Ticket Detail 會重新抓單張工單資料

因此工單狀態切換不需要手動 F5。

## 4. 站內通知與 Lark 的分工

### 4.1 站內通知

站內通知仍以 `notifications` 表為持久化來源。

SSE 只是讓前端更快得知「有新通知」，不是取代資料庫保存。

這樣的好處是：

- 使用者重整頁面仍可看到歷史通知
- SSE 中斷後，前端可再用 REST 補資料
- Audit 與通知記錄不依賴記憶體事件本身

### 4.2 Lark 通知

Lark 與站內通知是並行通道：

- 站內通知：平台內可追蹤、可回看
- Lark：讓 submitter、reviewer、executor 及時收到外部通知

Lark 收件人目前使用 `users.lark_recipient`，值需為可投遞的 `open_id`。

## 5. 目前架構的優點

- 即時性比輪詢好很多
- 仍保留 REST 作為初始化與補償機制
- 實作成本低於 WebSocket
- 對目前通知量與工單型產品很合適

## 6. 目前限制

目前 broker 是 app process 內記憶體實作，代表：

- 單機或單副本時行為最直接
- 若未來是多副本部署，不同 app instance 之間不會自動共享事件
- 若要真正水平擴展，需補 Redis Pub/Sub、NATS、outbox dispatcher 等跨節點事件層

另外，前端收到事件後目前多數仍採「重新拉 REST 資料」，而不是直接把 SSE payload 當成唯一真相來源。這是刻意的保守設計，可避免前後端局部狀態漂移。

## 7. 與舊文件的差異

若你看過較舊版本文件，最大的差異是：

- 舊版描述是「REST + 輪詢為主，SSE 是未來規劃」
- 現況已經是「REST 初始化 + SSE 即時更新」

因此後續討論通知問題時，應以本文件為準，不再把 SSE 當成尚未落地的藍圖。
