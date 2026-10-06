package my2sql

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExtractStatementsSkipsStatsOutput(t *testing.T) {
	raw := `
binlog              starttime            stoptime             startpos   stoppos    rows

binlog              starttime            stoptime             startpos   stoppos    inserts   updates   deletes   database   table
mysql-bin.000001    2026-08-06_08:07:08  2026-08-06_08:07:08  20558018   20558113   0         0         1         william    test_n

INSERT INTO ` + "`william`.`test_n` (`id`) VALUES (1);" + `
`

	got := extractStatements(raw, WorkTypeForward)
	want := "INSERT INTO `william`.`test_n` (`id`) VALUES (1);"
	if got != want {
		t.Fatalf("unexpected sql\nwant: %q\n got: %q", want, got)
	}
}

func TestBuildArgsOmitsPasswordAndUsesServerID(t *testing.T) {
	req := Request{
		WorkType: WorkTypeRollback,
		Host:     "mysql.internal",
		Port:     3306,
		Username: "binlog_reader",
		Password: "secret-value",
		Range: PositionRange{
			StartFile: "mysql-bin.000001",
			StartPos:  4,
			EndFile:   "mysql-bin.000002",
			EndPos:    900,
		},
		Databases: []string{"app"},
	}
	args := buildArgs(req, "/tmp/output", 12345)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, req.Password) || strings.Contains(joined, "-password") {
		t.Fatalf("password leaked into my2sql args: %q", joined)
	}
	for _, want := range []string{"-work-type rollback", "-server-id 12345", "-add-extraInfo", "-databases app", "-start-file mysql-bin.000001", "-stop-pos 900"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("my2sql args missing %q: %q", want, joined)
		}
	}
}

func TestExtractForwardStatementsPreservesEventMetadata(t *testing.T) {
	raw := "# datetime=2026-10-02_00:51:00 database=app table=items binlog=mysql-bin.000001 startpos=120 stoppos=180\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (1);\n"

	if got := extractStatements(raw, WorkTypeForward); got != strings.TrimSpace(raw) {
		t.Fatalf("event metadata must remain attached to its SQL\nwant: %q\n got: %q", strings.TrimSpace(raw), got)
	}
}

func TestExtractForwardStatementsGroupsRowsFromSameEvent(t *testing.T) {
	raw := "# datetime=2026-10-02_01:38:47 database=app table=items binlog=mysql-bin.000001 startpos=100 stoppos=180\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (1);\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (2);\n" +
		"# datetime=2026-10-02_01:39:14 database=app table=items binlog=mysql-bin.000001 startpos=180 stoppos=260\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (3);\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (4);\n"
	want := "# datetime=2026-10-02_01:38:47 database=app table=items binlog=mysql-bin.000001 startpos=100 stoppos=180\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (1);\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (2);\n\n" +
		"# datetime=2026-10-02_01:39:14 database=app table=items binlog=mysql-bin.000001 startpos=180 stoppos=260\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (3);\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (4);"

	if got := extractStatements(raw, WorkTypeForward); got != want {
		t.Fatalf("forward SQL rows from one event must stay in one visual group\nwant: %q\n got: %q", want, got)
	}
}

func TestExtractRollbackStatementsMovesSuffixMetadataBeforeEventGroup(t *testing.T) {
	raw := "INSERT INTO `app`.`items` (`id`) VALUES (4);\n" +
		"# datetime=2026-10-02_01:04:00 database=app table=items binlog=mysql-bin.000004 startpos=400 stoppos=480\n" +
		"UPDATE `app`.`items` SET `status`='old' WHERE `id`=3;\n" +
		"# datetime=2026-10-02_01:03:00 database=app table=items binlog=mysql-bin.000003 startpos=300 stoppos=380\n" +
		"DELETE FROM `app`.`items` WHERE `id`=4;\n" +
		"DELETE FROM `app`.`items` WHERE `id`=3;\n" +
		"# datetime=2026-10-02_01:02:00 database=app table=items binlog=mysql-bin.000002 startpos=200 stoppos=280\n" +
		"DELETE FROM `app`.`items` WHERE `id`=2;\n" +
		"DELETE FROM `app`.`items` WHERE `id`=1;\n" +
		"# datetime=2026-10-02_01:01:00 database=app table=items binlog=mysql-bin.000001 startpos=100 stoppos=180\n"
	want := "# datetime=2026-10-02_01:04:00 database=app table=items binlog=mysql-bin.000004 startpos=400 stoppos=480\n" +
		"INSERT INTO `app`.`items` (`id`) VALUES (4);\n\n" +
		"# datetime=2026-10-02_01:03:00 database=app table=items binlog=mysql-bin.000003 startpos=300 stoppos=380\n" +
		"UPDATE `app`.`items` SET `status`='old' WHERE `id`=3;\n\n" +
		"# datetime=2026-10-02_01:02:00 database=app table=items binlog=mysql-bin.000002 startpos=200 stoppos=280\n" +
		"DELETE FROM `app`.`items` WHERE `id`=4;\n" +
		"DELETE FROM `app`.`items` WHERE `id`=3;\n\n" +
		"# datetime=2026-10-02_01:01:00 database=app table=items binlog=mysql-bin.000001 startpos=100 stoppos=180\n" +
		"DELETE FROM `app`.`items` WHERE `id`=2;\n" +
		"DELETE FROM `app`.`items` WHERE `id`=1;"

	if got := extractStatements(raw, WorkTypeRollback); got != want {
		t.Fatalf("rollback metadata must precede the event group it describes\nwant: %q\n got: %q", want, got)
	}
}

