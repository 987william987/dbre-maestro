package handler

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/dbre-maestro/maestro/internal/sqlpolicy"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
	"github.com/jmoiron/sqlx"
	tidbast "github.com/pingcap/tidb/pkg/parser/ast"
	tidbformat "github.com/pingcap/tidb/pkg/parser/format"
	"github.com/redis/go-redis/v9"
)

const (
	reviewPhaseParser     = "parser"
	reviewPhaseValidation = "validation"

	validationStagePrepare = "prepare"
	validationStageExecute = "execute"

	validationMethodStaticRule     = "static_rule"
	validationMethodMySQLExplain   = "explain_mysql"
	validationMethodMySQLShadow    = "shadow_mysql"
	validationMethodRedisWhitelist = "redis_whitelist"
	validationMethodTicketPolicy   = "ticket_policy"
)

type mysqlSchemaInfo struct {
	charset   string
	collation string
}

func (h *TicketHandler) runTicketSQLReview(ctx context.Context, dbConnID uint64, sqlContent string, databaseName *string) []ticketReviewItem {
	return h.runTicketSQLReviewWithType(ctx, dbConnID, model.TicketTypeDDL, sqlContent, databaseName)
}

func (h *TicketHandler) runTicketSQLReviewWithType(ctx context.Context, dbConnID uint64, ticketType model.TicketType, sqlContent string, databaseName *string) []ticketReviewItem {
	if ticketType == model.TicketTypeRedisCommand {
		dbIndex, err := parseRedisDatabaseIndex(databaseName)
		if err != nil {
			return []ticketReviewItem{
				buildValidationReviewItem(1, strings.TrimSpace(sqlContent), validationMethodRedisWhitelist, nil, "redis_command", "redis_command", 0, []string{err.Error()}),
			}
		}
		return h.runRedisCommandTicketReview(sqlContent, dbIndex)
	}

	parsedStatements, dialect, err := h.parseTicketStatements(ctx, dbConnID, sqlContent)
	if err != nil {
		return buildSyntaxErrorReviewItems(err, sqlContent)
	}

	results := buildParserReviewItems(parsedStatements)

	if ticketType == model.TicketTypeDDL || ticketType == model.TicketTypeDML {
		if err := sqlpolicy.CheckTicketStatementKinds(ticketType, parsedStatements); err != nil {
			return append(results, buildTicketKindReviewItems(parsedStatements, err)...)
		}
	}

	rules, err := h.sqlReviewRules.List(ctx)
	if err == nil {
		ruleMap := make(map[string]bool, len(rules))
		var rowThreshold int64 = sqlreview.DefaultRowThreshold
		for _, rule := range rules {
			if !rule.Enabled {
				continue
			}
			ruleMap[rule.RuleName] = true
			if rule.RuleName == "high_row_count" && rule.Threshold != nil {
				rowThreshold = *rule.Threshold
			}
		}
		results = append(results, buildStaticValidationItems(parsedStatements, ruleMap)...)
		if ticketType == model.TicketTypeDML && dialect == sqlparse.DialectMySQL {
			results = append(results, h.runMySQLDMLExplainValidation(ctx, dbConnID, parsedStatements, databaseName, ruleMap, rowThreshold)...)
		}
	}

	if ticketType == model.TicketTypeDDL && dialect == sqlparse.DialectMySQL {
		results = append(results, h.runMySQLDDLShadowValidation(ctx, dbConnID, parsedStatements, databaseName)...)
	}

	return results
}

