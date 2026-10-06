package onlineddl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func toolRequest(mode string) ToolRequest {
	return ToolRequest{Statement: Statement{Database: "billing", Table: "orders", AlterClause: "ADD COLUMN note text"}, Parameters: DefaultParameters(mode), Host: "mysql.internal", Port: 3306, Username: "dba", Password: "super-secret-value", Timeout: 3 * time.Second}
}

func writeFakeTool(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestAdapterBuildArgsAreTypedSingleArgumentsWithoutPassword(t *testing.T) {
	req := toolRequest(ModeGhost)
	req.Statement.AlterClause = "ADD COLUMN note text; touch /tmp/never"
	args := NewGhostAdapter("gh-ost").buildArgs(req, "/tmp/work", "/tmp/client.cnf")
	if strings.Contains(strings.Join(args, " "), req.Password) {
		t.Fatal("password leaked into args")
	}
	want := "--alter=" + req.Statement.AlterClause
	found := false
	for _, arg := range args {
		if arg == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("alter clause was not preserved as one argv item: %#v", args)
	}
	if !containsArgument(args, "--throttle-additional-flag-file=/tmp/work/gh-ost.throttle") {
		t.Fatalf("gh-ost must use a process-scoped throttle flag: %#v", args)
	}
	if !containsArgument(args, "--ok-to-drop-table") {
		t.Fatalf("gh-ost must remove its old table after successful cut-over: %#v", args)
	}
	ptArgs := NewPTOSCAdapter("pt-online-schema-change").buildArgs(toolRequest(ModePTOSC), "/tmp/work", "/tmp/client.cnf")
	if len(ptArgs) < 2 || ptArgs[0] != "--defaults-file" || ptArgs[1] != "/tmp/client.cnf" {
		t.Fatalf("pt-osc defaults file missing: %#v", ptArgs)
	}
	if !containsArgument(ptArgs, "--force") {
		t.Fatalf("pt-osc must be non-interactive after platform preflight: %#v", ptArgs)
	}
	if !containsArgumentPair(ptArgs, "--recursion-method", "hosts") {
		t.Fatalf("pt-osc must discover registered replicas with their reported ports: %#v", ptArgs)
	}
	if !containsArgumentPair(ptArgs, "--pause-file", "/tmp/work/pt-osc.pause") {
		t.Fatalf("pt-osc must use a process-scoped pause file: %#v", ptArgs)
	}
	for _, flag := range []string{"--drop-old-table", "--drop-triggers", "--no-drop-new-table"} {
		if !containsArgument(ptArgs, flag) {
			t.Fatalf("pt-osc cleanup policy missing %s: %#v", flag, ptArgs)
		}
	}
}

func TestAdapterDryRunNeverIncludesExecute(t *testing.T) {
	ghost := toolRequest(ModeGhost)
	ghost.DryRun = true
	ghostArgs := NewGhostAdapter("gh-ost").buildArgs(ghost, "/tmp/work", "/tmp/client.cnf")
	if containsArgument(ghostArgs, "--execute") {
		t.Fatalf("gh-ost dry run must be noop: %#v", ghostArgs)
	}
	if containsArgument(ghostArgs, "--ok-to-drop-table") {
		t.Fatalf("gh-ost dry run must not opt into table cleanup: %#v", ghostArgs)
	}
	ptosc := toolRequest(ModePTOSC)
	ptosc.DryRun = true
	ptoscArgs := NewPTOSCAdapter("pt-online-schema-change").buildArgs(ptosc, "/tmp/work", "/tmp/client.cnf")
	if !containsArgument(ptoscArgs, "--dry-run") || containsArgument(ptoscArgs, "--execute") {
		t.Fatalf("pt-osc dry run flags are unsafe: %#v", ptoscArgs)
	}
	for _, flag := range []string{"--drop-old-table", "--drop-triggers", "--no-drop-new-table"} {
		if containsArgument(ptoscArgs, flag) {
			t.Fatalf("pt-osc dry run must not include execute cleanup flag %s: %#v", flag, ptoscArgs)
		}
	}
}

func containsArgument(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsArgumentPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestAdapterUses0600ConfigRedactsOutputAndCleansWorkdir(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	envFile := filepath.Join(dir, "env")
	release := filepath.Join(dir, "release")
	script := writeFakeTool(t, `if [ "$1" = "--version" ]; then echo "gh-ost 1.1.6"; exit 0; fi
printf '%s\n' "$@" > "$ONLINE_DDL_ARGS_FILE"
env > "$ONLINE_DDL_ENV_FILE"
printf 'stdout super-secret-value\n'
printf 'stderr super-secret-value\n' >&2
while [ ! -f "$ONLINE_DDL_RELEASE_FILE" ]; do sleep 0.01; done
`)
	t.Setenv("ONLINE_DDL_ARGS_FILE", argsFile)
	t.Setenv("ONLINE_DDL_ENV_FILE", envFile)
	t.Setenv("ONLINE_DDL_RELEASE_FILE", release)
	adapter := NewGhostAdapter(script)
	version, err := adapter.Version(context.Background())
	if err != nil || version != "gh-ost 1.1.6" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	process, err := adapter.Start(context.Background(), toolRequest(ModeGhost))
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, argsFile)
	rawArgs, _ := os.ReadFile(argsFile)
	args := string(rawArgs)
	if strings.Contains(args, "super-secret-value") {
		t.Fatal("password leaked into args")
	}
	var configPath string
	for _, arg := range strings.Fields(args) {
		if strings.HasPrefix(arg, "--conf=") {
			configPath = strings.TrimPrefix(arg, "--conf=")
		}
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%o", info.Mode().Perm())
	}
	config, _ := os.ReadFile(configPath)
	if !strings.Contains(string(config), `password="super-secret-value"`) {
		t.Fatalf("config=%q", config)
	}
	env, _ := os.ReadFile(envFile)
	if strings.Contains(string(env), "super-secret-value") {
		t.Fatal("password leaked into environment")
	}
	if err := os.WriteFile(release, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := process.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Stdout+result.Stderr, "super-secret-value") || !strings.Contains(result.Stdout+result.Stderr, "[REDACTED]") {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(filepath.Dir(configPath)); !os.IsNotExist(err) {
		t.Fatalf("workdir not removed: %v", err)
	}
}

func TestAdapterVersionUsesSanitizedEnvironment(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "version-env")
	script := writeFakeTool(t, `env > "$ONLINE_DDL_VERSION_ENV_FILE"
echo "gh-ost 1.1.6"
`)
	t.Setenv("ONLINE_DDL_VERSION_ENV_FILE", envFile)
	t.Setenv("DATABASE_PASSWORD", "must-not-reach-tool")
	version, err := NewGhostAdapter(script).Version(context.Background())
	if err != nil || version != "gh-ost 1.1.6" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	env, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "must-not-reach-tool") {
		t.Fatal("version command inherited a secret environment variable")
	}
}

