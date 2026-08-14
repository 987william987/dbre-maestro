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
	tidbast "github.com/pingcap/tidb/pkg/parser/ast"
	tidbformat "github.com/pingcap/tidb/pkg/parser/format"
)

type mysqlRollbackRuntime struct {
	supported bool
	reason    string
	method    string
	conn      *model.DBConnection
	password  string
	start     repository.RollbackRange
	prior     *mysqlPriorBackupPlan
	fallback  string
}

type mysqlBinlogPosition struct {
	File string
	Pos  uint64
}

const mysqlPriorBackupDatabase = "maestro_rollback"

type mysqlPriorBackupPlan struct {
	kind           sqlparse.StatementKind
	database       string
	backupDatabase string
	targets        []mysqlPriorBackupTablePlan
	table          string
	alias          string
	tableRefsSQL   string
	cteSQL         string
	backupTable    string
	whereSQL       string
	orderSQL       string
	limitSQL       string
	columns        []string
	primaryKeys    []string
	updateColumns  []string
	backupRowCount int64
	backupReady    bool
}

type mysqlPriorBackupTablePlan struct {
	database       string
	table          string
	qualifier      string
	backupTable    string
	columns        []string
	primaryKeys    []string
	updateColumns  []string
	backupRowCount int64
	backupReady    bool
}

func (h *TicketHandler) prepareMySQLRollbackRuntime(ctx context.Context, ticket *model.Ticket, execRow model.TicketExecution) mysqlRollbackRuntime {
	if h == nil || h.rollbacks == nil || h.settings == nil || h.dbConns == nil {
		return mysqlRollbackRuntime{reason: "rollback service is not configured"}
	}
	if ticket == nil || ticket.DBConnectionID == nil {
		return mysqlRollbackRuntime{reason: "ticket has no target db connection"}
	}
	kind, err := mysqlRollbackStatementKind(execRow.SQLStmt)
	if err != nil {
		return mysqlRollbackRuntime{reason: err.Error()}
	}
	settings, err := h.settings.Get(ctx)
	if err != nil {
		return mysqlRollbackRuntime{reason: "load rollback settings failed"}
	}
	if settings == nil || !settings.MySQLRollbackEnabled {
		return mysqlRollbackRuntime{reason: "mysql rollback is disabled"}
	}

	conn, err := h.dbConns.GetByID(ctx, *ticket.DBConnectionID)
	if err != nil || conn == nil {
		return mysqlRollbackRuntime{reason: "db connection not found"}
	}
	if conn.DBType != "mysql" {
		return mysqlRollbackRuntime{reason: "only mysql connections are supported for rollback"}
	}

	engine := model.NormalizeMySQLRollbackEngine(settings.MySQLRollbackEngine)
	if engine == model.MySQLRollbackEngineMy2SQL {
		return h.prepareMy2SQLRollbackRuntime(ctx, ticket, settings, conn, "")
	}

	if kind == sqlparse.StatementKindUpdate || kind == sqlparse.StatementKindDelete {
		plan, err := buildMySQLPriorBackupPlan(ticket, execRow, kind)
		if err != nil {
			if engine == model.MySQLRollbackEngineHybrid {
				return h.prepareMy2SQLRollbackRuntime(ctx, ticket, settings, conn, "prior backup parser fallback: "+err.Error())
			}
			return mysqlRollbackRuntime{reason: err.Error()}
		}
		return mysqlRollbackRuntime{supported: true, method: "prior_backup", prior: plan}
	}

	if engine == model.MySQLRollbackEnginePriorBackup {
		return mysqlRollbackRuntime{reason: "prior backup parser only supports mysql update/delete statements"}
	}
	return h.prepareMy2SQLRollbackRuntime(ctx, ticket, settings, conn, "")
}

func (h *TicketHandler) prepareMy2SQLRollbackRuntime(ctx context.Context, ticket *model.Ticket, settings *model.PlatformSettings, conn *model.DBConnection, fallback string) mysqlRollbackRuntime {
	if strings.TrimSpace(settings.MySQLRollbackMy2SQLPath) == "" {
		return mysqlRollbackRuntime{reason: joinRollbackReasons(fallback, "my2sql path is not configured")}
	}
	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleRollback)
	if err != nil {
		return mysqlRollbackRuntime{reason: joinRollbackReasons(fallback, "rollback credential is not configured")}
	}
	if ticket.DatabaseName != nil && strings.TrimSpace(*ticket.DatabaseName) != "" {
		dbName := strings.TrimSpace(*ticket.DatabaseName)
		resolvedConn.DatabaseName = &dbName
	}

	db, cleanup, err := openResolvedSQLDB(ctx, resolvedConn, password)
	if err != nil {
		return mysqlRollbackRuntime{reason: joinRollbackReasons(fallback, "rollback credential connection failed")}
	}
	defer cleanup()
	if err := checkMySQLRollbackVariables(ctx, db); err != nil {
		return mysqlRollbackRuntime{reason: joinRollbackReasons(fallback, err.Error())}
	}
	pos, err := readMySQLBinlogPosition(ctx, db)
	if err != nil {
		return mysqlRollbackRuntime{reason: joinRollbackReasons(fallback, "read mysql binlog start position failed")}
	}
	return mysqlRollbackRuntime{
		supported: true,
		method:    "my2sql",
		conn:      resolvedConn,
		password:  password,
		fallback:  fallback,
		start: repository.RollbackRange{
			StartFile: pos.File,
			StartPos:  pos.Pos,
		},
	}
}

