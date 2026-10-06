//go:build integration

package tableschema

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	_ "github.com/go-sql-driver/mysql"
)

type syncITStore struct {
	mu    sync.Mutex
	job   model.TableSchemaSyncJob
	items []model.TableSchemaSyncJobItem
}

func (s *syncITStore) ListQueued(context.Context, uint) ([]model.TableSchemaSyncJob, error) {
	return nil, nil
}
func (s *syncITStore) GetByID(context.Context, uint64) (*model.TableSchemaSyncJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.job
	return &v, nil
}
func (s *syncITStore) ListItems(context.Context, uint64) ([]model.TableSchemaSyncJobItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.TableSchemaSyncJobItem(nil), s.items...), nil
}
func (s *syncITStore) Claim(context.Context, uint64) (bool, error)           { return true, nil }
func (s *syncITStore) InterruptRunning(context.Context) (int64, error)       { return 0, nil }
func (s *syncITStore) MarkItemStarted(context.Context, uint64) (bool, error) { return true, nil }
func (s *syncITStore) MarkItemCreated(_ context.Context, id uint64, hash string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Status = model.TableSchemaSyncItemCreated
			s.items[i].TargetDDLSHA256 = &hash
			s.job.CreatedCount++
			return true, nil
		}
	}
	return false, nil
}
func (s *syncITStore) Fail(_ context.Context, _ uint64, itemID uint64, code string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == itemID {
			s.items[i].Status = model.TableSchemaSyncItemFailed
		} else if s.items[i].Status == model.TableSchemaSyncItemPending {
			s.items[i].Status = model.TableSchemaSyncItemNotStarted
		}
	}
	s.job.Status = model.TableSchemaSyncFailed
	s.job.FailedCount = 1
	s.job.ErrorCode = &code
	return nil
}
func (s *syncITStore) Complete(context.Context, uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.Status = model.TableSchemaSyncCompleted
	return true, nil
}
func (s *syncITStore) MarkCancelled(context.Context, uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.Status = model.TableSchemaSyncCancelled
	for i := range s.items {
		if s.items[i].Status == model.TableSchemaSyncItemPending {
			s.items[i].Status = model.TableSchemaSyncItemNotStarted
		}
	}
	return true, nil
}

type syncITConnections struct {
	connection model.DBConnection
	password   string
}

func (s syncITConnections) GetByID(context.Context, uint64) (*model.DBConnection, error) {
	v := s.connection
	return &v, nil
}
func (s syncITConnections) ResolveCredential(connection *model.DBConnection, _ string) (*model.DBConnection, string, error) {
	v := *connection
	return &v, s.password, nil
}

