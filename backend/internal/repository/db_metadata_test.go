package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestListObjectSnapshotsWithoutLimitReturnsAllRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewDBMetadataRepo(sqlx.NewDb(db, "sqlmock"))
	rows := sqlmock.NewRows([]string{
		"id",
		"snapshot_at",
		"db_connection_id",
		"connection_name_snapshot",
		"engine",
		"cluster_name",
		"node_name",
		"database_name",
		"schema_name",
		"table_name",
		"data_size_bytes",
		"index_size_bytes",
	}).AddRow(
		uint64(1),
		time.Date(2026, 6, 15, 5, 41, 16, 0, time.UTC),
		uint64(5),
		"aws-sg-bot-pg-nonprod",
		"postgres",
		"cluster-a",
		"node-a",
		"capy_indexer",
		"public",
		"watches",
		int64(16384),
		int64(8192),
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
		id,
		snapshot_at,
		db_connection_id,
		connection_name_snapshot,
		engine,
		cluster_name,
		node_name,
		database_name,
		schema_name,
		table_name,
		data_size_bytes,
		index_size_bytes
	FROM db_object_snapshots ORDER BY snapshot_at DESC, id DESC`)).
		WillReturnRows(rows)

	items, err := repo.ListObjectSnapshots(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("ListObjectSnapshots() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(ListObjectSnapshots()) = %d, want 1", len(items))
	}
	if items[0].ConnectionName != "aws-sg-bot-pg-nonprod" {
		t.Fatalf("items[0].ConnectionName = %q, want %q", items[0].ConnectionName, "aws-sg-bot-pg-nonprod")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestListObjectSnapshotsWithLimitKeepsLimitClause(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewDBMetadataRepo(sqlx.NewDb(db, "sqlmock"))
	rows := sqlmock.NewRows([]string{
		"id",
		"snapshot_at",
		"db_connection_id",
		"connection_name_snapshot",
		"engine",
		"cluster_name",
		"node_name",
		"database_name",
		"schema_name",
		"table_name",
		"data_size_bytes",
		"index_size_bytes",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
		id,
		snapshot_at,
		db_connection_id,
		connection_name_snapshot,
		engine,
		cluster_name,
		node_name,
		database_name,
		schema_name,
		table_name,
		data_size_bytes,
		index_size_bytes
	FROM db_object_snapshots WHERE db_connection_id = ? ORDER BY snapshot_at DESC, id DESC LIMIT ?`)).
		WithArgs(uint64(3), 100).
		WillReturnRows(rows)

	_, err = repo.ListObjectSnapshots(context.Background(), 3, 100)
	if err != nil {
		t.Fatalf("ListObjectSnapshots() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