func (h *TicketHandler) runRedisCommandTicketReview(sqlContent string, _ int) []ticketReviewItem {
	lines := splitRedisTicketCommands(sqlContent)
	if len(lines) == 0 {
		return []ticketReviewItem{
			buildParserErrorReviewItem(1, strings.TrimSpace(sqlContent), "empty redis command"),
		}
	}

	results := make([]ticketReviewItem, 0, len(lines)*2)
	for index, line := range lines {
		seq := index + 1
		cmd, _, err := sqlreview.ParseRedisCommand(line)
		if err != nil {
			results = append(results, buildParserErrorReviewItem(seq, line, err.Error()))
			continue
		}
		statementKind := strings.ToLower(cmd)
		results = append(results, buildParserPassReviewItem(seq, line, statementKind, "redis_command"))
		if err := sqlreview.CheckRedisTicketCommand(line); err != nil {
			results = append(results, buildValidationReviewItem(seq, line, validationMethodRedisWhitelist, nil, statementKind, "redis_command", 0, []string{err.Error()}))
			continue
		}
		results = append(results, buildValidationReviewItem(seq, line, validationMethodRedisWhitelist, nil, statementKind, "redis_command", 0, nil))
	}
	return results
}

func (h *TicketHandler) runMySQLDMLExplainValidation(
	ctx context.Context,
	dbConnID uint64,
	statements []sqlparse.ParsedStatement,
	databaseName *string,
	ruleMap map[string]bool,
	rowThreshold int64,
) []ticketReviewItem {
	if !ruleMap["full_table_scan"] && !ruleMap["high_row_count"] {
		return nil
	}

	queryDB, cleanup, err := h.openTicketSQLDB(ctx, dbConnID, model.DBCredentialRoleReadwrite, databaseName)
	if err != nil {
		return buildBatchValidationErrorItems(statements, validationMethodMySQLExplain, nil, "dml", "table", "open explain connection failed: "+err.Error())
	}
	defer cleanup()

	items := make([]ticketReviewItem, 0, len(statements))
	for _, stmt := range statements {
		if stmt.Kind != sqlparse.StatementKindInsert && stmt.Kind != sqlparse.StatementKindUpdate && stmt.Kind != sqlparse.StatementKindDelete {
			continue
		}
		issues, err := sqlreview.CheckExplain(ctx, queryDB, stmt.RawSQL, rowThreshold)
		statementKind := string(stmt.Kind)
		if err != nil {
			items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodMySQLExplain, nil, statementKind, "table", 0, []string{err.Error()}))
			continue
		}
		maxRows := int64(0)
		messages := make([]string, 0, len(issues))
		for _, issue := range issues {
			if issue.Rows > maxRows {
				maxRows = issue.Rows
			}
			if ruleMap[issue.Kind] {
				messages = append(messages, issue.Msg)
			}
		}
		items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodMySQLExplain, nil, statementKind, "table", maxRows, messages))
	}
	return items
}

func (h *TicketHandler) runMySQLDDLShadowValidation(
	ctx context.Context,
	dbConnID uint64,
	statements []sqlparse.ParsedStatement,
	databaseName *string,
) []ticketReviewItem {
	readonlyDB, cleanup, _, err := h.openTicketSQLDBWithConnection(ctx, dbConnID, model.DBCredentialRoleReadonly, nil)
	if err != nil {
		return buildBatchValidationErrorItems(statements, validationMethodMySQLShadow, stringPtr(validationStagePrepare), "ddl", "unknown", "open metadata connection failed: "+err.Error())
	}
	defer cleanup()

	metaDB := h.shadowValidationDB
	if metaDB == nil {
		metaDB = h.tickets.DB()
	}
	items := make([]ticketReviewItem, 0, len(statements)*2)

	var tableShadowDB string
	var tableCleanup func()
	tableShadowPrepared := false

	for _, stmt := range statements {
		statementKind := string(stmt.Kind)
		target, rewriteSQL, prepErr, execErr := h.prepareMySQLShadowValidation(ctx, readonlyDB, metaDB, stmt, nullableStringValue(databaseName), &tableShadowDB, &tableCleanup, &tableShadowPrepared)
		if prepErr != nil {
			items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodMySQLShadow, stringPtr(validationStagePrepare), statementKind, target.objectType, 0, []string{sanitizeMySQLShadowValidationError(prepErr)}))
			continue
		}
		items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodMySQLShadow, stringPtr(validationStagePrepare), statementKind, target.objectType, 0, nil))
		if execErr != nil {
			items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodMySQLShadow, stringPtr(validationStageExecute), statementKind, target.objectType, 0, []string{sanitizeMySQLShadowValidationError(execErr)}))
			continue
		}
		items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodMySQLShadow, stringPtr(validationStageExecute), statementKind, target.objectType, 0, nil))
		_ = rewriteSQL
	}

	if tableCleanup != nil {
		tableCleanup()
	}
	return items
}