func joinRollbackReasons(parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return strings.Join(values, "; ")
}

func (h *TicketHandler) finalizeMySQLRollback(ctx context.Context, ticket *model.Ticket, execRow model.TicketExecution, runtime mysqlRollbackRuntime, rowsAffected *int64) {
	if h == nil || h.rollbacks == nil || ticket == nil || ticket.DBConnectionID == nil {
		return
	}
	if !runtime.supported {
		_ = h.rollbacks.MarkUnsupported(ctx, ticket, execRow, runtime.reason)
		return
	}
	if runtime.method == "prior_backup" {
		if runtime.prior == nil {
			_ = h.rollbacks.MarkGenerationFailed(ctx, ticket, execRow, "prior backup plan is unavailable")
			return
		}
		if !runtime.prior.backupReady {
			return
		}
		generated := generateMySQLPriorBackupRollback(runtime.prior)
		if err := h.rollbacks.MarkGeneratedForExecution(ctx, ticket, execRow, generated); err != nil {
			slog.Warn("ticket prior backup rollback save failed", "ticket_id", ticket.ID, "execution_id", execRow.ID, "err", err)
		}
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
	h.runMySQLRollbackGenerationAsync(ticket, execRow, settings, runtime, binlogRange, rowsAffected)
}

func (h *TicketHandler) runMySQLRollbackGenerationAsync(ticket *model.Ticket, execRow model.TicketExecution, settings *model.PlatformSettings, runtime mysqlRollbackRuntime, binlogRange repository.RollbackRange, rowsAffected *int64) {
	if h == nil || h.rollbacks == nil || ticket == nil || settings == nil {
		return
	}
	ticketCopy := *ticket
	settingsCopy := *settings
	runtimeCopy := runtime
	if runtime.conn != nil {
		connCopy := *runtime.conn
		runtimeCopy.conn = &connCopy
	}
	var rowsAffectedCopy *int64
	if rowsAffected != nil {
		value := *rowsAffected
		rowsAffectedCopy = &value
	}

	go func() {
		release := h.acquireTicketRollbackJobSlot(ticketCopy.ID)
		defer release()

		jobCtx := context.Background()
		generated, err := runMy2SQLRollback(jobCtx, &settingsCopy, runtimeCopy.conn, runtimeCopy.password, binlogRange, ticketCopy.DatabaseName, rowsAffectedCopy, runtimeCopy.fallback)
		if err != nil {
			_ = h.rollbacks.MarkFailed(jobCtx, execRow.ID, err.Error())
			slog.Warn("ticket rollback generation failed", "ticket_id", ticketCopy.ID, "execution_id", execRow.ID, "err", err)
			h.publishTicketUpdate(jobCtx, &ticketCopy, nil)
			return
		}
		if err := h.rollbacks.MarkGenerated(jobCtx, execRow.ID, generated); err != nil {
			slog.Warn("ticket rollback save failed", "ticket_id", ticketCopy.ID, "execution_id", execRow.ID, "err", err)
			h.publishTicketUpdate(jobCtx, &ticketCopy, nil)
			return
		}
		slog.Info("ticket rollback generation completed", "ticket_id", ticketCopy.ID, "execution_id", execRow.ID, "seq", execRow.Seq, "statement_count", generated.StatementCount)
		h.publishTicketUpdate(jobCtx, &ticketCopy, nil)
	}()
}

func (h *TicketHandler) acquireTicketRollbackJobSlot(ticketID uint64) func() {
	if h == nil || ticketID == 0 {
		return func() {}
	}
	h.rollbackJobsMu.Lock()
	if h.rollbackJobs == nil {
		h.rollbackJobs = make(map[uint64]*ticketRollbackJobLimiter)
	}
	limiter := h.rollbackJobs[ticketID]
	if limiter == nil {
		limiter = &ticketRollbackJobLimiter{slots: make(chan struct{}, 2)}
		h.rollbackJobs[ticketID] = limiter
	}
	limiter.refs++
	h.rollbackJobsMu.Unlock()

	limiter.slots <- struct{}{}
	return func() {
		<-limiter.slots
		h.rollbackJobsMu.Lock()
		limiter.refs--
		if limiter.refs == 0 && len(limiter.slots) == 0 {
			delete(h.rollbackJobs, ticketID)
		}
		h.rollbackJobsMu.Unlock()
	}
}

func buildMySQLPriorBackupPlan(ticket *model.Ticket, execRow model.TicketExecution, kind sqlparse.StatementKind) (*mysqlPriorBackupPlan, error) {
	if ticket == nil || ticket.DatabaseName == nil || strings.TrimSpace(*ticket.DatabaseName) == "" {
		return nil, fmt.Errorf("prior backup requires a target database")
	}
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, execRow.SQLStmt)
	if err != nil {
		return nil, fmt.Errorf("parse prior backup statement failed: %w", err)
	}
	if len(parsed.Statements) != 1 {
		return nil, fmt.Errorf("prior backup supports one statement per execution")
	}
	stmt := parsed.Statements[0]
	if stmt.Kind != kind {
		return nil, fmt.Errorf("prior backup statement kind mismatch")
	}
	dbName := strings.TrimSpace(*ticket.DatabaseName)
	plan := &mysqlPriorBackupPlan{
		kind:           kind,
		database:       dbName,
		backupDatabase: mysqlPriorBackupDatabase,
	}
	baseBackupTable := fmt.Sprintf("_maestro_rb_t%d_e%d_s%d", ticket.ID, execRow.ID, execRow.Seq)
	switch node := stmt.AST.(type) {
	case *tidbast.UpdateStmt:
		cteSQL, cteNames, err := mysqlPriorBackupCTE(node.With)
		if err != nil {
			return nil, err
		}
		plan.cteSQL = cteSQL
		if node.Where == nil {
			return nil, fmt.Errorf("prior backup requires UPDATE to have a WHERE clause")
		}
		tableRefsSQL, err := restoreMySQLRollbackNode(node.TableRefs)
		if err != nil {
			return nil, fmt.Errorf("restore UPDATE table refs failed")
		}
		plan.tableRefsSQL = tableRefsSQL
		orderSQL, limitSQL, err := mysqlPriorBackupOrderLimitSQL(node.Order, node.Limit)
		if err != nil {
			return nil, err
		}
		plan.orderSQL = orderSQL
		plan.limitSQL = limitSQL
		targets, err := mysqlPriorBackupUpdateTargets(node)
		if err != nil {
			return nil, err
		}
		plan.targets, err = mysqlPriorBackupTablePlans(dbName, baseBackupTable, targets, cteNames)
		if err != nil {
			return nil, err
		}
		whereSQL, err := restoreMySQLRollbackNode(node.Where)
		if err != nil {
			return nil, fmt.Errorf("restore UPDATE WHERE failed")
		}
		plan.whereSQL = whereSQL
	case *tidbast.DeleteStmt:
		cteSQL, cteNames, err := mysqlPriorBackupCTE(node.With)
		if err != nil {
			return nil, err
		}
		plan.cteSQL = cteSQL
		if node.Where == nil {
			return nil, fmt.Errorf("prior backup requires DELETE to have a WHERE clause")
		}
		tableRefsSQL, err := restoreMySQLRollbackNode(node.TableRefs)
		if err != nil {
			return nil, fmt.Errorf("restore DELETE table refs failed")
		}
		plan.tableRefsSQL = tableRefsSQL
		orderSQL, limitSQL, err := mysqlPriorBackupOrderLimitSQL(node.Order, node.Limit)
		if err != nil {
			return nil, err
		}
		plan.orderSQL = orderSQL
		plan.limitSQL = limitSQL
		targets, err := mysqlPriorBackupDeleteTargets(node)
		if err != nil {
			return nil, err
		}
		plan.targets, err = mysqlPriorBackupTablePlans(dbName, baseBackupTable, targets, cteNames)
		if err != nil {
			return nil, err
		}
		whereSQL, err := restoreMySQLRollbackNode(node.Where)
		if err != nil {
			return nil, fmt.Errorf("restore DELETE WHERE failed")
		}
		plan.whereSQL = whereSQL
	default:
		return nil, fmt.Errorf("prior backup only supports UPDATE and DELETE")
	}
	plan.syncFirstTarget()
	if strings.TrimSpace(plan.table) == "" {
		return nil, fmt.Errorf("prior backup could not determine target table")
	}
	return plan, nil
}