func TestSyncWorkerIntegration(t *testing.T) {
	if os.Getenv("TABLE_SCHEMA_INTEGRATION") != "1" {
		t.Skip("run through make test-table-schema-integration")
	}
	portValue, _ := strconv.ParseUint(envTableSchema("TABLE_SCHEMA_IT_MYSQL_PORT", "13317"), 10, 16)
	host, port := envTableSchema("TABLE_SCHEMA_IT_HOST", "127.0.0.1"), uint16(portValue)
	db, err := sql.Open("mysql", fmt.Sprintf("root:table_schema_it@tcp(%s:%d)/?parseTime=true", host, port))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{"DROP DATABASE IF EXISTS ts_source", "DROP DATABASE IF EXISTS ts_target", "CREATE DATABASE ts_source", "CREATE DATABASE ts_target", "CREATE TABLE ts_source.parent (id BIGINT PRIMARY KEY) ENGINE=InnoDB", "CREATE TABLE ts_source.child (id BIGINT PRIMARY KEY, parent_id BIGINT, CONSTRAINT fk_parent FOREIGN KEY (parent_id) REFERENCES parent(id)) ENGINE=InnoDB"} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP DATABASE IF EXISTS ts_source")
		_, _ = db.Exec("DROP DATABASE IF EXISTS ts_target")
	})

	connection := model.DBConnection{DBType: "mysql", Host: host, Port: port, Username: "root"}
	connections := syncITConnections{connection: connection, password: "table_schema_it"}
	config, _ := json.Marshal(TransformationConfig{})
	makeStore := func(names ...string) *syncITStore {
		items := make([]model.TableSchemaSyncJobItem, len(names))
		for i, name := range names {
			var ddlName, ddl string
			if err := db.QueryRow("SHOW CREATE TABLE ts_source."+QuoteIdentifier(name)).Scan(&ddlName, &ddl); err != nil {
				t.Fatal(err)
			}
			items[i] = model.TableSchemaSyncJobItem{ID: uint64(i + 1), JobID: 1, TableName: name, DependencyOrder: uint(i), SourceDDLSHA256: hashDDL(ddl), Status: model.TableSchemaSyncItemPending}
		}
		return &syncITStore{job: model.TableSchemaSyncJob{ID: 1, SourceConnectionID: 1, SourceDatabase: "ts_source", TargetConnectionID: 2, TargetDatabase: "ts_target", TransformationConfig: config, Status: model.TableSchemaSyncRunning, TableCount: uint(len(items))}, items: items}
	}

	t.Run("dependency order creates parent before child", func(t *testing.T) {
		store := makeStore("parent", "child")
		worker := NewSyncWorker(store, connections, nil, nil)
		if err := worker.runJob(context.Background(), &store.job); err != nil {
			t.Fatal(err)
		}
		if store.job.Status != model.TableSchemaSyncCompleted || store.job.CreatedCount != 2 {
			t.Fatalf("job = %+v", store.job)
		}
	})

	if _, err := db.Exec("DROP DATABASE ts_target"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE DATABASE ts_target"); err != nil {
		t.Fatal(err)
	}
	t.Run("target race preserves partial success and stops remaining work", func(t *testing.T) {
		if _, err := db.Exec("CREATE TABLE ts_target.child (id BIGINT PRIMARY KEY)"); err != nil {
			t.Fatal(err)
		}
		store := makeStore("parent", "child")
		worker := NewSyncWorker(store, connections, nil, nil)
		if err := worker.runJob(context.Background(), &store.job); err == nil {
			t.Fatal("expected second CREATE to fail")
		}
		if store.job.Status != model.TableSchemaSyncFailed || store.job.CreatedCount != 1 || store.items[0].Status != model.TableSchemaSyncItemCreated || store.items[1].Status != model.TableSchemaSyncItemFailed {
			t.Fatalf("job = %+v items = %+v", store.job, store.items)
		}
	})

	resetTarget := func(t *testing.T) {
		t.Helper()
		if _, err := db.Exec("DROP DATABASE ts_target"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("CREATE DATABASE ts_target"); err != nil {
			t.Fatal(err)
		}
	}
	lockDDL := func(t *testing.T) func() {
		t.Helper()
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(context.Background(), "FLUSH TABLES WITH READ LOCK"); err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		return func() { _, _ = conn.ExecContext(context.Background(), "UNLOCK TABLES"); _ = conn.Close() }
	}
	tableExists := func(name string) bool {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='ts_target' AND TABLE_NAME=?", name).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count == 1
	}

	t.Run("cancel during blocked DDL marks remaining work cancelled", func(t *testing.T) {
		resetTarget(t)
		unlock := lockDDL(t)
		store := makeStore("parent")
		worker := NewSyncWorker(store, connections, nil, nil)
		done := make(chan error, 1)
		go func() { done <- worker.runJob(context.Background(), &store.job) }()
		time.Sleep(300 * time.Millisecond)
		store.mu.Lock()
		store.job.Status = model.TableSchemaSyncCancelRequested
		store.mu.Unlock()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cancel did not stop blocked CREATE")
		}
		unlock()
		if store.job.Status != model.TableSchemaSyncCancelled || tableExists("parent") {
			t.Fatalf("job = %+v, target exists = %v", store.job, tableExists("parent"))
		}
	})

	t.Run("parent timeout leaves job for restart recovery", func(t *testing.T) {
		resetTarget(t)
		unlock := lockDDL(t)
		store := makeStore("parent")
		worker := NewSyncWorker(store, connections, nil, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		err := worker.runJob(ctx, &store.job)
		unlock()
		if err == nil || store.job.Status != model.TableSchemaSyncRunning || tableExists("parent") {
			t.Fatalf("err = %v, job = %+v, target exists = %v", err, store.job, tableExists("parent"))
		}
	})
}