func TestAdapterBoundsOutputAndMapsFailures(t *testing.T) {
	large := writeFakeTool(t, `i=0; while [ "$i" -lt 200 ]; do printf '0123456789'; i=$((i+1)); done`)
	req := toolRequest(ModeGhost)
	req.MaxOutputBytes = 64
	p, err := NewGhostAdapter(large).Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if !result.OutputTruncated || len(result.Stdout) != 64 {
		t.Fatalf("result=%+v", result)
	}
	failing := writeFakeTool(t, `printf 'failed super-secret-value' >&2; exit 2`)
	p, err = NewGhostAdapter(failing).Start(context.Background(), toolRequest(ModeGhost))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Wait()
	if !errors.Is(err, ErrToolProcessFailed) || strings.Contains(err.Error(), "super-secret-value") {
		t.Fatalf("error=%v", err)
	}
	_, err = NewGhostAdapter(filepath.Join(t.TempDir(), "missing")).Start(context.Background(), toolRequest(ModeGhost))
	if !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("missing error=%v", err)
	}
}

func TestAdapterCapsRequestedOutputLimit(t *testing.T) {
	large := writeFakeTool(t, `head -c 1100000 /dev/zero | tr '\0' x`)
	req := toolRequest(ModeGhost)
	req.MaxOutputBytes = maxToolOutputBytes + 1024
	p, err := NewGhostAdapter(large).Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if !result.OutputTruncated || len(result.Stdout) != maxToolOutputBytes {
		t.Fatalf("stdout bytes=%d truncated=%v", len(result.Stdout), result.OutputTruncated)
	}
}

func TestAdapterTimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	childFile := filepath.Join(dir, "child")
	script := writeFakeTool(t, `sleep 30 &
echo $! > "$ONLINE_DDL_CHILD_FILE"
wait
`)
	t.Setenv("ONLINE_DDL_CHILD_FILE", childFile)
	req := toolRequest(ModeGhost)
	// Leave enough startup time under the race detector before verifying that
	// the timeout kills both the parent and its background child.
	req.Timeout = 2 * time.Second
	p, err := NewGhostAdapter(script).Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, childFile)
	_, err = p.Wait()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	raw, _ := os.ReadFile(childFile)
	pid64, _ := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 32)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(int(pid64), 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child process %d survived timeout", pid64)
}