func (p *mysqlPriorBackupPlan) syncFirstTarget() {
	if p == nil || len(p.targets) == 0 {
		return
	}
	first := p.targets[0]
	p.table = first.table
	p.alias = first.qualifier
	p.backupTable = first.backupTable
	p.columns = first.columns
	p.primaryKeys = first.primaryKeys
	p.updateColumns = first.updateColumns
	p.backupRowCount = first.backupRowCount
}

func (h *TicketHandler) executeMySQLPriorBackup(ctx context.Context, conn *sql.Conn, ticket *model.Ticket, plan *mysqlPriorBackupPlan) error {
	if h == nil || conn == nil || ticket == nil || plan == nil {
		return fmt.Errorf("prior backup is not configured")
	}
	if len(plan.targets) == 0 {
		return fmt.Errorf("prior backup could not determine target table")
	}
	if _, err := conn.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+quoteMySQLIdentifier(plan.backupDatabase)); err != nil {
		return fmt.Errorf("create prior backup database failed: %w", err)
	}

	for i := range plan.targets {
		target := &plan.targets[i]
		columns, primaryKeys, err := loadMySQLRollbackTableMetadata(ctx, conn, target.database, target.table)
		if err != nil {
			return err
		}
		if len(columns) == 0 {
			return fmt.Errorf("prior backup found no regular columns for %s.%s", target.database, target.table)
		}
		target.columns = columns
		target.primaryKeys = primaryKeys
		if plan.kind == sqlparse.StatementKindUpdate {
			if len(primaryKeys) == 0 {
				return fmt.Errorf("prior backup requires UPDATE target table to have a primary key")
			}
			if mysqlColumnsOverlap(primaryKeys, target.updateColumns) {
				return fmt.Errorf("prior backup does not support UPDATE that modifies primary key columns")
			}
		}

		sourceTable := qualifiedMySQLName(target.database, target.table)
		backupTable := qualifiedMySQLName(plan.backupDatabase, target.backupTable)
		if _, err := conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+backupTable); err != nil {
			return fmt.Errorf("drop existing prior backup table failed: %w", err)
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s LIKE %s", backupTable, sourceTable)); err != nil {
			return fmt.Errorf("create prior backup table failed: %w", err)
		}
		insertSQL := buildMySQLPriorBackupInsertSQLForTarget(plan, *target)
		res, err := conn.ExecContext(ctx, insertSQL)
		if err != nil {
			return fmt.Errorf("populate prior backup table failed: %w", err)
		}
		if rows, err := res.RowsAffected(); err == nil {
			target.backupRowCount = rows
		}
		target.backupReady = true
	}
	plan.syncFirstTarget()
	plan.backupReady = true
	return nil
}