type ddlShadowTarget struct {
	objectType string
}

func (h *TicketHandler) prepareMySQLShadowValidation(
	ctx context.Context,
	readonlyDB *sql.DB,
	metaDB *sqlx.DB,
	stmt sqlparse.ParsedStatement,
	selectedDatabase string,
	tableShadowDB *string,
	tableCleanup *func(),
	tableShadowPrepared *bool,
) (ddlShadowTarget, string, error, error) {
	target := ddlShadowTarget{objectType: inferDDLObjectType(stmt)}
	rewrittenSQL, explicitDatabase, needsClone, err := rewriteMySQLDDLForShadow(stmt, selectedDatabase)
	if err != nil {
		return target, "", err, nil
	}

	switch stmt.AST.(type) {
	case *tidbast.CreateTableStmt, *tidbast.AlterTableStmt, *tidbast.DropTableStmt, *tidbast.TruncateTableStmt:
		if selectedDatabase == "" {
			return target, "", fmt.Errorf("database_name is required for table validation"), nil
		}
		if !*tableShadowPrepared {
			shadowName, cleanup, err := cloneMySQLDatabaseToShadow(ctx, readonlyDB, metaDB, selectedDatabase)
			if err != nil {
				return target, "", err, nil
			}
			*tableShadowDB = shadowName
			*tableCleanup = cleanup
			*tableShadowPrepared = true
		}
		rewrittenSQL, _, _, err = rewriteMySQLDDLForShadow(stmt, *tableShadowDB)
		if err != nil {
			return target, "", err, nil
		}
		if err := executeShadowDDL(ctx, metaDB, *tableShadowDB, rewrittenSQL); err != nil {
			return target, rewrittenSQL, nil, err
		}
		return target, rewrittenSQL, nil, nil
	case *tidbast.AlterDatabaseStmt, *tidbast.DropDatabaseStmt:
		sourceDatabase := explicitDatabase
		if sourceDatabase == "" {
			sourceDatabase = selectedDatabase
		}
		if sourceDatabase == "" {
			return target, "", fmt.Errorf("database_name is required for database validation"), nil
		}
		shadowName, cleanup, err := cloneMySQLDatabaseToShadow(ctx, readonlyDB, metaDB, sourceDatabase)
		if err != nil {
			return target, "", err, nil
		}
		defer cleanup()
		rewrittenSQL, _, _, err = rewriteMySQLDDLForShadow(stmt, shadowName)
		if err != nil {
			return target, "", err, nil
		}
		if err := executeShadowDDL(ctx, metaDB, shadowName, rewrittenSQL); err != nil {
			return target, rewrittenSQL, nil, err
		}
		return target, rewrittenSQL, nil, nil
	case *tidbast.CreateDatabaseStmt:
		if explicitDatabase == "" {
			return target, "", fmt.Errorf("CREATE DATABASE target name is empty"), nil
		}
		exists, err := mysqlDatabaseExists(ctx, readonlyDB, explicitDatabase)
		if err != nil {
			return target, "", fmt.Errorf("check database exists failed: %w", err), nil
		}
		if exists {
			return target, "", nil, fmt.Errorf("database %q already exists", explicitDatabase)
		}
		shadowName := generateShadowDatabaseName("shadow_create_db")
		rewrittenSQL, _, _, err = rewriteMySQLDDLForShadow(stmt, shadowName)
		if err != nil {
			return target, "", err, nil
		}
		if err := executeStandaloneShadowDDL(ctx, metaDB, rewrittenSQL, shadowName); err != nil {
			return target, rewrittenSQL, nil, err
		}
		return target, rewrittenSQL, nil, nil
	default:
		if needsClone {
			return target, "", fmt.Errorf("unsupported DDL object for shadow validation"), nil
		}
		return target, "", fmt.Errorf("unsupported DDL object for shadow validation"), nil
	}
}

