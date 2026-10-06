package tableschema

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestBuildRetryPlanRejectsNonTerminalJobBeforeQueries(t *testing.T) {
	_, err := BuildRetryPlan(context.Background(), nil, nil, &model.TableSchemaSyncJob{Status: model.TableSchemaSyncRunning}, []model.TableSchemaSyncJobItem{{TableName: "orders"}})
	if stableCode(err) != ErrorInvalidRetryState {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildRetryPlanRejectsSourceDriftBeforeTargetChecks(t *testing.T) {
	source, mock, _ := sqlmock.New()
	defer source.Close()
	ddl := "CREATE TABLE `orders` (`id` bigint) ENGINE=InnoDB"
	mock.ExpectQuery("SHOW CREATE TABLE").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("orders", ddl))
	job := &model.TableSchemaSyncJob{Status: model.TableSchemaSyncFailed, SourceDatabase: "source"}
	items := []model.TableSchemaSyncJobItem{{TableName: "orders", SourceDDLSHA256: "old", Status: model.TableSchemaSyncItemFailed}}
	_, err := BuildRetryPlan(context.Background(), source, nil, job, items)
	if stableCode(err) != ErrorSourceDDLChanged {
		t.Fatalf("error = %v", err)
	}
}

func TestRetryTargetDDLOnlyTreatsMissingTableAsAbsent(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SHOW CREATE TABLE").WillReturnError(&mysqlDriver.MySQLError{Number: 1146, Message: "missing"})
	_, exists, err := retryTargetDDL(context.Background(), db, "target", "orders")
	if err != nil || exists {
		t.Fatalf("missing table = %v, %v", exists, err)
	}

	mock.ExpectQuery("SHOW CREATE TABLE").WillReturnError(errors.New("permission denied"))
	_, _, err = retryTargetDDL(context.Background(), db, "target", "orders")
	if err == nil {
		t.Fatal("query failures must not be treated as a safe missing table")
	}
}

func TestBuildRetryPlanProvesCreatedOwnershipAndPlansOnlyMissingItems(t *testing.T) {
	for _, tc := range []struct {
		name, parentTargetDDL string
		childExists           bool
		wantCode              ErrorCode
	}{
		{name: "matching created hash is skipped"},
		{name: "created target drift is rejected", parentTargetDDL: "CREATE TABLE `parent` (`id` bigint, `changed` int) ENGINE=InnoDB", wantCode: ErrorRetryTargetDrifted},
		{name: "unowned remaining target is rejected", childExists: true, wantCode: ErrorRetryTargetDrifted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, sourceMock, _ := sqlmock.New()
			defer source.Close()
			target, targetMock, _ := sqlmock.New()
			defer target.Close()
			parentDDL := "CREATE TABLE `parent` (`id` bigint) ENGINE=InnoDB"
			childDDL := "CREATE TABLE `child` (`id` bigint, `parent_id` bigint, CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parent` (`id`)) ENGINE=InnoDB"
			sourceMock.ExpectQuery("SHOW CREATE TABLE").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("child", childDDL))
			sourceMock.ExpectQuery("SHOW CREATE TABLE").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("parent", parentDDL))
			sourceMock.ExpectQuery("FROM information_schema.KEY_COLUMN_USAGE").WithArgs("source", "child", "parent").WillReturnRows(sqlmock.NewRows([]string{"TABLE_SCHEMA", "TABLE_NAME", "REFERENCED_TABLE_SCHEMA", "REFERENCED_TABLE_NAME"}).AddRow("source", "child", "source", "parent"))
			targetMock.ExpectQuery("SHOW ENGINES").WillReturnRows(sqlmock.NewRows([]string{"Engine", "Support", "Comment", "Transactions", "XA", "Savepoints"}).AddRow("InnoDB", "DEFAULT", "", "YES", "YES", "YES"))
			targetMock.ExpectQuery("FROM information_schema.COLLATIONS").WillReturnRows(sqlmock.NewRows([]string{"CHARACTER_SET_NAME", "COLLATION_NAME"}))
			returnedParent := tc.parentTargetDDL
			if returnedParent == "" {
				returnedParent = parentDDL
			}
			targetMock.ExpectQuery("SHOW CREATE TABLE").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("parent", returnedParent))
			if tc.wantCode == ErrorRetryTargetDrifted && tc.parentTargetDDL != "" {
				// Ownership failure stops before checking remaining tables.
			} else if tc.childExists {
				targetMock.ExpectQuery("SHOW CREATE TABLE").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("child", childDDL))
			} else {
				targetMock.ExpectQuery("SHOW CREATE TABLE").WillReturnError(&mysqlDriver.MySQLError{Number: 1146, Message: "missing"})
			}
			parentHash := hashDDL(parentDDL)
			job := &model.TableSchemaSyncJob{Status: model.TableSchemaSyncFailed, SourceDatabase: "source", TargetDatabase: "target", TransformationConfig: []byte(`{}`)}
			items := []model.TableSchemaSyncJobItem{{TableName: "child", DependencyOrder: 1, SourceDDLSHA256: hashDDL(childDDL), Status: model.TableSchemaSyncItemFailed}, {TableName: "parent", DependencyOrder: 0, SourceDDLSHA256: hashDDL(parentDDL), TargetDDLSHA256: &parentHash, Status: model.TableSchemaSyncItemCreated}}
			plan, err := BuildRetryPlan(context.Background(), source, target, job, items)
			if stableCode(err) != tc.wantCode {
				t.Fatalf("error = %v", err)
			}
			if tc.wantCode == "" && (len(plan) != 1 || plan[0].TableName != "child" || plan[0].DependencyOrder != 1) {
				t.Fatalf("plan = %+v", plan)
			}
			if err := sourceMock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if err := targetMock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
