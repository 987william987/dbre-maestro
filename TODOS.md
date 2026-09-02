# TODOS

## 1. 拆分 `ticket.go` 檔案

**What：** 把 `backend/internal/handler/ticket.go`（目前 4400+ 行）依職責拆成多個檔案（例如 execution/stop/registry、review、CRUD 各自獨立）。

**Why：** 檔案過大會降低可維護性，新功能持續堆加在同一個檔案上會讓它更難改。

**Pros：** 職責邊界清楚、日後改動時 diff 更小更好審查、降低合併衝突機率。

**Cons：** 純重構工作，短期沒有功能價值；拆分過程中容易誤動到不相關的邏輯，需要仔細切割。

**Context：** 2026-08 的工單執行容錯強化（`TicketExecutionRegistry` + `TerminateExecution`）review 時發現這個檔案已經過大，決定這次先把新程式碼放進 `ticket.go`（避免跟事故修復的改動混在一起難以審查），檔案拆分留到後續獨立處理。

**Depends on / blocked by：** 無強制依賴，但建議在工單執行容錯強化上線穩定後再進行，避免同時間有兩組大改動。

---

## 2. 補齊多副本部署 readiness

**What：** 平台目前以單副本部署，多個 runtime 元件仍依賴單一 process。擴成多副本前，需要逐項處理：

- `TicketExecutionRegistry`：改用集中式狀態或保證 stop request 路由到執行工單的 pod。
- SQL Editor active query registry：確保 cancel request 能找到原查詢所在 pod 與 DB backend identifier。
- SSE event broker：加入跨 pod event distribution，避免不同 pod 的 client 收不到事件。
- Ticket scheduler、Scheduled SQL Report、DB Metadata inventory/object jobs：確認具備 leader election、distributed lock 或可重入設計，避免同一輪工作重複執行。
- Migration：改由獨立 Job 執行，application containers 設定 `RUN_MIGRATIONS_ON_STARTUP=false`。

**Why：** 單副本下 process-local 狀態與 background jobs 都在同一個 server 內，行為可預期。直接增加 replica 會讓 stop/cancel 找不到狀態、SSE 遺失事件、排程重複執行，並可能讓多個 pod 同時搶 migration。

**Pros：** 提前記錄下來，避免未來擴容時被遺忘，直到某次事故才發現這個限制。

**Cons：** 目前規模下沒有急迫性，過早設計容易變成過度工程。

**Context：** `TicketExecutionRegistry`、active query registry 與 SSE broker 明確是 process-local；background jobs 也由每個 app process 啟動。在完成上述改造前，單副本是必要部署前提，不應只調高 `replicaCount`。

**Depends on / blocked by：** 需要先有實際擴容到多副本的計畫才需要動工，目前無明確時程。