func cloneMySQLDatabaseToShadow(ctx context.Context, readonlyDB *sql.DB, metaDB *sqlx.DB, sourceDatabase string) (string, func(), error) {
	schemaInfo, err := loadMySQLSchemaInfo(ctx, readonlyDB, sourceDatabase)
	if err != nil {
		return "", nil, err
	}
	shadowName := generateShadowDatabaseName(sourceDatabase)
	createSQL := fmt.Sprintf("CREATE DATABASE %s CHARACTER SET %s COLLATE %s", quoteMySQLIdentifier(shadowName), quoteMySQLIdentifier(schemaInfo.charset), quoteMySQLIdentifier(schemaInfo.collation))
	if _, err := metaDB.ExecContext(ctx, createSQL); err != nil {
		return "", nil, fmt.Errorf("create shadow database failed: %w", err)
	}
	cleanup := func() {
		_, _ = metaDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+quoteMySQLIdentifier(shadowName))
	}

	tables, err := listMySQLBaseTables(ctx, readonlyDB, sourceDatabase)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	for _, tableName := range tables {
		createTableSQL, err := loadMySQLCreateTableSQL(ctx, readonlyDB, sourceDatabase, tableName)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		if err := executeShadowDDL(ctx, metaDB, shadowName, createTableSQL); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("clone table %s failed: %w", tableName, err)
		}
	}
	return shadowName, cleanup, nil
}

func sanitizeMySQLShadowValidationError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	lower := strings.ToLower(message)
	if strings.Contains(lower, "create shadow database failed:") && strings.Contains(lower, "access denied") {
		slog.Warn("mysql shadow validation unavailable: create shadow database permission denied", "err", message)
		return "shadow validation is not available because the platform validation database privilege is not configured"
	}
	return message
}

func executeShadowDDL(ctx context.Context, metaDB *sqlx.DB, shadowDatabase, sqlText string) error {
	conn, err := metaDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	var originalDatabase sql.NullString
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&originalDatabase); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SET foreign_key_checks = 1")
		if originalDatabase.Valid && strings.TrimSpace(originalDatabase.String) != "" {
			_, _ = conn.ExecContext(context.Background(), "USE "+quoteMySQLIdentifier(originalDatabase.String))
		}
	}()

	if _, err := conn.ExecContext(ctx, "USE "+quoteMySQLIdentifier(shadowDatabase)); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "SET foreign_key_checks = 0"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, sqlText); err != nil {
		return err
	}
	return nil
}

func executeStandaloneShadowDDL(ctx context.Context, metaDB *sqlx.DB, sqlText, createdDatabase string) error {
	if _, err := metaDB.ExecContext(ctx, sqlText); err != nil {
		return err
	}
	_, _ = metaDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+quoteMySQLIdentifier(createdDatabase))
	return nil
}

