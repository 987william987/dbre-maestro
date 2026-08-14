package handler

import (
	"strings"
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

func TestExtractMy2SQLStatementsSkipsStatsOutput(t *testing.T) {
	raw := `
binlog              starttime            stoptime             startpos   stoppos    rows

binlog              starttime            stoptime             startpos   stoppos    inserts   updates   deletes   database   table
mysql-bin.000001    2026-08-06_08:07:08  2026-08-06_08:07:08  20558018   20558113   0         0         1         william    test_n

INSERT INTO ` + "`william`.`test_n` (`id`) VALUES (1);" + `
`

	got := extractMy2SQLStatements(raw)
	want := "INSERT INTO `william`.`test_n` (`id`) VALUES (1);"
	if got != want {
		t.Fatalf("unexpected rollback sql\nwant: %q\n got: %q", want, got)
	}
}

func TestMySQLPriorBackupPlanForUpdate(t *testing.T) {
	dbName := "app"
	ticket := &model.Ticket{ID: 7, DatabaseName: &dbName}
	execRow := model.TicketExecution{ID: 11, Seq: 2, SQLStmt: "UPDATE accounts SET balance = balance - 10 WHERE id = 1"}

	plan, err := buildMySQLPriorBackupPlan(ticket, execRow, sqlparse.StatementKindUpdate)
	if err != nil {
		t.Fatalf("buildMySQLPriorBackupPlan() error = %v", err)
	}
	plan.columns = []string{"id", "balance", "updated_at"}
	plan.primaryKeys = []string{"id"}

	insertSQL := buildMySQLPriorBackupInsertSQL(plan)
	if !strings.Contains(insertSQL, "INSERT INTO `maestro_rollback`.`_maestro_rb_t7_e11_s2` (`id`, `balance`, `updated_at`)") {
		t.Fatalf("backup insert SQL missing backup table/columns: %s", insertSQL)
	}
	if !strings.Contains(insertSQL, "FROM `accounts` WHERE `id`=1") {
		t.Fatalf("backup insert SQL missing source WHERE: %s", insertSQL)
	}

	restoreSQL := buildMySQLPriorBackupRestoreSQL(plan)
	want := "INSERT INTO `app`.`accounts` (`id`, `balance`, `updated_at`) SELECT `id`, `balance`, `updated_at` FROM `maestro_rollback`.`_maestro_rb_t7_e11_s2` ON DUPLICATE KEY UPDATE `balance` = VALUES(`balance`);"
	if restoreSQL != want {
		t.Fatalf("restore SQL mismatch\nwant: %s\n got: %s", want, restoreSQL)
	}
}

func TestMySQLPriorBackupPlanForDelete(t *testing.T) {
	dbName := "app"
	ticket := &model.Ticket{ID: 7, DatabaseName: &dbName}
	execRow := model.TicketExecution{ID: 12, Seq: 3, SQLStmt: "DELETE FROM accounts WHERE status = 'closed'"}

	plan, err := buildMySQLPriorBackupPlan(ticket, execRow, sqlparse.StatementKindDelete)
	if err != nil {
		t.Fatalf("buildMySQLPriorBackupPlan() error = %v", err)
	}
	plan.columns = []string{"id", "status"}

	restoreSQL := buildMySQLPriorBackupRestoreSQL(plan)
	want := "INSERT INTO `app`.`accounts` (`id`, `status`) SELECT `id`, `status` FROM `maestro_rollback`.`_maestro_rb_t7_e12_s3`;"
	if restoreSQL != want {
		t.Fatalf("restore SQL mismatch\nwant: %s\n got: %s", want, restoreSQL)
	}
}

func TestMySQLPriorBackupPlanSupportsOrderedLimit(t *testing.T) {
	dbName := "app"
	ticket := &model.Ticket{ID: 7, DatabaseName: &dbName}
	tests := []struct {
		name string
		sql  string
		kind sqlparse.StatementKind
	}{
		{
			name: "update order limit",
			sql:  "UPDATE accounts SET balance = 0 WHERE status = 'closed' ORDER BY id LIMIT 1",
			kind: sqlparse.StatementKindUpdate,
		},
		{
			name: "delete order limit",
			sql:  "DELETE FROM accounts WHERE status = 'closed' ORDER BY id LIMIT 1",
			kind: sqlparse.StatementKindDelete,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := buildMySQLPriorBackupPlan(ticket, model.TicketExecution{ID: 12, Seq: 3, SQLStmt: tt.sql}, tt.kind)
			if err != nil {
				t.Fatalf("buildMySQLPriorBackupPlan() error = %v", err)
			}
			plan.columns = []string{"id", "balance", "status"}
			insertSQL := buildMySQLPriorBackupInsertSQL(plan)
			if !strings.Contains(insertSQL, "WHERE `status`=") {
				t.Fatalf("backup insert SQL missing WHERE clause: %s", insertSQL)
			}
			if !strings.Contains(insertSQL, "ORDER BY `id` LIMIT 1") {
				t.Fatalf("backup insert SQL missing ordered limit: %s", insertSQL)
			}
		})
	}
}

