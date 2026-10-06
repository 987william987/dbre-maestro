package tableschema

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dbre-maestro/maestro/internal/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

func BuildRetryPlan(ctx context.Context, source, target Queryer, job *model.TableSchemaSyncJob, items []model.TableSchemaSyncJobItem) ([]RetryItem, error) {
	if job == nil || (job.Status != model.TableSchemaSyncFailed && job.Status != model.TableSchemaSyncCancelled && job.Status != model.TableSchemaSyncInterrupted) || len(items) == 0 {
		return nil, &StableError{Code: ErrorInvalidRetryState, Message: "sync job is not retryable"}
	}
	names := make([]string, len(items))
	byName := make(map[string]model.TableSchemaSyncJobItem, len(items))
	for i, item := range items {
		names[i] = item.TableName
		byName[item.TableName] = item
	}
	snapshot, err := LoadSnapshot(ctx, source, job.SourceDatabase, names)
	if err != nil {
		return nil, err
	}
	for _, table := range snapshot.Tables {
		if hashDDL(table.CreateSQL) != byName[table.Name].SourceDDLSHA256 {
			return nil, &StableError{Code: ErrorSourceDDLChanged, Message: "source table definition changed; create a new preview"}
		}
	}
	deps, err := LoadDependencies(ctx, source, job.SourceDatabase, names)
	if err != nil {
		return nil, err
	}
	analysis, err := AnalyzeDependencies(job.SourceDatabase, names, deps)
	if err != nil {
		return nil, err
	}
	if len(analysis.Cycles) > 0 {
		return nil, &StableError{Code: ErrorForeignKeyCycle, Message: "selected tables contain a foreign key cycle"}
	}
	for _, dep := range analysis.External {
		database := dep.ReferencedDatabase
		if database == job.SourceDatabase {
			database = job.TargetDatabase
		}
		exists, err := tableExists(ctx, target, database, dep.ReferencedTable)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, &StableError{Code: ErrorExternalMissing, Message: "an external foreign key dependency is missing on target"}
		}
	}
	var config TransformationConfig
	if err := json.Unmarshal(job.TransformationConfig, &config); err != nil {
		return nil, &StableError{Code: ErrorInvalidRetryState, Message: "stored transformation is invalid"}
	}
	caps, err := LoadCapabilities(ctx, target)
	if err != nil {
		return nil, err
	}
	result := make([]RetryItem, 0, len(items))
	for _, name := range analysis.Order {
		item := byName[name]
		ddl, exists, err := retryTargetDDL(ctx, target, job.TargetDatabase, name)
		if err != nil {
			return nil, err
		}
		if item.Status == model.TableSchemaSyncItemCreated {
			if item.TargetDDLSHA256 == nil || !exists || hashDDL(ddl) != *item.TargetDDLSHA256 {
				return nil, &StableError{Code: ErrorRetryTargetDrifted, Message: "a previously created target table changed or is missing"}
			}
			continue
		}
		if exists {
			return nil, &StableError{Code: ErrorRetryTargetDrifted, Message: "an unowned target table now exists"}
		}
		parsed, err := ParseCreateTable(snapshotTableDDL(snapshot, name))
		if err != nil {
			return nil, err
		}
		if _, err := TransformCreateTable(parsed, job.TargetDatabase, config, caps); err != nil {
			return nil, err
		}
		result = append(result, RetryItem{TableName: name, DependencyOrder: item.DependencyOrder, SourceDDLSHA256: item.SourceDDLSHA256})
	}
	if len(result) == 0 {
		return nil, &StableError{Code: ErrorInvalidRetryState, Message: "sync job has no remaining tables"}
	}
	return result, nil
}

func retryTargetDDL(ctx context.Context, target Queryer, database, table string) (string, bool, error) {
	ddl, err := readCreateTable(ctx, target, database, table)
	if err == nil {
		return ddl, true, nil
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1146 {
		return "", false, nil
	}
	return "", false, err
}

func snapshotTableDDL(snapshot *Snapshot, name string) string {
	for _, table := range snapshot.Tables {
		if table.Name == name {
			return table.CreateSQL
		}
	}
	return ""
}
