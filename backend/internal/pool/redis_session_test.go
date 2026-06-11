package pool

import (
	"context"
	"fmt"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedisManagerDoInDBSelectsDatabaseBeforeCommand(t *testing.T) {
	original := newRedisSession
	defer func() { newRedisSession = original }()

	recorder := &fakeRedisSession{}
	newRedisSession = func(client *redis.Client) redisSession {
		return recorder
	}

	manager := &RedisManager{clients: make(map[string]redis.UniversalClient)}
	_, err := manager.DoInDB(context.Background(), RedisConnOptions{
		ConnID:  1,
		Host:    "redis.internal",
		Port:    6379,
		DB:      5,
		SSLMode: "disable",
	}, "GET", "user:1")
	if err != nil {
		t.Fatalf("DoInDB() error = %v", err)
	}

	if len(recorder.commands) != 2 {
		t.Fatalf("commands = %#v, want 2", recorder.commands)
	}
	if recorder.commands[0] != "SELECT 5" {
		t.Fatalf("commands[0] = %q, want %q", recorder.commands[0], "SELECT 5")
	}
	if recorder.commands[1] != "GET user:1" {
		t.Fatalf("commands[1] = %q, want %q", recorder.commands[1], "GET user:1")
	}
}

type fakeRedisSession struct {
	commands []string
}

func (s *fakeRedisSession) Do(_ context.Context, args ...interface{}) *redis.Cmd {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, fmt.Sprint(arg))
	}
	s.commands = append(s.commands, joinRedisCommand(parts))

	cmd := redis.NewCmd(context.Background(), args...)
	if len(parts) > 0 && parts[0] == "GET" {
		cmd.SetVal("value")
	} else {
		cmd.SetVal("OK")
	}
	return cmd
}

func (s *fakeRedisSession) Close() error {
	return nil
}

func joinRedisCommand(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out += " " + part
	}
	return out
}