func TestMySQLPriorBackupPlanSupportsSingleTargetJoin(t *testing.T) {
	dbName := "app"
	ticket := &model.Ticket{ID: 7, DatabaseName: &dbName}
	tests := []struct {
		name           string
		sql            string
		kind           sqlparse.StatementKind
		wantSelectFrom string
		wantColumn     string
	}{
		{
			name:           "joined update",
			sql:            "UPDATE accounts a JOIN users u ON u.id = a.user_id SET a.balance = 0 WHERE u.disabled = 1",
			kind:           sqlparse.StatementKindUpdate,
			wantSelectFrom: "FROM `accounts` AS `a` JOIN `users` AS `u` ON `u`.`id`=`a`.`user_id`",
			wantColumn:     "SELECT `a`.`id`, `a`.`balance`, `a`.`status`",
		},
		{
			name:           "joined delete",
			sql:            "DELETE a FROM accounts a JOIN users u ON u.id = a.user_id WHERE u.disabled = 1",
			kind:           sqlparse.StatementKindDelete,
			wantSelectFrom: "FROM `accounts` AS `a` JOIN `users` AS `u` ON `u`.`id`=`a`.`user_id`",
			wantColumn:     "SELECT `a`.`id`, `a`.`balance`, `a`.`status`",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := buildMySQLPriorBackupPlan(ticket, model.TicketExecution{ID: 12, Seq: 3, SQLStmt: tt.sql}, tt.kind)
			if err != nil {
				t.Fatalf("buildMySQLPriorBackupPlan() error = %v", err)
			}
			plan.columns = []string{"id", "balance", "status"}
			insertSQL := buildMySQLPriorBackupInsertSQL(plan)
			if !strings.Contains(insertSQL, tt.wantColumn) {
				t.Fatalf("backup insert SQL missing target-qualified columns\nwant fragment: %s\n got: %s", tt.wantColumn, insertSQL)
			}
			if !strings.Contains(insertSQL, tt.wantSelectFrom) {
				t.Fatalf("backup insert SQL missing joined table refs\nwant fragment: %s\n got: %s", tt.wantSelectFrom, insertSQL)
			}
		})
	}
}

func TestMySQLPriorBackupPlanSupportsMultiTargetJoin(t *testing.T) {
	dbName := "app"
	ticket := &model.Ticket{ID: 7, DatabaseName: &dbName}
	tests := []struct {
		name           string
		sql            string
		kind           sqlparse.StatementKind
		wantStatements []string
	}{
		{
			name: "multi target update",
			sql:  "UPDATE accounts a JOIN users u ON u.id = a.user_id SET a.balance = 0, u.status = 'disabled' WHERE u.disabled = 1",
			kind: sqlparse.StatementKindUpdate,
			wantStatements: []string{
				"FROM `maestro_rollback`.`_maestro_rb_t7_e12_s3_01_a` ON DUPLICATE KEY UPDATE `balance` = VALUES(`balance`);",
				"FROM `maestro_rollback`.`_maestro_rb_t7_e12_s3_02_u` ON DUPLICATE KEY UPDATE `status` = VALUES(`status`);",
			},
		},
		{
			name: "multi target delete",
			sql:  "DELETE a, u FROM accounts a JOIN users u ON u.id = a.user_id WHERE u.disabled = 1",
			kind: sqlparse.StatementKindDelete,
			wantStatements: []string{
				"FROM `maestro_rollback`.`_maestro_rb_t7_e12_s3_01_a`;",
				"FROM `maestro_rollback`.`_maestro_rb_t7_e12_s3_02_u`;",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := buildMySQLPriorBackupPlan(ticket, model.TicketExecution{ID: 12, Seq: 3, SQLStmt: tt.sql}, tt.kind)
			if err != nil {
				t.Fatalf("buildMySQLPriorBackupPlan() error = %v", err)
			}
			if len(plan.targets) != 2 {
				t.Fatalf("expected two prior backup targets, got %d", len(plan.targets))
			}
			for i := range plan.targets {
				plan.targets[i].columns = []string{"id", "balance", "status"}
			}
			restoreSQL := buildMySQLPriorBackupRestoreSQL(plan)
			for _, want := range tt.wantStatements {
				if !strings.Contains(restoreSQL, want) {
					t.Fatalf("restore SQL missing fragment\nwant: %s\n got: %s", want, restoreSQL)
				}
			}
		})
	}
}

