//go:build integration

package onlineddl

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestOnlineDDLToolsIntegration(t *testing.T) {
	if os.Getenv("ONLINE_DDL_INTEGRATION") != "1" {
		t.Skip("run through make test-online-ddl-integration")
	}
	host := envOnlineDDL("ONLINE_DDL_IT_HOST", "mysql80-source")
	portValue, err := strconv.ParseUint(envOnlineDDL("ONLINE_DDL_IT_PORT", "3306"), 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	password := envOnlineDDL("ONLINE_DDL_IT_PASSWORD", "online_ddl_it")
	db, err := sql.Open("mysql", fmt.Sprintf("root:%s@tcp(%s:%d)/?parseTime=true&multiStatements=true", password, host, portValue))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP DATABASE IF EXISTS online_ddl_it",
		"CREATE DATABASE online_ddl_it",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS online_ddl_it") })

	adapters := map[string]Adapter{
		ModeGhost: NewGhostAdapter("/usr/local/bin/gh-ost"),
		ModePTOSC: NewPTOSCAdapter("/usr/local/bin/pt-online-schema-change"),
	}
	for mode, adapter := range adapters {
		version, err := adapter.Version(ctx)
		if err != nil || strings.TrimSpace(version) == "" {
			t.Fatalf("%s version=%q err=%v", mode, version, err)
		}
		t.Logf("%s version: %s", mode, version)
	}

	t.Run("source and replica use ROW FULL replication", func(t *testing.T) {
		var format, image string
		var replicas int
		if err := db.QueryRowContext(ctx, "SELECT @@GLOBAL.binlog_format, @@GLOBAL.binlog_row_image").Scan(&format, &image); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM performance_schema.replication_group_members").Scan(&replicas); err != nil {
			// Async replication is proven by SHOW REPLICAS below; group membership may be empty.
			replicas = 0
		}
		rows, err := db.QueryContext(ctx, "SHOW REPLICAS")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		replicaRegistered := rows.Next()
		if !replicaRegistered || !strings.EqualFold(format, "ROW") || !strings.EqualFold(image, "FULL") {
			t.Fatalf("format=%s image=%s group_members=%d replica_registered=%v", format, image, replicas, replicaRegistered)
		}
	})

	for _, mode := range []string{ModeGhost, ModePTOSC} {
		mode := mode
		t.Run(mode+" cut-over", func(t *testing.T) {
			table := integrationTableName(mode, "cutover")
			seedOnlineDDLTable(t, db, table, 11)
			process := startOnlineDDLTool(t, adapters[mode], onlineDDLRequest(host, uint16(portValue), password, mode, table, "ADD COLUMN tool_note VARCHAR(64) NULL"))
			result, waitErr := waitOnlineDDLProcess(t, process, 2*time.Minute)
			if waitErr != nil {
				t.Fatalf("%s failed: %v\nstdout=%s\nstderr=%s", mode, waitErr, result.Stdout, result.Stderr)
			}
			assertColumnExists(t, db, table, "tool_note")
			artifacts, err := DiscoverArtifacts(ctx, SQLArtifactProbe{DB: db}, mode, "online_ddl_it", table)
			if err != nil {
				t.Fatal(err)
			}
			if artifacts.HasArtifacts() {
				t.Fatalf("%s successful cut-over left artifacts: %+v", mode, artifacts.Items)
			}
		})

		t.Run(mode+" pause resume with concurrent DML", func(t *testing.T) {
			table := integrationTableName(mode, "control")
			seedOnlineDDLTable(t, db, table, 16)
			req := onlineDDLRequest(host, uint16(portValue), password, mode, table, "ADD COLUMN controlled_note VARCHAR(64) NULL")
			process := startOnlineDDLTool(t, adapters[mode], req)
			done := make(chan struct{})
			var result ToolResult
			var waitErr error
			go func() { result, waitErr = process.Wait(); close(done) }()
			pauseOnlineDDLProcess(t, process, done, mode)
			select {
			case <-done:
				t.Fatalf("%s completed before pause could be observed: %v\n%s", mode, waitErr, result.Stderr)
			case <-time.After(250 * time.Millisecond):
			}
			updateResult, err := db.ExecContext(ctx, "UPDATE online_ddl_it."+quoteIntegration(table)+" SET payload='during-copy' WHERE id <= 50")
			if err != nil {
				t.Fatal(err)
			}
			wantUpdated, err := updateResult.RowsAffected()
			if err != nil {
				t.Fatal(err)
			}
			if err := process.Resume(); err != nil {
				t.Fatalf("resume %s: %v", mode, err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Minute):
				_ = process.Terminate()
				t.Fatalf("%s did not finish after resume", mode)
			}
			if waitErr != nil {
				t.Fatalf("%s failed after resume: %v\nstdout=%s\nstderr=%s", mode, waitErr, result.Stdout, result.Stderr)
			}
			assertColumnExists(t, db, table, "controlled_note")
			var updated int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM online_ddl_it."+quoteIntegration(table)+" WHERE payload='during-copy'").Scan(&updated); err != nil || int64(updated) != wantUpdated {
				t.Fatalf("concurrent DML rows=%d want=%d err=%v", updated, wantUpdated, err)
			}
		})

		t.Run(mode+" cancel preserves source and inventories artifacts", func(t *testing.T) {
			table := integrationTableName(mode, "cancel")
			seedOnlineDDLTable(t, db, table, 16)
			process := startOnlineDDLTool(t, adapters[mode], onlineDDLRequest(host, uint16(portValue), password, mode, table, "ADD COLUMN cancelled_note VARCHAR(64) NULL"))
			done := make(chan struct{})
			go func() { _, _ = process.Wait(); close(done) }()
			pauseOnlineDDLProcess(t, process, done, mode)
			if err := process.CancelGracefully(); err != nil {
				t.Fatalf("cancel %s: %v", mode, err)
			}
			select {
			case <-done:
			case <-time.After(20 * time.Second):
				_ = process.Terminate()
				<-done
			}
			var sourceExists bool
			if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA='online_ddl_it' AND TABLE_NAME=?)", table).Scan(&sourceExists); err != nil || !sourceExists {
				t.Fatalf("source exists=%v err=%v", sourceExists, err)
			}
			artifacts, err := DiscoverArtifacts(ctx, SQLArtifactProbe{DB: db}, mode, "online_ddl_it", table)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s cancel artifacts: %+v", mode, artifacts.Items)
		})
	}

	t.Run("credential and network failures remain bounded", func(t *testing.T) {
		for name, mutate := range map[string]func(*ToolRequest){
			"invalid credential":   func(req *ToolRequest) { req.Password = "wrong" },
			"unreachable endpoint": func(req *ToolRequest) { req.Host, req.Port = "127.0.0.1", 1 },
		} {
			t.Run(name, func(t *testing.T) {
				req := onlineDDLRequest(host, uint16(portValue), password, ModePTOSC, "ptosc_cutover", "ADD COLUMN never_added INT NULL")
				req.Timeout = 10 * time.Second
				mutate(&req)
				process := startOnlineDDLTool(t, adapters[ModePTOSC], req)
				_, err := waitOnlineDDLProcess(t, process, 15*time.Second)
				if !errors.Is(err, ErrToolProcessFailed) && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("error=%v", err)
				}
			})
		}
	})
}

