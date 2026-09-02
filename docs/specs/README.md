# Specs 分類

本目錄存放的是「設計時點的規格文件」，不是保證永遠與現況完全一致的 reference。為了避免把已落地或已被後續設計推翻的 spec 跟仍可延用的 spec 混在一起，目前分成兩類：

- `active/`：仍可作為後續實作依據，與現況大方向相容
- `archive/`：已完成、已被吸收，或已被後續設計取代，保留作歷史脈絡

所有 spec 現在都會在 front matter 補一個 `status` 欄位，值只使用：

- `active`
- `archived`

## 仍有效

### [20260616 Audit Logs Retention Strategy](active/20260616-audit-logs-retention-strategy.md)

判定：

- 這是從舊 `TODOS.md` 唯一未完成事項 `TE9` 收斂出的獨立 spec
- 目前仍未落地，且屬於後續治理與維運策略需要補完的 active work
- 尚未被其他文件取代

### [20260618 Query Access Ticket And SQL Editor Query Scope Spec](active/20260618-120025-query-access-ticket-and-sql-editor-query-scope-spec.md)

判定：

- 這是 Query Access 工單、SQL Editor 細粒度查詢權限、DB Scope / Sensitive Access 邊界的主設計文件
- 第一版已大部分落地；本文保留作為現行 Query Access 行為與後續演進的設計基線
- 沒有被其他文件取代

### [20260618 Query Access Implementation Checklist](active/20260618-123500-query-access-implementation-checklist.md)

判定：

- Query Access 核心第一版已落地；清單保留設計時的拆解狀態，未勾選項目不代表核心功能尚未上線
- 內容偏 execution checklist，不取代主 spec；後續應以 reference 與實際程式碼判斷現行行為
- 應與主 spec 一起維護

### [20260618 Query Access Backend Task List](active/20260618-124500-query-access-backend-task-list.md)

判定：

- Query Access 第一版後端核心流程已落地；原始 checkbox 保留作設計時快照
- 主要涵蓋 migration、domain、service、API、parser 整合、audit 與 tests
- 屬於 implementation-ready 文件

### [20260618 Query Access Frontend Task List](active/20260618-124700-query-access-frontend-task-list.md)

判定：

- Query Access 第一版前端核心流程已落地；原始 checkbox 保留作設計時快照
- 主要涵蓋 New Ticket、SQL Editor CTA、Ticket List / Detail、文案與 QA
- 屬於 implementation-ready 文件

### [20260612 DB Metadata Module Spec](active/20260612-111500-db-metadata-module-spec.md)

判定：

- 目前 `Inventory` / `Objects` / `Settings` / 多憑證角色等主軸仍與現況一致
- 雖然其中部分內容已落地，但整份 spec 仍可作為後續 DB Metadata 演進的設計基線
- 沒有被後續文件明確推翻

### [20260702 Security Audit Remediation Spec](active/20260702-security-audit-remediation-spec.md)

判定：

- S-01 至 S-07 的程式修復與驗收文件已落地
- 本文保留安全問題、修復邊界與驗收條件，舊「目前問題」段落是審計當時的基線
- C-01 readonly database account 仍是持續性的部署與維運要求

### [Dynamic RBAC Refactor Spec](active/DYNAMIC_RBAC_REFACTOR_SPEC.md)

判定：

- 現行 permission-driven navigation、auth group、direct permission、DB scope 模型，仍大致沿著這份 spec 的方向
- 即使已有部分實作落地，這份 spec 仍可作為 RBAC 後續演進的總設計文件
- 沒有被更晚版本的完整 RBAC spec 取代

## 歷史檔案

### [20260611 SQL Editor / Export / Sensitive Access / Settings / Notifications](archive/20260611-160346-sql-editor-export-sensitive-access-settings-notifications.md)

判定：

- 其中大量內容已落地
- 但 `Settings` 承擔特殊審批人配置這一點，已被後續 permission / page 設計改寫
- 比較適合保留作功能演進歷史，而不是現行規格

### [20260611 API Namespace Consolidation](archive/20260611-api-namespace-consolidation.md)

判定：

- `/api/*` namespace 已落地
- 文件內容屬於已完成變更，不再是待實作 spec

### [20260611 MySQL Masking Global Whitelist Sensitive Override](archive/20260611-mysql-masking-global-whitelist-sensitive-override.md)

判定：

- 後續已演進到更完整的 masking DSL、更多 mask mode、query lineage 與 parser/semantic layer
- 「只支援 MySQL、global column masking + object-level unmask whitelist」已不是當前完整真相
- 已被新版 masking 文件與實作超越

### [Backend RBAC API Gap Spec](archive/BACKEND_RBAC_API_GAP_SPEC.md)

判定：

- 這份文件建立在較早期的 `resource group` / 固定 auth group 脈絡上
- 後續被 Dynamic RBAC Refactor 的方向吸收並取代
- 屬於中間過渡規格

### [Frontend Spec](archive/FRONTEND_SPEC.md)

判定：

- 這是只有 setup/login/tickets 初期階段的前端規格
- 現況已遠超這份文件範圍，且其中多項「非目標」已經變成正式功能
- 保留作歷史脈絡即可