func TestMySQLPriorBackupPlanSupportsCTE(t *testing.T) {
	dbName := "app"
	ticket := &model.Ticket{ID: 7, DatabaseName: &dbName}
	tests := []struct {
		name string
		sql  string
		kind sqlparse.StatementKind
	}{
		{
			name: "cte update",
			sql:  "WITH recent_users AS (SELECT id FROM users WHERE active = 1) UPDATE accounts a JOIN recent_users u ON u.id = a.user_id SET a.balance = 0 WHERE a.status = 'closed'",
			kind: sqlparse.StatementKindUpdate,
		},
		{
			name: "cte delete",
			sql:  "WITH recent_users AS (SELECT id FROM users WHERE active = 1) DELETE a FROM accounts a JOIN recent_users u ON u.id = a.user_id WHERE a.status = 'closed'",
			kind: sqlparse.StatementKindDelete,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := buildMySQLPriorBackupPlan(ticket, model.TicketExecution{ID: 12, Seq: 3, SQLStmt: tt.sql}, tt.kind)
			if err != nil {
				t.Fatalf("buildMySQLPriorBackupPlan() error = %v", err)
			}
			plan.targets[0].columns = []string{"id", "balance", "status"}
			insertSQL := buildMySQLPriorBackupInsertSQL(plan)
			if !strings.Contains(insertSQL, "WITH `recent_users` AS") {
				t.Fatalf("backup insert SQL missing CTE: %s", insertSQL)
			}
			if !strings.Contains(insertSQL, "SELECT `a`.`id`, `a`.`balance`, `a`.`status`") {
				t.Fatalf("backup insert SQL missing target-qualified columns: %s", insertSQL)
			}
			if !strings.Contains(insertSQL, "JOIN `recent_users` AS `u`") {
				t.Fatalf("backup insert SQL missing CTE join table ref: %s", insertSQL)
			}
		})
	}
}

func TestMySQLPriorBackupPlanSupportsCTEWithMultiTargetUpdate(t *testing.T) {
	dbName := "app"
	ticket := &model.Ticket{ID: 7, DatabaseName: &dbName}
	sqlText := "WITH recent_users AS (SELECT id FROM users WHERE active = 1) UPDATE accounts a JOIN recent_users u ON u.id = a.user_id JOIN users real_u ON real_u.id = u.id SET a.balance = 0, real_u.status = 'disabled' WHERE a.status = 'closed'"

	plan, err := buildMySQLPriorBackupPlan(ticket, model.TicketExecution{ID: 12, Seq: 3, SQLStmt: sqlText}, sqlparse.StatementKindUpdate)
	if err != nil {
		t.Fatalf("buildMySQLPriorBackupPlan() error = %v", err)
	}
	if len(plan.targets) != 2 {
		t.Fatalf("expected two prior backup targets, got %d", len(plan.targets))
	}
	for i := range plan.targets {
		plan.targets[i].columns = []string{"id", "balance", "status"}
		insertSQL := buildMySQLPriorBackupInsertSQLForTarget(plan, plan.targets[i])
		if !strings.Contains(insertSQL, "WITH `recent_users` AS") {
			t.Fatalf("backup insert SQL missing CTE for target %d: %s", i, insertSQL)
		}
	}
}

func TestMySQLPriorBackupPlanRejectsUnsafeStatements(t *testing.T) {
	dbName := "app"
	ticket := &model.Ticket{ID: 7, DatabaseName: &dbName}
	tests := []struct {
		name string
		sql  string
		kind sqlparse.StatementKind
	}{
		{name: "update without where", sql: "UPDATE accounts SET balance = 0", kind: sqlparse.StatementKindUpdate},
		{name: "delete without where", sql: "DELETE FROM accounts", kind: sqlparse.StatementKindDelete},
		{name: "update with unordered limit", sql: "UPDATE accounts SET balance = 0 WHERE status = 'closed' LIMIT 1", kind: sqlparse.StatementKindUpdate},
		{name: "delete with unordered limit", sql: "DELETE FROM accounts WHERE status = 'closed' LIMIT 1", kind: sqlparse.StatementKindDelete},
		{name: "joined update with unqualified set column", sql: "UPDATE accounts a JOIN users u ON u.id = a.user_id SET balance = 0 WHERE u.disabled = 1", kind: sqlparse.StatementKindUpdate},
		{name: "self joined update with multiple target aliases", sql: "UPDATE accounts a JOIN accounts b ON b.parent_id = a.id SET a.balance = 0, b.balance = 1 WHERE a.id = 1", kind: sqlparse.StatementKindUpdate},
		{name: "self joined delete with multiple target aliases", sql: "DELETE a, b FROM accounts a JOIN accounts b ON b.parent_id = a.id WHERE a.id = 1", kind: sqlparse.StatementKindDelete},
		{name: "update cte target", sql: "WITH target_rows AS (SELECT * FROM accounts WHERE id = 1) UPDATE target_rows SET balance = 0 WHERE id = 1", kind: sqlparse.StatementKindUpdate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildMySQLPriorBackupPlan(ticket, model.TicketExecution{ID: 1, Seq: 1, SQLStmt: tt.sql}, tt.kind)
			if err == nil {
				t.Fatal("expected unsafe statement to be rejected")
			}
		})
	}
}
