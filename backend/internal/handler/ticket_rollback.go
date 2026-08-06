package handler

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

type mysqlRollbackRuntime struct {
	supported bool
	reason    string
	conn      *model.DBConnection
	password  string
	start     repository.RollbackRange
}

type mysqlBinlogPosition struct {
	File string
	Pos  uint64
}

func (h *TicketHandler) prepareMySQLRollbackRuntime(ctx context.Context, ticket *model.Ticket, execRow model.TicketExecution) mysqlRollbackRuntime {
	if h == nil || h.rollbacks == nil || h.settings == nil || h.dbConns == nil {
		return mysqlRollbackRuntime{reason: "rollback service is not configured"}
	}
	if ticket == nil || ticket.DBConnectionID == nil {
		return mysqlRollbackRuntime{reason: "ticket has no target db connection"}
	}
	if !isMySQLDMLStatement(execRow.SQLStmt) {
		return mysqlRollbackRuntime{reason: "only mysql insert/update/delete statements are supported"}
	}
	settings, err := h.settings.Get(ctx)
	if err != nil {
		return mysqlRollbackRuntime{reason: "load rollback settings failed"}
	}
	if settings == nil || !settings.MySQLRollbackEnabled {
		return mysqlRollbackRuntime{reason: "mysql rollback is disabled"}
	}
	if strings.TrimSpace(settings.MySQLRollbackMy2SQLPath) == "" {
		return mysqlRollbackRuntime{reason: "my2sql path is not configured"}
	}

	conn, err := h.dbConns.GetByID(ctx, *ticket.DBConnectionID)
	if err != nil || conn == nil {
		return mysqlRollbackRuntime{reason: "db connection not found"}
	}
	if conn.DBType != "mysql" {
		return mysqlRollbackRuntime{reason: "only mysql connections are supported in rollback v1"}
	}
	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleRollback)
	if err != nil {
		return mysqlRollbackRuntime{reason: "rollback credential is not configured"}
	}
	if ticket.DatabaseName != nil && strings.TrimSpace(*ticket.DatabaseName) != "" {
		dbName := strings.TrimSpace(*ticket.DatabaseName)
		resolvedConn.DatabaseName = &dbName
	}

	db, cleanup, err := openResolvedSQLDB(ctx, resolvedConn, password)
	if err != nil {
		return mysqlRollbackRuntime{reason: "rollback credential connection failed"}
	}
	defer cleanup()
	if err := checkMySQLRollbackVariables(ctx, db); err != nil {
		return mysqlRollbackRuntime{reason: err.Error()}
	}
	pos, err := readMySQLBinlogPosition(ctx, db)
	if err != nil {
		return mysqlRollbackRuntime{reason: "read mysql binlog start position failed"}
	}
	return mysqlRollbackRuntime{
		supported: true,
		conn:      resolvedConn,
		password:  password,
		start: repository.RollbackRange{
			StartFile: pos.File,
			StartPos:  pos.Pos,
		},
	}
}

func (h *TicketHandler) finalizeMySQLRollback(ctx context.Context, ticket *model.Ticket, execRow model.TicketExecution, runtime mysqlRollbackRuntime, rowsAffected *int64) {
	if h == nil || h.rollbacks == nil || ticket == nil || ticket.DBConnectionID == nil {
		return
	}
	if !runtime.supported {
		_ = h.rollbacks.MarkUnsupported(ctx, ticket, execRow, runtime.reason)
		return
	}
	settings, err := h.settings.Get(ctx)
	if err != nil || settings == nil || !settings.MySQLRollbackEnabled {
		_ = h.rollbacks.MarkGenerationFailed(ctx, ticket, execRow, "rollback settings became unavailable")
		return
	}
	db, cleanup, err := openResolvedSQLDB(ctx, runtime.conn, runtime.password)
	if err != nil {
		_ = h.rollbacks.MarkGenerationFailed(ctx, ticket, execRow, "rollback credential connection failed")
		return
	}
	defer cleanup()
	endPos, err := readMySQLBinlogPosition(ctx, db)
	if err != nil {
		_ = h.rollbacks.MarkGenerationFailed(ctx, ticket, execRow, "read mysql binlog end position failed")
		return
	}
	binlogRange := runtime.start
	binlogRange.EndFile = endPos.File
	binlogRange.EndPos = endPos.Pos
	if err := h.rollbacks.MarkGenerating(ctx, ticket, execRow, binlogRange, "my2sql"); err != nil {
		slog.Warn("ticket rollback generation state update failed", "ticket_id", ticket.ID, "execution_id", execRow.ID, "err", err)
		return
	}

	generated, err := runMy2SQLRollback(ctx, settings, runtime.conn, runtime.password, binlogRange, ticket.DatabaseName, rowsAffected)
	if err != nil {
		_ = h.rollbacks.MarkFailed(ctx, execRow.ID, err.Error())
		slog.Warn("ticket rollback generation failed", "ticket_id", ticket.ID, "execution_id", execRow.ID, "err", err)
		return
	}
	if err := h.rollbacks.MarkGenerated(ctx, execRow.ID, generated); err != nil {
		slog.Warn("ticket rollback save failed", "ticket_id", ticket.ID, "execution_id", execRow.ID, "err", err)
	}
}

