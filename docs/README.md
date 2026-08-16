# 文件總覽

本目錄依 Diataxis 分成四類文件：

- `tutorials/`：給第一次上手的人，從零做到一個完整流程
- `how-to/`：給已經知道系統概念的人，快速完成特定任務
- `reference/`：準確描述 API、設定、權限、資料模型與功能行為
- `explanation/`：說明系統為什麼這樣設計，以及核心取捨

## Tutorials

- [本機開發教學](tutorials/getting-started-local-dev.md)

## How-To

- [How to RD 使用 DBRE Maestro](how-to/rd-user-guide.md)
- [How to DBA/Admin 管理 DBRE Maestro](how-to/dba-admin-user-guide.md)
- [How to 排查線上與部署問題](how-to/troubleshoot-operations.md)
- [How to 驗證安全審計修復](how-to/verify-security-audit-remediation.md)
- [How to 使用 SQL Editor](how-to/use-sql-editor.md)
- [How to 建立與執行 Tickets](how-to/create-and-execute-tickets.md)
- [How to 設定與使用 MySQL Rollback](how-to/configure-and-use-mysql-rollback.md)
- [How to 設定 Workflow Rules](how-to/configure-workflow-rules.md)
- [How to 建立 Scheduled SQL Report](how-to/create-scheduled-sql-report.md)
- [How to 部署到 AWS EKS](how-to/deploy-to-aws-eks.md)
- [How to 設定 Masking Rules](how-to/configure-masking-rules.md)
- [How to 綁定使用者 Lark Open ID](how-to/bind-lark-open-id.md)

## Reference

- [設定與環境變數](reference/configuration.md)
- [後端維護參考](reference/backend-maintenance.md)
- [前端維護參考](reference/frontend-maintenance.md)
- [後端 API 與權限對照](reference/backend-api-and-permissions.md)
- [登入安全與 Session](reference/auth-and-sessions.md)
- [DB Connections](reference/db-connections.md)
- [SQL Editor](reference/sql-editor.md)
- [Scheduled SQL Reports](reference/scheduled-sql-reports.md)
- [Tickets](reference/tickets.md)
- [MySQL Rollback](reference/mysql-rollback.md)
- [Workflow Rules](reference/workflow-rules.md)
- [Users / RBAC](reference/users-and-rbac.md)
- [Masking 與 DSL](reference/masking-and-dsl.md)
- [DB Metadata](reference/db-metadata.md)
- [平台 Settings](reference/settings.md)
- [時間欄位與時區規範](reference/time-handling.md)
- [Workflow Dashboard Data Dictionary](reference/workflow-dashboard-data-dictionary.md)

## Explanation

- [專案導覽](explanation/project-map.md)
- [架構總覽](explanation/architecture-overview.md)
- [安全邊界說明](explanation/security-boundaries.md)
- [SQL Editor 查詢取消機制](explanation/sql-editor-query-cancellation.md)
- [MySQL Rollback 設計說明](explanation/mysql-rollback-design.md)
- [權限模型](explanation/permission-model.md)
- [通知與工單更新架構：REST 初始化 + SSE 即時更新](explanation/notification-architecture-rest-vs-sse.md)

## Specs

- [Specs 分類總覽](specs/README.md)
- [20260616 Audit Logs Retention Strategy](specs/active/20260616-audit-logs-retention-strategy.md)
- [20260612 DB Metadata Module Spec](specs/active/20260612-111500-db-metadata-module-spec.md)
- [Dynamic RBAC Refactor Spec](specs/active/DYNAMIC_RBAC_REFACTOR_SPEC.md)
- [20260611 SQL Editor / Export / Sensitive Access / Settings / Notifications](specs/archive/20260611-160346-sql-editor-export-sensitive-access-settings-notifications.md)
- [20260611 API Namespace Consolidation](specs/archive/20260611-api-namespace-consolidation.md)
- [20260611 MySQL Masking Global Whitelist Sensitive Override](specs/archive/20260611-mysql-masking-global-whitelist-sensitive-override.md)
- [Backend RBAC API Gap Spec](specs/archive/BACKEND_RBAC_API_GAP_SPEC.md)
- [Frontend Spec](specs/archive/FRONTEND_SPEC.md)