func onlineDDLRequest(host string, port uint16, password, mode, table, alter string) ToolRequest {
	parameters := DefaultParameters(mode)
	if mode == ModeGhost {
		parameters.Ghost.ChunkSize = 100
		parameters.Ghost.NiceRatio = 1
	} else {
		chunk := 100
		parameters.PTOSC.ChunkSize = &chunk
	}
	return ToolRequest{Statement: Statement{Database: "online_ddl_it", Table: table, AlterClause: alter}, Parameters: parameters, Host: host, Port: port, Username: "root", Password: password, Timeout: 3 * time.Minute, MaxOutputBytes: maxToolOutputBytes}
}

func startOnlineDDLTool(t *testing.T, adapter Adapter, request ToolRequest) ToolProcess {
	t.Helper()
	process, err := adapter.Start(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	return process
}

func waitOnlineDDLProcess(t *testing.T, process ToolProcess, timeout time.Duration) (ToolResult, error) {
	t.Helper()
	done := make(chan struct{})
	var result ToolResult
	var err error
	go func() { result, err = process.Wait(); close(done) }()
	select {
	case <-done:
		return result, err
	case <-time.After(timeout):
		_ = process.Terminate()
		<-done
		t.Fatalf("tool process timed out after %s", timeout)
		return ToolResult{}, context.DeadlineExceeded
	}
}

func pauseOnlineDDLProcess(t *testing.T, process ToolProcess, done <-chan struct{}, mode string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("%s completed before pause became available", mode)
		default:
		}
		if err := process.Pause(); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = process.Terminate()
	t.Fatalf("%s pause did not become available", mode)
}

func seedOnlineDDLTable(t *testing.T, db *sql.DB, table string, doublings int) {
	t.Helper()
	quoted := "online_ddl_it." + quoteIntegration(table)
	if _, err := db.Exec("DROP TABLE IF EXISTS " + quoted); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE " + quoted + " (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, payload VARCHAR(255) NOT NULL) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO " + quoted + " (payload) VALUES (REPEAT('x', 200))"); err != nil {
		t.Fatal(err)
	}
	for range doublings {
		if _, err := db.Exec("INSERT INTO " + quoted + " (payload) SELECT payload FROM " + quoted); err != nil {
			t.Fatal(err)
		}
	}
}

func assertColumnExists(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='online_ddl_it' AND TABLE_NAME=? AND COLUMN_NAME=?)", table, column).Scan(&exists); err != nil || !exists {
		t.Fatalf("column %s.%s exists=%v err=%v", table, column, exists, err)
	}
}

func integrationTableName(mode, suffix string) string {
	return strings.NewReplacer("-", "", " ", "_").Replace(mode + "_" + suffix)
}
func quoteIntegration(value string) string { return "`" + strings.ReplaceAll(value, "`", "``") + "`" }
func envOnlineDDL(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