func rewriteMySQLDDLForShadow(stmt sqlparse.ParsedStatement, shadowDatabase string) (rewrittenSQL string, explicitDatabase string, needsClone bool, err error) {
	node, ok := stmt.AST.(tidbast.StmtNode)
	if !ok || node == nil {
		return "", "", false, fmt.Errorf("mysql ddl AST is unavailable")
	}

	switch ddl := node.(type) {
	case *tidbast.CreateTableStmt:
		needsClone = true
		ddl.Table.Schema = tidbast.NewCIStr(shadowDatabase)
		return restoreMySQLNode(ddl), "", needsClone, nil
	case *tidbast.AlterTableStmt:
		needsClone = true
		ddl.Table.Schema = tidbast.NewCIStr(shadowDatabase)
		return restoreMySQLNode(ddl), "", needsClone, nil
	case *tidbast.DropTableStmt:
		needsClone = true
		for _, table := range ddl.Tables {
			table.Schema = tidbast.NewCIStr(shadowDatabase)
		}
		return restoreMySQLNode(ddl), "", needsClone, nil
	case *tidbast.TruncateTableStmt:
		needsClone = true
		ddl.Table.Schema = tidbast.NewCIStr(shadowDatabase)
		return restoreMySQLNode(ddl), "", needsClone, nil
	case *tidbast.RenameTableStmt:
		needsClone = true
		for _, tablePair := range ddl.TableToTables {
			if tablePair == nil {
				continue
			}
			if tablePair.OldTable != nil {
				tablePair.OldTable.Schema = tidbast.NewCIStr(shadowDatabase)
			}
			if tablePair.NewTable != nil {
				tablePair.NewTable.Schema = tidbast.NewCIStr(shadowDatabase)
			}
		}
		return restoreMySQLNode(ddl), "", needsClone, nil
	case *tidbast.AlterDatabaseStmt:
		needsClone = true
		explicitDatabase = ddl.Name.O
		ddl.AlterDefaultDatabase = false
		ddl.Name = tidbast.NewCIStr(shadowDatabase)
		return restoreMySQLNode(ddl), explicitDatabase, needsClone, nil
	case *tidbast.DropDatabaseStmt:
		needsClone = true
		explicitDatabase = ddl.Name.O
		ddl.Name = tidbast.NewCIStr(shadowDatabase)
		return restoreMySQLNode(ddl), explicitDatabase, needsClone, nil
	case *tidbast.CreateDatabaseStmt:
		explicitDatabase = ddl.Name.O
		ddl.Name = tidbast.NewCIStr(shadowDatabase)
		return restoreMySQLNode(ddl), explicitDatabase, false, nil
	default:
		return "", "", false, fmt.Errorf("unsupported DDL object for shadow validation")
	}
}

func restoreMySQLNode(node tidbast.StmtNode) string {
	var builder strings.Builder
	restoreCtx := tidbformat.NewRestoreCtx(tidbformat.DefaultRestoreFlags, &builder)
	if err := node.Restore(restoreCtx); err != nil {
		return ""
	}
	return strings.TrimSpace(builder.String())
}

func inferDDLObjectType(stmt sqlparse.ParsedStatement) string {
	switch stmt.AST.(type) {
	case *tidbast.CreateDatabaseStmt, *tidbast.AlterDatabaseStmt, *tidbast.DropDatabaseStmt:
		return "database"
	case *tidbast.CreateTableStmt, *tidbast.AlterTableStmt, *tidbast.DropTableStmt, *tidbast.TruncateTableStmt:
		return "table"
	case *tidbast.RenameTableStmt:
		return "table"
	default:
		return "unknown"
	}
}

func loadMySQLSchemaInfo(ctx context.Context, db *sql.DB, databaseName string) (*mysqlSchemaInfo, error) {
	var info mysqlSchemaInfo
	err := db.QueryRowContext(ctx,
		`SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME
		 FROM information_schema.SCHEMATA
		 WHERE SCHEMA_NAME = ?`,
		databaseName,
	).Scan(&info.charset, &info.collation)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("target database %q does not exist", databaseName)
	}
	if err != nil {
		return nil, fmt.Errorf("load database schema info failed: %w", err)
	}
	return &info, nil
}

