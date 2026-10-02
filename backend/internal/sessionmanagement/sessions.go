package sessionmanagement

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/redis/go-redis/v9"
)

const maxSessions = 1000

type Session struct {
	ID              string  `json:"id"`
	User            string  `json:"user,omitempty"`
	Database        string  `json:"database,omitempty"`
	Client          string  `json:"client,omitempty"`
	State           string  `json:"state,omitempty"`
	DurationSeconds float64 `json:"duration_seconds"`
	Query           string  `json:"query,omitempty"`
	Command         string  `json:"command,omitempty"`
	Protected       bool    `json:"protected"`
	ProtectedReason string  `json:"protected_reason,omitempty"`
	QueryHash       string  `json:"query_hash"`
	BackendStart    string  `json:"backend_start,omitempty"`
}

type SessionResult struct {
	Items     []Session `json:"items"`
	Truncated bool      `json:"truncated"`
}

func ListSessions(ctx context.Context, conn *model.DBConnection, password string) (SessionResult, error) {
	switch strings.ToLower(strings.TrimSpace(conn.DBType)) {
	case "mysql":
		return listMySQLSessions(ctx, conn, password)
	case "postgres", "postgresql":
		return listPostgresSessions(ctx, conn, password)
	case "redis":
		return listRedisSessions(ctx, conn, password)
	default:
		return SessionResult{}, fmt.Errorf("unsupported DB type %q", conn.DBType)
	}
}