func TestExtractRollbackStatementsPreservesSQLWithoutMetadata(t *testing.T) {
	raw := "DELETE FROM `app`.`items` WHERE `id`=2;\nDELETE FROM `app`.`items` WHERE `id`=1;\n"
	want := "DELETE FROM `app`.`items` WHERE `id`=2;\nDELETE FROM `app`.`items` WHERE `id`=1;"

	if got := extractStatements(raw, WorkTypeRollback); got != want {
		t.Fatalf("rollback SQL without metadata must remain available\nwant: %q\n got: %q", want, got)
	}
}

func TestCollectOutputOrdersForwardAscendingAndRollbackDescending(t *testing.T) {
	dir := t.TempDir()
	first := "# datetime=2026-10-02_00:51:00 database=app table=items binlog=mysql-bin.000001 startpos=120 stoppos=180\nDELETE FROM `app`.`items` WHERE `id`=1;\n"
	second := "# datetime=2026-10-02_00:52:00 database=app table=items binlog=mysql-bin.000002 startpos=4 stoppos=80\nINSERT INTO `app`.`items` (`id`) VALUES (2);\n"
	if err := os.WriteFile(filepath.Join(dir, "forward.9.sql"), []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "forward.10.sql"), []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "binlog_status.txt"), []byte("statistics should not consume artifact bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	forward, err := collectOutput(dir, "", 4096, WorkTypeForward)
	if err != nil {
		t.Fatal(err)
	}
	rollback, err := collectOutput(dir, "", 4096, WorkTypeRollback)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(forward, "00:51:00") > strings.Index(forward, "00:52:00") {
		t.Fatalf("forward events must be chronological: %q", forward)
	}
	if strings.Index(rollback, "00:52:00") > strings.Index(rollback, "00:51:00") {
		t.Fatalf("rollback events must be reverse chronological: %q", rollback)
	}
}

func TestBuildArgsUsesOneTimeSnapshotAndFilters(t *testing.T) {
	req := Request{WorkType: WorkTypeForward, Host: "mysql.internal", Port: 3306, Username: "reader",
		TimeRange: &TimeRange{Start: "2026-10-01 08:00:00", End: "2026-10-01 08:05:00"},
		Range:     PositionRange{EndFile: "mysql-bin.000003", EndPos: 4567},
		Databases: []string{"app"}, Tables: []string{"orders", "items"}, SQLTypes: []string{"insert", "update"}}
	joined := strings.Join(buildArgs(req, "/tmp/output", 12345), " ")
	for _, want := range []string{"-start-datetime 2026-10-01 08:00:00", "-stop-datetime 2026-10-01 08:05:00", "-stop-file mysql-bin.000003", "-stop-pos 4567", "-tables orders,items", "-sql insert,update"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("my2sql args missing %q: %q", want, joined)
		}
	}
	if strings.Contains(joined, "-start-file") {
		t.Fatalf("time range must not invent a start position: %q", joined)
	}
}

func TestNextServerIDIsNonZeroAndUnique(t *testing.T) {
	first := nextServerID()
	second := nextServerID()
	if first == 0 || second == 0 {
		t.Fatalf("server IDs must be non-zero: %d, %d", first, second)
	}
	if first == second {
		t.Fatalf("server IDs must be unique per invocation: %d", first)
	}
}