func listMySQLBaseTables(ctx context.Context, db *sql.DB, databaseName string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT TABLE_NAME
		 FROM information_schema.TABLES
		 WHERE TABLE_SCHEMA = ?
		   AND TABLE_TYPE = 'BASE TABLE'
		 ORDER BY TABLE_NAME`,
		databaseName,
	)
	if err != nil {
		return nil, fmt.Errorf("list base tables failed: %w", err)
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		items = append(items, tableName)
	}
	return items, rows.Err()
}

func loadMySQLCreateTableSQL(ctx context.Context, db *sql.DB, databaseName, tableName string) (string, error) {
	query := fmt.Sprintf("SHOW CREATE TABLE %s.%s", quoteMySQLIdentifier(databaseName), quoteMySQLIdentifier(tableName))
	var name string
	var createSQL string
	if err := db.QueryRowContext(ctx, query).Scan(&name, &createSQL); err != nil {
		return "", fmt.Errorf("load create table for %s failed: %w", tableName, err)
	}
	return createSQL, nil
}

func mysqlDatabaseExists(ctx context.Context, db *sql.DB, databaseName string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		 FROM information_schema.SCHEMATA
		 WHERE SCHEMA_NAME = ?`,
		databaseName,
	).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func generateShadowDatabaseName(seed string) string {
	normalized := strings.ToLower(strings.TrimSpace(seed))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, ".", "_")
	if normalized == "" {
		normalized = "shadow"
	}
	if len(normalized) > 20 {
		normalized = normalized[:20]
	}
	return fmt.Sprintf("shadow_%s_%d", normalized, time.Now().UTC().UnixMilli())
}

func splitRedisTicketCommands(sqlContent string) []string {
	lines := strings.Split(sqlContent, "\n")
	items := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		items = append(items, trimmed)
	}
	return items
}

func parseRedisDatabaseIndex(databaseName *string) (int, error) {
	value := strings.TrimSpace(nullableStringValue(databaseName))
	if value == "" {
		return 0, fmt.Errorf("database_name is required")
	}
	index, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("redis database index must be an integer")
	}
	if index < 0 || index > 15 {
		return 0, fmt.Errorf("redis database index must be between 0 and 15")
	}
	return index, nil
}

func (h *TicketHandler) runTicketRedisCommands(ticket *model.Ticket, executorID uint64) {
	ctx := context.Background()
	executorName, err := h.lookupUsername(ctx, executorID)
	if err != nil {
		executorName = ""
	}

	conn, err := h.dbConns.GetByID(ctx, *ticket.DBConnectionID)
	if err != nil || conn == nil {
		h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "redis connection not found")
		return
	}

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadwrite)
	if err != nil {
		h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "resolve redis credential failed")
		return
	}

	dbIndex, err := parseRedisDatabaseIndex(ticket.DatabaseName)
	if err != nil {
		h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, err.Error())
		return
	}

	commands := splitRedisTicketCommands(ticket.SQLContent)
	finalStatus := model.TicketStatusCompleted
	for index, commandLine := range commands {
		current, err := h.tickets.GetByID(ctx, ticket.ID)
		if err == nil && current != nil && current.Status == model.TicketStatusStopped {
			return
		}

		execID, err := h.tickets.CreateExecution(ctx, &model.TicketExecution{
			TicketID: ticket.ID,
			Seq:      index + 1,
			SQLStmt:  commandLine,
		})
		if err != nil {
			h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "record redis execution failed")
			return
		}
		_ = h.tickets.MarkExecutionRunning(ctx, execID)

		cmd, args, parseErr := sqlreview.ParseRedisCommand(commandLine)
		if parseErr != nil {
			msg := parseErr.Error()
			_ = h.tickets.MarkExecutionDone(ctx, execID, nil, 0, &msg)
			finalStatus = model.TicketStatusFailed
			break
		}
		ifaces := make([]interface{}, len(args))
		for i, arg := range args {
			if cmd == "EXPIRE" && i == 1 {
				if seconds, err := strconv.Atoi(arg); err == nil {
					ifaces[i] = seconds
				} else {
					ifaces[i] = arg
				}
				continue
			}
			ifaces[i] = arg
		}

		startedAt := time.Now()
		_, execErr := pool.RedisGlobal().DoInDB(ctx, pool.RedisConnOptions{
			ConnID:   resolvedConn.ID,
			Host:     resolvedConn.Host,
			Port:     resolvedConn.Port,
			Username: resolvedConn.Username,
			Password: password,
			DB:       dbIndex,
			SSLMode:  resolvedConn.SSLMode,
		}, append([]interface{}{cmd}, ifaces...)...)
		durationMs := time.Since(startedAt).Milliseconds()
		if execErr != nil && execErr != redis.Nil {
			msg := execErr.Error()
			_ = h.tickets.MarkExecutionDone(ctx, execID, nil, durationMs, &msg)
			finalStatus = model.TicketStatusFailed
			break
		}
		_ = h.tickets.MarkExecutionDone(ctx, execID, nil, durationMs, nil)
	}

	h.finishTicket(ctx, ticket.ID, finalStatus, "")

	actionType := "ticket_execute_complete"
	if finalStatus == model.TicketStatusFailed {
		actionType = "ticket_execute_failed"
	}
	h.audit.Log(ctx, repository.AuditEntry{
		ActorID:      &executorID,
		ActorName:    executorName,
		ActionType:   actionType,
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
		Details:      map[string]string{"status": string(finalStatus)},
	})

	if finalStatus == model.TicketStatusCompleted {
		h.dispatchTicketNotification(ctx, ticket, ticketEventCompleted, &executorID, "工單已執行完成。")
	} else {
		h.dispatchTicketNotification(ctx, ticket, ticketEventExecutionFailed, &executorID, "工單執行失敗，請查看 execution log。")
	}
}

