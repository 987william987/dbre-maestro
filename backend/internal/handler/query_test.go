package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
)

func TestWriteQueryExecutionErrorTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeQueryExecutionError(recorder, context.DeadlineExceeded, "query", defaultQueryTimeout)

	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "query timed out after 30s" {
		t.Fatalf("error = %q, want %q", body["error"], "query timed out after 30s")
	}
}

func TestWriteQueryExecutionErrorCanceled(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeQueryExecutionError(recorder, context.Canceled, "query", defaultQueryTimeout)

	if recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestTimeout)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "query was cancelled" {
		t.Fatalf("error = %q, want %q", body["error"], "query was cancelled")
	}
}

func TestInjectLimitStripsTrailingSemicolon(t *testing.T) {
	got := injectLimit("SELECT * FROM t_user;   ", 200, "mysql")
	want := "SELECT * FROM `t_user` LIMIT 200"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitPreservesExistingLimit(t *testing.T) {
	got := injectLimit("SELECT * FROM t_user LIMIT 10;", 200, "mysql")
	want := "SELECT * FROM `t_user` LIMIT 10"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitSkipsRedis(t *testing.T) {
	got := injectLimit("GET user:1", 200, "redis")
	want := "GET user:1"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitSkipsShowStatements(t *testing.T) {
	got := injectLimit("SHOW GRANTS FOR dev;", 200, "mysql")
	want := "SHOW GRANTS FOR dev"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitLeavesMultiStatementSQLUntouched(t *testing.T) {
	got := injectLimit("SELECT 1;\nSHOW GRANTS FOR dev;", 200, "mysql")
	want := "SELECT 1;\nSHOW GRANTS FOR dev;"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitSupportsCTESelect(t *testing.T) {
	got := injectLimit("WITH cte AS (SELECT id FROM users) SELECT * FROM cte;", 200, "mysql")
	want := "WITH `cte` AS (SELECT `id` FROM `users`) SELECT * FROM `cte` LIMIT 200"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitPreservesTopLevelLimitForCTESelect(t *testing.T) {
	got := injectLimit("WITH cte AS (SELECT id FROM users LIMIT 5) SELECT * FROM cte LIMIT 10;", 200, "mysql")
	want := "WITH `cte` AS (SELECT `id` FROM `users` LIMIT 5) SELECT * FROM `cte` LIMIT 10"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitLeavesCTEMultiStatementSQLUntouched(t *testing.T) {
	got := injectLimit("WITH cte AS (SELECT 1) SELECT * FROM cte;\nSHOW GRANTS FOR dev;", 200, "mysql")
	want := "WITH cte AS (SELECT 1) SELECT * FROM cte;\nSHOW GRANTS FOR dev;"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestExecuteSQLQueryUsesDatabaseOnPinnedConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("USE `analytics`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET SESSION max_execution_time = 25000").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT \\* FROM t_user LIMIT 200").
		WillReturnRows(sqlmock.NewRows([]string{"t_user.id"}).AddRow(1))

	result, err := executeSQLQuery(
		context.Background(),
		&model.DBConnection{DBType: "mysql"},
		db,
		"SELECT * FROM t_user LIMIT 200",
		queryExecutionContext{DatabaseName: "analytics"},
		defaultSQLEditorTimeoutSettings(),
	)
	if err != nil {
		t.Fatalf("executeSQLQuery() error = %v", err)
	}
	if len(result.Rows) != 1 || len(result.Columns) != 1 {
		t.Fatalf("unexpected result = %#v", result)
	}
	if result.Columns[0] != "id" {
		t.Fatalf("display column = %q, want %q", result.Columns[0], "id")
	}
	if len(result.RawColumns) != 1 || result.RawColumns[0] != "t_user.id" {
		t.Fatalf("raw columns = %#v, want [t_user.id]", result.RawColumns)
	}
	if result.Origins[0].Database != "analytics" {
		t.Fatalf("origin database = %q, want %q", result.Origins[0].Database, "analytics")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestExecuteSQLQueryRunsMultiStatementsSequentially(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("USE `analytics`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET SESSION max_execution_time = 25000").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT 1 LIMIT 200").
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectQuery("SELECT \\* FROM t_user LIMIT 200").
		WillReturnRows(sqlmock.NewRows([]string{"t_user.id"}).AddRow(2))

	result, err := executeSQLQuery(
		context.Background(),
		&model.DBConnection{DBType: "mysql"},
		db,
		"SELECT 1 LIMIT 200; SELECT * FROM t_user LIMIT 200",
		queryExecutionContext{DatabaseName: "analytics"},
		defaultSQLEditorTimeoutSettings(),
	)
	if err != nil {
		t.Fatalf("executeSQLQuery() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != int64(2) {
		t.Fatalf("unexpected result rows = %#v", result.Rows)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "id" {
		t.Fatalf("unexpected result columns = %#v", result.Columns)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestExecuteSQLQueryResolvesMySQLLineageAfterRowsAreClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("USE `analytics`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET SESSION max_execution_time = 25000").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT DATE_FORMAT").
		WillReturnRows(sqlmock.NewRows([]string{"time", "fee", "unlockEdge"}).
			AddRow("2026-06-15", "1.479688", "0.986758"))
	mock.ExpectQuery("FROM information_schema\\.COLUMNS").
		WithArgs("analytics", "act_earn_unlock_edge").
		WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).
			AddRow("addtime").
			AddRow("fee").
			AddRow("unlock_edge"))

	result, err := executeSQLQuery(
		context.Background(),
		&model.DBConnection{DBType: "mysql"},
		db,
		`SELECT DATE_FORMAT(FROM_UNIXTIME(addtime), '%Y-%m-%d') AS time,
  IFNULL(sum(fee), 0) AS fee,
  IFNULL(sum(unlock_edge), 0) AS unlockEdge
FROM act_earn_unlock_edge
WHERE userid = 759477505764622593
GROUP BY time
ORDER BY time ASC`,
		queryExecutionContext{DatabaseName: "analytics"},
		defaultSQLEditorTimeoutSettings(),
	)
	if err != nil {
		t.Fatalf("executeSQLQuery() error = %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("len(result.Rows) = %d, want 1", len(result.Rows))
	}
	if result.RawColumns[1] != "fee" || result.Columns[1] != "fee" {
		t.Fatalf("unexpected result columns = raw:%#v display:%#v", result.RawColumns, result.Columns)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueryHandlerExecuteAuditsQueryAccessPolicyBlock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewQueryHandler(
		repository.NewDBConnectionRepo(sqlxDB, []byte("0123456789abcdef0123456789abcdef")),
		repository.NewUserRepo(sqlxDB),
		repository.NewMaskingRuleRepo(sqlxDB),
		repository.NewAuditRepo(sqlxDB),
		nil,
		nil,
		nil,
		nil,
		repository.NewQueryAccessRepo(sqlxDB),
		nil,
		nil,
		nil,
		nil,
		nil,
		"",
		[]byte("test-secret"),
	)

	now := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	userID := uint64(42)
	connID := uint64(2)

	mock.ExpectQuery(`SELECT \* FROM db_connections WHERE id = \?`).
		WithArgs(connID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "db_type", "host", "port", "readonly_host", "readonly_port", "readwrite_host", "readwrite_port",
			"database_name", "username", "password_encrypted", "encryption_key_version", "ssl_mode", "extra_params",
			"created_by", "created_at", "updated_at",
		}).AddRow(connID, "analytics-db", "mysql", "db.internal", uint16(3306), "db.internal", uint16(3306), "db.internal", uint16(3306),
			nil, "readonly", []byte("encrypted"), uint(1), "prefer", nil, userID, now, now))
	mock.ExpectQuery(`SELECT \* FROM db_connection_credentials WHERE db_connection_id = \? ORDER BY credential_role`).
		WithArgs(connID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "db_connection_id", "credential_role", "username", "password_encrypted", "encryption_key_version", "created_at", "updated_at",
		}))

	expectNonAllPermissionsUser(mock, userID, now)
	mock.ExpectQuery(`SELECT DISTINCT db_connection_id FROM \(`).
		WithArgs(userID, userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"db_connection_id"}).AddRow(connID))

	expectNonAllPermissionsUser(mock, userID, now)
	mock.ExpectQuery(`SELECT DISTINCT id FROM \(`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT id, subject_type, subject_id, effect, connection_id, database_pattern, table_pattern,`).
		WithArgs(userID, connID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "subject_type", "subject_id", "effect", "connection_id", "database_pattern", "table_pattern",
			"granted_via", "source_ticket_id", "expires_at", "revoked_at", "revoked_by", "created_by", "updated_by", "created_at", "updated_at",
		}))
	mock.ExpectExec(`INSERT INTO audit_logs \(actor_id, actor_name, action_type, resource_type, resource_id, details, ip_address, created_at\)`).
		WithArgs(sqlmock.AnyArg(), "pedro", "query_blocked", "db_connection", sqlmock.AnyArg(), auditDetailsReason("query_access_policy"), "10.0.0.9:12345", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodPost, "/api/query/execute", bytes.NewBufferString(`{
		"db_connection_id": 2,
		"database": "analytics",
		"sql": "SELECT email FROM users"
	}`))
	req.RemoteAddr = "10.0.0.9:12345"
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUsername, "pedro")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.Execute(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCollectPostgresQueryResultResolvesOriginsAfterRowsClosed(t *testing.T) {
	rows := &fakePGXRows{
		fields: []pgconn.FieldDescription{{
			Name:                 "id",
			TableOID:             10,
			TableAttributeNumber: 1,
		}},
		values: [][]any{{int32(1)}},
	}

	result, err := collectPostgresQueryResult(
		context.Background(),
		rows,
		&model.DBConnection{DBType: "postgres"},
		queryExecutionContext{DatabaseName: "postgres"},
		func(_ context.Context, fields []pgconn.FieldDescription) ([]masking.ColumnOrigin, error) {
			if !rows.closed {
				return nil, fmt.Errorf("origin resolver called before result rows were closed")
			}
			if len(fields) != 1 || fields[0].Name != "id" {
				return nil, fmt.Errorf("unexpected fields: %#v", fields)
			}
			return []masking.ColumnOrigin{{
				Database: "postgres",
				Schema:   "public",
				Table:    "watches",
				Column:   "id",
			}}, nil
		},
	)
	if err != nil {
		t.Fatalf("collectPostgresQueryResult() error = %v", err)
	}
	if !rows.closed {
		t.Fatal("rows were not closed")
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != int32(1) {
		t.Fatalf("unexpected rows = %#v", result.Rows)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "id" {
		t.Fatalf("unexpected columns = %#v", result.Columns)
	}
	if len(result.Origins) != 1 || result.Origins[0].Table != "watches" {
		t.Fatalf("unexpected origins = %#v", result.Origins)
	}
}

func expectNonAllPermissionsUser(mock sqlmock.Sqlmock, userID uint64, now time.Time) {
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "username", "email", "lark_recipient", "password", "is_setup", "is_protected", "is_active", "mfa_enabled", "mfa_secret_encrypted", "mfa_enabled_at", "created_at", "updated_at",
		}).AddRow(userID, "pedro", "pedro@example.com", "", "hash", true, false, true, false, []byte{}, nil, now, now))
	mock.ExpectQuery(`SELECT EXISTS \(`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"has_all_permissions"}).AddRow(false))
}

func TestBuildDisplayColumnsUsesOriginColumnName(t *testing.T) {
	rawColumns := []string{"t_deposit.id", "t_deposit.user_id", "t_deposit.account_id"}
	origins := []struct {
		database string
		table    string
		column   string
	}{
		{database: "analytics", table: "t_deposit", column: "id"},
		{database: "analytics", table: "t_deposit", column: "user_id"},
		{database: "analytics", table: "t_deposit", column: "account_id"},
	}

	display := buildDisplayColumns(rawColumns, []masking.ColumnOrigin{
		{Database: origins[0].database, Table: origins[0].table, Column: origins[0].column},
		{Database: origins[1].database, Table: origins[1].table, Column: origins[1].column},
		{Database: origins[2].database, Table: origins[2].table, Column: origins[2].column},
	})

	want := []string{"id", "user_id", "account_id"}
	for i := range want {
		if display[i] != want[i] {
			t.Fatalf("display[%d] = %q, want %q", i, display[i], want[i])
		}
	}
}

type fakePGXRows struct {
	fields []pgconn.FieldDescription
	values [][]any
	index  int
	closed bool
	err    error
}

func (r *fakePGXRows) Close() {
	r.closed = true
}

func (r *fakePGXRows) Err() error {
	return r.err
}

func (r *fakePGXRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *fakePGXRows) FieldDescriptions() []pgconn.FieldDescription {
	return r.fields
}

func (r *fakePGXRows) Next() bool {
	if r.index >= len(r.values) {
		r.Close()
		return false
	}
	r.index++
	return true
}

func (r *fakePGXRows) Scan(...any) error {
	return fmt.Errorf("Scan is not implemented")
}

func (r *fakePGXRows) Values() ([]any, error) {
	if r.index == 0 || r.index > len(r.values) {
		return nil, fmt.Errorf("Values called before Next")
	}
	return r.values[r.index-1], nil
}

func (r *fakePGXRows) RawValues() [][]byte {
	return nil
}

func (r *fakePGXRows) Conn() *pgx.Conn {
	return nil
}

func TestQueryHandlerTicketLinkUsesAppBaseURL(t *testing.T) {
	handler := &QueryHandler{appBaseURL: "https://maestro.example.com"}

	got := handler.ticketLink("TK-20260622-080000000-ABCDEF")
	want := "https://maestro.example.com/tickets/TK-20260622-080000000-ABCDEF"
	if got != want {
		t.Fatalf("ticketLink() = %q, want %q", got, want)
	}
}