func listMySQLSessions(ctx context.Context, conn *model.DBConnection, password string) (SessionResult, error) {
	db, err := sql.Open("mysql", pool.BuildMySQLDSN(conn.Host, conn.Port, conn.Username, password, ""))
	if err != nil {
		return SessionResult{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	connection, err := db.Conn(ctx)
	if err != nil {
		return SessionResult{}, err
	}
	defer connection.Close()
	rows, err := connection.QueryContext(ctx, `SELECT ID, USER, HOST, COALESCE(DB, ''), COMMAND, TIME, COALESCE(STATE, ''), COALESCE(INFO, ''), ID = CONNECTION_ID() FROM information_schema.PROCESSLIST ORDER BY TIME DESC LIMIT 1001`)
	if err != nil {
		return SessionResult{}, fmt.Errorf("query MySQL processlist: %w", err)
	}
	defer rows.Close()
	items := make([]Session, 0)
	for rows.Next() {
		var id uint64
		var user, client, database, command, state, query string
		var seconds int64
		var own bool
		if err := rows.Scan(&id, &user, &client, &database, &command, &seconds, &state, &query, &own); err != nil {
			return SessionResult{}, err
		}
		item := Session{ID: strconv.FormatUint(id, 10), User: user, Client: client, Database: database, Command: command, State: state, DurationSeconds: float64(seconds), Query: query}
		protectMySQL(&item, own)
		item.QueryHash = sessionHash(item.Query, item.Command)
		items = append(items, item)
	}
	return boundedSessions(items, rows.Err())
}

func listPostgresSessions(ctx context.Context, conn *model.DBConnection, password string) (SessionResult, error) {
	db, err := sql.Open("pgx", pool.BuildPostgresDSN(conn.Host, conn.Port, conn.Username, password, conn.DatabaseName, conn.SSLMode))
	if err != nil {
		return SessionResult{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.QueryContext(ctx, `SELECT pid, COALESCE(usename, ''), COALESCE(datname, ''), COALESCE(client_addr::text, ''), COALESCE(state, ''), GREATEST(0, EXTRACT(EPOCH FROM (clock_timestamp() - COALESCE(query_start, backend_start)))), COALESCE(query, ''), COALESCE(backend_type, ''), COALESCE(backend_start::text, ''), pid = pg_backend_pid() FROM pg_stat_activity ORDER BY query_start NULLS LAST LIMIT 1001`)
	if err != nil {
		return SessionResult{}, fmt.Errorf("query PostgreSQL activity: %w", err)
	}
	defer rows.Close()
	items := make([]Session, 0)
	for rows.Next() {
		var pid int64
		var user, database, client, state, query, backendType, backendStart string
		var seconds float64
		var own bool
		if err := rows.Scan(&pid, &user, &database, &client, &state, &seconds, &query, &backendType, &backendStart, &own); err != nil {
			return SessionResult{}, err
		}
		item := Session{ID: strconv.FormatInt(pid, 10), User: user, Database: database, Client: client, State: state, DurationSeconds: seconds, Query: query, BackendStart: backendStart}
		protectPostgres(&item, backendType, own)
		item.QueryHash = sessionHash(item.Query, item.Command)
		items = append(items, item)
	}
	return boundedSessions(items, rows.Err())
}

func listRedisSessions(ctx context.Context, conn *model.DBConnection, password string) (SessionResult, error) {
	useTLS := []bool{false, true}
	if strings.EqualFold(conn.SSLMode, "require") {
		useTLS = []bool{true}
	}
	if strings.EqualFold(conn.SSLMode, "disable") {
		useTLS = []bool{false}
	}
	var lastErr error
	for _, enabled := range useTLS {
		result, err := listRedisSessionsWithTLS(ctx, conn, password, enabled)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return SessionResult{}, lastErr
}

func listRedisSessionsWithTLS(ctx context.Context, conn *model.DBConnection, password string, useTLS bool) (SessionResult, error) {
	options := &redis.Options{Addr: pool.BuildRedisAddr(conn.Host, conn.Port), Username: conn.Username, Password: password, DialTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 3 * time.Second}
	if useTLS {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	client := redis.NewClient(options)
	defer client.Close()
	dedicated := client.Conn()
	defer dedicated.Close()
	ownID, err := dedicated.ClientID(ctx).Result()
	if err != nil {
		return SessionResult{}, fmt.Errorf("read Redis client id: %w", err)
	}
	raw, err := dedicated.ClientList(ctx).Result()
	if err != nil {
		return SessionResult{}, fmt.Errorf("read Redis client list: %w", err)
	}
	return boundedSessions(parseRedisClientList(raw, ownID), nil)
}

func parseRedisClientList(raw string, ownID int64) []Session {
	items := make([]Session, 0)
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		fields := map[string]string{}
		for _, field := range strings.Fields(strings.TrimSpace(line)) {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) == 2 {
				fields[parts[0]] = parts[1]
			}
		}
		if fields["id"] == "" {
			continue
		}
		age, _ := strconv.ParseFloat(fields["age"], 64)
		item := Session{ID: fields["id"], User: fields["user"], Database: fields["db"], Client: fields["addr"], State: fields["flags"], DurationSeconds: age, Command: fields["cmd"]}
		id, _ := strconv.ParseInt(fields["id"], 10, 64)
		item.QueryHash = sessionHash(item.Query, item.Command)
		protectRedis(&item, id == ownID)
		items = append(items, item)
	}
	return items
}

func sessionHash(query, command string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(query+"\x00"+command)))
}

func SignalSession(ctx context.Context, conn *model.DBConnection, password, sessionID, action string) error {
	if action != "cancel" && action != "terminate" {
		return errors.New("unsupported session action")
	}
	switch strings.ToLower(strings.TrimSpace(conn.DBType)) {
	case "mysql":
		id, err := strconv.ParseUint(sessionID, 10, 64)
		if err != nil {
			return err
		}
		db, err := sql.Open("mysql", pool.BuildMySQLDSN(conn.Host, conn.Port, conn.Username, password, ""))
		if err != nil {
			return err
		}
		defer db.Close()
		verb := "QUERY"
		if action == "terminate" {
			verb = "CONNECTION"
		}
		_, err = db.ExecContext(ctx, fmt.Sprintf("KILL %s %d", verb, id))
		return err
	case "postgres", "postgresql":
		id, err := strconv.ParseInt(sessionID, 10, 32)
		if err != nil {
			return err
		}
		db, err := sql.Open("pgx", pool.BuildPostgresDSN(conn.Host, conn.Port, conn.Username, password, conn.DatabaseName, conn.SSLMode))
		if err != nil {
			return err
		}
		defer db.Close()
		fn := "pg_cancel_backend"
		if action == "terminate" {
			fn = "pg_terminate_backend"
		}
		var ok bool
		if err := db.QueryRowContext(ctx, "SELECT "+fn+"($1)", id).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errors.New("database rejected session signal")
		}
		return nil
	case "redis":
		if action != "terminate" {
			return errors.New("Redis only supports terminate")
		}
		return signalRedis(ctx, conn, password, sessionID)
	default:
		return errors.New("unsupported DB type")
	}
}

func signalRedis(ctx context.Context, conn *model.DBConnection, password, sessionID string) error {
	useTLS := []bool{false, true}
	if strings.EqualFold(conn.SSLMode, "require") {
		useTLS = []bool{true}
	}
	if strings.EqualFold(conn.SSLMode, "disable") {
		useTLS = []bool{false}
	}
	var last error
	for _, enabled := range useTLS {
		opts := &redis.Options{Addr: pool.BuildRedisAddr(conn.Host, conn.Port), Username: conn.Username, Password: password, DialTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 3 * time.Second}
		if enabled {
			opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		client := redis.NewClient(opts)
		err := client.Do(ctx, "CLIENT", "KILL", "ID", sessionID).Err()
		_ = client.Close()
		if err == nil {
			return nil
		}
		last = err
	}
	return last
}

func protectMySQL(item *Session, own bool) {
	user := strings.ToLower(item.User)
	if own {
		item.Protected, item.ProtectedReason = true, "tool-owned session"
	} else if user == "system user" || user == "event_scheduler" || strings.HasPrefix(user, "rds") {
		item.Protected, item.ProtectedReason = true, "system session"
	}
}

func protectPostgres(item *Session, backendType string, own bool) {
	if own {
		item.Protected, item.ProtectedReason = true, "tool-owned session"
	} else if backendType != "client backend" || strings.HasPrefix(strings.ToLower(item.User), "rds") {
		item.Protected, item.ProtectedReason = true, "system session"
	}
}

func protectRedis(item *Session, own bool) {
	if own {
		item.Protected, item.ProtectedReason = true, "tool-owned session"
	} else if strings.ContainsAny(item.State, "MS") {
		item.Protected, item.ProtectedReason = true, "replication session"
	}
}

func boundedSessions(items []Session, err error) (SessionResult, error) {
	if err != nil {
		return SessionResult{}, err
	}
	if len(items) > maxSessions {
		return SessionResult{Items: items[:maxSessions], Truncated: true}, nil
	}
	return SessionResult{Items: items}, nil
}
