package repository

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestDBConnectionRepoDeleteCleansConfigurationReferences(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewDBConnectionRepo(sqlx.NewDb(db, "sqlmock"), []byte("01234567890123456789012345678901"))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM platform_settings WHERE key_name = ?`)).
		WithArgs(settingDBMetadataObjectConnectionIDs).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(`[1,10,11]`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO platform_settings (key_name, value, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = VALUES(updated_at)`)).
		WithArgs(settingDBMetadataObjectConnectionIDs, `[1,11]`, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	for _, query := range []string{
		`DELETE FROM db_connection_credentials WHERE db_connection_id = ?`,
		`DELETE FROM user_db_connections WHERE db_connection_id = ?`,
		`DELETE FROM auth_group_db_connections WHERE db_connection_id = ?`,
		`DELETE FROM db_object_snapshots WHERE db_connection_id = ?`,
		`DELETE FROM ticket_execution_rollbacks WHERE source_connection_id = ?`,
		`DELETE FROM masking_whitelist WHERE db_connection_id = ?`,
		`DELETE FROM masking_rules WHERE db_connection_id = ?`,
		`DELETE FROM redis_sensitive_key_prefixes WHERE db_connection_id = ?`,
		`DELETE FROM query_access_rules WHERE connection_id = ?`,
		`DELETE FROM query_access_grants WHERE connection_id = ?`,
		`DELETE FROM scheduled_sql_report_runs WHERE report_id IN (SELECT id FROM scheduled_sql_reports WHERE db_connection_id = ?)`,
		`DELETE FROM scheduled_sql_reports WHERE db_connection_id = ?`,
	} {
		mock.ExpectExec(regexp.QuoteMeta(query)).
			WithArgs(uint64(10)).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE workflow_rules SET db_connection_id = NULL, updated_at = ? WHERE db_connection_id = ?`)).
		WithArgs(sqlmock.AnyArg(), uint64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE db_connections SET deleted_at = ?, deleted_by = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`)).
		WithArgs(sqlmock.AnyArg(), uint64(99), sqlmock.AnyArg(), uint64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.Delete(context.Background(), 10, 99); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