func openResolvedSQLDB(ctx context.Context, conn *model.DBConnection, password string) (*sql.DB, func(), error) {
	driver, dsn := pool.BuildDSN(conn, password)
	db, err := pool.Open(driver, dsn, pool.ProfileExec)
	if err != nil {
		return nil, nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return db, func() { _ = db.Close() }, nil
}

func checkMySQLRollbackVariables(ctx context.Context, db *sql.DB) error {
	values := map[string]string{}
	rows, err := db.QueryContext(ctx, `SHOW VARIABLES WHERE Variable_name IN ('log_bin', 'binlog_format', 'binlog_row_image')`)
	if err != nil {
		return fmt.Errorf("read mysql rollback variables failed")
	}
	defer rows.Close()
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return fmt.Errorf("read mysql rollback variables failed")
		}
		values[strings.ToLower(name)] = strings.ToUpper(strings.TrimSpace(value))
	}
	if values["log_bin"] != "ON" && values["log_bin"] != "1" {
		return fmt.Errorf("mysql binlog is not enabled")
	}
	if values["binlog_format"] != "ROW" {
		return fmt.Errorf("mysql binlog_format is not ROW")
	}
	if values["binlog_row_image"] != "FULL" {
		return fmt.Errorf("mysql binlog_row_image is not FULL")
	}
	return nil
}

func readMySQLBinlogPosition(ctx context.Context, db *sql.DB) (mysqlBinlogPosition, error) {
	pos, err := readMySQLBinlogPositionWithQuery(ctx, db, "SHOW MASTER STATUS")
	if err == nil {
		return pos, nil
	}
	return readMySQLBinlogPositionWithQuery(ctx, db, "SHOW BINARY LOG STATUS")
}

func readMySQLBinlogPositionWithQuery(ctx context.Context, db *sql.DB, query string) (mysqlBinlogPosition, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return mysqlBinlogPosition{}, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return mysqlBinlogPosition{}, err
	}
	if !rows.Next() {
		return mysqlBinlogPosition{}, fmt.Errorf("mysql binlog status is empty")
	}
	values := make([]sql.NullString, len(cols))
	scanArgs := make([]any, len(cols))
	for i := range values {
		scanArgs[i] = &values[i]
	}
	if err := rows.Scan(scanArgs...); err != nil {
		return mysqlBinlogPosition{}, err
	}
	item := map[string]string{}
	for i, col := range cols {
		if values[i].Valid {
			item[strings.ToLower(col)] = values[i].String
		}
	}
	file := firstNonEmptyRollbackString(item["file"], item["log_name"])
	posRaw := firstNonEmptyRollbackString(item["position"], item["pos"])
	pos, err := strconv.ParseUint(strings.TrimSpace(posRaw), 10, 64)
	if strings.TrimSpace(file) == "" || err != nil || pos == 0 {
		return mysqlBinlogPosition{}, fmt.Errorf("mysql binlog position is unavailable")
	}
	return mysqlBinlogPosition{File: strings.TrimSpace(file), Pos: pos}, nil
}

