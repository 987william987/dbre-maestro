package repository

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestNotificationHealthStatsScansSnakeCaseColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewNotificationRepo(sqlx.NewDb(db, "sqlmock"))
	mock.ExpectQuery(`(?s)SELECT.*lark_failed_7d.*FROM notification_deliveries.*WHERE created_at >= \?`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"lark_failed_7d",
			"interactive_callback_failed_7d",
			"retry_or_failure_7d",
			"missing_lark_recipient_7d",
			"recipient_conflict_7d",
		}).AddRow(1, 2, 3, 4, 5))
	mock.ExpectQuery(`(?s)SELECT notification_type AS key_name.*FROM notification_deliveries.*GROUP BY notification_type`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"key_name", "count"}).AddRow("ticket_pending_review", 3))

	stats, err := repo.HealthStats(context.Background())
	if err != nil {
		t.Fatalf("HealthStats() error = %v", err)
	}
	if stats.LarkFailed7d != 1 || stats.InteractiveCallbackFailed7d != 2 || stats.RetryOrFailure7d != 3 || stats.MissingLarkRecipient7d != 4 || stats.RecipientConflict7d != 5 {
		t.Fatalf("stats = %#v, want snake_case columns scanned into notification health fields", stats)
	}
	if len(stats.ByType) != 1 || stats.ByType[0].Key != "ticket_pending_review" {
		t.Fatalf("ByType = %#v, want ticket_pending_review", stats.ByType)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
