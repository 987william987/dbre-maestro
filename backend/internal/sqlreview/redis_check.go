package sqlreview

import (
	"fmt"
	"strings"
)

type redisCommandCategory string

const (
	redisCategoryRead        redisCommandCategory = "read"
	redisCategoryWrite       redisCommandCategory = "write"
	redisCategoryDangerous   redisCommandCategory = "dangerous"
	redisCategoryTransaction redisCommandCategory = "transaction"
	redisCategoryScripting   redisCommandCategory = "scripting"
	redisCategoryAdmin       redisCommandCategory = "admin"
	redisCategoryUnknown     redisCommandCategory = "unknown"
)

var redisCommandCategories = map[string]redisCommandCategory{
	"GET":              redisCategoryRead,
	"MGET":             redisCategoryRead,
	"GETRANGE":         redisCategoryRead,
	"STRLEN":           redisCategoryRead,
	"HGET":             redisCategoryRead,
	"HMGET":            redisCategoryRead,
	"HGETALL":          redisCategoryRead,
	"HKEYS":            redisCategoryRead,
	"HVALS":            redisCategoryRead,
	"HLEN":             redisCategoryRead,
	"HEXISTS":          redisCategoryRead,
	"LRANGE":           redisCategoryRead,
	"LLEN":             redisCategoryRead,
	"LINDEX":           redisCategoryRead,
	"SMEMBERS":         redisCategoryRead,
	"SCARD":            redisCategoryRead,
	"SISMEMBER":        redisCategoryRead,
	"SMISMEMBER":       redisCategoryRead,
	"SRANDMEMBER":      redisCategoryRead,
	"ZRANGE":           redisCategoryRead,
	"ZRANGEBYSCORE":    redisCategoryRead,
	"ZRANGEBYLEX":      redisCategoryRead,
	"ZREVRANGE":        redisCategoryRead,
	"ZREVRANGEBYSCORE": redisCategoryRead,
	"ZCARD":            redisCategoryRead,
	"ZSCORE":           redisCategoryRead,
	"ZMSCORE":          redisCategoryRead,
	"ZRANK":            redisCategoryRead,
	"ZCOUNT":           redisCategoryRead,
	"KEYS":             redisCategoryRead,
	"SCAN":             redisCategoryRead,
	"HSCAN":            redisCategoryRead,
	"SSCAN":            redisCategoryRead,
	"ZSCAN":            redisCategoryRead,
	"TYPE":             redisCategoryRead,
	"TTL":              redisCategoryRead,
	"PTTL":             redisCategoryRead,
	"EXISTS":           redisCategoryRead,
	"OBJECT":           redisCategoryRead,
	"INFO":             redisCategoryRead,
	"DBSIZE":           redisCategoryRead,
	"PING":             redisCategoryRead,
	"TIME":             redisCategoryRead,
	"MEMORY":           redisCategoryRead,

	"SET":         redisCategoryWrite,
	"MSET":        redisCategoryWrite,
	"DEL":         redisCategoryWrite,
	"INCR":        redisCategoryWrite,
	"DECR":        redisCategoryWrite,
	"EXPIRE":      redisCategoryWrite,
	"HSET":        redisCategoryWrite,
	"HMSET":       redisCategoryWrite,
	"LPUSH":       redisCategoryWrite,
	"RPUSH":       redisCategoryWrite,
	"SADD":        redisCategoryWrite,
	"ZADD":        redisCategoryWrite,
	"FLUSHDB":     redisCategoryDangerous,
	"FLUSHALL":    redisCategoryDangerous,
	"SHUTDOWN":    redisCategoryDangerous,
	"CONFIG":      redisCategoryDangerous,
	"DEBUG":       redisCategoryDangerous,
	"MULTI":       redisCategoryTransaction,
	"EXEC":        redisCategoryTransaction,
	"DISCARD":     redisCategoryTransaction,
	"WATCH":       redisCategoryTransaction,
	"UNWATCH":     redisCategoryTransaction,
	"EVAL":        redisCategoryScripting,
	"EVALSHA":     redisCategoryScripting,
	"SCRIPT":      redisCategoryScripting,
	"FUNCTION":    redisCategoryScripting,
	"ACL":         redisCategoryAdmin,
	"CLIENT":      redisCategoryAdmin,
	"COMMAND":     redisCategoryAdmin,
	"LATENCY":     redisCategoryAdmin,
	"MODULE":      redisCategoryAdmin,
	"MONITOR":     redisCategoryAdmin,
	"PSUBSCRIBE":  redisCategoryAdmin,
	"PUBLISH":     redisCategoryAdmin,
	"PUBSUB":      redisCategoryAdmin,
	"SUBSCRIBE":   redisCategoryAdmin,
	"UNSUBSCRIBE": redisCategoryAdmin,
}

// CheckRedisReadOnly returns an error if the command is not in the read-only whitelist.
func CheckRedisReadOnly(cmdLine string) error {
	cmd, _, err := ParseRedisCommand(cmdLine)
	if err != nil {
		return err
	}
	category := categorizeRedisCommand(cmd)
	switch category {
	case redisCategoryRead:
		return nil
	case redisCategoryWrite, redisCategoryDangerous, redisCategoryTransaction, redisCategoryScripting, redisCategoryAdmin:
		return fmt.Errorf("command %q is not allowed in SQL Editor (category: %s)", cmd, category)
	default:
		return fmt.Errorf("command %q is not recognized or not allowed", cmd)
	}
}

// ParseRedisCommand splits a command line into command + args.
func ParseRedisCommand(cmdLine string) (cmd string, args []string, err error) {
	parts, err := tokenizeRedisCommand(cmdLine)
	if err != nil {
		return "", nil, err
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}
	return strings.ToUpper(parts[0]), parts[1:], nil
}

func categorizeRedisCommand(cmd string) redisCommandCategory {
	category, ok := redisCommandCategories[strings.ToUpper(strings.TrimSpace(cmd))]
	if !ok {
		return redisCategoryUnknown
	}
	return category
}

func tokenizeRedisCommand(cmdLine string) ([]string, error) {
	input := strings.TrimSpace(cmdLine)
	if input == "" {
		return nil, nil
	}

	var parts []string
	var current strings.Builder
	quote := rune(0)
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		parts = append(parts, current.String())
		current.Reset()
	}

	for _, ch := range input {
		switch {
		case escaped:
			current.WriteRune(ch)
			escaped = false
		case ch == '\\':
			escaped = true
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				current.WriteRune(ch)
			}
		case ch == '\'' || ch == '"':
			quote = ch
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			flush()
		default:
			current.WriteRune(ch)
		}
	}

	if escaped {
		return nil, fmt.Errorf("unterminated escape sequence")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	flush()
	return parts, nil
}
