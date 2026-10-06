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

const defaultRedisQueryLimit = 200

var redisCommandCategories = map[string]redisCommandCategory{
	"GET":              redisCategoryRead,
	"MGET":             redisCategoryRead,
	"GETRANGE":         redisCategoryRead,
	"STRLEN":           redisCategoryRead,
	"HGET":             redisCategoryRead,
	"HMGET":            redisCategoryRead,
	"HLEN":             redisCategoryRead,
	"HEXISTS":          redisCategoryRead,
	"LLEN":             redisCategoryRead,
	"LINDEX":           redisCategoryRead,
	"SCARD":            redisCategoryRead,
	"SISMEMBER":        redisCategoryRead,
	"SMISMEMBER":       redisCategoryRead,
	"SRANDMEMBER":      redisCategoryRead,
	"ZCARD":            redisCategoryRead,
	"ZSCORE":           redisCategoryRead,
	"ZMSCORE":          redisCategoryRead,
	"ZRANK":            redisCategoryRead,
	"ZCOUNT":           redisCategoryRead,
	"SCAN":             redisCategoryRead,
	"HSCAN":            redisCategoryRead,
	"SSCAN":            redisCategoryRead,
	"ZSCAN":            redisCategoryRead,
	"TYPE":             redisCategoryRead,
	"TTL":              redisCategoryRead,
	"PTTL":             redisCategoryRead,
	"EXISTS":           redisCategoryRead,
	"PING":             redisCategoryRead,
	"GETBIT":           redisCategoryRead,
	"BITCOUNT":         redisCategoryRead,
	"BITPOS":           redisCategoryRead,
	"HSTRLEN":          redisCategoryRead,
	"HRANDFIELD":       redisCategoryRead,
	"LRANGE":           redisCategoryRead,
	"LPOS":             redisCategoryRead,
	"ZRANGE":           redisCategoryRead,
	"ZREVRANGE":        redisCategoryRead,
	"ZRANGEBYSCORE":    redisCategoryRead,
	"ZREVRANGEBYSCORE": redisCategoryRead,
	"ZRANGEBYLEX":      redisCategoryRead,
	"ZREVRANGEBYLEX":   redisCategoryRead,
	"ZREVRANK":         redisCategoryRead,
	"ZLEXCOUNT":        redisCategoryRead,
	"ZRANDMEMBER":      redisCategoryRead,
	"XLEN":             redisCategoryRead,
	"XRANGE":           redisCategoryRead,
	"XREVRANGE":        redisCategoryRead,
	"GEOHASH":          redisCategoryRead,
	"GEOPOS":           redisCategoryRead,
	"GEODIST":          redisCategoryRead,
	"GEOSEARCH":        redisCategoryRead,
	"OBJECT":           redisCategoryRead,

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
	"KEYS":        redisCategoryDangerous,
	"HGETALL":     redisCategoryDangerous,
	"HKEYS":       redisCategoryDangerous,
	"HVALS":       redisCategoryDangerous,
	"SMEMBERS":    redisCategoryDangerous,
	"INFO":        redisCategoryDangerous,
	"DBSIZE":      redisCategoryDangerous,
	"TIME":        redisCategoryDangerous,
	"MEMORY":      redisCategoryDangerous,
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
	_, _, err := PrepareRedisReadOnly(cmdLine, defaultRedisQueryLimit)
	return err
}

func PrepareRedisReadOnly(cmdLine string, limit int) (string, []string, error) {
	cmd, args, err := ParseRedisCommand(cmdLine)
	if err != nil {
		return "", nil, err
	}
	category := categorizeRedisCommand(cmd)
	switch category {
	case redisCategoryRead:
		bounded, err := normalizeRedisReadOnlyArgs(cmd, args, limit)
		if err != nil {
			return "", nil, err
		}
		return cmd, bounded, nil
	case redisCategoryWrite, redisCategoryDangerous, redisCategoryTransaction, redisCategoryScripting, redisCategoryAdmin:
		return "", nil, fmt.Errorf("command %q is not allowed in SQL Editor (category: %s)", cmd, category)
	default:
		return "", nil, fmt.Errorf("command %q is not recognized or not allowed", cmd)
	}
}

