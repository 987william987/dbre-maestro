package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/auth"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/oidcbearer"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

type fakeBearer struct {
	user   *model.User
	err    error
	called int
}

func (f *fakeBearer) Authenticate(ctx context.Context, rawToken string) (*model.User, error) {
	f.called++
	return f.user, f.err
}

type seen struct {
	userID    uint64
	username  string
	sessionID uint64
	hit       bool
}

func serve(t *testing.T, secret []byte, bearer BearerAuthenticator, authz string) (int, seen) {
	t.Helper()
	var got seen
	h := RequireAuth(secret, bearer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{UserIDFromCtx(r.Context()), UsernameFromCtx(r.Context()), SessionIDFromCtx(r.Context()), true}
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, got
}

func TestRequireAuthSessionTokenDoesNotConsultBearer(t *testing.T) {
	secret := []byte("s")
	tok, err := auth.NewAccessToken(1, "alice", 5, secret)
	if err != nil {
		t.Fatal(err)
	}
	fb := &fakeBearer{err: errors.New("must not be called")}
	code, got := serve(t, secret, fb, "Bearer "+tok)
	if code != http.StatusOK || got != (seen{1, "alice", 5, true}) {
		t.Fatalf("code=%d ctx=%+v", code, got)
	}
	if fb.called != 0 {
		t.Fatal("bearer authenticator consulted for a valid session token")
	}
}

func TestRequireAuthFallsBackToBearer(t *testing.T) {
	fb := &fakeBearer{user: &model.User{ID: 7, Username: "brian"}}
	code, got := serve(t, []byte("s"), fb, "Bearer eyJ.not-a-session-token.x")
	if code != http.StatusOK || got != (seen{7, "brian", 0, true}) {
		t.Fatalf("code=%d ctx=%+v", code, got)
	}
}

func TestRequireAuthRejectsWhenBearerFails(t *testing.T) {
	for name, fb := range map[string]*fakeBearer{
		"error":   {err: errors.New("bad token")},
		"no user": {},
	} {
		t.Run(name, func(t *testing.T) {
			code, got := serve(t, []byte("s"), fb, "Bearer eyJ.x.y")
			if code != http.StatusUnauthorized || got.hit {
				t.Fatalf("code=%d ctx=%+v", code, got)
			}
		})
	}
}

func TestRequireAuthWithoutBearerKeepsOldBehaviour(t *testing.T) {
	if code, got := serve(t, []byte("s"), nil, "Bearer eyJ.x.y"); code != http.StatusUnauthorized || got.hit {
		t.Fatalf("code=%d ctx=%+v", code, got)
	}
	if code, _ := serve(t, []byte("s"), nil, ""); code != http.StatusUnauthorized {
		t.Fatalf("code=%d without header", code)
	}
}

type fakeVerifier struct {
	id  oidcbearer.Identity
	err error
}

func (f fakeVerifier) Verify(ctx context.Context, rawToken string) (oidcbearer.Identity, error) {
	return f.id, f.err
}

func newUserRepo(t *testing.T) (*repository.UserRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return repository.NewUserRepo(sqlx.NewDb(db, "sqlmock")), mock
}

func TestOIDCBearerAuthFindsBoundIdentityFirst(t *testing.T) {
	users, mock := newUserRepo(t)
	mock.ExpectQuery(`SELECT \* FROM users WHERE external_identity_source`).WithArgs("oidc", "uuid-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "is_active"}).AddRow(7, "brian", "brian@example.com", true))

	a := OIDCBearerAuth{Verifier: fakeVerifier{id: oidcbearer.Identity{Subject: "uuid-1", Email: "other@example.com"}}, Users: users, Provider: "oidc"}
	user, err := a.Authenticate(context.Background(), "raw")
	if err != nil || user == nil || user.ID != 7 {
		t.Fatalf("user=%+v err=%v", user, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOIDCBearerAuthFallsBackToEmailAndNeverCreates(t *testing.T) {
	users, mock := newUserRepo(t)
	mock.ExpectQuery(`SELECT \* FROM users WHERE external_identity_source`).WithArgs("oidc", "uuid-1").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT \* FROM users WHERE email`).WithArgs("brian@example.com").WillReturnError(sql.ErrNoRows)

	a := OIDCBearerAuth{Verifier: fakeVerifier{id: oidcbearer.Identity{Subject: "uuid-1", Email: "brian@example.com"}}, Users: users, Provider: "oidc"}
	user, err := a.Authenticate(context.Background(), "raw")
	if err != nil || user != nil {
		t.Fatalf("user=%+v err=%v, want nil, nil (find-only)", user, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOIDCBearerAuthRejectsVerifierError(t *testing.T) {
	users, mock := newUserRepo(t)
	a := OIDCBearerAuth{Verifier: fakeVerifier{err: errors.New("expired")}, Users: users, Provider: "oidc"}
	if user, err := a.Authenticate(context.Background(), "raw"); err == nil || user != nil {
		t.Fatalf("user=%+v err=%v", user, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
