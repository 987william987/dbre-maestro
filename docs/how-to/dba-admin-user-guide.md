# How to DBA/Admin 管理 DBRE Maestro

本文給 DBA / Admin，說明從平台初始化到日常營運的主要操作：建立初始管理員、管理使用者與權限、配置 DB Connections、Workflow Rules、Masking、通知、MFA recovery，以及部署後常見檢查。

## 前置條件

- 平台已部署完成，Pod 狀態為 running
- Meta DB migration 已成功完成
- 你可以打開平台 URL，或能透過 `kubectl port-forward` 進入服務
- 你擁有初始 admin 帳號，或正在進行首次 setup

測試環境若 `Service` 是 `ClusterIP`，可用：

```bash
kubectl -n sre-test port-forward svc/dbre-maestro 8080:8080
```

然後打開：

```text
http://localhost:8080
```

## 1. 首次初始化 admin

空庫第一次打開平台時，系統會進入 setup 流程。

1. 開啟平台首頁。
2. 建立初始 admin。
3. 使用可識別的 username 與 email。

測試環境範例：

```text
Username: admin
Email: dbre-maestro-admin-sre-test@example.com
Password: <strong-password>
```

正式環境建議：

- 初始 `admin` 只作 bootstrap / break-glass 用途
- 使用團隊管理信箱，不使用個人臨時信箱
- 建立第二個 admin 後，不共用初始 admin 帳密
- 啟用 MFA 時，每位 admin 使用自己的 MFA QR code

## 2. 建立第二個 admin

正式環境至少保留兩個獨立 admin，避免單一帳號 MFA、密碼或裝置失效後無人可登入。

1. 用初始 admin 登入。
2. 打開 `Users`。
3. 建立新的管理員使用者。
4. 將使用者加入 `admin` auth group。
5. 確認該使用者可登入。
6. 若 MFA 已啟用，讓該使用者自行完成 MFA setup。

不要把初始 admin 的 MFA QR code 截圖保存後給其他人共用。每個 user 的 MFA secret 都是獨立的。

## 3. 管理 Users / Auth Groups / Resources

Users 模組有三個視角：

- `Users`：管理使用者、active 狀態、direct permissions、direct DB scope
- `Auth Groups`：管理群組權限、群組 DB scope、群組成員
- `Resources`：反查 DB connection 被哪些 user / group 使用

建議流程：

1. 先建立或確認 auth group。
2. 把功能權限配置到 auth group。
3. 把 DB connection scope 配置到 auth group。
4. 把 user 加入 auth group。
5. 只在例外情況使用 direct permission 或 direct DB scope。

常見群組語義：

| Auth Group | 用途 |
|---|---|
| `admin` | 平台管理者 |
| `dba` | DDL / DML / Redis 執行者 |
| `data_owner` | 一般資料變更與 Query Access 審批 |
| `security` | Export、Sensitive Access 審批 |
| `developer` | 一般 RD 使用者 |

## 4. 建立 DB Connections

DB Connections 是 SQL Editor、Tickets、Metadata 與 DB Scope 的基礎。

1. 打開 `DB Connections`。
2. 建立 connection。
3. 選擇 DB type：
   - MySQL
   - PostgreSQL
   - Redis
4. 填寫 readonly endpoint。
5. 視需要填寫 readwrite endpoint。
6. 填寫 readonly / readwrite credential。
7. 儲存後點擊 connection test。
8. 到 `Users` 或 `Auth Groups` 配置 DB scope。

建議權限分工：

- readonly credential：SQL Editor、metadata、export、sensitive analysis
- readwrite credential：DDL / DML / Redis ticket execute

若未配置 readwrite endpoint，系統會 fallback 到 readonly endpoint。正式環境不建議依賴這個 fallback 執行變更。

## 5. 配置 RD 使用者權限

一般 RD 常見需要：

| 能力 | 權限 |
|---|---|
| 看 Tickets | `tickets.read` |
| 提 DDL / DML / Redis / Query Access | `tickets.apply` |
| 看 SQL Editor | `sql_editor.read` |
| 查詢與 metadata | `sql_editor.query` |
| 建立 SQL Export | `sql_editor.export` |
| 申請敏感臨時查看 | `sql_editor.sensitive_apply` |

配置完成後，用 Resources 視角確認該 user 最終有效 DB scope 是否符合預期。

## 6. 配置 Workflow Rules

Workflow Rules 決定不同工單由誰審、誰執行。

1. 打開 `Settings`。
2. 找到 Workflow Rules。
3. 建立或調整 rule。
4. 指定 Ticket Type。
5. 指定 DB Connection，或使用 All connections。
6. 指定 Approval Auth Groups。
7. 對 DDL / DML / Redis 指定 Executor Auth Groups。
8. 檢查 Effective Preview。
9. 檢查 Conflict Preview。
10. 儲存並用測試工單驗證。

建議分工：

- DDL / DML / Redis：`data_owner` 審批，`dba` 執行
- Query Access：`data_owner` 審批，審批通過即生效
- SQL Export：`security` 審批
- Sensitive Query Access：`security` 審批

若工單進入 `needs_admin_attention`，通常代表找不到有效 rule、有效 reviewer 或有效 executor。修正 Workflow Rules 後，再從工單詳情重新解析。

## 7. 配置 Masking Rules

Masking Rules 用於控制敏感欄位的脫敏顯示。

