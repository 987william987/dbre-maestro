# 時間欄位與時區規範

本文件定義平台目前的時間欄位策略、UTC 寫入規則、前端顯示規則，以及仍保留在 schema 中的 `CURRENT_TIMESTAMP(6)` fallback 欄位。

## 現行規則

1. 所有新時間欄位一律使用 `DATETIME(6)`。
2. 禁止新增 `TIMESTAMP` 欄位。
3. 禁止新增秒級精度的 `DATETIME` 欄位；至少要到微秒級 `DATETIME(6)`。
4. App 寫入 DB 時，必須顯式傳入 `UTC` 時間。
5. Go 程式碼寫入 DB 的當前時間，統一使用 `time.Now().UTC()`；本專案優先使用 `internal/timeutil.NowUTC()`。
6. 不依賴 MySQL session timezone 或 `CURRENT_TIMESTAMP(6)` 做隱式時區轉換。
7. 前端顯示時間時，預設以 browser timezone 格式化。

## 前端顯示規則

- 共用格式化入口：`frontend/src/shared/lib/format.ts`
- 預設時區來源：`Intl.DateTimeFormat().resolvedOptions().timeZone`
- 目前 `Tickets`、`Users`、`DB Connections`、`SQL Editor`、`Audit Logs`、`DB Metadata`、`Masking Rules` 等頁面都走共用 `formatDateTime()`

這代表：

- DB 與 API 一律保存 / 傳輸 UTC 時間
- UI 顯示時，會依使用者目前瀏覽器所在時區自動轉換
- 若使用者人在台北，通常會看到 `Asia/Taipei (+08:00)` 對應時間

## App 已改為顯式 UTC 寫入的資料表

以下資料表目前已由 app 層顯式寫入時間，不再依賴 DB 自動填入當前時間：

- `audit_logs.created_at`
- `auth_groups.created_at`, `auth_groups.updated_at`
- `auth_group_permissions.created_at`
- `auth_group_db_connections.created_at`
- `db_connections.created_at`, `db_connections.updated_at`, `db_connections.last_tested_at`
- `db_connection_credentials.created_at`
- `cloud_db_inventory_snapshots.created_at`
- `db_object_snapshots.created_at`
- `export_requests.created_at`, `export_requests.downloaded_at`, `export_requests.expires_at`
- `masking_rules.created_at`
- `masking_whitelist.created_at`
- `notifications.created_at`
- `platform_settings.created_at`, `platform_settings.updated_at`
- `query_history.created_at`
- `saved_queries.created_at`, `saved_queries.updated_at`
- `sessions.created_at`, `sessions.revoked_at`, `sessions.expires_at`
- `ticket_review_results.created_at`
- `ticket_scopes.created_at`
- `tickets.created_at`, `tickets.updated_at`, `tickets.started_at`, `tickets.completed_at`, `tickets.approved_until`, `tickets.revoked_at`, `tickets.scheduled_at`
- `ticket_executions.started_at`, `ticket_executions.completed_at`
- `users.created_at`, `users.updated_at`
- `user_permissions.created_at`
- `user_db_connections.created_at`
- `auth_group_memberships.created_at`, `auth_group_memberships.expires_at`

## 目前 schema 仍保留 `CURRENT_TIMESTAMP(6)` fallback 的欄位

以下欄位在 migration 後仍保留 `DEFAULT CURRENT_TIMESTAMP(6)` 或 `ON UPDATE CURRENT_TIMESTAMP(6)`，但 app 已不應再依賴它們作為主要寫入來源：

- `users.created_at`, `users.updated_at`
- `auth_group_memberships.created_at`
- `platform_settings.updated_at`
- `sessions.created_at`
- `tickets.created_at`, `tickets.updated_at`
- `audit_logs.created_at`
- `db_connections.created_at`, `db_connections.updated_at`
- `export_requests.created_at`
- `masking_rules.created_at`
- `sql_review_rules.updated_at`
- `resource_groups.created_at`, `resource_groups.updated_at`
- `resource_group_users.granted_at`
- `notifications.created_at`
- `masking_whitelist.created_at`
- `auth_groups.created_at`, `auth_groups.updated_at`
- `permissions.created_at`, `permissions.updated_at`
- `user_permissions.created_at`
- `user_auth_groups.created_at`
- `user_db_connections.created_at`
- `ticket_scopes.created_at`
- `db_connection_credentials.created_at`, `db_connection_credentials.updated_at`
- `cloud_db_inventory_snapshots.created_at`
- `db_object_snapshots.created_at`
- `ticket_review_results.created_at`
- `query_history.created_at`
- `saved_queries.created_at`, `saved_queries.updated_at`

保留這些 default 的目的主要是：

- 舊資料與 migration 相容
- SQL 工具或人工維運時仍有最基本 fallback
- 避免某些歷史 insert 在漏帶時間時直接失敗

但工程實務上，新增或修改 app 邏輯時，不能把這些 DB default 當成正常流程依賴。

## 舊欄位盤點

已完成的統一工作如下：

- `audit_logs.created_at` 已升級為 `DATETIME(6)`
- `query_history.created_at`、`saved_queries.created_at`、`saved_queries.updated_at` 已從 `TIMESTAMP` 改為 `DATETIME(6)`
- 其餘主要業務表已透過 migration 升級到 `DATETIME(6)`

## 開發檢查清單

當你新增任何時間欄位或寫入邏輯時，必須同時確認：

1. schema 是否使用 `DATETIME(6)`
2. Go 寫入是否走 `timeutil.NowUTC()`
3. 若有更新行為，是否由 app 顯式傳 `updated_at`
4. 前端顯示是否走 `formatDateTime()`
5. 是否不再新增 `TIMESTAMP` / 秒級 `DATETIME`

## 相關檔案

- `backend/internal/timeutil/utc.go`
- `frontend/src/shared/lib/format.ts`
- `backend/migrations/031_all_datetime_precision.up.sql`
- `backend/migrations/032_query_history_saved_queries_datetime6.up.sql`
