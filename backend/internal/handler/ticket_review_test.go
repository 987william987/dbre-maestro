package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/jmoiron/sqlx"
)

func TestSanitizeMySQLShadowValidationError(t *testing.T) {
	t.Run("sanitizes shadow database privilege failure", func(t *testing.T) {
		got := sanitizeMySQLShadowValidationError(assertErr("create shadow database failed: Error 1044 (42000): Access denied for user 'maestro_app'@'%' to database 'shadow_demo'"))
		want := "shadow validation is not available because the platform validation database privilege is not configured"
		if got != want {
			t.Fatalf("expected sanitized message %q, got %q", want, got)
		}
	})

	t.Run("keeps business validation errors readable", func(t *testing.T) {
		got := sanitizeMySQLShadowValidationError(assertErr("database \"foo\" already exists"))
		if got != "database \"foo\" already exists" {
			t.Fatalf("unexpected sanitized message: %q", got)
		}
	})
}

func TestRewriteMySQLDDLForShadowSupportsRenameTable(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "RENAME TABLE `tb_announcement` TO `tb_announcement_legacy_20260715`")
	if err != nil {
		t.Fatalf("parse rename table: %v", err)
	}
	if len(parsed.Statements) != 1 {
		t.Fatalf("statement count = %d, want 1", len(parsed.Statements))
	}

	rewritten, explicitDatabase, needsClone, err := rewriteMySQLDDLForShadow(parsed.Statements[0], "shadow_mainnet")
	if err != nil {
		t.Fatalf("rewriteMySQLDDLForShadow() error = %v", err)
	}
	if !needsClone {
		t.Fatal("needsClone = false, want true for RENAME TABLE")
	}
	if explicitDatabase != "" {
		t.Fatalf("explicitDatabase = %q, want empty", explicitDatabase)
	}
	want := "RENAME TABLE `shadow_mainnet`.`tb_announcement` TO `shadow_mainnet`.`tb_announcement_legacy_20260715`"
	if rewritten != want {
		t.Fatalf("rewritten = %q, want %q", rewritten, want)
	}
	if got := inferDDLObjectType(parsed.Statements[0]); got != "table" {
		t.Fatalf("object type = %q, want table", got)
	}
}

func TestMySQLDDLShadowCloneTablesUsesStatementScope(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []mysqlShadowCloneTable
	}{
		{
			name: "drop table clones only target table",
			sql:  "DROP TABLE william",
			want: []mysqlShadowCloneTable{{database: "app", table: "william", required: true}},
		},
		{
			name: "drop table if exists allows missing target",
			sql:  "DROP TABLE IF EXISTS william",
			want: []mysqlShadowCloneTable{{database: "app", table: "william", required: false}},
		},
		{
			name: "alter table with foreign key clones target and referenced table",
			sql:  "ALTER TABLE orders ADD CONSTRAINT fk_orders_user FOREIGN KEY (user_id) REFERENCES users(id)",
			want: []mysqlShadowCloneTable{
				{database: "app", table: "orders", required: true},
				{database: "app", table: "users", required: true},
			},
		},
		{
			name: "create table like clones referenced table only",
			sql:  "CREATE TABLE new_orders LIKE orders",
			want: []mysqlShadowCloneTable{{database: "app", table: "orders", required: true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, tt.sql)
			if err != nil {
				t.Fatalf("parse SQL: %v", err)
			}
			got, err := mysqlDDLShadowCloneTables(parsed.Statements[0], "app")
			if err != nil {
				t.Fatalf("mysqlDDLShadowCloneTables() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len(got) = %d, want %d; got = %#v", len(got), len(tt.want), got)
			}
			for index := range tt.want {
				if got[index] != tt.want[index] {
					t.Fatalf("got[%d] = %#v, want %#v", index, got[index], tt.want[index])
				}
			}
		})
	}
}