func TestSchemaCompatibilityIntegration(t *testing.T) {
	if os.Getenv("TABLE_SCHEMA_INTEGRATION") != "1" {
		t.Skip("run through make test-table-schema-integration")
	}
	host := envTableSchema("TABLE_SCHEMA_IT_HOST", "127.0.0.1")
	tests := []struct {
		name       string
		sourcePort string
		targetPort string
	}{
		{name: "mysql57_same_connection", sourcePort: envTableSchema("TABLE_SCHEMA_IT_MYSQL57_PORT", "13319"), targetPort: envTableSchema("TABLE_SCHEMA_IT_MYSQL57_PORT", "13319")},
		{name: "mysql80_cross_connection", sourcePort: envTableSchema("TABLE_SCHEMA_IT_MYSQL_PORT", "13317"), targetPort: envTableSchema("TABLE_SCHEMA_IT_MYSQL80_TARGET_PORT", "13318")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := openSchemaITDB(t, host, tt.sourcePort)
			target := source
			if tt.targetPort != tt.sourcePort {
				target = openSchemaITDB(t, host, tt.targetPort)
			}
			setupSchemaITSource(t, source)
			resetSchemaITDatabase(t, target, "ts_compat_target")
			request := ExportRequest{Source: SourceSelection{Database: "ts_compat_source", Tables: []string{"parent", "child", "events"}}, Transformation: TransformationConfig{ResetAutoIncrement: true, Engine: "InnoDB", Charset: "utf8mb4", Collation: "utf8mb4_general_ci", RowFormat: "COMPACT"}}
			result, err := BuildExport(context.Background(), source, request)
			if err != nil {
				t.Fatal(err)
			}
			syncResult, _, err := BuildSyncPreview(context.Background(), source, target, SyncPreviewRequest{Source: request.Source, Target: TargetSelection{Database: "ts_compat_target"}, Transformation: request.Transformation})
			if err != nil {
				t.Fatal(err)
			}
			if syncResult.Script != result.Script {
				t.Fatal("export and sync preview produced different DDL")
			}
			if fmt.Sprint(result.Order) != "[events parent child]" {
				t.Fatalf("unexpected dependency order: %v", result.Order)
			}
			for _, statement := range splitSchemaITScript(syncResult.Script) {
				if _, err := target.Exec(statement); err != nil {
					t.Fatalf("execute generated DDL: %v\n%s", err, statement)
				}
			}
			for _, name := range []string{"parent", "child", "events"} {
				var tableName, ddl string
				if err := target.QueryRow("SHOW CREATE TABLE ts_compat_target."+QuoteIdentifier(name)).Scan(&tableName, &ddl); err != nil {
					t.Fatalf("read target %s: %v", name, err)
				}
			}
			var childDDL, childName, eventsDDL, eventsName string
			_ = target.QueryRow("SHOW CREATE TABLE ts_compat_target.child").Scan(&childName, &childDDL)
			_ = target.QueryRow("SHOW CREATE TABLE ts_compat_target.events").Scan(&eventsName, &eventsDDL)
			for _, want := range []string{"GENERATED ALWAYS", "UNIQUE KEY", "FOREIGN KEY", "ROW_FORMAT=COMPACT"} {
				if !strings.Contains(childDDL, want) {
					t.Fatalf("child DDL missing %q: %s", want, childDDL)
				}
			}
			if !strings.Contains(eventsDDL, "PARTITION BY RANGE") {
				t.Fatalf("partition DDL was not preserved: %s", eventsDDL)
			}
		})
	}
}

func openSchemaITDB(t *testing.T, host, port string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", fmt.Sprintf("root:table_schema_it@tcp(%s:%s)/?parseTime=true&multiStatements=true", host, port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func resetSchemaITDatabase(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	if _, err := db.Exec("DROP DATABASE IF EXISTS " + QuoteIdentifier(name)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE DATABASE " + QuoteIdentifier(name)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS " + QuoteIdentifier(name)) })
}

func setupSchemaITSource(t *testing.T, db *sql.DB) {
	t.Helper()
	resetSchemaITDatabase(t, db, "ts_compat_source")
	statements := []string{
		"CREATE TABLE ts_compat_source.parent (id BIGINT NOT NULL AUTO_INCREMENT, code VARCHAR(32) NOT NULL, PRIMARY KEY (id), UNIQUE KEY uq_parent_code (code)) ENGINE=InnoDB AUTO_INCREMENT=42 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC",
		"CREATE TABLE ts_compat_source.child (id BIGINT NOT NULL, parent_id BIGINT NOT NULL, slug VARCHAR(64) NOT NULL, slug_len INT GENERATED ALWAYS AS (char_length(slug)) STORED, PRIMARY KEY (id), KEY idx_child_slug (slug), CONSTRAINT fk_compat_parent FOREIGN KEY (parent_id) REFERENCES parent(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC",
		"CREATE TABLE ts_compat_source.events (id BIGINT NOT NULL, created_day DATE NOT NULL, payload VARCHAR(64), PRIMARY KEY (id, created_day)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci PARTITION BY RANGE (YEAR(created_day)) (PARTITION p2025 VALUES LESS THAN (2026), PARTITION pmax VALUES LESS THAN MAXVALUE)",
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func splitSchemaITScript(script string) []string {
	parts := strings.Split(strings.TrimSpace(script), ";")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if statement := strings.TrimSpace(part); statement != "" {
			result = append(result, statement)
		}
	}
	return result
}

func envTableSchema(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