func CheckRedisOfficialReadOnly(cmdLine string, officialReadOnly bool, limit int) error {
	if _, _, err := PrepareRedisReadOnly(cmdLine, limit); err != nil {
		return err
	}
	if !officialReadOnly {
		cmd, _, _ := ParseRedisCommand(cmdLine)
		return fmt.Errorf("command %q is not marked readonly by the target Redis server", cmd)
	}
	return nil
}

func CheckRedisFallbackReadOnly(cmdLine string, limit int) error {
	cmd, _, err := ParseRedisCommand(cmdLine)
	if err != nil {
		return err
	}
	switch cmd {
	case "GET", "MGET", "GETRANGE", "STRLEN", "HGET", "HMGET", "HLEN", "HEXISTS", "LLEN", "LINDEX", "SCARD", "SISMEMBER", "SMISMEMBER", "SRANDMEMBER", "ZCARD", "ZSCORE", "ZMSCORE", "ZRANK", "ZCOUNT", "SCAN", "HSCAN", "SSCAN", "ZSCAN", "TYPE", "TTL", "PTTL", "EXISTS", "PING":
		_, _, err := PrepareRedisReadOnly(cmdLine, limit)
		return err
	default:
		return fmt.Errorf("command %q requires Redis readonly metadata", cmd)
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

// CheckRedisSensitiveKeyPrefixes rejects Redis read commands that return values or
// collection content for keys under sensitive prefixes. Key discovery/metadata
// commands such as SCAN, TYPE, TTL, PTTL, EXISTS, and length/count commands are
// intentionally allowed.
func CheckRedisSensitiveKeyPrefixes(cmd string, args []string, prefixes []string) error {
	if len(prefixes) == 0 {
		return nil
	}
	for _, key := range redisValueContentKeys(cmd, args) {
		if redisKeyHasSensitivePrefix(key, prefixes) {
			return fmt.Errorf("redis key value is blocked by sensitive key policy")
		}
	}
	return nil
}

func redisValueContentKeys(cmd string, args []string) []string {
	if len(args) == 0 {
		return nil
	}
	switch strings.ToUpper(strings.TrimSpace(cmd)) {
	case "GET", "GETRANGE", "GETBIT", "BITCOUNT", "BITPOS", "HGET", "HMGET", "HSCAN", "HSTRLEN", "HRANDFIELD", "LINDEX", "LRANGE", "LPOS", "SISMEMBER", "SMISMEMBER", "SSCAN", "SRANDMEMBER", "ZSCORE", "ZMSCORE", "ZRANK", "ZREVRANK", "ZCOUNT", "ZLEXCOUNT", "ZRANGE", "ZREVRANGE", "ZRANGEBYSCORE", "ZREVRANGEBYSCORE", "ZRANGEBYLEX", "ZREVRANGEBYLEX", "ZRANDMEMBER", "ZSCAN", "XLEN", "XRANGE", "XREVRANGE", "GEOHASH", "GEOPOS", "GEODIST", "GEOSEARCH":
		return []string{args[0]}
	case "MGET":
		return args
	default:
		return nil
	}
}

func redisKeyHasSensitivePrefix(key string, prefixes []string) bool {
	for _, prefix := range prefixes {
		trimmed := strings.TrimSpace(prefix)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(key, trimmed) {
			return true
		}
	}
	return false
}

func normalizeRedisReadOnlyArgs(cmd string, args []string, limit int) ([]string, error) {
	if limit < 1 {
		limit = defaultRedisQueryLimit
	}
	args = append([]string(nil), args...)
	switch cmd {
	case "SCAN", "HSCAN", "SSCAN", "ZSCAN":
		count, ok, err := redisScanCount(args)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%s requires COUNT between 1 and %d", cmd, limit)
		}
		if count < 1 || count > limit {
			return nil, fmt.Errorf("%s COUNT must be between 1 and %d", cmd, limit)
		}
	case "LRANGE", "ZRANGE", "ZREVRANGE":
		if len(args) < 3 {
			return nil, fmt.Errorf("%s requires key, start, and stop", cmd)
		}
		if cmd == "ZRANGE" && (containsRedisArg(args[3:], "BYSCORE") || containsRedisArg(args[3:], "BYLEX")) {
			return normalizeRedisLimitOption(cmd, args, limit)
		}
		start, err1 := strconv.Atoi(args[1])
		stop, err2 := strconv.Atoi(args[2])
		if err1 != nil || err2 != nil || ((start < 0) != (stop < 0) && stop != -1) {
			return nil, fmt.Errorf("%s range must use comparable integer indexes", cmd)
		}
		if stop == -1 || stop-start+1 > limit {
			args[2] = strconv.Itoa(start + limit - 1)
		}
	case "ZRANGEBYSCORE", "ZREVRANGEBYSCORE", "ZRANGEBYLEX", "ZREVRANGEBYLEX":
		return normalizeRedisLimitOption(cmd, args, limit)
	case "XRANGE", "XREVRANGE", "GEOSEARCH":
		return normalizeRedisCountOption(cmd, args, limit)
	case "HRANDFIELD", "ZRANDMEMBER":
		if len(args) >= 2 {
			count, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, fmt.Errorf("%s count must be numeric", cmd)
			}
			if count > limit {
				args[1] = strconv.Itoa(limit)
			}
			if count < -limit {
				args[1] = strconv.Itoa(-limit)
			}
		}
	case "LPOS":
		var err error
		args, err = capRedisNamedCount(args, "COUNT", limit)
		if err != nil {
			return nil, fmt.Errorf("%s COUNT must be positive and numeric", cmd)
		}
		if !containsRedisArg(args, "MAXLEN") {
			args = append(args, "MAXLEN", strconv.Itoa(limit))
		}
	case "OBJECT":
		if len(args) != 2 || !containsRedisArg([]string{"ENCODING", "FREQ", "IDLETIME", "REFCOUNT"}, args[0]) {
			return nil, fmt.Errorf("OBJECT only allows ENCODING, FREQ, IDLETIME, or REFCOUNT")
		}
	case "MGET":
		if len(args) > limit {
			args = args[:limit]
		}
	case "HMGET", "SMISMEMBER", "ZMSCORE", "GEOHASH", "GEOPOS":
		if len(args) > limit+1 {
			args = args[:limit+1]
		}
	}
	return args, nil
}

