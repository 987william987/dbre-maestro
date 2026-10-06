# Session Management 規格

> 狀態：Implemented；S1-S8 已完成。保留於 active 供後續 Session Management 功能延伸。
> UI route：`/dba-tools/sessions-management`

## 目標

提供 DBA 一個統一工作區，即時查看 MySQL、PostgreSQL 與 Redis 實體節點上的 sessions，並在明確權限、目標重驗證與完整審計下取消 query、終止 session，或建立有期限的 SQL prefix 循環取消工作。

本功能不是一般 SQL console，也不依賴 Metadata Inventory snapshot。AWS topology 在頁面首次載入與使用者手動 Refresh clusters 時直接向 AWS API 取得，頁面後續操作使用該次 server snapshot；非 AWS 節點由使用者在當次操作輸入。

## 已確認決策

- 功能名稱為 Session Management，使用單一路由與 engine-specific table/action。
- AWS 直接呼叫 `DescribeDBClusters`、`DescribeDBInstances`、`DescribeReplicationGroups`、`DescribeCacheClusters` 取得最新實體節點；不 import 或複製 inventory nodes。
- 非 AWS node 的 role、host、port 每次使用時輸入，不建立可重用 node 設定。
- 新增獨立 `operations` credential；不擴張 readonly/readwrite credential 權限。
- 權限拆為 `db_sessions.read`、`db_sessions.kill`、`db_sessions.loop_kill`，全部受 DB Connection Scope 約束。
- MySQL/PostgreSQL 單筆操作提供 Cancel Query 與 Terminate Session；Redis 只提供 Disconnect Client。
- Prefix/Loop Kill 僅支援 MySQL/PostgreSQL，且只能 Cancel Query。
- system/replication/maintenance/tool-owned sessions 可查看但不可操作。
- Audit 不保存原始 SQL literal，只保存去 literal 的 query shape 與原始 SQL SHA-256。

## 架構與資料流

```text
React Session Management
          |
          v
Session Management Handler
  |-- AWS Live Topology Adapter
  |-- MySQL Session Adapter
  |-- PostgreSQL Session Adapter
  |-- Redis Session Adapter
  |-- Prefix Matcher / SQL Sanitizer
  `-- Bounded Loop Kill Worker
          |
   operations credential
          |
   selected physical node
```

AWS cluster 必須能由所選 DB Connection 的 readonly/readwrite endpoint 驗證歸屬；使用自訂 CNAME 或非 AWS endpoint 時走 manual target。AWS API 失敗時 fail loud，不回退 Metadata Inventory snapshot。只有頁面首次載入與 Refresh clusters 會掃描 AWS；選 cluster、session refresh 與 destructive action 使用該次 snapshot。若 target 已不存在，DB connect/signal 必須 fail loud。

## Engine 能力

| Engine | Session 來源 | Cancel | Terminate |
|---|---|---|---|
| MySQL | full processlist / performance schema | `KILL QUERY`、RDS procedure | `KILL CONNECTION`、RDS procedure |
| PostgreSQL | `pg_stat_activity` | `pg_cancel_backend` | `pg_terminate_backend` |
| Redis | `CLIENT LIST` | 不支援 | `CLIENT KILL ID` |

每次 destructive action 前重新查詢 session 並核對 identity。MySQL 使用 thread ID、user、host、database、query hash；PostgreSQL 加入 PID 與 `backend_start`；Redis 使用 client ID 與 address。工具自己的 operations connection 一律列為 protected。

## Prefix 與 Loop Kill

Prefix matcher 只做大小寫與連續空白正規化後的 `startsWith`，不支援 regex 或 wildcard。必須指定 physical node、database、至少 16 個有效字元的 prefix，且只匹配 active/running、達 minimum age 的 session。執行前必須取得 60 秒短效 preview token，後端執行時重新查詢並再次比對。

Loop job 預設間隔 2 秒、有效 10 分鐘、最多 cancel 100 次；允許範圍分別為 1-30 秒、最長 60 分鐘、最多 1,000 次。連續錯誤 3 次停止，同一 physical node 同時最多一個 active loop。離開頁面不停止；server restart 後標記 `interrupted` 且不自動恢復。

```text
pending -> running -> completed
                   -> stopped
                   -> expired
                   -> limit_reached
                   -> failed
                   -> interrupted