func TestMySQLDDLShadowCloneTablesForStatementsUsesBatchScope(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "ALTER TABLE orders ADD COLUMN memo VARCHAR(255); DROP TABLE old_orders")
	if err != nil {
		t.Fatalf("parse SQL: %v", err)
	}

	got, err := mysqlDDLShadowCloneTablesForStatements(parsed.Statements, "app")
	if err != nil {
		t.Fatalf("mysqlDDLShadowCloneTablesForStatements() error = %v", err)
	}

	want := []mysqlShadowCloneTable{
		{database: "app", table: "orders", required: true},
		{database: "app", table: "old_orders", required: true},
	}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d; got = %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got[%d] = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func TestMySQLDDLTableExistenceChecks(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []mysqlDDLTableExistenceCheck
	}{
		{
			name: "create table target must not exist",
			sql:  "CREATE TABLE william (id BIGINT PRIMARY KEY)",
			want: []mysqlDDLTableExistenceCheck{{database: "app", table: "william", expectation: mysqlTableMustNotExist}},
		},
		{
			name: "create table if not exists target is optional",
			sql:  "CREATE TABLE IF NOT EXISTS william (id BIGINT PRIMARY KEY)",
			want: []mysqlDDLTableExistenceCheck{{database: "app", table: "william", expectation: mysqlTableMustNotExist, optional: true}},
		},
		{
			name: "alter table target must exist",
			sql:  "ALTER TABLE william ADD COLUMN memo VARCHAR(255)",
			want: []mysqlDDLTableExistenceCheck{{database: "app", table: "william", expectation: mysqlTableMustExist}},
		},
		{
			name: "drop table target must exist",
			sql:  "DROP TABLE william",
			want: []mysqlDDLTableExistenceCheck{{database: "app", table: "william", expectation: mysqlTableMustExist}},
		},
		{
			name: "drop table if exists target is optional",
			sql:  "DROP TABLE IF EXISTS william",
			want: []mysqlDDLTableExistenceCheck{{database: "app", table: "william", expectation: mysqlTableMustExist, optional: true}},
		},
		{
			name: "create table like checks target and dependency",
			sql:  "CREATE TABLE new_orders LIKE orders",
			want: []mysqlDDLTableExistenceCheck{
				{database: "app", table: "new_orders", expectation: mysqlTableMustNotExist},
				{database: "app", table: "orders", expectation: mysqlTableMustExist},
			},
		},
		{
			name: "rename table checks old and new target",
			sql:  "RENAME TABLE orders TO archived_orders",
			want: []mysqlDDLTableExistenceCheck{
				{database: "app", table: "orders", expectation: mysqlTableMustExist},
				{database: "app", table: "archived_orders", expectation: mysqlTableMustNotExist},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, tt.sql)
			if err != nil {
				t.Fatalf("parse SQL: %v", err)
			}
			got, err := mysqlDDLTableExistenceChecks(parsed.Statements[0], "app")
			if err != nil {
				t.Fatalf("mysqlDDLTableExistenceChecks() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len(got) = %d, want %d; got = %#v", len(got), len(tt.want), got)
			}
			for index := range tt.want {
				if got[index] != tt.want[index] {
					t.Fatalf("got[%d] = %#v, want %#v", index, got[index], tt.want[index])
				}
			}
		})
	}
}

func TestValidateMySQLDDLTableExistence(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		count   int
		wantErr string
	}{
		{
			name:    "create table rejects existing target",
			sql:     "CREATE TABLE william (id BIGINT PRIMARY KEY)",
			count:   1,
			wantErr: `table "william" already exists`,
		},
		{
			name:    "drop table rejects missing target",
			sql:     "DROP TABLE william",
			count:   0,
			wantErr: `table "william" does not exist`,
		},
		{
			name:  "drop table if exists allows missing target",
			sql:   "DROP TABLE IF EXISTS william",
			count: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()
			mock.ExpectQuery(`FROM information_schema\.TABLES`).
				WithArgs("app", "william").
				WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(tt.count))

			parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, tt.sql)
			if err != nil {
				t.Fatalf("parse SQL: %v", err)
			}
			err = validateMySQLDDLTableExistence(context.Background(), db, parsed.Statements[0], "app")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateMySQLDDLTableExistence() error = %v", err)
				}
			} else if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("validateMySQLDDLTableExistence() error = %v, want %q", err, tt.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestInferReviewObjectTypeReturnsTableForDML(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "INSERT INTO sys_menu (id) SELECT id FROM sys_menu WHERE id = 1")
	if err != nil {
		t.Fatalf("parse insert select: %v", err)
	}
	if len(parsed.Statements) != 1 {
		t.Fatalf("statement count = %d, want 1", len(parsed.Statements))
	}
	if got := inferReviewObjectType(parsed.Statements[0]); got != "table" {
		t.Fatalf("object type = %q, want table", got)
	}
}

func TestBuildRedisTicketDatabaseOptions(t *testing.T) {
	items := buildRedisTicketDatabaseOptions()

	if len(items) != 16 {
		t.Fatalf("len(items) = %d, want 16", len(items))
	}
	if items[0].Name != "0" {
		t.Fatalf("items[0].Name = %q, want 0", items[0].Name)
	}
	if items[15].Name != "15" {
		t.Fatalf("items[15].Name = %q, want 15", items[15].Name)
	}
}

func assertErr(message string) error {
	return testError(message)
}

type testError string

func (e testError) Error() string {
	return string(e)
}

