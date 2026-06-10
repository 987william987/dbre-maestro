package sqlreview

import (
	"fmt"
	"strings"
)

// redisReadOnlyCommands is the whitelist of allowed Redis commands in the SQL Editor.
var redisReadOnlyCommands = map[string]bool{
	"GET": true, "MGET": true, "GETRANGE": true, "STRLEN": true,
	"HGET": true, "HMGET": true, "HGETALL": true, "HKEYS": true, "HVALS": true, "HLEN": true, "HEXISTS": true,
	"LRANGE": true, "LLEN": true, "LINDEX": true,
	"SMEMBERS": true, "SCARD": true, "SISMEMBER": true, "SMISMEMBER": true, "SRANDMEMBER": true,
	"ZRANGE": true, "ZRANGEBYSCORE": true, "ZRANGEBYLEX": true, "ZREVRANGE": true,
	"ZREVRANGEBYSCORE": true, "ZCARD": true, "ZSCORE": true, "ZMSCORE": true, "ZRANK": true, "ZCOUNT": true,
	"KEYS": true, "SCAN": true, "HSCAN": true, "SSCAN": true, "ZSCAN": true,
	"TYPE": true, "TTL": true, "PTTL": true, "EXISTS": true, "OBJECT": true,
	"INFO": true, "DBSIZE": true, "PING": true, "TIME": true, "MEMORY": true,
}

// CheckRedisReadOnly returns an error if the command is not in the read-only whitelist.
func CheckRedisReadOnly(cmdLine string) error {
	parts := strings.Fields(strings.TrimSpace(cmdLine))
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}
	cmd := strings.ToUpper(parts[0])
	if !redisReadOnlyCommands[cmd] {
		return fmt.Errorf("command %q is not allowed; only read-only commands are permitted", cmd)
	}
	return nil
}

// ParseRedisCommand splits a command line into command + args.
func ParseRedisCommand(cmdLine string) (cmd string, args []string) {
	parts := strings.Fields(strings.TrimSpace(cmdLine))
	if len(parts) == 0 {
		return "", nil
	}
	return strings.ToUpper(parts[0]), parts[1:]
}
