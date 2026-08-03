# 安全邊界說明

DBRE Maestro 的安全模型不是單一 permission 判斷，而是多層邊界疊加：登入、MFA、RBAC、DB Scope、Workflow、Masking、Host Policy、Audit Log 與部署配置共同形成控制面。

## 問題

DBA 平台同時持有兩種高風險能力：

- 可以查詢或匯出資料
- 可以執行 DDL / DML / Redis 變更

如果只靠前端隱藏按鈕，或只靠單一 `admin` 角色，任何誤配、帳號外洩或流程繞過都會放大成資料外洩或資料庫變更風險。

## 分層邊界

```text
User
  |
  v
Login + active user + optional MFA
  |
  v
RBAC permission
  |
  v
DB Scope / resource ownership
  |
  +-- SQL Editor --------> readonly credential + query access + masking
  |
  +-- Ticket workflow ----> approval/execution separation + readwrite credential
  |
  +-- Export/report ------> readonly credential + masking + review/token checks
  |
  v
Audit log + notification + host policy
```

## 代碼負責的邊界

| 邊界 | 代碼責任 |
|---|---|
| Auth | JWT、refresh cookie、session revoke、active user 檢查 |
| MFA | admin MFA policy、challenge server-side state、失敗次數、一次性使用 |
| RBAC | permission gate、protected user / protected auth group 高危操作限制 |
| DB Scope | 使用者只能操作被授權的 DB Connection |
| Workflow | submitter 不能審批或執行自己的工單；executor 不能等於 reviewer |
| Masking | MySQL / PostgreSQL 查詢結果遮罩；Redis sensitive key prefix 阻擋 value 查詢 |
| Export | 下載需登入、需 requester 或 review 權限、token 一次性使用、log redaction |
| Host Policy | DB / Redis endpoint host allowlist、CIDR allowlist / denylist |
| Audit | 敏感操作、阻擋事件、權限變更、下載與執行事件可追溯 |

## 部署與 DBA/SRE 負責的邊界

| 邊界 | 運維責任 |
|---|---|
| Meta DB account | `DB_DSN` 使用 app user，`MIGRATION_DSN` 使用 migration 專用 user |
| Readonly credential | DB Connection readonly 帳號必須在 DB 端真的只有 SELECT / metadata read 所需權限 |
| Readwrite credential | 只授予 ticket execute 所需最小權限 |
| Secrets | EKS 使用 AWS Secrets Manager，不把 secret 寫入 Git 或 ArgoCD values |
| IRSA | Pod service account 只允許讀取必要 secret 與必要 AWS inventory API |
| Network | EKS subnet、security group、DB subnet、DNS、WAF 規則需和平台使用方式匹配 |
| Host Policy env | test 可先 `warn`，production 應使用 `enforce` |
| Lark | 使用者 `lark_recipient` 需要可投遞 open_id，Lark App ID / Secret 要在 Settings 設定 |

## Readonly credential 是硬要求

SQL Editor、Export、Scheduled Report、metadata read path 會使用 readonly credential。這能降低平台誤用風險，但前提是 DBA/SRE 在資料庫端真的把該帳號設為只讀。

代碼無法證明某個 MySQL/PostgreSQL 帳號沒有被授予危險權限。每次新增或修改 DB Connection 時，都要由 DBA/SRE 檢查：

- MySQL：不可有 `INSERT`、`UPDATE`、`DELETE`、DDL、`FILE`、`SUPER`、UDF/plugin 管理等權限
- PostgreSQL：不可是 superuser，不可有 server file、dangerous extension、dblink、sequence write、危險 `SECURITY DEFINER` 函式使用權
- Redis：若存在敏感 prefix，應在平台配置 Redis sensitive key prefix，阻擋讀 value 類命令

SQL Editor Stop Query 仍使用 readonly credential。MySQL cancel 會用 readonly credential 依序嘗試 `mysql.rds_kill_query`、`mysql.rds_kill`、`KILL QUERY <thread_id>`；PostgreSQL cancel 會用 readonly credential 執行 `pg_cancel_backend(<backend_pid>)`。Aurora/RDS MySQL 需要 DBA 額外授予 readonly user 執行前兩個 routine 的權限；標準 MySQL 取消自己的 connection query 通常不需要額外授權。

## Redis 的特殊設計

Redis 不使用欄位級 masking，因為 Redis 資料模型不是 table/column。平台採用更簡單的策略：

- key name 可以被看見
- `TYPE`、`TTL`、`EXISTS`、`SCAN` 類命令可以使用
- 命中 sensitive prefix 的 value/content 查詢會被阻擋

這表示 `SCAN` 掃出敏感 key name 是允許的；安全目標是阻止讀出敏感 value。

## Host Policy 的取捨

DB Connection host policy 是第一階段防線，用來防止 DB connection 管理者把 app Pod 當成 SSRF 或內網探測工具。

支援模式：

- `off`：不檢查
- `warn`：記錄 violation，但不阻擋
- `enforce`：阻擋違規 endpoint

`warn` 適合測試與盤點既有 endpoint；production 應切 `enforce`。目前 policy 在新增/修改 DB Connection 與 runtime resolved endpoint 前檢查，尚未改成 driver custom dialer。

## 工單職責分離

工單流程至少保證：

- submitter 不能 approve 自己的工單
- submitter 不能 execute 自己的工單
- reviewer 不能 execute 同一張工單

admin 不豁免這個邊界。若 admin William 提交工單，需要另一位 admin 或合格審批人處理。

## 相關文件

- [安全審計修復驗收](../how-to/verify-security-audit-remediation.md)
- [部署到 AWS EKS](../how-to/deploy-to-aws-eks.md)
- [設定與環境變數](../reference/configuration.md)
- [Users / RBAC](../reference/users-and-rbac.md)
- [Masking 與 DSL](../reference/masking-and-dsl.md)
- [SQL Editor 查詢取消機制](sql-editor-query-cancellation.md)