func containsRedisArg(args []string, name string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, name) {
			return true
		}
	}
	return false
}

func capRedisNamedCount(args []string, name string, limit int) ([]string, error) {
	for i := 0; i+1 < len(args); i++ {
		if strings.EqualFold(args[i], name) {
			value, err := strconv.Atoi(args[i+1])
			if err != nil || value < 1 {
				return nil, fmt.Errorf("invalid count")
			}
			if value > limit {
				args[i+1] = strconv.Itoa(limit)
			}
			return args, nil
		}
	}
	return args, nil
}

func normalizeRedisCountOption(cmd string, args []string, limit int) ([]string, error) {
	if containsRedisArg(args, "COUNT") {
		return capRedisNamedCount(args, "COUNT", limit)
	}
	return append(args, "COUNT", strconv.Itoa(limit)), nil
}

func normalizeRedisLimitOption(cmd string, args []string, limit int) ([]string, error) {
	for i := 0; i < len(args); i++ {
		if !strings.EqualFold(args[i], "LIMIT") {
			continue
		}
		if i+2 >= len(args) {
			return nil, fmt.Errorf("%s LIMIT requires offset and count", cmd)
		}
		count, err := strconv.Atoi(args[i+2])
		if err != nil || count < 1 {
			return nil, fmt.Errorf("%s LIMIT count must be positive", cmd)
		}
		if count > limit {
			args[i+2] = strconv.Itoa(limit)
		}
		return args, nil
	}
	for i, arg := range args {
		if strings.EqualFold(arg, "WITHSCORES") {
			bounded := append([]string{}, args[:i]...)
			bounded = append(bounded, "LIMIT", "0", strconv.Itoa(limit))
			return append(bounded, args[i:]...), nil
		}
	}
	return append(args, "LIMIT", "0", strconv.Itoa(limit)), nil
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
