package handler

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
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