func runMy2SQLRollback(ctx context.Context, settings *model.PlatformSettings, conn *model.DBConnection, password string, binlogRange repository.RollbackRange, databaseName *string, rowsAffected *int64) (repository.GeneratedRollback, error) {
	timeout := settings.MySQLRollbackGenerationTimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	maxBytes := settings.MySQLRollbackMaxSQLBytes
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}
	commandCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	outputDir, err := os.MkdirTemp("", "maestro-my2sql-*")
	if err != nil {
		return repository.GeneratedRollback{}, fmt.Errorf("create my2sql output dir failed")
	}
	defer os.RemoveAll(outputDir)

	args := []string{
		"-mode", "repl",
		"-work-type", "rollback",
		"-user", conn.Username,
		"-password", password,
		"-host", conn.Host,
		"-port", strconv.Itoa(int(conn.Port)),
		"-start-file", binlogRange.StartFile,
		"-start-pos", strconv.FormatUint(binlogRange.StartPos, 10),
		"-stop-file", binlogRange.EndFile,
		"-stop-pos", strconv.FormatUint(binlogRange.EndPos, 10),
		"-output-dir", outputDir,
	}
	if databaseName != nil && strings.TrimSpace(*databaseName) != "" {
		args = append(args, "-databases", strings.TrimSpace(*databaseName))
	}
	cmd := exec.CommandContext(commandCtx, strings.TrimSpace(settings.MySQLRollbackMy2SQLPath), args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return repository.GeneratedRollback{}, fmt.Errorf("my2sql rollback generation failed: %s", truncate(strings.TrimSpace(stderr.String()), 500))
	}
	rollbackSQL, err := collectMy2SQLOutput(outputDir, stdout.String(), maxBytes)
	if err != nil {
		return repository.GeneratedRollback{}, err
	}
	if strings.TrimSpace(rollbackSQL) == "" {
		return repository.GeneratedRollback{}, fmt.Errorf("my2sql produced empty rollback sql")
	}
	statementCount := countSQLStatements(rollbackSQL)
	confidence := "medium"
	warning := "Review generated rollback SQL before execution; binlog range may include concurrent changes on the same objects."
	if rowsAffected != nil && *rowsAffected >= 0 && statementCount > 0 && int64(statementCount) == *rowsAffected {
		confidence = "high"
	}
	version := my2SQLVersion(ctx, strings.TrimSpace(settings.MySQLRollbackMy2SQLPath))
	return repository.GeneratedRollback{
		Generator:        "my2sql",
		GeneratorVersion: version,
		SQL:              rollbackSQL,
		StatementCount:   statementCount,
		Confidence:       confidence,
		Warning:          warning,
	}, nil
}

func collectMy2SQLOutput(outputDir string, stdout string, maxBytes int) (string, error) {
	files := []string{}
	if entries, err := os.ReadDir(outputDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			files = append(files, filepath.Join(outputDir, entry.Name()))
		}
	}
	sort.Strings(files)
	var out strings.Builder
	total := 0
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		total += len(data)
		if total > maxBytes {
			return "", fmt.Errorf("generated rollback sql exceeds size limit")
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.Write(data)
	}
	if sqlOnly := extractMy2SQLStatements(out.String()); sqlOnly != "" {
		return sqlOnly, nil
	}
	if len([]byte(stdout)) > maxBytes {
		return "", fmt.Errorf("generated rollback sql exceeds size limit")
	}
	if sqlOnly := extractMy2SQLStatements(stdout); sqlOnly != "" {
		return sqlOnly, nil
	}
	return "", fmt.Errorf("my2sql produced no rollback sql statements")
}

func extractMy2SQLStatements(raw string) string {
	var statements []string
	var current strings.Builder
	inStatement := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !inStatement && !isMy2SQLStatementStart(trimmed) {
			continue
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(trimmed)
		inStatement = true
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, strings.TrimSpace(current.String()))
			current.Reset()
			inStatement = false
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		statements = append(statements, strings.TrimSpace(current.String()))
	}
	return strings.Join(statements, "\n\n")
}

func isMy2SQLStatementStart(line string) bool {
	upper := strings.ToUpper(strings.TrimSpace(line))
	return strings.HasPrefix(upper, "INSERT ") ||
		strings.HasPrefix(upper, "UPDATE ") ||
		strings.HasPrefix(upper, "DELETE ") ||
		strings.HasPrefix(upper, "REPLACE ")
}

func my2SQLVersion(ctx context.Context, path string) string {
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, path, "-v").CombinedOutput()
	if err != nil {
		return ""
	}
	return truncate(strings.TrimSpace(string(out)), 120)
}

func isMySQLDMLStatement(sqlText string) bool {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, sqlText)
	if err != nil || len(parsed.Statements) != 1 {
		return false
	}
	switch parsed.Statements[0].Kind {
	case sqlparse.StatementKindInsert, sqlparse.StatementKindUpdate, sqlparse.StatementKindDelete:
		return true
	default:
		return false
	}
}

func countSQLStatements(sqlText string) int {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, sqlText)
	if err == nil && len(parsed.Statements) > 0 {
		return len(parsed.Statements)
	}
	count := 0
	for _, part := range strings.Split(sqlText, ";") {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func firstNonEmptyRollbackString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
