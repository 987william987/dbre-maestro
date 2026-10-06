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

	t.Run("allows scan commands with count at or below limit", func(t *testing.T) {
		for _, cmdLine := range []string{
			"SCAN 0 COUNT 200",
			"SCAN 0 MATCH user:* COUNT 50",
			"HSCAN profile:1 0 COUNT 200",
			"SSCAN online-users 0 count 100",
			"ZSCAN leaderboard 0 MATCH user:* COUNT 1",
		} {
			if err := CheckRedisReadOnly(cmdLine); err != nil {
				t.Fatalf("CheckRedisReadOnly(%q) error = %v", cmdLine, err)
			}
		}
	})

	t.Run("blocks scan commands without bounded count", func(t *testing.T) {
		for _, cmdLine := range []string{
			"SCAN 0",
			"SCAN 0 COUNT 201",
			"SCAN 0 COUNT 0",
			"SCAN 0 COUNT many",
			"SCAN 0 COUNT",
			"HSCAN profile:1 0",
			"SSCAN online-users 0 COUNT 1000",
			"ZSCAN leaderboard 0 COUNT -1",
		} {
			if err := CheckRedisReadOnly(cmdLine); err == nil {
				t.Fatalf("CheckRedisReadOnly(%q) expected error, got nil", cmdLine)
			}
		}
	})

	t.Run("blocks write command", func(t *testing.T) {
		err := CheckRedisReadOnly("SET user:1 alice")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("blocks keys command", func(t *testing.T) {
		err := CheckRedisReadOnly("KEYS *")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("blocks unbounded collection dump commands", func(t *testing.T) {
		for _, cmdLine := range []string{
			"HGETALL profile:1",
			"HKEYS profile:1",
			"HVALS profile:1",
			"SMEMBERS online-users",
		} {
			if err := CheckRedisReadOnly(cmdLine); err == nil {
				t.Fatalf("CheckRedisReadOnly(%q) expected error, got nil", cmdLine)
			}
		}
	})

	t.Run("blocks redis introspection commands", func(t *testing.T) {
		for _, cmdLine := range []string{
			"INFO",
			"DBSIZE",
			"MEMORY USAGE user:1",
			"TIME",
		} {
			if err := CheckRedisReadOnly(cmdLine); err == nil {
				t.Fatalf("CheckRedisReadOnly(%q) expected error, got nil", cmdLine)
			}
		}
	})

	t.Run("allows only safe object metadata subcommands", func(t *testing.T) {
		for _, cmdLine := range []string{"OBJECT ENCODING user:1", "OBJECT FREQ user:1", "OBJECT IDLETIME user:1", "OBJECT REFCOUNT user:1"} {
			if err := CheckRedisReadOnly(cmdLine); err != nil {
				t.Fatalf("CheckRedisReadOnly(%q) error = %v", cmdLine, err)
			}
		}
		if err := CheckRedisReadOnly("OBJECT HELP"); err == nil {
			t.Fatal("unsafe OBJECT subcommand unexpectedly allowed")
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

func TestPrepareRedisReadOnlyBoundsCollectionCommands(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"LRANGE queue 0 -1", []string{"0", "199"}},
		{"ZRANGE leaderboard 0 -1 WITHSCORES", []string{"0", "199", "WITHSCORES"}},
		{"ZRANGE leaderboard -inf +inf BYSCORE WITHSCORES", []string{"-inf", "+inf", "BYSCORE", "LIMIT", "0", "200", "WITHSCORES"}},
		{"ZRANGEBYSCORE leaderboard -inf +inf", []string{"-inf", "+inf", "LIMIT", "0", "200"}},
		{"XRANGE events - +", []string{"-", "+", "COUNT", "200"}},
		{"GEOSEARCH places FROMLONLAT 0 0 BYRADIUS 10 km", []string{"FROMLONLAT", "0", "0", "BYRADIUS", "10", "km", "COUNT", "200"}},
	}
	for _, tt := range tests {
		_, args, err := PrepareRedisReadOnly(tt.input, 200)
		if err != nil {
			t.Fatalf("PrepareRedisReadOnly(%q): %v", tt.input, err)
		}
		if !reflect.DeepEqual(args[1:], tt.want) {
			t.Fatalf("PrepareRedisReadOnly(%q) args=%#v want tail=%#v", tt.input, args, tt.want)
		}
	}
}

func TestPrepareRedisReadOnlyBoundsMultiValueArguments(t *testing.T) {
	_, args, err := PrepareRedisReadOnly("MGET a b c", 2)
	if err != nil || !reflect.DeepEqual(args, []string{"a", "b"}) {
		t.Fatalf("bounded MGET args=%#v err=%v", args, err)
	}
	_, args, err = PrepareRedisReadOnly("GEOPOS places a b c", 2)
	if err != nil || !reflect.DeepEqual(args, []string{"places", "a", "b"}) {
		t.Fatalf("bounded GEOPOS args=%#v err=%v", args, err)
	}
}

func TestCheckRedisOfficialReadOnlyRequiresBothPolicies(t *testing.T) {
	if err := CheckRedisOfficialReadOnly("GET user:1", true, 200); err != nil {
		t.Fatalf("official readonly GET rejected: %v", err)
	}
	if err := CheckRedisOfficialReadOnly("GET user:1", false, 200); err == nil {
		t.Fatal("target Redis readonly metadata must be authoritative")
	}
	if err := CheckRedisOfficialReadOnly("KEYS *", true, 200); err == nil {
		t.Fatal("official readonly flag must not bypass the local dangerous-command policy")
	}
}

func TestCheckRedisFallbackReadOnlyDoesNotEnableNewCommands(t *testing.T) {
	if err := CheckRedisFallbackReadOnly("GET user:1", 200); err != nil {
		t.Fatalf("legacy safe command rejected: %v", err)
	}
	if err := CheckRedisFallbackReadOnly("ZRANGE leaderboard 0 10", 200); err == nil {
		t.Fatal("new command must require target Redis readonly metadata")
	}
}

func TestPrepareRedisReadOnlyUsesSQLEditorLimitAboveDefault(t *testing.T) {
	_, args, err := PrepareRedisReadOnly("ZRANGE leaderboard 0 -1", 500)
	if err != nil || !reflect.DeepEqual(args, []string{"leaderboard", "0", "499"}) {
		t.Fatalf("args=%#v err=%v", args, err)
	}
	_, args, err = PrepareRedisReadOnly("XRANGE events - +", 1000)
	if err != nil || !reflect.DeepEqual(args, []string{"events", "-", "+", "COUNT", "1000"}) {
		t.Fatalf("args=%#v err=%v", args, err)
	}
	if _, _, err = PrepareRedisReadOnly("SCAN 0 COUNT 500", 500); err != nil {
		t.Fatalf("SCAN should use SQL Editor limit: %v", err)
	}
	if _, _, err = PrepareRedisReadOnly("SCAN 0 COUNT 500", 200); err == nil {
		t.Fatal("SCAN count above the requested SQL Editor limit must be rejected")
	}
}

func TestCheckRedisSensitiveKeyPrefixes(t *testing.T) {
	prefixes := []string{"session:", "token:"}

	t.Run("blocks commands that return value or collection content", func(t *testing.T) {
		for _, cmdLine := range []string{
			"GET session:abc",
			"MGET safe:key token:abc",
			"GETRANGE session:abc 0 10",
			"HGET session:abc email",
			"HMGET session:abc email phone",
			"HSCAN session:abc 0 COUNT 200",
			"LINDEX session:queue 0",
			"SISMEMBER session:set member",
			"SMISMEMBER session:set a b",
			"SSCAN session:set 0 COUNT 200",
			"SRANDMEMBER session:set",
			"ZSCORE token:z member",
			"ZMSCORE token:z a b",
			"ZRANK token:z member",
			"ZCOUNT token:z 0 10",
			"ZSCAN token:z 0 COUNT 200",
			"LRANGE session:queue 0 199",
			"ZRANGE token:z 0 199",
			"XRANGE session:events - + COUNT 200",
			"GEOSEARCH token:places FROMLONLAT 0 0 BYRADIUS 10 km COUNT 200",
		} {
			cmd, args, err := ParseRedisCommand(cmdLine)
			if err != nil {
				t.Fatalf("ParseRedisCommand(%q) error = %v", cmdLine, err)
			}
			if err := CheckRedisSensitiveKeyPrefixes(cmd, args, prefixes); err == nil {
				t.Fatalf("CheckRedisSensitiveKeyPrefixes(%q) expected error, got nil", cmdLine)
			}
		}
	})

	t.Run("allows key discovery and metadata commands", func(t *testing.T) {
		for _, cmdLine := range []string{
			"SCAN 0 MATCH session:* COUNT 200",
			"TYPE session:abc",
			"TTL session:abc",
			"PTTL session:abc",
			"EXISTS session:abc",
			"STRLEN session:abc",
			"HLEN session:abc",
			"HEXISTS session:abc email",
			"LLEN session:list",
			"SCARD session:set",
			"ZCARD token:z",
		} {
			cmd, args, err := ParseRedisCommand(cmdLine)
			if err != nil {
				t.Fatalf("ParseRedisCommand(%q) error = %v", cmdLine, err)
			}
			if err := CheckRedisSensitiveKeyPrefixes(cmd, args, prefixes); err != nil {
				t.Fatalf("CheckRedisSensitiveKeyPrefixes(%q) error = %v", cmdLine, err)
			}
		}
	})

	t.Run("allows non-sensitive keys for content commands", func(t *testing.T) {
		cmd, args, err := ParseRedisCommand("GET public:abc")
		if err != nil {
			t.Fatalf("ParseRedisCommand() error = %v", err)
		}
		if err := CheckRedisSensitiveKeyPrefixes(cmd, args, prefixes); err != nil {
			t.Fatalf("CheckRedisSensitiveKeyPrefixes() error = %v", err)
		}
	})
}