func generateMySQLPriorBackupRollback(plan *mysqlPriorBackupPlan) repository.GeneratedRollback {
	sqlText := buildMySQLPriorBackupRestoreSQL(plan)
	var warnings []string
	for _, target := range plan.targets {
		message := fmt.Sprintf("%s.%s rows=%d", plan.backupDatabase, target.backupTable, target.backupRowCount)
		warnings = append(warnings, message)
	}
	warning := "Rollback SQL was generated from prior backup tables. Review generated SQL before execution."
	if len(warnings) > 0 {
		warning += " Backup tables: " + strings.Join(warnings, "; ") + "."
	}
	if len(plan.targets) > 1 {
		warning += " Multiple target tables are restored as separate statements; review execution order before submitting rollback."
	}
	return repository.GeneratedRollback{
		Generator:        "prior_backup",
		GeneratorVersion: "maestro-v1",
		SQL:              sqlText,
		StatementCount:   len(plan.targets),
		Confidence:       "high",
		Warning:          warning,
	}
}

type mysqlPriorBackupTarget struct {
	table         *tidbast.TableName
	qualifier     string
	updateColumns []string
}

type mysqlPriorBackupTableRef struct {
	table     *tidbast.TableName
	qualifier string
}

func mysqlPriorBackupUpdateTargets(stmt *tidbast.UpdateStmt) ([]mysqlPriorBackupTarget, error) {
	if stmt == nil {
		return nil, fmt.Errorf("prior backup could not determine UPDATE target table")
	}
	refs, err := mysqlPriorBackupTableRefMap(stmt.TableRefs)
	if err != nil {
		return nil, err
	}
	joined := mysqlPriorBackupHasJoin(stmt.TableRefs)
	targets := map[string]*mysqlPriorBackupTarget{}
	var order []string
	for _, assignment := range stmt.List {
		if assignment == nil || assignment.Column == nil {
			continue
		}
		col := assignment.Column
		if col.Schema.O != "" {
			return nil, fmt.Errorf("prior backup does not support schema-qualified UPDATE columns")
		}
		var current mysqlPriorBackupTarget
		if strings.TrimSpace(col.Table.O) == "" {
			if joined {
				return nil, fmt.Errorf("prior backup joined UPDATE requires SET columns to be table-qualified")
			}
			first, err := mysqlPriorBackupFirstTableRef(stmt.TableRefs)
			if err != nil {
				return nil, err
			}
			current = mysqlPriorBackupTarget{table: first.table, qualifier: first.qualifier}
		} else {
			ref, ok := refs[strings.ToLower(strings.TrimSpace(col.Table.O))]
			if !ok {
				return nil, fmt.Errorf("prior backup UPDATE column %s references unknown table %s", col.Name.O, col.Table.O)
			}
			current = mysqlPriorBackupTarget{table: ref.table, qualifier: ref.qualifier}
		}
		if current.table == nil {
			return nil, fmt.Errorf("prior backup could not determine UPDATE target table")
		}
		key := mysqlPriorBackupTargetKey(current)
		if existing, ok := targets[key]; ok && !mysqlPriorBackupSameTarget(*existing, current) {
			return nil, fmt.Errorf("prior backup could not uniquely determine UPDATE target table")
		}
		if mysqlPriorBackupTargetsSamePhysicalTable(targets, current, key) {
			return nil, fmt.Errorf("prior backup does not support updating multiple aliases of the same table")
		}
		name := strings.TrimSpace(col.Name.O)
		if name == "" {
			continue
		}
		target := targets[key]
		if target == nil {
			copyTarget := current
			target = &copyTarget
			targets[key] = target
			order = append(order, key)
		}
		if !mysqlStringSliceContainsFold(target.updateColumns, name) {
			target.updateColumns = append(target.updateColumns, name)
		}
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("prior backup could not determine UPDATE target table")
	}
	result := make([]mysqlPriorBackupTarget, 0, len(order))
	for _, key := range order {
		target := targets[key]
		if target == nil || len(target.updateColumns) == 0 {
			return nil, fmt.Errorf("prior backup could not determine UPDATE columns")
		}
		result = append(result, *target)
	}
	return result, nil
}

