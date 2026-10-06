package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	appcrypto "github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

func newSessionLoopTestRepo(t *testing.T) (*SessionLoopJobRepo, sqlmock.Sqlmock, []byte) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	key := []byte("01234567890123456789012345678901")
	return NewSessionLoopJobRepo(sqlx.NewDb(db, "sqlmock"), key), mock, key
}

func TestSessionLoopPrefixEncryptionRoundTrip(t *testing.T) {
	repo, _, key := newSessionLoopTestRepo(t)
	encrypted, err := appcrypto.Encrypt(key, []byte("select * from orders"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.DecryptPrefix(&model.SessionLoopJob{PrefixEncrypted: encrypted})
	if err != nil || got != "select * from orders" {
		t.Fatalf("DecryptPrefix() = (%q, %v)", got, err)
	}
}

func TestSessionLoopFinishClearsPrefixAndActiveTargetAtomically(t *testing.T) {
	repo, mock, _ := newSessionLoopTestRepo(t)
	mock.ExpectExec(`UPDATE db_session_loop_jobs SET status = \?, prefix_encrypted = NULL, active_target_key = NULL`).
		WithArgs(model.SessionLoopStatusExpired, nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	changed, err := repo.Finish(context.Background(), 42, model.SessionLoopStatusExpired, "", "")
	if err != nil || !changed {
		t.Fatalf("Finish() = (%v, %v)", changed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionLoopCreateMapsActiveTargetUniquenessConflict(t *testing.T) {
	repo, mock, _ := newSessionLoopTestRepo(t)
	mock.ExpectExec(`INSERT INTO db_session_loop_jobs`).WillReturnError(&mysqlDriver.MySQLError{Number: 1062, Message: "duplicate active_target_key"})
	_, err := repo.Create(context.Background(), SessionLoopJobInput{Prefix: "select * from orders"})
	if !errors.Is(err, ErrSessionLoopTargetActive) {
		t.Fatalf("Create() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