func TestRunProvidesPasswordOnStdinAndCleansOutputDir(t *testing.T) {
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "args")
	stdinFile := filepath.Join(tempDir, "stdin")
	script := filepath.Join(tempDir, "fake-my2sql")
	scriptBody := `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo "my2sql-test-version"
  exit 0
fi
printf '%s\n' "$@" > "$MY2SQL_ARGS_FILE"
IFS= read -r password
printf '%s' "$password" > "$MY2SQL_STDIN_FILE"
output_dir=""
previous=""
for argument in "$@"; do
  if [ "$previous" = "-output-dir" ]; then output_dir="$argument"; fi
  previous="$argument"
done
printf 'DELETE FROM ` + "`app`.`items`" + ` WHERE ` + "`id`" + `=1;\n' > "$output_dir/result.sql"
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("write fake my2sql: %v", err)
	}
	t.Setenv("MY2SQL_ARGS_FILE", argsFile)
	t.Setenv("MY2SQL_STDIN_FILE", stdinFile)

	result, err := Run(context.Background(), Request{
		BinaryPath: script,
		WorkType:   WorkTypeRollback,
		Host:       "mysql.internal",
		Port:       3306,
		Username:   "binlog_reader",
		Password:   "secret-value",
		Range: PositionRange{
			StartFile: "mysql-bin.000001",
			StartPos:  4,
			EndFile:   "mysql-bin.000001",
			EndPos:    900,
		},
		Timeout:  5 * time.Second,
		MaxBytes: 1024,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.SQL != "DELETE FROM `app`.`items` WHERE `id`=1;" || result.StatementCount != 1 || result.Version != "my2sql-test-version" {
		t.Fatalf("unexpected result: %#v", result)
	}
	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if string(stdin) != "secret-value" {
		t.Fatalf("stdin password = %q", stdin)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	if strings.Contains(string(args), "secret-value") || strings.Contains(string(args), "-password") {
		t.Fatalf("password leaked into args: %q", args)
	}
	outputDir := argumentValue(string(args), "-output-dir")
	if outputDir == "" {
		t.Fatal("captured args missing output directory")
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("output directory was not removed: %s, err=%v", outputDir, err)
	}
	serverID := argumentValue(string(args), "-server-id")
	if id, err := strconv.ParseUint(serverID, 10, 32); err != nil || id == 0 {
		t.Fatalf("invalid server ID %q", serverID)
	}
}

func TestCollectOutputRejectsOversizeSQL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "result.sql"), []byte("INSERT INTO t VALUES (1);"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := collectOutput(dir, "", 10, WorkTypeRollback)
	if err == nil || err.Error() != "generated rollback sql exceeds size limit" {
		t.Fatalf("collectOutput() error = %v", err)
	}
}

func TestCollectOutputClassifiesNoMatchingStatements(t *testing.T) {
	_, err := collectOutput(t.TempDir(), "statistics only", 1024, WorkTypeForward)
	if !errors.Is(err, ErrNoMatchingStatements) {
		t.Fatalf("collectOutput() error = %v", err)
	}
}

func TestRunStopsWhenTemporaryFilesExceedLimit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-my2sql")
	body := `#!/bin/sh
if [ "$1" = "-v" ]; then exit 0; fi
IFS= read -r password
previous=""
for argument in "$@"; do
  if [ "$previous" = "-output-dir" ]; then output_dir="$argument"; fi
  previous="$argument"
done
dd if=/dev/zero of="$output_dir/large.sql" bs=1024 count=32 2>/dev/null
sleep 2
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Request{BinaryPath: script, WorkType: WorkTypeForward, Host: "mysql", Port: 3306,
		Username: "reader", Password: "secret", Range: PositionRange{StartFile: "a", StartPos: 4, EndFile: "a", EndPos: 5},
		Timeout: 5 * time.Second, MaxBytes: 1024, MaxTempBytes: 2048})
	if err == nil || !strings.Contains(err.Error(), "temporary disk limit") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunTimesOutAndTerminatesChildProcess(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-my2sql")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nIFS= read -r password\nsleep 10\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err := Run(context.Background(), Request{BinaryPath: script, WorkType: WorkTypeForward, Timeout: 100 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Run() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("timed-out child was not terminated promptly: %v", elapsed)
	}
}

func TestRunCancellationTerminatesChildProcess(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-my2sql")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nIFS= read -r password\nsleep 10\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	started := time.Now()
	_, err := Run(ctx, Request{BinaryPath: script, WorkType: WorkTypeForward, Timeout: 5 * time.Second})
	if err == nil {
		t.Fatal("cancelled Run() must return an error")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("cancelled child was not terminated promptly: %v", elapsed)
	}
}

func TestBuildArgsKeepsFilterValuesAsSingleArguments(t *testing.T) {
	args := buildArgs(Request{WorkType: WorkTypeForward, Databases: []string{"app name"}, Tables: []string{"orders;touch /tmp/nope"}}, "/tmp/output", 7)
	if got := args[indexOf(args, "-databases")+1]; got != "app name" {
		t.Fatalf("database argument was split: %q", got)
	}
	if got := args[indexOf(args, "-tables")+1]; got != "orders;touch /tmp/nope" {
		t.Fatalf("table argument was split: %q", got)
	}
}

func indexOf(items []string, target string) int {
	for i, item := range items {
		if item == target {
			return i
		}
	}
	return -1
}

func argumentValue(raw string, name string) string {
	parts := strings.Fields(raw)
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == name {
			return parts[i+1]
		}
	}
	return ""
}
