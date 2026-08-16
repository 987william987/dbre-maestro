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
	pg_query "github.com/pganalyze/pg_query_go/v6"
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

type mysqlShadowCloneTable struct {
	database string
	table    string
	required bool
}

type mysqlTableExistenceExpectation string

const (
	mysqlTableMustExist    mysqlTableExistenceExpectation = "must_exist"
	mysqlTableMustNotExist mysqlTableExistenceExpectation = "must_not_exist"
)

type mysqlDDLTableExistenceCheck struct {
	database    string
	table       string
	expectation mysqlTableExistenceExpectation
	optional    bool
}

type reviewTableTarget struct {
	database string
	schema   string
	table    string
}

type reviewTableMetadata struct {
	databaseName  string
	schemaName    string
	tableName     string
	rowCount      *int64
	dataSizeBytes *int64
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
	tableMetadataBySeq := h.loadReviewTableMetadata(ctx, dbConnID, dialect, parsedStatements, nullableStringValue(databaseName))

	if ticketType == model.TicketTypeDDL || ticketType == model.TicketTypeDML {
		if err := sqlpolicy.CheckTicketStatementKinds(ticketType, parsedStatements); err != nil {
			return applyReviewTableMetadata(append(results, buildTicketKindReviewItems(parsedStatements, err)...), tableMetadataBySeq)
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

	return applyReviewTableMetadata(results, tableMetadataBySeq)
}

func (h *TicketHandler) loadReviewTableMetadata(ctx context.Context, dbConnID uint64, dialect sqlparse.Dialect, statements []sqlparse.ParsedStatement, selectedDatabase string) map[int][]reviewTableMetadata {
	if len(statements) == 0 || h.tickets == nil {
		return nil
	}
	repo := repository.NewDBMetadataRepo(h.tickets.DB())
	items := make(map[int][]reviewTableMetadata)
	for _, stmt := range statements {
		for _, target := range reviewTableTargetsForStatement(dialect, stmt, selectedDatabase) {
			if strings.TrimSpace(target.database) == "" || strings.TrimSpace(target.schema) == "" || strings.TrimSpace(target.table) == "" {
				continue
			}
			metadata := reviewTableMetadata{
				databaseName: target.database,
				schemaName:   target.schema,
				tableName:    target.table,
			}
			snapshot, err := repo.FindObjectSnapshot(ctx, dbConnID, target.database, target.schema, target.table)
			if err != nil {
				slog.Warn("load sql review table metadata failed",
					"connection_id", dbConnID,
					"database", target.database,
					"schema", target.schema,
					"table", target.table,
					"err", err,
				)
			} else if snapshot != nil {
				rowCount := snapshot.RowCount
				dataSizeBytes := snapshot.DataSizeBytes
				metadata.tableName = snapshot.TableName
				metadata.rowCount = &rowCount
				metadata.dataSizeBytes = &dataSizeBytes
			}
			items[stmt.Seq] = append(items[stmt.Seq], metadata)
		}
	}
	return items
}

func applyReviewTableMetadata(items []ticketReviewItem, metadataBySeq map[int][]reviewTableMetadata) []ticketReviewItem {
	if len(metadataBySeq) == 0 {
		return items
	}
	for index := range items {
		metadataItems, ok := metadataBySeq[items[index].Seq]
		if !ok {
			continue
		}
		items[index].Tables = make([]ticketReviewTableMetadata, 0, len(metadataItems))
		for _, metadata := range metadataItems {
			items[index].Tables = append(items[index].Tables, ticketReviewTableMetadata{
				DatabaseName:  metadata.databaseName,
				SchemaName:    metadata.schemaName,
				TableName:     metadata.tableName,
				RowCount:      metadata.rowCount,
				DataSizeBytes: metadata.dataSizeBytes,
			})
		}
	}
	return items
}

func reviewTableTargetsForStatement(dialect sqlparse.Dialect, stmt sqlparse.ParsedStatement, selectedDatabase string) []reviewTableTarget {
	var targets []reviewTableTarget
	switch dialect {
	case sqlparse.DialectMySQL:
		targets = mysqlReviewTableTargets(stmt, selectedDatabase)
	case sqlparse.DialectPostgres:
		targets = postgresReviewTableTargets(stmt, selectedDatabase)
	default:
		return nil
	}
	return dedupeReviewTableTargets(targets)
}

func dedupeReviewTableTargets(targets []reviewTableTarget) []reviewTableTarget {
	items := make([]reviewTableTarget, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		databaseName := strings.TrimSpace(target.database)
		schemaName := strings.TrimSpace(target.schema)
		tableName := strings.TrimSpace(target.table)
		if databaseName == "" || schemaName == "" || tableName == "" {
			continue
		}
		key := strings.ToLower(databaseName + "\x00" + schemaName + "\x00" + tableName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, reviewTableTarget{database: databaseName, schema: schemaName, table: tableName})
	}
	return items
}

func mysqlReviewTableTargets(stmt sqlparse.ParsedStatement, selectedDatabase string) []reviewTableTarget {
	tables := make([]*tidbast.TableName, 0, 1)
	switch node := stmt.AST.(type) {
	case *tidbast.CreateTableStmt:
		tables = append(tables, node.Table)
	case *tidbast.AlterTableStmt:
		tables = append(tables, node.Table)
	case *tidbast.DropTableStmt:
		tables = append(tables, node.Tables...)
	case *tidbast.TruncateTableStmt:
		tables = append(tables, node.Table)
	case *tidbast.RenameTableStmt:
		for _, pair := range node.TableToTables {
			if pair != nil {
				tables = append(tables, pair.OldTable)
			}
		}
	case *tidbast.InsertStmt:
		tables = append(tables, firstMySQLTableNameFromTableRefs(node.Table))
	case *tidbast.UpdateStmt:
		tables = append(tables, mysqlUpdateTargetTables(node)...)
	case *tidbast.DeleteStmt:
		tables = append(tables, mysqlDeleteTargetTables(node)...)
	default:
		return nil
	}
	targets := make([]reviewTableTarget, 0, len(tables))
	for _, table := range tables {
		if target := mysqlReviewTargetForTable(table, selectedDatabase); target != nil {
			targets = append(targets, *target)
		}
	}
	return targets
}

func mysqlReviewTargetForTable(table *tidbast.TableName, selectedDatabase string) *reviewTableTarget {
	if table == nil {
		return nil
	}
	databaseName := strings.TrimSpace(table.Schema.O)
	if databaseName == "" {
		databaseName = strings.TrimSpace(selectedDatabase)
	}
	tableName := strings.TrimSpace(table.Name.O)
	if databaseName == "" || tableName == "" {
		return nil
	}
	return &reviewTableTarget{database: databaseName, schema: databaseName, table: tableName}
}

func mysqlUpdateTargetTables(stmt *tidbast.UpdateStmt) []*tidbast.TableName {
	if stmt == nil {
		return nil
	}
	aliases := mysqlTableAliasMap(stmt.TableRefs)
	targets := make([]*tidbast.TableName, 0)
	for _, assignment := range stmt.List {
		if assignment == nil || assignment.Column == nil || strings.TrimSpace(assignment.Column.Table.O) == "" {
			continue
		}
		targets = append(targets, resolveMySQLTableName(&tidbast.TableName{
			Schema: tidbast.NewCIStr(assignment.Column.Schema.O),
			Name:   tidbast.NewCIStr(assignment.Column.Table.O),
		}, aliases))
	}
	if len(targets) > 0 {
		return targets
	}
	return []*tidbast.TableName{firstMySQLTableNameFromTableRefs(stmt.TableRefs)}
}

func mysqlDeleteTargetTables(stmt *tidbast.DeleteStmt) []*tidbast.TableName {
	if stmt == nil {
		return nil
	}
	if stmt.Tables != nil && len(stmt.Tables.Tables) > 0 {
		aliases := mysqlTableAliasMap(stmt.TableRefs)
		targets := make([]*tidbast.TableName, 0, len(stmt.Tables.Tables))
		for _, table := range stmt.Tables.Tables {
			targets = append(targets, resolveMySQLTableName(table, aliases))
		}
		return targets
	}
	return []*tidbast.TableName{firstMySQLTableNameFromTableRefs(stmt.TableRefs)}
}

func mysqlTableAliasMap(refs *tidbast.TableRefsClause) map[string]*tidbast.TableName {
	aliases := make(map[string]*tidbast.TableName)
	if refs == nil {
		return aliases
	}
	collectMySQLTableAliases(refs.TableRefs, aliases)
	return aliases
}

func collectMySQLTableAliases(node tidbast.ResultSetNode, aliases map[string]*tidbast.TableName) {
	switch typed := node.(type) {
	case *tidbast.TableName:
		if strings.TrimSpace(typed.Name.O) != "" {
			aliases[strings.ToLower(typed.Name.O)] = typed
		}
	case *tidbast.TableSource:
		if table, ok := typed.Source.(*tidbast.TableName); ok {
			if strings.TrimSpace(table.Name.O) != "" {
				aliases[strings.ToLower(table.Name.O)] = table
			}
			if strings.TrimSpace(typed.AsName.O) != "" {
				aliases[strings.ToLower(typed.AsName.O)] = table
			}
			return
		}
		collectMySQLTableAliases(typed.Source, aliases)
	case *tidbast.Join:
		collectMySQLTableAliases(typed.Left, aliases)
		collectMySQLTableAliases(typed.Right, aliases)
	}
}

func resolveMySQLTableName(table *tidbast.TableName, aliases map[string]*tidbast.TableName) *tidbast.TableName {
	if table == nil || strings.TrimSpace(table.Schema.O) != "" {
		return table
	}
	if resolved, ok := aliases[strings.ToLower(table.Name.O)]; ok {
		return resolved
	}
	return table
}

func firstMySQLTableNameFromTableRefs(refs *tidbast.TableRefsClause) *tidbast.TableName {
	if refs == nil {
		return nil
	}
	return firstMySQLTableNameFromResultSet(refs.TableRefs)
}

func firstMySQLTableNameFromResultSet(node tidbast.ResultSetNode) *tidbast.TableName {
	switch typed := node.(type) {
	case *tidbast.TableName:
		return typed
	case *tidbast.TableSource:
		return firstMySQLTableNameFromResultSet(typed.Source)
	case *tidbast.Join:
		if table := firstMySQLTableNameFromResultSet(typed.Left); table != nil {
			return table
		}
		return firstMySQLTableNameFromResultSet(typed.Right)
	default:
		return nil
	}
}

func postgresReviewTableTargets(stmt sqlparse.ParsedStatement, selectedDatabase string) []reviewTableTarget {
	node, ok := stmt.AST.(*pg_query.Node)
	if !ok || node == nil {
		return nil
	}
	relations := make([]*pg_query.RangeVar, 0, 1)
	switch {
	case node.GetInsertStmt() != nil:
		relations = append(relations, node.GetInsertStmt().Relation)
	case node.GetUpdateStmt() != nil:
		relations = append(relations, node.GetUpdateStmt().Relation)
	case node.GetDeleteStmt() != nil:
		relations = append(relations, node.GetDeleteStmt().Relation)
	case node.GetCreateStmt() != nil:
		relations = append(relations, node.GetCreateStmt().Relation)
	case node.GetAlterTableStmt() != nil:
		relations = append(relations, node.GetAlterTableStmt().Relation)
	case node.GetTruncateStmt() != nil:
		for _, relation := range node.GetTruncateStmt().Relations {
			relations = append(relations, relation.GetRangeVar())
		}
	case node.GetDropStmt() != nil:
		relations = append(relations, postgresDropRelations(node.GetDropStmt())...)
	default:
		return nil
	}
	targets := make([]reviewTableTarget, 0, len(relations))
	for _, relation := range relations {
		if target := postgresRangeVarReviewTableTarget(relation, selectedDatabase); target != nil {
			targets = append(targets, *target)
		}
	}
	return targets
}

func postgresRangeVarReviewTableTarget(relation *pg_query.RangeVar, selectedDatabase string) *reviewTableTarget {
	if relation == nil {
		return nil
	}
	databaseName := strings.TrimSpace(relation.Catalogname)
	if databaseName == "" {
		databaseName = strings.TrimSpace(selectedDatabase)
	}
	schemaName := strings.TrimSpace(relation.Schemaname)
	if schemaName == "" {
		schemaName = "public"
	}
	tableName := strings.TrimSpace(relation.Relname)
	if databaseName == "" || tableName == "" {
		return nil
	}
	return &reviewTableTarget{database: databaseName, schema: schemaName, table: tableName}
}

func postgresDropRelations(stmt *pg_query.DropStmt) []*pg_query.RangeVar {
	if stmt == nil || len(stmt.Objects) == 0 {
		return nil
	}
	relations := make([]*pg_query.RangeVar, 0, len(stmt.Objects))
	for _, object := range stmt.Objects {
		list := object.GetList()
		if list == nil || len(list.Items) == 0 {
			continue
		}
		names := make([]string, 0, len(list.Items))
		for _, item := range list.Items {
			if item.GetString_() != nil {
				names = append(names, item.GetString_().Sval)
			}
		}
		if len(names) == 0 {
			continue
		}
		relation := &pg_query.RangeVar{Relname: names[len(names)-1]}
		if len(names) >= 2 {
			relation.Schemaname = names[len(names)-2]
		}
		if len(names) >= 3 {
			relation.Catalogname = names[len(names)-3]
		}
		relations = append(relations, relation)
	}
	return relations
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
		explainResult, err := sqlreview.CheckExplainWithStats(ctx, queryDB, stmt.RawSQL, rowThreshold)
		statementKind := string(stmt.Kind)
		if err != nil {
			items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodMySQLExplain, nil, statementKind, "table", 0, []string{err.Error()}))
			continue
		}
		messages := make([]string, 0, len(explainResult.Issues))
		for _, issue := range explainResult.Issues {
			if ruleMap[issue.Kind] {
				messages = append(messages, issue.Msg)
			}
		}
		items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodMySQLExplain, nil, statementKind, "table", explainResult.MaxRows, messages))
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
	tableShadowCloneTables, tableShadowCloneErr := mysqlDDLShadowCloneTablesForValidStatements(ctx, readonlyDB, statements, nullableStringValue(databaseName))

	for _, stmt := range statements {
		statementKind := string(stmt.Kind)
		target, rewriteSQL, prepErr, execErr := h.prepareMySQLShadowValidation(ctx, readonlyDB, metaDB, stmt, nullableStringValue(databaseName), tableShadowCloneTables, tableShadowCloneErr, &tableShadowDB, &tableCleanup, &tableShadowPrepared)
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
	tableShadowCloneTables []mysqlShadowCloneTable,
	tableShadowCloneErr error,
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
		if tableShadowCloneErr != nil {
			return target, "", tableShadowCloneErr, nil
		}
		if err := validateMySQLDDLTableExistence(ctx, readonlyDB, stmt, selectedDatabase); err != nil {
			return target, rewrittenSQL, nil, err
		}
		if !*tableShadowPrepared {
			shadowName, cleanup, err := cloneMySQLDatabaseTablesToShadow(ctx, readonlyDB, metaDB, selectedDatabase, tableShadowCloneTables)
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
	tables, err := listMySQLBaseTables(ctx, readonlyDB, sourceDatabase)
	if err != nil {
		return "", nil, err
	}
	cloneTables := make([]mysqlShadowCloneTable, 0, len(tables))
	for _, tableName := range tables {
		cloneTables = append(cloneTables, mysqlShadowCloneTable{
			database: sourceDatabase,
			table:    tableName,
			required: true,
		})
	}
	return cloneMySQLDatabaseTablesToShadow(ctx, readonlyDB, metaDB, sourceDatabase, cloneTables)
}

func cloneMySQLDatabaseTablesToShadow(ctx context.Context, readonlyDB *sql.DB, metaDB *sqlx.DB, sourceDatabase string, tables []mysqlShadowCloneTable) (string, func(), error) {
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

	seen := make(map[string]bool, len(tables))
	for _, table := range tables {
		tableDatabase := strings.TrimSpace(table.database)
		if tableDatabase == "" {
			tableDatabase = sourceDatabase
		}
		tableName := strings.TrimSpace(table.table)
		if tableName == "" {
			continue
		}
		key := tableDatabase + "." + tableName
		if seen[key] {
			continue
		}
		seen[key] = true
		createTableSQL, err := loadMySQLCreateTableSQL(ctx, readonlyDB, tableDatabase, tableName)
		if err != nil {
			if !table.required && isMySQLMissingTableError(err) {
				continue
			}
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

func mysqlDDLShadowCloneTables(stmt sqlparse.ParsedStatement, selectedDatabase string) ([]mysqlShadowCloneTable, error) {
	switch ddl := stmt.AST.(type) {
	case *tidbast.CreateTableStmt:
		if ddl.Table == nil {
			return nil, fmt.Errorf("CREATE TABLE target is empty")
		}
		items := make([]mysqlShadowCloneTable, 0, 1)
		if ddl.ReferTable != nil {
			items = append(items, mysqlShadowCloneTableForName(ddl.ReferTable, selectedDatabase, true))
		}
		items = append(items, mysqlCreateTableReferenceClones(ddl, selectedDatabase)...)
		return items, nil
	case *tidbast.AlterTableStmt:
		if ddl.Table == nil {
			return nil, fmt.Errorf("ALTER TABLE target is empty")
		}
		items := []mysqlShadowCloneTable{mysqlShadowCloneTableForName(ddl.Table, selectedDatabase, true)}
		items = append(items, mysqlAlterTableReferenceClones(ddl, selectedDatabase)...)
		return items, nil
	case *tidbast.DropTableStmt:
		items := make([]mysqlShadowCloneTable, 0, len(ddl.Tables))
		for _, table := range ddl.Tables {
			if table == nil {
				continue
			}
			items = append(items, mysqlShadowCloneTableForName(table, selectedDatabase, !ddl.IfExists))
		}
		return items, nil
	case *tidbast.TruncateTableStmt:
		if ddl.Table == nil {
			return nil, fmt.Errorf("TRUNCATE TABLE target is empty")
		}
		return []mysqlShadowCloneTable{mysqlShadowCloneTableForName(ddl.Table, selectedDatabase, true)}, nil
	case *tidbast.RenameTableStmt:
		items := make([]mysqlShadowCloneTable, 0, len(ddl.TableToTables)*2)
		for _, tablePair := range ddl.TableToTables {
			if tablePair == nil {
				continue
			}
			if tablePair.OldTable != nil {
				items = append(items, mysqlShadowCloneTableForName(tablePair.OldTable, selectedDatabase, true))
			}
			if tablePair.NewTable != nil {
				items = append(items, mysqlShadowCloneTableForName(tablePair.NewTable, selectedDatabase, false))
			}
		}
		return items, nil
	default:
		return nil, nil
	}
}

func mysqlDDLShadowCloneTablesForStatements(statements []sqlparse.ParsedStatement, selectedDatabase string) ([]mysqlShadowCloneTable, error) {
	items := make([]mysqlShadowCloneTable, 0)
	for _, stmt := range statements {
		nextItems, err := mysqlDDLShadowCloneTables(stmt, selectedDatabase)
		if err != nil {
			return nil, err
		}
		items = append(items, nextItems...)
	}
	return items, nil
}

func mysqlDDLShadowCloneTablesForValidStatements(ctx context.Context, db *sql.DB, statements []sqlparse.ParsedStatement, selectedDatabase string) ([]mysqlShadowCloneTable, error) {
	items := make([]mysqlShadowCloneTable, 0)
	for _, stmt := range statements {
		if isMySQLTableScopedDDL(stmt) {
			if err := validateMySQLDDLTableExistence(ctx, db, stmt, selectedDatabase); err != nil {
				continue
			}
		}
		nextItems, err := mysqlDDLShadowCloneTables(stmt, selectedDatabase)
		if err != nil {
			return nil, err
		}
		items = append(items, nextItems...)
	}
	return items, nil
}

func isMySQLTableScopedDDL(stmt sqlparse.ParsedStatement) bool {
	switch stmt.AST.(type) {
	case *tidbast.CreateTableStmt, *tidbast.AlterTableStmt, *tidbast.DropTableStmt, *tidbast.TruncateTableStmt, *tidbast.RenameTableStmt:
		return true
	default:
		return false
	}
}

func mysqlDDLTableExistenceChecks(stmt sqlparse.ParsedStatement, selectedDatabase string) ([]mysqlDDLTableExistenceCheck, error) {
	switch ddl := stmt.AST.(type) {
	case *tidbast.CreateTableStmt:
		if ddl.Table == nil {
			return nil, fmt.Errorf("CREATE TABLE target is empty")
		}
		items := []mysqlDDLTableExistenceCheck{
			mysqlTableExistenceCheckForName(ddl.Table, selectedDatabase, mysqlTableMustNotExist, ddl.IfNotExists),
		}
		if ddl.ReferTable != nil {
			items = append(items, mysqlTableExistenceCheckForName(ddl.ReferTable, selectedDatabase, mysqlTableMustExist, false))
		}
		items = append(items, mysqlCreateTableReferenceExistenceChecks(ddl, selectedDatabase)...)
		return items, nil
	case *tidbast.AlterTableStmt:
		if ddl.Table == nil {
			return nil, fmt.Errorf("ALTER TABLE target is empty")
		}
		items := []mysqlDDLTableExistenceCheck{
			mysqlTableExistenceCheckForName(ddl.Table, selectedDatabase, mysqlTableMustExist, false),
		}
		items = append(items, mysqlAlterTableReferenceExistenceChecks(ddl, selectedDatabase)...)
		return items, nil
	case *tidbast.DropTableStmt:
		items := make([]mysqlDDLTableExistenceCheck, 0, len(ddl.Tables))
		for _, table := range ddl.Tables {
			if table == nil {
				continue
			}
			items = append(items, mysqlTableExistenceCheckForName(table, selectedDatabase, mysqlTableMustExist, ddl.IfExists))
		}
		return items, nil
	case *tidbast.TruncateTableStmt:
		if ddl.Table == nil {
			return nil, fmt.Errorf("TRUNCATE TABLE target is empty")
		}
		return []mysqlDDLTableExistenceCheck{
			mysqlTableExistenceCheckForName(ddl.Table, selectedDatabase, mysqlTableMustExist, false),
		}, nil
	case *tidbast.RenameTableStmt:
		items := make([]mysqlDDLTableExistenceCheck, 0, len(ddl.TableToTables)*2)
		for _, tablePair := range ddl.TableToTables {
			if tablePair == nil {
				continue
			}
			if tablePair.OldTable != nil {
				items = append(items, mysqlTableExistenceCheckForName(tablePair.OldTable, selectedDatabase, mysqlTableMustExist, false))
			}
			if tablePair.NewTable != nil {
				items = append(items, mysqlTableExistenceCheckForName(tablePair.NewTable, selectedDatabase, mysqlTableMustNotExist, false))
			}
		}
		return items, nil
	default:
		return nil, nil
	}
}

func validateMySQLDDLTableExistence(ctx context.Context, db *sql.DB, stmt sqlparse.ParsedStatement, selectedDatabase string) error {
	checks, err := mysqlDDLTableExistenceChecks(stmt, selectedDatabase)
	if err != nil {
		return err
	}
	for _, check := range checks {
		if strings.TrimSpace(check.database) == "" || strings.TrimSpace(check.table) == "" {
			return fmt.Errorf("table name is empty")
		}
		exists, err := mysqlTableExists(ctx, db, check.database, check.table)
		if err != nil {
			return fmt.Errorf("check table exists failed: %w", err)
		}
		switch check.expectation {
		case mysqlTableMustExist:
			if !exists && !check.optional {
				return fmt.Errorf("table %q does not exist", check.table)
			}
		case mysqlTableMustNotExist:
			if exists && !check.optional {
				return fmt.Errorf("table %q already exists", check.table)
			}
		}
	}
	return nil
}

func mysqlTableExistenceCheckForName(table *tidbast.TableName, selectedDatabase string, expectation mysqlTableExistenceExpectation, optional bool) mysqlDDLTableExistenceCheck {
	databaseName := table.Schema.O
	if strings.TrimSpace(databaseName) == "" {
		databaseName = selectedDatabase
	}
	return mysqlDDLTableExistenceCheck{
		database:    databaseName,
		table:       table.Name.O,
		expectation: expectation,
		optional:    optional,
	}
}

func mysqlShadowCloneTableForName(table *tidbast.TableName, selectedDatabase string, required bool) mysqlShadowCloneTable {
	databaseName := table.Schema.O
	if strings.TrimSpace(databaseName) == "" {
		databaseName = selectedDatabase
	}
	return mysqlShadowCloneTable{
		database: databaseName,
		table:    table.Name.O,
		required: required,
	}
}

func mysqlCreateTableReferenceExistenceChecks(ddl *tidbast.CreateTableStmt, selectedDatabase string) []mysqlDDLTableExistenceCheck {
	items := make([]mysqlDDLTableExistenceCheck, 0)
	for _, clone := range mysqlCreateTableReferenceClones(ddl, selectedDatabase) {
		items = append(items, mysqlDDLTableExistenceCheck{
			database:    clone.database,
			table:       clone.table,
			expectation: mysqlTableMustExist,
		})
	}
	return items
}

func mysqlAlterTableReferenceExistenceChecks(ddl *tidbast.AlterTableStmt, selectedDatabase string) []mysqlDDLTableExistenceCheck {
	items := make([]mysqlDDLTableExistenceCheck, 0)
	for _, clone := range mysqlAlterTableReferenceClones(ddl, selectedDatabase) {
		items = append(items, mysqlDDLTableExistenceCheck{
			database:    clone.database,
			table:       clone.table,
			expectation: mysqlTableMustExist,
		})
	}
	return items
}

func mysqlCreateTableReferenceClones(ddl *tidbast.CreateTableStmt, selectedDatabase string) []mysqlShadowCloneTable {
	items := make([]mysqlShadowCloneTable, 0)
	for _, column := range ddl.Cols {
		if column == nil {
			continue
		}
		for _, option := range column.Options {
			if option != nil && option.Refer != nil && option.Refer.Table != nil {
				items = append(items, mysqlShadowCloneTableForName(option.Refer.Table, selectedDatabase, true))
			}
		}
	}
	for _, constraint := range ddl.Constraints {
		if constraint != nil && constraint.Refer != nil && constraint.Refer.Table != nil {
			items = append(items, mysqlShadowCloneTableForName(constraint.Refer.Table, selectedDatabase, true))
		}
	}
	return items
}

func mysqlAlterTableReferenceClones(ddl *tidbast.AlterTableStmt, selectedDatabase string) []mysqlShadowCloneTable {
	items := make([]mysqlShadowCloneTable, 0)
	for _, spec := range ddl.Specs {
		if spec == nil {
			continue
		}
		for _, column := range spec.NewColumns {
			if column == nil {
				continue
			}
			for _, option := range column.Options {
				if option != nil && option.Refer != nil && option.Refer.Table != nil {
					items = append(items, mysqlShadowCloneTableForName(option.Refer.Table, selectedDatabase, true))
				}
			}
		}
		if spec.Constraint != nil && spec.Constraint.Refer != nil && spec.Constraint.Refer.Table != nil {
			items = append(items, mysqlShadowCloneTableForName(spec.Constraint.Refer.Table, selectedDatabase, true))
		}
		for _, constraint := range spec.NewConstraints {
			if constraint != nil && constraint.Refer != nil && constraint.Refer.Table != nil {
				items = append(items, mysqlShadowCloneTableForName(constraint.Refer.Table, selectedDatabase, true))
			}
		}
	}
	return items
}

func isMySQLMissingTableError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "error 1146") || strings.Contains(message, "doesn't exist")
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

func inferReviewObjectType(stmt sqlparse.ParsedStatement) string {
	switch stmt.Kind {
	case sqlparse.StatementKindInsert, sqlparse.StatementKindUpdate, sqlparse.StatementKindDelete:
		return "table"
	default:
		return inferDDLObjectType(stmt)
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

func mysqlTableExists(ctx context.Context, db *sql.DB, databaseName, tableName string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		 FROM information_schema.TABLES
		 WHERE TABLE_SCHEMA = ?
		   AND TABLE_NAME = ?
		   AND TABLE_TYPE = 'BASE TABLE'`,
		databaseName,
		tableName,
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

func redisTicketExecutionRows(ticketID uint64, sqlContent string) []model.TicketExecution {
	commands := splitRedisTicketCommands(sqlContent)
	rows := make([]model.TicketExecution, 0, len(commands))
	for index, commandLine := range commands {
		rows = append(rows, model.TicketExecution{
			TicketID: ticketID,
			Seq:      index + 1,
			SQLStmt:  commandLine,
		})
	}
	return rows
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

func (h *TicketHandler) runTicketRedisCommands(ticket *model.Ticket, executorID uint64, opts ticketExecutionRunOptions) {
	ctx := context.Background()
	executorName, err := h.lookupUsername(ctx, executorID)
	if err != nil {
		executorName = ""
	}
	if opts.Automated {
		executorName = "workflow automation"
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

	notificationActorID := &executorID
	if opts.Automated {
		notificationActorID = &opts.ReviewerID
	}
	if err := h.tickets.EnsureExecutions(ctx, ticket.ID, redisTicketExecutionRows(ticket.ID, ticket.SQLContent)); err != nil {
		h.finishTicketExecutionStartFailure(ctx, ticket, executorID, opts, "prepare redis executions failed: "+err.Error())
		return
	}
	executions, err := h.tickets.ListExecutions(ctx, ticket.ID)
	if err != nil {
		h.finishTicketExecutionStartFailure(ctx, ticket, executorID, opts, "load redis executions failed: "+err.Error())
		return
	}
	h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)

	finalStatus := model.TicketStatusCompleted
	for _, execRow := range executions {
		commandLine := strings.TrimSpace(execRow.SQLStmt)
		if execRow.Status != "pending" {
			if execRow.Status == "failed" || execRow.Status == "stopped" {
				finalStatus = model.TicketStatusFailed
				break
			}
			continue
		}

		current, err := h.tickets.GetByID(ctx, ticket.ID)
		if err == nil && current != nil && current.Status == model.TicketStatusStopped {
			return
		}

		ok, err := h.tickets.MarkExecutionRunningIfPending(ctx, execRow.ID)
		if err != nil {
			msg := err.Error()
			_ = h.tickets.MarkExecutionDone(ctx, execRow.ID, nil, nil, &msg)
			finalStatus = model.TicketStatusFailed
			h.refreshTicketStatusFromExecutions(ctx, ticket.ID)
			h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)
			break
		}
		if !ok {
			currentExec, _ := h.tickets.GetExecution(ctx, ticket.ID, execRow.ID)
			if currentExec != nil && (currentExec.Status == "failed" || currentExec.Status == "stopped") {
				finalStatus = model.TicketStatusFailed
				break
			}
			continue
		}
		h.refreshTicketStatusFromExecutions(ctx, ticket.ID)
		h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)

		cmd, args, parseErr := sqlreview.ParseRedisCommand(commandLine)
		if parseErr != nil {
			msg := parseErr.Error()
			_ = h.tickets.MarkExecutionDone(ctx, execRow.ID, nil, nil, &msg)
			finalStatus = model.TicketStatusFailed
			h.refreshTicketStatusFromExecutions(ctx, ticket.ID)
			h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)
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

		statementCtx, cancel := context.WithCancel(ctx)
		queryID := ticketExecutionQueryID(execRow.ID)
		if h.activeExecutions != nil {
			if canceled := h.activeExecutions.register(queryID, activeSQLQuery{
				UserID:       executorID,
				ConnectionID: resolvedConn.ID,
				TicketID:     ticket.ID,
				DBType:       "redis",
				Statement:    commandLine,
				Conn:         resolvedConn,
				Cancel:       cancel,
				RegisteredAt: time.Now(),
			}); canceled {
				cancel()
				_ = h.tickets.MarkExecutionStopped(ctx, execRow.ID, "manually stopped")
				finalStatus = model.TicketStatusFailed
				h.refreshTicketStatusFromExecutions(ctx, ticket.ID)
				h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)
				break
			}
		}

		startedAt := time.Now()
		_, execErr := pool.RedisGlobal().DoInDB(statementCtx, pool.RedisConnOptions{
			ConnID:   resolvedConn.ID,
			Host:     resolvedConn.Host,
			Port:     resolvedConn.Port,
			Username: resolvedConn.Username,
			Password: password,
			DB:       dbIndex,
			SSLMode:  resolvedConn.SSLMode,
		}, append([]interface{}{cmd}, ifaces...)...)
		if h.activeExecutions != nil {
			h.activeExecutions.remove(queryID)
		}
		cancel()
		durationMs := time.Since(startedAt).Milliseconds()
		currentExec, _ := h.tickets.GetExecution(ctx, ticket.ID, execRow.ID)
		if currentExec != nil && currentExec.Status == "stopped" {
			finalStatus = model.TicketStatusFailed
			h.refreshTicketStatusFromExecutions(ctx, ticket.ID)
			h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)
			break
		}
		if execErr != nil && execErr != redis.Nil {
			msg := execErr.Error()
			_ = h.tickets.MarkExecutionDone(ctx, execRow.ID, nil, &durationMs, &msg)
			finalStatus = model.TicketStatusFailed
			h.refreshTicketStatusFromExecutions(ctx, ticket.ID)
			h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)
			break
		}
		_ = h.tickets.MarkExecutionDone(ctx, execRow.ID, nil, &durationMs, nil)
		h.refreshTicketStatusFromExecutions(ctx, ticket.ID)
		h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)
	}

	h.finishTicket(ctx, ticket.ID, finalStatus, "")

	actionType := "ticket_execute_complete"
	if finalStatus == model.TicketStatusFailed {
		actionType = "ticket_execute_failed"
	}
	if opts.Automated {
		actionType = "workflow_auto_execute_complete"
		if finalStatus != model.TicketStatusCompleted {
			actionType = "workflow_auto_execute_failed"
		}
	}
	details := map[string]any{"status": string(finalStatus)}
	if opts.Automated {
		details["automated"] = true
		details["reviewer_id"] = opts.ReviewerID
		details["workflow_rule_name"] = opts.WorkflowRuleName
		if opts.WorkflowRuleID != nil {
			details["workflow_rule_id"] = *opts.WorkflowRuleID
		}
	}
	auditActorID := &executorID
	if opts.Automated {
		auditActorID = &opts.ReviewerID
	}
	h.audit.Log(ctx, repository.AuditEntry{
		ActorID:      auditActorID,
		ActorName:    executorName,
		ActionType:   actionType,
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
		Details:      details,
	})

	if finalStatus == model.TicketStatusCompleted {
		detail := "工單已執行完成。"
		if opts.Automated {
			detail = "Workflow Rule 已自動執行完成。"
		}
		h.dispatchTicketNotification(ctx, ticket, ticketEventCompleted, notificationActorID, detail)
	} else if opts.Automated {
		h.dispatchTicketNotification(ctx, ticket, ticketEventExecutionFailed, notificationActorID, "Workflow Rule 自動執行失敗，請 DBA/Admin 查看 execution log 並重新處理。")
	} else {
		h.dispatchTicketNotification(ctx, ticket, ticketEventExecutionFailed, &executorID, "工單執行失敗，請查看 execution log。")
	}
	h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)
}

func buildParserReviewItems(statements []sqlparse.ParsedStatement) []ticketReviewItem {
	items := make([]ticketReviewItem, 0, len(statements))
	for _, stmt := range statements {
		items = append(items, buildParserPassReviewItem(stmt.Seq, stmt.RawSQL, string(stmt.Kind), inferReviewObjectType(stmt)))
	}
	return items
}

func buildStaticValidationItems(statements []sqlparse.ParsedStatement, ruleMap map[string]bool) []ticketReviewItem {
	items := make([]ticketReviewItem, 0, len(statements))
	for _, stmt := range statements {
		issues := sqlreview.RunStaticChecksParsed(stmt, ruleMap)
		items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodStaticRule, nil, string(stmt.Kind), inferReviewObjectType(stmt), 0, issues))
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
		message := strings.Join(issues, "\n")
		item.Message = &message
	}
	return item
}

func stringPtr(value string) *string {
	return &value
}