func TestCanViewFullTicketQueueAllowsDBAGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	userID := uint64(7)
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"username",
			"email",
			"lark_recipient",
			"password",
			"is_setup",
			"is_protected",
			"is_active",
			"created_at",
			"updated_at",
		}).AddRow(userID, "fly", "fly@example.com", "", "hash", false, false, true, now, now))
	mock.ExpectQuery(`SELECT EXISTS \(`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT DISTINCT auth_group`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"auth_group"}).AddRow(model.AuthGroupDBA))

	handler := &TicketHandler{users: repository.NewUserRepo(sqlx.NewDb(db, "sqlmock"))}
	allowed, err := handler.canViewFullTicketQueue(context.Background(), userID)
	if err != nil {
		t.Fatalf("canViewFullTicketQueue() error = %v", err)
	}
	if !allowed {
		t.Fatal("DBA group should be allowed to view the full ticket queue")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestCanReviewTicketRejectsSubmitterEvenWithReviewPermission(t *testing.T) {
	userID := uint64(7)
	ctx := context.WithValue(context.Background(), middleware.CtxPermissions, []string{permissionTicketReview})
	handler := &TicketHandler{}
	ticket := &model.Ticket{
		ID:          1,
		TicketType:  model.TicketTypeDDL,
		Status:      model.TicketStatusPendingReview,
		SubmitterID: userID,
	}

	allowed, err := handler.canReviewTicket(ctx, ticket, userID)
	if err != nil {
		t.Fatalf("canReviewTicket() error = %v", err)
	}
	if allowed {
		t.Fatal("submitter must not be allowed to review their own ticket")
	}
}

func TestBuildTicketNotificationBodyIncludesSubmitterAndOmitsActionDetail(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	submitterID := uint64(7)
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(submitterID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"username",
			"email",
			"lark_recipient",
			"password",
			"external_identity_source",
			"external_identity_id",
			"password_login_disabled",
			"lark_login_open_id",
			"lark_login_union_id",
			"lark_display_name",
			"lark_avatar_url",
			"lark_bound_at",
			"lark_binding_status",
			"is_setup",
			"is_protected",
			"is_active",
			"mfa_enabled",
			"mfa_secret_encrypted",
			"mfa_enabled_at",
			"created_at",
			"updated_at",
		}).AddRow(submitterID, "william", "william@example.com", "", "hash", "", "", false, "", "", "", "", nil, "", true, false, true, false, nil, nil, now, now))

	databaseName := "testnet_edgex_opt"
	ticket := &model.Ticket{
		TicketNo:     "TK-20260716-142959000-503E32",
		TicketType:   model.TicketTypeDDL,
		Status:       model.TicketStatusPendingReview,
		SubmitterID:  submitterID,
		DatabaseName: &databaseName,
	}
	handler := &TicketHandler{users: repository.NewUserRepo(sqlx.NewDb(db, "sqlmock"))}

	body := handler.buildTicketNotificationBody(context.Background(), ticket, model.TicketStatusPendingReview)
	for _, part := range []string{
		"工單類型：DDL",
		"目前狀態：待審核",
		"提交者：william",
		"數據庫：testnet_edgex_opt",
		"工單連結：/tickets/TK-20260716-142959000-503E32",
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("body missing %q: %s", part, body)
		}
	}
	for _, removed := range []string{"待執行操作：", "說明："} {
		if strings.Contains(body, removed) {
			t.Fatalf("body should not include %q: %s", removed, body)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestCanExecuteTicketRejectsSubmitterEvenWithExecutePermission(t *testing.T) {
	userID := uint64(7)
	ctx := context.WithValue(context.Background(), middleware.CtxPermissions, []string{permissionTicketExecute})
	handler := &TicketHandler{}
	connID := uint64(3)
	ticket := &model.Ticket{
		ID:             1,
		TicketType:     model.TicketTypeDDL,
		Status:         model.TicketStatusPendingExecution,
		SubmitterID:    userID,
		DBConnectionID: &connID,
	}

	allowed, err := handler.canExecuteTicket(ctx, ticket, userID)
	if err != nil {
		t.Fatalf("canExecuteTicket() error = %v", err)
	}
	if allowed {
		t.Fatal("submitter must not be allowed to execute their own ticket")
	}
}

func TestCanExecuteTicketRejectsReviewerEvenWhenListedAsExecutor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	submitterID := uint64(5)
	reviewerID := uint64(7)
	executorID := uint64(8)
	connID := uint64(3)
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	ticket := &model.Ticket{
		ID:             1,
		TicketType:     model.TicketTypeDDL,
		Status:         model.TicketStatusPendingExecution,
		SubmitterID:    submitterID,
		ReviewerID:     &reviewerID,
		DBConnectionID: &connID,
	}
	ctx := context.WithValue(context.Background(), middleware.CtxPermissions, []string{permissionTicketExecute})
	handler := &TicketHandler{tickets: repository.NewTicketRepo(sqlx.NewDb(db, "sqlmock"))}

	allowed, err := handler.canExecuteTicket(ctx, ticket, reviewerID)
	if err != nil {
		t.Fatalf("canExecuteTicket() reviewer error = %v", err)
	}
	if allowed {
		t.Fatal("reviewer must not be allowed to execute the same ticket")
	}

	mock.ExpectQuery(`SELECT ticket_id, workflow_rule_id, workflow_rule_name, approval_enabled,`).
		WithArgs(ticket.ID).
		WillReturnRows(sqlmock.NewRows([]string{
			"ticket_id",
			"workflow_rule_id",
			"workflow_rule_name",
			"approval_enabled",
			"approval_user_ids",
			"executor_user_ids",
			"admin_user_ids",
			"error_code",
			"error_message",
			"resolution_trace",
			"resolved_at",
			"created_at",
			"updated_at",
		}).AddRow(ticket.ID, nil, "test", true, "[7]", "[7,8]", "[]", "", "", "{}", now, now, now))
	allowed, err = handler.canExecuteTicket(ctx, ticket, executorID)
	if err != nil {
		t.Fatalf("canExecuteTicket() executor error = %v", err)
	}
	if !allowed {
		t.Fatal("non-reviewer executor candidate should be allowed to execute")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestWorkflowResolutionExcludesSubmitter(t *testing.T) {
	connID := uint64(3)
	ticket := &model.Ticket{
		ID:             1,
		TicketType:     model.TicketTypeDDL,
		Status:         model.TicketStatusPendingReview,
		SubmitterID:    7,
		DBConnectionID: &connID,
	}
	resolution := &model.WorkflowResolution{
		TicketType:        model.TicketTypeDDL,
		DBConnectionID:    &connID,
		ApprovalEnabled:   true,
		ApprovalUserIDs:   []uint64{7, 8},
		ExecutorUserIDs:   []uint64{7, 9},
		AdminUserIDs:      []uint64{},
		ErrorCode:         "",
		ErrorMessage:      "",
		RuleName:          "test",
		ExportSensitivity: nil,
	}

	excludeSubmitterFromWorkflowResolution(ticket, resolution)

	if uint64InSlice(7, resolution.ApprovalUserIDs) {
		t.Fatalf("submitter still appears in approval candidates: %#v", resolution.ApprovalUserIDs)
	}
	if uint64InSlice(7, resolution.ExecutorUserIDs) {
		t.Fatalf("submitter still appears in executor candidates: %#v", resolution.ExecutorUserIDs)
	}
	if !uint64InSlice(8, resolution.ApprovalUserIDs) || !uint64InSlice(9, resolution.ExecutorUserIDs) {
		t.Fatalf("non-submitter candidates were removed: approval=%#v executor=%#v", resolution.ApprovalUserIDs, resolution.ExecutorUserIDs)
	}
	if resolution.ErrorCode != "" {
		t.Fatalf("resolution should remain valid when other candidates exist, got %s", resolution.ErrorCode)
	}
}

func TestWorkflowResolutionAutoExecutionDoesNotRequireExecutorAfterSubmitterExclusion(t *testing.T) {
	connID := uint64(3)
	for _, ticketType := range []model.TicketType{
		model.TicketTypeDDL,
		model.TicketTypeDML,
		model.TicketTypeRedisCommand,
	} {
		t.Run(string(ticketType), func(t *testing.T) {
			ticket := &model.Ticket{
				ID:             1,
				TicketType:     ticketType,
				Status:         model.TicketStatusPendingReview,
				SubmitterID:    7,
				DBConnectionID: &connID,
			}
			resolution := &model.WorkflowResolution{
				TicketType:        ticketType,
				DBConnectionID:    &connID,
				ApprovalEnabled:   false,
				ExecutionMode:     workflowExecutionModeAutoApproval,
				ApprovalUserIDs:   []uint64{7, 8},
				ExecutorUserIDs:   []uint64{},
				AdminUserIDs:      []uint64{},
				ErrorCode:         "",
				ErrorMessage:      "",
				RuleName:          "auto " + string(ticketType),
				ExportSensitivity: nil,
			}

			excludeSubmitterFromWorkflowResolution(ticket, resolution)

			if resolution.ErrorCode != "" {
				t.Fatalf("auto execution should not require executor candidates, got %s: %s", resolution.ErrorCode, resolution.ErrorMessage)
			}
			if uint64InSlice(7, resolution.ApprovalUserIDs) {
				t.Fatalf("submitter still appears in approval candidates: %#v", resolution.ApprovalUserIDs)
			}
		})
	}
}