```

原始 prefix 以 application encryption key 加密，只保留到 job 結束；終態只保留 query shape、hash、target snapshot 與統計。

## API 規劃

```text
GET  /api/dba-tools/session-management/aws/clusters
GET  /api/dba-tools/session-management/aws/topology
POST /api/dba-tools/session-management/sessions
POST /api/dba-tools/session-management/sessions/{id}/cancel
POST /api/dba-tools/session-management/sessions/{id}/terminate
POST /api/dba-tools/session-management/prefix/preview
POST /api/dba-tools/session-management/prefix/cancel
GET  /api/dba-tools/session-management/loop-jobs
POST /api/dba-tools/session-management/loop-jobs
POST /api/dba-tools/session-management/loop-jobs/{id}/stop
```

所有 endpoint 都要求 authentication、active user、對應 permission 與 DB scope。Manual host 每次套用 host/CIDR policy。成功、失敗、重新比對後跳過及拒絕操作都寫入 audit。

## UI

頁面先選 DB Connection，再選 AWS cluster/physical node，或輸入 manual target。Session table 提供 user、database、client、state、duration 與 SQL/command filters。預設手動刷新，可選 2/5/10/30 秒；hidden tab、離頁、切換 target 時停止並取消舊請求。單次最多 1,000 sessions，超出時顯示 truncated 並要求縮小 filter。

System sessions 預設可透過 filter 顯示，row 帶 `protected` 與 `protected_reason`，所有 destructive actions disabled。Prefix Preview 與 Loop Jobs 只在 MySQL/PostgreSQL 顯示。

## 審計與敏感資料

Session list 是即時 response，不寫入 Meta DB。Destructive audit 保存 actor、DB Connection、engine、node snapshot、session identity、DB user、database、client、action、result、duration、去 literal query shape 與 SHA-256。不得把完整 SQL、credential、未遮罩 prefix 或 Redis command arguments寫入 audit/structured log。

## 實作階段

1. S1：本規格、三級 permissions、`operations` credential 與 DB Connection UI/API。
2. S2：AWS live topology adapter、endpoint ownership 與 manual target validation。
3. S3：三種 engine 的 read-only session adapters 與頁面。
4. S4：單一 Cancel/Terminate/Disconnect 與 audit。
5. S5：SQL sanitizer、prefix matcher、Preview token 與一次性 prefix cancel。
6. S6：持久化 Loop Kill job、受控 worker、限制與 restart interruption。
7. S7：auto refresh、filters、分頁及完整 UI states。
8. S8：隔離 integration fixtures、安全測試、完整 lint/test/build 與 canonical 文件同步。

## 測試基線

- Unit：adapter parsing、protected classification、literal sanitization、prefix boundaries、job state/limits。
- Handler：permission、DB scope、AWS target ownership、forged manual host、stale preview、session identity race。
- Worker：同 node 互斥、stop、expiry、kill limit、連續錯誤、shutdown/restart。
- Frontend：三種 engine、readonly mode、confirm flows、auto-refresh cleanup、error/empty/protected/truncated states。
- Integration：Docker MySQL/PostgreSQL/Redis 僅建立並終止 fixture 自己的 sessions；AWS adapter 使用 mocked SDK。

真實 adapter fixture 由 `make test-session-management-integration` 執行；預設開啟 host ports `13316`、`15432`、`16379`，可用 `SESSION_IT_*_PORT` 覆寫。測試完成或失敗時都會移除專用 containers 與 volumes。

## 已知部署邊界

平台目前以單副本為基線。Loop worker 第一版遵循此限制；多副本前必須加入跨 pod claim/lease。部署前需為 operations account 配置各 engine 的最小 session visibility 與 signal/kill 權限，並確認 runtime IAM Role 已具備上述 AWS Describe APIs。
