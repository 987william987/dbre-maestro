package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestRedisSensitiveKeyPrefixRepoListActiveForConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRedisSensitiveKeyPrefixRepo(sqlx.NewDb(db, "sqlmock"))
	now := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, db_connection_id, redis_db_index, key_prefix, reason, is_active, created_by, created_at, updated_at
		 FROM redis_sensitive_key_prefixes
		 WHERE db_connection_id = ?
		   AND is_active = 1
		   AND (redis_db_index IS NULL OR redis_db_index = ?)
		 ORDER BY redis_db_index IS NULL DESC, key_prefix`)).
		WithArgs(uint64(3), 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"db_connection_id",
			"redis_db_index",
			"key_prefix",
			"reason",
			"is_active",
			"created_by",
			"created_at",
			"updated_at",
		}).
			AddRow(uint64(1), uint64(3), nil, "session:", "login session", true, uint64(9), now, now).
			AddRow(uint64(2), uint64(3), 2, "token:", nil, true, uint64(9), now, now))

	prefixes, err := repo.ListActiveForConnection(context.Background(), 3, 2)
	if err != nil {
		t.Fatalf("ListActiveForConnection() error = %v", err)
	}
	values := RedisSensitiveKeyPrefixValues(prefixes)
	if len(values) != 2 || values[0] != "session:" || values[1] != "token:" {
		t.Fatalf("unexpected prefix values: %#v", values)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
