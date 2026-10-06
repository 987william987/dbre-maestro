package sessionmanagement

import (
	"context"
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
)

func TestParseRedisClientListPreservesOnlySafeCommandMetadata(t *testing.T) {
	items := parseRedisClientList("id=7 addr=10.0.0.7:51111 age=12 flags=N db=2 cmd=get user=app\nid=8 addr=127.0.0.1:1 age=1 flags=N db=0 cmd=client user=ops\nid=9 addr=10.0.0.9:6379 age=30 flags=S db=0 cmd=replconf user=default", 8)
	if len(items) != 3 || items[0].ID != "7" || items[0].Command != "get" || items[0].DurationSeconds != 12 {
		t.Fatalf("parseRedisClientList() = %#v", items)
	}
	if !items[1].Protected || items[1].ProtectedReason != "tool-owned session" {
		t.Fatalf("own Redis session = %#v, want protected", items[1])
	}
	if !items[2].Protected || items[2].ProtectedReason != "replication session" {
		t.Fatalf("Redis replication session = %#v, want protected", items[2])
	}
}

func TestProtectedClassificationCoversToolAndSystemSessions(t *testing.T) {
	mysql := Session{User: "rdsadmin"}
	protectMySQL(&mysql, false)
	postgres := Session{User: "app"}
	protectPostgres(&postgres, "autovacuum worker", false)
	tool := Session{User: "app"}
	protectPostgres(&tool, "client backend", true)
	if !mysql.Protected || !postgres.Protected || !tool.Protected || tool.ProtectedReason != "tool-owned session" {
		t.Fatalf("classification failed: mysql=%#v postgres=%#v tool=%#v", mysql, postgres, tool)
	}
}

func TestBoundedSessionsReportsTruncation(t *testing.T) {
	items := make([]Session, maxSessions+1)
	result, err := boundedSessions(items, nil)
	if err != nil || !result.Truncated || len(result.Items) != maxSessions {
		t.Fatalf("boundedSessions() = (%#v, %v)", result, err)
	}
}

func TestSignalSessionRejectsUnknownActionBeforeConnecting(t *testing.T) {
	err := SignalSession(context.Background(), &model.DBConnection{DBType: "mysql"}, "secret", "42", "restart")
	if err == nil || err.Error() != "unsupported session action" {
		t.Fatalf("SignalSession() error = %v, want unsupported action", err)
	}
}
