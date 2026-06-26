package sqlreview

import (
	"fmt"
	"strconv"
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

const maxRedisScanCount = 200

var redisCommandCategories = map[string]redisCommandCategory{
	"GET":         redisCategoryRead,
	"MGET":        redisCategoryRead,
	"GETRANGE":    redisCategoryRead,
	"STRLEN":      redisCategoryRead,
	"HGET":        redisCategoryRead,
	"HMGET":       redisCategoryRead,
	"HLEN":        redisCategoryRead,
	"HEXISTS":     redisCategoryRead,
	"LLEN":        redisCategoryRead,
	"LINDEX":      redisCategoryRead,
	"SCARD":       redisCategoryRead,
	"SISMEMBER":   redisCategoryRead,
	"SMISMEMBER":  redisCategoryRead,
	"SRANDMEMBER": redisCategoryRead,
	"ZCARD":       redisCategoryRead,
	"ZSCORE":      redisCategoryRead,
	"ZMSCORE":     redisCategoryRead,
	"ZRANK":       redisCategoryRead,
	"ZCOUNT":      redisCategoryRead,
	"SCAN":        redisCategoryRead,
	"HSCAN":       redisCategoryRead,
	"SSCAN":       redisCategoryRead,
	"ZSCAN":       redisCategoryRead,
	"TYPE":        redisCategoryRead,
	"TTL":         redisCategoryRead,
	"PTTL":        redisCategoryRead,
	"EXISTS":      redisCategoryRead,
	"PING":        redisCategoryRead,

	"SET":              redisCategoryWrite,
	"MSET":             redisCategoryWrite,
	"DEL":              redisCategoryWrite,
	"INCR":             redisCategoryWrite,
	"DECR":             redisCategoryWrite,
	"EXPIRE":           redisCategoryWrite,
	"HSET":             redisCategoryWrite,
	"HMSET":            redisCategoryWrite,
	"LPUSH":            redisCategoryWrite,
	"RPUSH":            redisCategoryWrite,
	"SADD":             redisCategoryWrite,
	"ZADD":             redisCategoryWrite,
	"KEYS":             redisCategoryDangerous,
	"HGETALL":          redisCategoryDangerous,
	"HKEYS":            redisCategoryDangerous,
	"HVALS":            redisCategoryDangerous,
	"LRANGE":           redisCategoryDangerous,
	"SMEMBERS":         redisCategoryDangerous,
	"ZRANGE":           redisCategoryDangerous,
	"ZRANGEBYSCORE":    redisCategoryDangerous,
	"ZRANGEBYLEX":      redisCategoryDangerous,
	"ZREVRANGE":        redisCategoryDangerous,
	"ZREVRANGEBYSCORE": redisCategoryDangerous,
	"OBJECT":           redisCategoryDangerous,
	"INFO":             redisCategoryDangerous,
	"DBSIZE":           redisCategoryDangerous,
	"TIME":             redisCategoryDangerous,
	"MEMORY":           redisCategoryDangerous,
	"FLUSHDB":          redisCategoryDangerous,
	"FLUSHALL":         redisCategoryDangerous,
	"SHUTDOWN":         redisCategoryDangerous,
	"CONFIG":           redisCategoryDangerous,
	"DEBUG":            redisCategoryDangerous,
	"MULTI":            redisCategoryTransaction,
	"EXEC":             redisCategoryTransaction,
	"DISCARD":          redisCategoryTransaction,
	"WATCH":            redisCategoryTransaction,
	"UNWATCH":          redisCategoryTransaction,
	"EVAL":             redisCategoryScripting,
	"EVALSHA":          redisCategoryScripting,
	"SCRIPT":           redisCategoryScripting,
	"FUNCTION":         redisCategoryScripting,
	"ACL":              redisCategoryAdmin,
	"CLIENT":           redisCategoryAdmin,
	"COMMAND":          redisCategoryAdmin,
	"LATENCY":          redisCategoryAdmin,
	"MODULE":           redisCategoryAdmin,
	"MONITOR":          redisCategoryAdmin,
	"PSUBSCRIBE":       redisCategoryAdmin,
	"PUBLISH":          redisCategoryAdmin,
	"PUBSUB":           redisCategoryAdmin,
	"SUBSCRIBE":        redisCategoryAdmin,
	"UNSUBSCRIBE":      redisCategoryAdmin,
}

var redisTicketAllowedCommands = map[string]struct{}{
	"SET":    {},
	"DEL":    {},
	"HSET":   {},
	"LPUSH":  {},
	"SADD":   {},
	"ZADD":   {},
	"EXPIRE": {},
}

// CheckRedisReadOnly returns an error if the command is not in the read-only whitelist.
func CheckRedisReadOnly(cmdLine string) error {
	cmd, args, err := ParseRedisCommand(cmdLine)
	if err != nil {
		return err
	}
	category := categorizeRedisCommand(cmd)
	switch category {
	case redisCategoryRead:
		if err := validateRedisReadOnlyArgs(cmd, args); err != nil {
			return err
		}
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

func CheckRedisTicketCommand(cmdLine string) error {
	cmd, args, err := ParseRedisCommand(cmdLine)
	if err != nil {
		return err
	}
	if _, ok := redisTicketAllowedCommands[cmd]; !ok {
		return fmt.Errorf("command %q is not allowed in redis tickets", cmd)
	}
	if err := validateRedisTicketArity(cmd, len(args)); err != nil {
		return err
	}
	return nil
}

func validateRedisReadOnlyArgs(cmd string, args []string) error {
	switch cmd {
	case "SCAN", "HSCAN", "SSCAN", "ZSCAN":
		count, ok, err := redisScanCount(args)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s requires COUNT between 1 and %d", cmd, maxRedisScanCount)
		}
		if count < 1 || count > maxRedisScanCount {
			return fmt.Errorf("%s COUNT must be between 1 and %d", cmd, maxRedisScanCount)
		}
	}
	return nil
}

func redisScanCount(args []string) (int, bool, error) {
	for i := 0; i < len(args); i++ {
		if !strings.EqualFold(args[i], "COUNT") {
			continue
		}
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("COUNT requires a numeric value")
		}
		count, err := strconv.Atoi(args[i+1])
		if err != nil {
			return 0, true, fmt.Errorf("COUNT requires a numeric value")
		}
		return count, true, nil
	}
	return 0, false, nil
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

func validateRedisTicketArity(cmd string, argCount int) error {
	switch cmd {
	case "SET":
		if argCount < 2 {
			return fmt.Errorf("SET requires at least key and value")
		}
	case "DEL":
		if argCount < 1 {
			return fmt.Errorf("DEL requires at least one key")
		}
	case "HSET":
		if argCount < 3 || argCount%2 == 0 {
			return fmt.Errorf("HSET requires key plus one or more field/value pairs")
		}
	case "LPUSH":
		if argCount < 2 {
			return fmt.Errorf("LPUSH requires key plus one or more values")
		}
	case "SADD":
		if argCount < 2 {
			return fmt.Errorf("SADD requires key plus one or more members")
		}
	case "ZADD":
		if argCount < 3 || argCount%2 == 0 {
			return fmt.Errorf("ZADD requires key plus one or more score/member pairs")
		}
	case "EXPIRE":
		if argCount != 2 {
			return fmt.Errorf("EXPIRE requires key and seconds")
		}
	}
	return nil
}