func buildParserReviewItems(statements []sqlparse.ParsedStatement) []ticketReviewItem {
	items := make([]ticketReviewItem, 0, len(statements))
	for _, stmt := range statements {
		items = append(items, buildParserPassReviewItem(stmt.Seq, stmt.RawSQL, string(stmt.Kind), inferDDLObjectType(stmt)))
	}
	return items
}

func buildStaticValidationItems(statements []sqlparse.ParsedStatement, ruleMap map[string]bool) []ticketReviewItem {
	items := make([]ticketReviewItem, 0, len(statements))
	for _, stmt := range statements {
		issues := sqlreview.RunStaticChecksParsed(stmt, ruleMap)
		items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodStaticRule, nil, string(stmt.Kind), inferDDLObjectType(stmt), 0, issues))
	}
	return items
}

func buildBatchValidationErrorItems(statements []sqlparse.ParsedStatement, method string, stage *string, statementKind, objectType, message string) []ticketReviewItem {
	items := make([]ticketReviewItem, 0, len(statements))
	for _, stmt := range statements {
		items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, method, stage, statementKind, objectType, 0, []string{message}))
	}
	return items
}

func buildParserPassReviewItem(seq int, stmt, statementKind, objectType string) ticketReviewItem {
	return ticketReviewItem{
		Seq:           seq,
		SQLStmt:       stmt,
		Phase:         reviewPhaseParser,
		StatementKind: optionalTrimmedString(statementKind),
		ObjectType:    optionalTrimmedString(objectType),
		Status:        "pass",
	}
}

func buildParserErrorReviewItem(seq int, stmt, message string) ticketReviewItem {
	return ticketReviewItem{
		Seq:     seq,
		SQLStmt: stmt,
		Phase:   reviewPhaseParser,
		Status:  "error",
		Message: optionalTrimmedString(message),
	}
}

func buildValidationReviewItem(seq int, stmt, method string, stage *string, statementKind, objectType string, scanRows int64, issues []string) ticketReviewItem {
	item := ticketReviewItem{
		Seq:              seq,
		SQLStmt:          stmt,
		Phase:            reviewPhaseValidation,
		ValidationStage:  stage,
		StatementKind:    optionalTrimmedString(statementKind),
		ObjectType:       optionalTrimmedString(objectType),
		ValidationMethod: optionalTrimmedString(method),
		ScanRows:         scanRows,
		Status:           "pass",
	}
	if len(issues) > 0 {
		item.Status = "error"
		message := strings.Join(issues, "; ")
		item.Message = &message
	}
	return item
}

func stringPtr(value string) *string {
	return &value
}