func mysqlPriorBackupDeleteTargets(stmt *tidbast.DeleteStmt) ([]mysqlPriorBackupTarget, error) {
	if stmt == nil {
		return nil, fmt.Errorf("prior backup could not determine DELETE target table")
	}
	if stmt.IsMultiTable {
		if stmt.Tables == nil || len(stmt.Tables.Tables) == 0 {
			return nil, fmt.Errorf("prior backup could not determine DELETE target table")
		}
		refs, err := mysqlPriorBackupTableRefMap(stmt.TableRefs)
		if err != nil {
			return nil, err
		}
		targets := make([]mysqlPriorBackupTarget, 0, len(stmt.Tables.Tables))
		for _, targetName := range stmt.Tables.Tables {
			if targetName == nil {
				return nil, fmt.Errorf("prior backup could not determine DELETE target table")
			}
			key := strings.ToLower(strings.TrimSpace(targetName.Name.O))
			ref, ok := refs[key]
			if !ok {
				return nil, fmt.Errorf("prior backup DELETE target table %s was not found in table refs", targetName.Name.O)
			}
			current := mysqlPriorBackupTarget{table: ref.table, qualifier: strings.TrimSpace(targetName.Name.O)}
			if mysqlPriorBackupTargetsSamePhysicalTableSlice(targets, current) {
				return nil, fmt.Errorf("prior backup does not support deleting multiple aliases of the same table")
			}
			targets = append(targets, current)
		}
		return targets, nil
	}
	first, err := mysqlPriorBackupFirstTableRef(stmt.TableRefs)
	if err != nil {
		return nil, err
	}
	return []mysqlPriorBackupTarget{{table: first.table, qualifier: first.qualifier}}, nil
}

func mysqlPriorBackupCTE(with *tidbast.WithClause) (string, map[string]bool, error) {
	names := map[string]bool{}
	if with == nil {
		return "", names, nil
	}
	for _, cte := range with.CTEs {
		if cte == nil || strings.TrimSpace(cte.Name.O) == "" {
			continue
		}
		names[strings.ToLower(strings.TrimSpace(cte.Name.O))] = true
	}
	cteSQL, err := restoreMySQLRollbackNode(with)
	if err != nil {
		return "", nil, fmt.Errorf("restore CTE failed")
	}
	return strings.TrimSpace(cteSQL), names, nil
}

func mysqlPriorBackupTablePlans(database string, baseBackupTable string, targets []mysqlPriorBackupTarget, cteNames map[string]bool) ([]mysqlPriorBackupTablePlan, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("prior backup could not determine target table")
	}
	plans := make([]mysqlPriorBackupTablePlan, 0, len(targets))
	for i, target := range targets {
		if target.table == nil || strings.TrimSpace(target.table.Name.O) == "" {
			return nil, fmt.Errorf("prior backup could not determine target table")
		}
		if target.table.Schema.O == "" && cteNames[strings.ToLower(strings.TrimSpace(target.table.Name.O))] {
			return nil, fmt.Errorf("prior backup target table cannot be a CTE")
		}
		targetDB := strings.TrimSpace(target.table.Schema.O)
		if targetDB == "" {
			targetDB = database
		}
		if !strings.EqualFold(targetDB, database) {
			return nil, fmt.Errorf("prior backup does not support cross-database target table")
		}
		plans = append(plans, mysqlPriorBackupTablePlan{
			database:      targetDB,
			table:         strings.TrimSpace(target.table.Name.O),
			qualifier:     strings.TrimSpace(target.qualifier),
			backupTable:   mysqlPriorBackupBackupTableName(baseBackupTable, target.qualifier, i, len(targets)),
			updateColumns: target.updateColumns,
		})
	}
	return plans, nil
}

func mysqlPriorBackupBackupTableName(base string, qualifier string, index int, total int) string {
	base = strings.TrimSpace(base)
	if total <= 1 {
		return truncate(base, 64)
	}
	suffix := mysqlPriorBackupIdentifierSuffix(qualifier)
	if suffix == "" {
		suffix = fmt.Sprintf("target_%d", index+1)
	}
	suffix = fmt.Sprintf("%02d_%s", index+1, suffix)
	if len(suffix) > 30 {
		suffix = suffix[:30]
	}
	maxBase := 64 - len(suffix) - 1
	if maxBase < 1 {
		maxBase = 1
	}
	return truncate(base, maxBase) + "_" + suffix
}