1. 打開 Masking Rules。
2. 建立欄位規則。
3. 選擇 match type 與 mask mode。
4. 視需要配置 DSL 條件。
5. 測試 SQL Editor 查詢結果是否被正確遮罩。
6. 若有固定人員可看原值，使用 whitelist 或授權流程處理。

建議原則：

- 預設以最小必要範圍開放
- 敏感原值查看走 Sensitive Query Access
- 不用永久 whitelist 取代審批流程，除非是明確職責需要

## 8. 配置 Scheduled SQL Reports

若需要定期執行唯讀 SQL 並推送結果：

1. 確認使用者有 `scheduled_sql_reports.read` / `scheduled_sql_reports.write`。
2. 建立報表。
3. 選擇 DB connection、database、SQL、cron expression、timezone。
4. 指定收件人。
5. 啟用報表。
6. 觀察下一次 run history。

報表應只執行唯讀 SQL。若報表需要發送到 Lark，先完成 Lark recipient 配置。

## 9. 配置 Lark 通知

平台通知包含站內通知與 Lark。

建議流程：

1. 在 Settings 配置 Lark App。
2. 為每個使用者綁定可投遞的 Lark `open_id`。
3. 用測試工單確認通知可送達。
4. 若使用 webhook fallback，理解它只能廣播，不能依 submitter / reviewer / executor 定向投遞。

使用者的 Lark recipient 存在 user profile 上。若通知沒送到，先查 user 是否有正確 `lark_recipient`。

## 10. MFA 管理與 Recovery

MFA 由部署環境控制：

| 設定 | 行為 |
|---|---|
| `MFA_ENFORCEMENT=disabled` | 不強制 MFA |
| `MFA_ENFORCEMENT=required_for_admins` | admin user 與 admin group 成員必須使用 MFA |

`APP_ENV=production` 預設會要求 admin MFA。測試環境通常設為 disabled。

營運規則：

- 每個 user 的 MFA QR code / setup key 都不同
- 不共用 admin 帳號與 MFA QR code
- admin 可幫其他 user reset MFA
- admin 也可 reset 自己 MFA，但 self-reset 會撤銷自己的 session
- 正式環境至少保留兩個 admin，讓管理員可互相 reset MFA

若所有 admin 都無法登入，使用 break-glass CLI：

```bash
make reset-mfa USERNAME=admin
```

或在 backend 目錄執行：

```bash
go run ./cmd/server -reset-mfa-username admin
```

## 11. 部署後健康檢查

部署完成後至少檢查：

1. ArgoCD 顯示 `Healthy`、`Synced`、`Sync OK`。
2. Pod 狀態為 `running 1/1`。
3. Logs 出現：

   ```text
   migrations complete
   server starting
   ```

4. 服務入口可打開。
5. 初始 admin 可登入。
6. `/api/health` 正常。
7. 可建立測試 DB connection 並連線測試。
8. 一般 RD 測試帳號可進 SQL Editor。
9. 測試工單可完成提交、審批、執行或授權。

如果 Service 是 `ClusterIP` 且沒有 Ingress，代表服務只在 cluster 內部可訪問。需要請 DevOps 補 ingress / route，或用 port-forward 臨時測試。

## 12. Migration 與部署注意事項

測試環境單副本可使用：

```text
RUN_MIGRATIONS_ON_STARTUP=true
```

Pod 啟動時會自動檢查 migration。已套用過的 migration 不會重跑。

正式環境建議：

- 多副本 Deployment 設 `RUN_MIGRATIONS_ON_STARTUP=false`
- 先用 Kubernetes Job 執行 `/app/maestro -migrate-only`
- migration 成功後再 rollout app

如果出現 dirty migration state：

1. 先看失敗版本的 migration SQL。
2. 確認 SQL 是否已部分完成。
3. 不要直接刪 `schema_migrations`。
4. 修正 DB 狀態或權限後，再清理該版本 dirty state。
5. 重新跑 migration。

## 常見問題

### 使用者看不到 DB connection

到 `Users > Resources` 反查：

- 該 connection 是否綁到 user
- 該 connection 是否綁到 user 所屬 auth group
- user 是否 active
- user 是否有 `sql_editor.query` 或相關 ticket 權限

### 工單沒有人收到通知

檢查：

- Workflow Rules 是否命中
- Effective Preview 是否有有效 reviewer / executor
- 目標 auth group 成員是否 active
- 成員是否具備對應 review / execute permission
- Lark recipient 是否存在

### SQL Editor 查詢 timeout

檢查 Settings：

- `sql_editor_app_timeout_seconds`
- `sql_editor_mysql_max_execution_time_ms`
- `sql_editor_postgres_statement_timeout_ms`

### Export 可以通過但下載失敗

檢查 token 是否過期，以及是否超過每分鐘 3 次下載限制。

### Admin 被 MFA 鎖住

優先請另一個 admin reset MFA。若沒有任何 admin 可登入，再使用 break-glass CLI。

## 相關文件

- [RD 使用手冊](rd-user-guide.md)
- [Users / RBAC](../reference/users-and-rbac.md)
- [DB Connections](../reference/db-connections.md)
- [Workflow Rules](../reference/workflow-rules.md)
- [How to 設定 Workflow Rules](configure-workflow-rules.md)
- [How to 設定 Masking Rules](configure-masking-rules.md)
- [How to 建立 Scheduled SQL Report](create-scheduled-sql-report.md)
- [How to 綁定使用者 Lark Open ID](bind-lark-open-id.md)
- [登入安全與 Session](../reference/auth-and-sessions.md)
- [AWS EKS 部署流程](deploy-to-aws-eks.md)
