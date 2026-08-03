# TODOS

## 1. 拆分 `ticket.go` 檔案

**What：** 把 `backend/internal/handler/ticket.go`（目前 2900+ 行）依職責拆成多個檔案（例如 execution/stop/registry、review、CRUD 各自獨立）。

**Why：** 檔案過大會降低可維護性，新功能持續堆加在同一個檔案上會讓它更難改。

**Pros：** 職責邊界清楚、日後改動時 diff 更小更好審查、降低合併衝突機率。

**Cons：** 純重構工作，短期沒有功能價值；拆分過程中容易誤動到不相關的邏輯，需要仔細切割。

**Context：** 2026-08 的工單執行容錯強化（`TicketExecutionRegistry` + `TerminateExecution`）review 時發現這個檔案已經過大，決定這次先把新程式碼放進 `ticket.go`（避免跟事故修復的改動混在一起難以審查），檔案拆分留到後續獨立處理。

**Depends on / blocked by：** 無強制依賴，但建議在工單執行容錯強化上線穩定後再進行，避免同時間有兩組大改動。

---

## 2. 多副本部署下 `TicketExecutionRegistry` 需要重新設計

**What：** 目前 `TicketExecutionRegistry` 是 in-process 記憶體結構，若平台從單一 pod 擴展為多副本部署，需要改用集中式儲存（例如 meta DB 或分散式鎖）記錄執行中工單的 registry 狀態，否則每個 pod 只知道自己那份工單的狀態。

**Why：** 現在 1-2 DBA 規模、單副本部署下沒問題，但若未來擴容，Stop/crash recovery 邏輯可能因為請求落在不同 pod 上而找不到對應的 registry entry，導致 KILL 機制失效。

**Pros：** 提前記錄下來，避免未來擴容時被遺忘，直到某次事故才發現這個限制。

**Cons：** 目前規模下沒有急迫性，過早設計容易變成過度工程。

**Context：** 2026-08 工單執行容錯強化（因應 SRE 誤觸發版事故）設計文件與 eng review 中明確排除多副本設計，只在架構層面留下這條限制記錄。

**Depends on / blocked by：** 需要先有實際擴容到多副本的計畫才需要動工，目前無明確時程。