func mysqlPriorBackupIdentifierSuffix(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func mysqlPriorBackupTableRefMap(refs *tidbast.TableRefsClause) (map[string]mysqlPriorBackupTableRef, error) {
	result := map[string]mysqlPriorBackupTableRef{}
	if refs == nil || refs.TableRefs == nil {
		return result, fmt.Errorf("prior backup could not determine target table")
	}
	if err := collectMySQLPriorBackupTableRefs(refs.TableRefs, result); err != nil {
		return result, err
	}
	return result, nil
}

func collectMySQLPriorBackupTableRefs(node tidbast.ResultSetNode, refs map[string]mysqlPriorBackupTableRef) error {
	switch typed := node.(type) {
	case *tidbast.TableSource:
		table, ok := typed.Source.(*tidbast.TableName)
		if !ok || table == nil {
			return nil
		}
		if len(table.PartitionNames) > 0 {
			return fmt.Errorf("prior backup does not support partition-qualified table references")
		}
		if table.AsOf != nil {
			return fmt.Errorf("prior backup does not support AS OF table references")
		}
		qualifier := strings.TrimSpace(typed.AsName.O)
		if qualifier == "" {
			qualifier = strings.TrimSpace(table.Name.O)
		}
		ref := mysqlPriorBackupTableRef{table: table, qualifier: qualifier}
		if strings.TrimSpace(table.Name.O) != "" {
			refs[strings.ToLower(strings.TrimSpace(table.Name.O))] = ref
		}
		if strings.TrimSpace(typed.AsName.O) != "" {
			refs[strings.ToLower(strings.TrimSpace(typed.AsName.O))] = ref
		}
	case *tidbast.Join:
		if err := collectMySQLPriorBackupTableRefs(typed.Left, refs); err != nil {
			return err
		}
		if err := collectMySQLPriorBackupTableRefs(typed.Right, refs); err != nil {
			return err
		}
	}
	return nil
}

func mysqlPriorBackupFirstTableRef(refs *tidbast.TableRefsClause) (mysqlPriorBackupTableRef, error) {
	if refs == nil || refs.TableRefs == nil {
		return mysqlPriorBackupTableRef{}, fmt.Errorf("prior backup could not determine target table")
	}
	return mysqlPriorBackupFirstTableRefFromNode(refs.TableRefs)
}

func mysqlPriorBackupFirstTableRefFromNode(node tidbast.ResultSetNode) (mysqlPriorBackupTableRef, error) {
	switch typed := node.(type) {
	case *tidbast.TableSource:
		table, ok := typed.Source.(*tidbast.TableName)
		if !ok || table == nil {
			return mysqlPriorBackupTableRef{}, fmt.Errorf("prior backup only supports direct target table references")
		}
		if len(table.PartitionNames) > 0 {
			return mysqlPriorBackupTableRef{}, fmt.Errorf("prior backup does not support partition-qualified table references")
		}
		if table.AsOf != nil {
			return mysqlPriorBackupTableRef{}, fmt.Errorf("prior backup does not support AS OF table references")
		}
		qualifier := strings.TrimSpace(typed.AsName.O)
		if qualifier == "" {
			qualifier = strings.TrimSpace(table.Name.O)
		}
		return mysqlPriorBackupTableRef{table: table, qualifier: qualifier}, nil
	case *tidbast.Join:
		if typed.Left != nil {
			return mysqlPriorBackupFirstTableRefFromNode(typed.Left)
		}
	}
	return mysqlPriorBackupTableRef{}, fmt.Errorf("prior backup could not determine target table")
}

func mysqlPriorBackupHasJoin(refs *tidbast.TableRefsClause) bool {
	return refs != nil && refs.TableRefs != nil && refs.TableRefs.Right != nil
}

func mysqlPriorBackupSameTarget(a mysqlPriorBackupTarget, b mysqlPriorBackupTarget) bool {
	if a.table == nil || b.table == nil {
		return false
	}
	return strings.EqualFold(a.table.Schema.O, b.table.Schema.O) &&
		strings.EqualFold(a.table.Name.O, b.table.Name.O) &&
		strings.EqualFold(a.qualifier, b.qualifier)
}

func mysqlPriorBackupTargetKey(target mysqlPriorBackupTarget) string {
	return strings.ToLower(strings.TrimSpace(target.qualifier))
}

func mysqlPriorBackupTargetsSamePhysicalTable(targets map[string]*mysqlPriorBackupTarget, current mysqlPriorBackupTarget, currentKey string) bool {
	for key, target := range targets {
		if key == currentKey || target == nil || target.table == nil || current.table == nil {
			continue
		}
		if mysqlPriorBackupSamePhysicalTable(target.table, current.table) {
			return true
		}
	}
	return false
}

func mysqlPriorBackupTargetsSamePhysicalTableSlice(targets []mysqlPriorBackupTarget, current mysqlPriorBackupTarget) bool {
	for _, target := range targets {
		if target.table == nil || current.table == nil {
			continue
		}
		if mysqlPriorBackupSamePhysicalTable(target.table, current.table) {
			return true
		}
	}
	return false
}

func mysqlPriorBackupSamePhysicalTable(a *tidbast.TableName, b *tidbast.TableName) bool {
	if a == nil || b == nil || !strings.EqualFold(a.Name.O, b.Name.O) {
		return false
	}
	if a.Schema.O == "" || b.Schema.O == "" {
		return true
	}
	return strings.EqualFold(a.Schema.O, b.Schema.O)
}

func mysqlStringSliceContainsFold(items []string, needle string) bool {
	for _, item := range items {
		if strings.EqualFold(item, needle) {
			return true
		}
	}
	return false
}

func mysqlPriorBackupOrderLimitSQL(order *tidbast.OrderByClause, limit *tidbast.Limit) (string, string, error) {
	if limit != nil && order == nil {
		return "", "", fmt.Errorf("prior backup requires ORDER BY when UPDATE/DELETE uses LIMIT")
	}
	var orderSQL string
	if order != nil {
		restored, err := restoreMySQLRollbackNode(order)
		if err != nil {
			return "", "", fmt.Errorf("restore ORDER BY failed")
		}
		orderSQL = restored
	}
	var limitSQL string
	if limit != nil {
		restored, err := restoreMySQLRollbackNode(limit)
		if err != nil {
			return "", "", fmt.Errorf("restore LIMIT failed")
		}
		limitSQL = restored
	}
	return orderSQL, limitSQL, nil
}

func loadMySQLRollbackTableMetadata(ctx context.Context, conn *sql.Conn, database string, table string) ([]string, []string, error) {
	rows, err := conn.QueryContext(ctx,
		`SELECT COLUMN_NAME, COLUMN_KEY, EXTRA, GENERATION_EXPRESSION
		   FROM information_schema.COLUMNS
		  WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		  ORDER BY ORDINAL_POSITION`,
		database,
		table,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("read table metadata for prior backup failed: %w", err)
	}
	defer rows.Close()
	var columns []string
	var primaryKeys []string
	for rows.Next() {
		var name, columnKey, extra string
		var generation sql.NullString
		if err := rows.Scan(&name, &columnKey, &extra, &generation); err != nil {
			return nil, nil, fmt.Errorf("read table metadata for prior backup failed: %w", err)
		}
		if generation.Valid && strings.TrimSpace(generation.String) != "" {
			continue
		}
		if strings.Contains(strings.ToUpper(extra), "GENERATED") {
			continue
		}
		columns = append(columns, name)
		if strings.EqualFold(columnKey, "PRI") {
			primaryKeys = append(primaryKeys, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read table metadata for prior backup failed: %w", err)
	}
	return columns, primaryKeys, nil
}

func buildMySQLPriorBackupInsertSQL(plan *mysqlPriorBackupPlan) string {
	if plan != nil && len(plan.targets) > 0 {
		target := plan.targets[0]
		if len(target.columns) == 0 && len(plan.columns) > 0 {
			target.columns = plan.columns
		}
		return buildMySQLPriorBackupInsertSQLForTarget(plan, target)
	}
	target := mysqlPriorBackupTablePlan{
		database:    plan.database,
		table:       plan.table,
		qualifier:   plan.alias,
		backupTable: plan.backupTable,
		columns:     plan.columns,
	}
	return buildMySQLPriorBackupInsertSQLForTarget(plan, target)
}

func buildMySQLPriorBackupInsertSQLForTarget(plan *mysqlPriorBackupPlan, target mysqlPriorBackupTablePlan) string {
	columnList := quoteMySQLIdentifierList(target.columns)
	tableRefsSQL := strings.TrimSpace(plan.tableRefsSQL)
	if tableRefsSQL == "" {
		tableRefsSQL = qualifiedMySQLName(target.database, target.table)
		if strings.TrimSpace(target.qualifier) != "" && !strings.EqualFold(target.qualifier, target.table) {
			tableRefsSQL += " AS " + quoteMySQLIdentifier(target.qualifier)
		}
	}
	selectPrefix := "SELECT"
	if strings.TrimSpace(plan.cteSQL) != "" {
		selectPrefix = strings.TrimSpace(plan.cteSQL) + " SELECT"
	}
	sqlText := fmt.Sprintf("INSERT INTO %s (%s) %s %s FROM %s WHERE %s",
		qualifiedMySQLName(plan.backupDatabase, target.backupTable),
		columnList,
		selectPrefix,
		quoteMySQLQualifiedColumnList(mysqlPriorBackupTargetQualifier(target), target.columns),
		tableRefsSQL,
		plan.whereSQL,
	)
	if strings.TrimSpace(plan.orderSQL) != "" {
		sqlText += " " + strings.TrimSpace(plan.orderSQL)
	}
	if strings.TrimSpace(plan.limitSQL) != "" {
		sqlText += " " + strings.TrimSpace(plan.limitSQL)
	}
	return sqlText
}

func buildMySQLPriorBackupRestoreSQL(plan *mysqlPriorBackupPlan) string {
	if plan != nil && len(plan.targets) > 0 {
		statements := make([]string, 0, len(plan.targets))
		for _, target := range plan.targets {
			if len(target.columns) == 0 && len(plan.columns) > 0 {
				target.columns = plan.columns
			}
			if len(target.updateColumns) == 0 && len(plan.updateColumns) > 0 {
				target.updateColumns = plan.updateColumns
			}
			statements = append(statements, buildMySQLPriorBackupRestoreSQLForTarget(plan, target))
		}
		return strings.Join(statements, "\n")
	}
	target := mysqlPriorBackupTablePlan{
		database:      plan.database,
		table:         plan.table,
		qualifier:     plan.alias,
		backupTable:   plan.backupTable,
		columns:       plan.columns,
		updateColumns: plan.updateColumns,
	}
	return buildMySQLPriorBackupRestoreSQLForTarget(plan, target)
}

func buildMySQLPriorBackupRestoreSQLForTarget(plan *mysqlPriorBackupPlan, target mysqlPriorBackupTablePlan) string {
	columnList := quoteMySQLIdentifierList(target.columns)
	base := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
		qualifiedMySQLName(target.database, target.table),
		columnList,
		columnList,
		qualifiedMySQLName(plan.backupDatabase, target.backupTable),
	)
	if plan.kind == sqlparse.StatementKindDelete {
		return base + ";"
	}
	var assigns []string
	for _, col := range target.updateColumns {
		assigns = append(assigns, fmt.Sprintf("%s = VALUES(%s)", quoteMySQLIdentifier(col), quoteMySQLIdentifier(col)))
	}
	return base + " ON DUPLICATE KEY UPDATE " + strings.Join(assigns, ", ") + ";"
}

func (p *mysqlPriorBackupPlan) aliasOrTable() string {
	if p != nil && strings.TrimSpace(p.alias) != "" {
		return p.alias
	}
	if p == nil {
		return ""
	}
	return p.table
}

func mysqlPriorBackupTargetQualifier(target mysqlPriorBackupTablePlan) string {
	if strings.TrimSpace(target.qualifier) != "" {
		return target.qualifier
	}
	return target.table
}

func quoteMySQLIdentifierList(columns []string) string {
	quoted := make([]string, 0, len(columns))
	for _, col := range columns {
		quoted = append(quoted, quoteMySQLIdentifier(col))
	}
	return strings.Join(quoted, ", ")
}

func quoteMySQLQualifiedColumnList(table string, columns []string) string {
	quoted := make([]string, 0, len(columns))
	prefix := ""
	if strings.TrimSpace(table) != "" {
		prefix = quoteMySQLIdentifier(table) + "."
	}
	for _, col := range columns {
		quoted = append(quoted, prefix+quoteMySQLIdentifier(col))
	}
	return strings.Join(quoted, ", ")
}

func qualifiedMySQLName(database string, table string) string {
	return quoteMySQLIdentifier(database) + "." + quoteMySQLIdentifier(table)
}

func mysqlColumnsOverlap(a []string, b []string) bool {
	set := map[string]bool{}
	for _, item := range a {
		set[strings.ToLower(strings.TrimSpace(item))] = true
	}
	for _, item := range b {
		if set[strings.ToLower(strings.TrimSpace(item))] {
			return true
		}
	}
	return false
}

func restoreMySQLRollbackNode(node tidbast.Node) (string, error) {
	if node == nil {
		return "", fmt.Errorf("mysql AST node is nil")
	}
	var sb strings.Builder
	restoreCtx := tidbformat.NewRestoreCtx(tidbformat.DefaultRestoreFlags, &sb)
	if err := node.Restore(restoreCtx); err != nil {
		return "", err
	}
	return strings.TrimSpace(sb.String()), nil
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

func runMy2SQLRollback(ctx context.Context, settings *model.PlatformSettings, conn *model.DBConnection, password string, binlogRange repository.RollbackRange, databaseName *string, rowsAffected *int64, fallback string) (repository.GeneratedRollback, error) {
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
	if strings.TrimSpace(fallback) != "" {
		warning = strings.TrimSpace(fallback) + ". " + warning
	}
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
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.Write(data)
	}
	if sqlOnly := extractMy2SQLStatements(out.String()); sqlOnly != "" {
		if len([]byte(sqlOnly)) > maxBytes {
			return "", fmt.Errorf("generated rollback sql exceeds size limit")
		}
		return sqlOnly, nil
	}
	if sqlOnly := extractMy2SQLStatements(stdout); sqlOnly != "" {
		if len([]byte(sqlOnly)) > maxBytes {
			return "", fmt.Errorf("generated rollback sql exceeds size limit")
		}
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

func mysqlRollbackStatementKind(sqlText string) (sqlparse.StatementKind, error) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, sqlText)
	if err != nil || len(parsed.Statements) != 1 {
		return "", fmt.Errorf("only mysql insert/update/delete statements are supported")
	}
	switch parsed.Statements[0].Kind {
	case sqlparse.StatementKindInsert, sqlparse.StatementKindUpdate, sqlparse.StatementKindDelete:
		return parsed.Statements[0].Kind, nil
	default:
		return "", fmt.Errorf("only mysql insert/update/delete statements are supported")
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
