package sqlreview

import (
	"reflect"
	"testing"
)

func TestParseRedisCommand(t *testing.T) {
	t.Run("parses quoted arguments", func(t *testing.T) {
		cmd, args, err := ParseRedisCommand(`GET "user profile"`)
		if err != nil {
			t.Fatalf("ParseRedisCommand() error = %v", err)
		}
		if cmd != "GET" {
			t.Fatalf("cmd = %q, want GET", cmd)
		}
		if !reflect.DeepEqual(args, []string{"user profile"}) {
			t.Fatalf("args = %#v", args)
		}
	})

	t.Run("parses escaped whitespace", func(t *testing.T) {
		cmd, args, err := ParseRedisCommand(`GET user\ profile`)
		if err != nil {
			t.Fatalf("ParseRedisCommand() error = %v", err)
		}
		if cmd != "GET" {
			t.Fatalf("cmd = %q, want GET", cmd)
		}
		if !reflect.DeepEqual(args, []string{"user profile"}) {
			t.Fatalf("args = %#v", args)
		}
	})

	t.Run("rejects unterminated quote", func(t *testing.T) {
		_, _, err := ParseRedisCommand(`GET "user profile`)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestCheckRedisReadOnly(t *testing.T) {
	t.Run("allows read command", func(t *testing.T) {
		if err := CheckRedisReadOnly(`GET "user profile"`); err != nil {
			t.Fatalf("CheckRedisReadOnly() error = %v", err)
		}
	})

	t.Run("blocks write command", func(t *testing.T) {
		err := CheckRedisReadOnly("SET user:1 alice")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("blocks scripting command", func(t *testing.T) {
		err := CheckRedisReadOnly("EVAL 'return 1' 0")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("blocks unknown command", func(t *testing.T) {
		err := CheckRedisReadOnly("FOO bar")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
