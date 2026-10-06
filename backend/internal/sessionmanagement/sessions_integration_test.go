//go:build integration

package sessionmanagement

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/redis/go-redis/v9"
)

func TestSessionManagementIntegration(t *testing.T) {
	if os.Getenv("SESSION_MANAGEMENT_INTEGRATION") != "1" {
		t.Skip("run through make test-session-management-integration")
	}
	host := envOr("SESSION_IT_HOST", "127.0.0.1")

	t.Run("MySQL lists and cancels only the recorded fixture query", func(t *testing.T) {
		port := envPort(t, "SESSION_IT_MYSQL_PORT", 13316)
		db := openFixtureDB(t, "mysql", fmt.Sprintf("root:session_it@tcp(%s:%d)/session_it?parseTime=true", host, port))
		conn := fixtureConn(t, db)
		var id uint64
		if err := conn.QueryRowContext(context.Background(), "SELECT CONNECTION_ID()").Scan(&id); err != nil {
			t.Fatal(err)
		}
		done := runBlockingQuery(conn, "SELECT SLEEP(30)")
		target := &model.DBConnection{DBType: "mysql", Host: host, Port: port, Username: "root"}
		awaitListed(t, target, "session_it", strconv.FormatUint(id, 10), "sleep")
		if err := SignalSession(context.Background(), target, "session_it", strconv.FormatUint(id, 10), "cancel"); err != nil {
			t.Fatal(err)
		}
		awaitStopped(t, done)
	})

	t.Run("PostgreSQL lists and terminates only the recorded fixture backend", func(t *testing.T) {
		port := envPort(t, "SESSION_IT_POSTGRES_PORT", 15432)
		db := openFixtureDB(t, "pgx", fmt.Sprintf("postgres://postgres:session_it@%s:%d/session_it?sslmode=disable", host, port))
		conn := fixtureConn(t, db)
		var id int64
		if err := conn.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&id); err != nil {
			t.Fatal(err)
		}
		done := runBlockingQuery(conn, "SELECT pg_sleep(30)")
		database := "session_it"
		target := &model.DBConnection{DBType: "postgres", Host: host, Port: port, Username: "postgres", DatabaseName: &database, SSLMode: "disable"}
		awaitListed(t, target, "session_it", strconv.FormatInt(id, 10), "pg_sleep")
		if err := SignalSession(context.Background(), target, "session_it", strconv.FormatInt(id, 10), "terminate"); err != nil {
			t.Fatal(err)
		}
		awaitStopped(t, done)
	})

	t.Run("Redis lists and disconnects only the recorded fixture client", func(t *testing.T) {
		port := envPort(t, "SESSION_IT_REDIS_PORT", 16379)
		client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%d", host, port)})
		t.Cleanup(func() { _ = client.Close() })
		conn := client.Conn()
		t.Cleanup(func() { _ = conn.Close() })
		id, err := conn.ClientID(context.Background()).Result()
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- conn.BLPop(context.Background(), 30*time.Second, "session-management-fixture").Err() }()
		target := &model.DBConnection{DBType: "redis", Host: host, Port: port, SSLMode: "disable"}
		awaitListed(t, target, "", strconv.FormatInt(id, 10), "blpop")
		if err := SignalSession(context.Background(), target, "", strconv.FormatInt(id, 10), "terminate"); err != nil {
			t.Fatal(err)
		}
		awaitStopped(t, done)
	})
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envPort(t *testing.T, key string, fallback uint16) uint16 {
	t.Helper()
	value, err := strconv.ParseUint(envOr(key, strconv.Itoa(int(fallback))), 10, 16)
	if err != nil || value == 0 {
		t.Fatalf("invalid %s", key)
	}
	return uint16(value)
}
func openFixtureDB(t *testing.T, driver, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
func fixtureConn(t *testing.T, db *sql.DB) *sql.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
func runBlockingQuery(conn *sql.Conn, query string) <-chan error {
	done := make(chan error, 1)
	go func() { _, err := conn.ExecContext(context.Background(), query); done <- err }()
	return done
}
func awaitListed(t *testing.T, target *model.DBConnection, password, id, command string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result, err := ListSessions(context.Background(), target, password)
		if err == nil {
			for _, item := range result.Items {
				visibleCommand := strings.ToLower(item.Query + " " + item.Command)
				if item.ID == id && strings.Contains(visibleCommand, command) {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("fixture session %s was not visible for %s", id, target.DBType)
}
func awaitStopped(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("signaled fixture did not stop")
	}
}
