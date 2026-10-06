package my2sql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

type WorkType string

const (
	WorkTypeForward  WorkType = "2sql"
	WorkTypeRollback WorkType = "rollback"
)

type PositionRange struct {
	StartFile string
	StartPos  uint64
	EndFile   string
	EndPos    uint64
}

type TimeRange struct {
	Start string
	End   string
}

type Request struct {
	BinaryPath   string
	WorkType     WorkType
	Host         string
	Port         uint16
	Username     string
	Password     string
	Range        PositionRange
	Databases    []string
	Tables       []string
	SQLTypes     []string
	TimeRange    *TimeRange
	Timeout      time.Duration
	MaxBytes     int
	MaxTempBytes int64
}

type Result struct {
	SQL            string
	Version        string
	StatementCount int
}

var ErrNoMatchingStatements = errors.New("my2sql produced no matching sql statements")

var serverIDCounter atomic.Uint32

func init() {
	serverIDCounter.Store(uint32(time.Now().UnixNano()))
}

func Run(ctx context.Context, req Request) (Result, error) {
	if req.WorkType != WorkTypeForward && req.WorkType != WorkTypeRollback {
		return Result{}, fmt.Errorf("unsupported my2sql work type %q", req.WorkType)
	}
	req = normalizeRequest(req)
	outputDir, stdout, cleanup, err := execute(ctx, req)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	sqlText, err := collectOutput(outputDir, stdout, req.MaxBytes, req.WorkType)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(sqlText) == "" {
		return Result{}, fmt.Errorf("%w for %s", ErrNoMatchingStatements, workTypeLabel(req.WorkType))
	}

	return Result{
		SQL:            sqlText,
		Version:        version(ctx, strings.TrimSpace(req.BinaryPath)),
		StatementCount: countStatements(sqlText),
	}, nil
}

func normalizeRequest(req Request) Request {
	if req.Timeout <= 0 {
		req.Timeout = 30 * time.Second
	}
	if req.MaxBytes <= 0 {
		req.MaxBytes = 5 * 1024 * 1024
	}
	if req.MaxTempBytes <= 0 {
		req.MaxTempBytes = int64(req.MaxBytes) * 2
	}
	return req
}

func execute(ctx context.Context, req Request) (string, string, func(), error) {
	if strings.TrimSpace(req.BinaryPath) == "" {
		return "", "", nil, fmt.Errorf("my2sql path is not configured")
	}
	commandCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	outputDir, err := os.MkdirTemp("", "maestro-my2sql-*")
	if err != nil {
		cancel()
		return "", "", nil, fmt.Errorf("create my2sql output dir failed")
	}
	cleanup := func() { cancel(); _ = os.RemoveAll(outputDir) }
	cmd := exec.CommandContext(commandCtx, strings.TrimSpace(req.BinaryPath), buildArgs(req, outputDir, nextServerID())...)
	cmd.Stdin = strings.NewReader(req.Password + "\n")
	stdout, stderr := newLimitedBuffer(req.MaxBytes+64*1024), newLimitedBuffer(64*1024)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("start my2sql %s failed", workTypeLabel(req.WorkType))
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case runErr := <-wait:
			if runErr != nil {
				cleanup()
				if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
					return "", "", nil, fmt.Errorf("my2sql %s timed out", workTypeLabel(req.WorkType))
				}
				return "", "", nil, fmt.Errorf("my2sql %s failed: %s", workTypeLabel(req.WorkType), truncate(strings.TrimSpace(stderr.String()), 500))
			}
			if directoryBytes(outputDir) > req.MaxTempBytes {
				cleanup()
				return "", "", nil, fmt.Errorf("generated %s files exceed temporary disk limit", workTypeLabel(req.WorkType))
			}
			return outputDir, stdout.String(), cleanup, nil
		case <-ticker.C:
			if directoryBytes(outputDir) > req.MaxTempBytes {
				cancel()
				<-wait
				cleanup()
				return "", "", nil, fmt.Errorf("generated %s files exceed temporary disk limit", workTypeLabel(req.WorkType))
			}
		}
	}
}

func directoryBytes(root string) int64 {
	var total int64
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

func buildArgs(req Request, outputDir string, serverID uint32) []string {
	args := []string{
		"-mode", "repl",
		"-work-type", string(req.WorkType),
		"-user", req.Username,
		"-host", req.Host,
		"-port", strconv.Itoa(int(req.Port)),
		"-server-id", strconv.FormatUint(uint64(serverID), 10),
		"-output-dir", outputDir,
		"-add-extraInfo",
	}
	if req.TimeRange != nil {
		args = append(args, "-start-datetime", req.TimeRange.Start, "-stop-datetime", req.TimeRange.End)
		if req.Range.EndFile != "" && req.Range.EndPos > 0 {
			args = append(args, "-stop-file", req.Range.EndFile, "-stop-pos", strconv.FormatUint(req.Range.EndPos, 10))
		}
	} else {
		args = append(args,
			"-start-file", req.Range.StartFile,
			"-start-pos", strconv.FormatUint(req.Range.StartPos, 10),
			"-stop-file", req.Range.EndFile,
			"-stop-pos", strconv.FormatUint(req.Range.EndPos, 10),
		)
	}
	if len(req.Databases) > 0 {
		args = append(args, "-databases", strings.Join(req.Databases, ","))
	}
	if len(req.Tables) > 0 {
		args = append(args, "-tables", strings.Join(req.Tables, ","))
	}
	if len(req.SQLTypes) > 0 {
		args = append(args, "-sql", strings.Join(req.SQLTypes, ","))
	}
	return args
}

func nextServerID() uint32 {
	id := serverIDCounter.Add(1)
	if id == 0 {
		id = serverIDCounter.Add(1)
	}
	return id
}

func collectOutput(outputDir string, stdout string, maxBytes int, workType WorkType) (string, error) {
	files := []string{}
	if entries, err := os.ReadDir(outputDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".sql") {
				continue
			}
			files = append(files, filepath.Join(outputDir, entry.Name()))
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		left, leftOK := outputFileSequence(files[i])
		right, rightOK := outputFileSequence(files[j])
		if leftOK && rightOK && left != right {
			return left < right
		}
		return files[i] < files[j]
	})
	if workType == WorkTypeRollback {
		for left, right := 0, len(files)-1; left < right; left, right = left+1, right-1 {
			files[left], files[right] = files[right], files[left]
		}
	}
	var out strings.Builder
	for _, name := range files {
		file, err := os.Open(name)
		if err != nil {
			continue
		}
		remaining := maxBytes - out.Len()
		if remaining <= 0 {
			file.Close()
			return "", fmt.Errorf("generated %s sql exceeds size limit", workTypeLabel(workType))
		}
		data, readErr := io.ReadAll(io.LimitReader(file, int64(remaining)+1))
		file.Close()
		if readErr != nil {
			return "", readErr
		}
		if len(data) > remaining {
			return "", fmt.Errorf("generated %s sql exceeds size limit", workTypeLabel(workType))
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.Write(data)
	}
	if sqlOnly := extractStatements(out.String(), workType); sqlOnly != "" {
		if len([]byte(sqlOnly)) > maxBytes {
			return "", fmt.Errorf("generated %s sql exceeds size limit", workTypeLabel(workType))
		}
		return sqlOnly, nil
	}
	if sqlOnly := extractStatements(stdout, workType); sqlOnly != "" {
		if len([]byte(sqlOnly)) > maxBytes {
			return "", fmt.Errorf("generated %s sql exceeds size limit", workTypeLabel(workType))
		}
		return sqlOnly, nil
	}
	return "", fmt.Errorf("%w for %s", ErrNoMatchingStatements, workTypeLabel(workType))
}

func outputFileSequence(name string) (uint64, bool) {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	separator := strings.LastIndexByte(base, '.')
	if separator < 0 || separator == len(base)-1 {
		return 0, false
	}
	sequence, err := strconv.ParseUint(base[separator+1:], 10, 64)
	return sequence, err == nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func newLimitedBuffer(limit int) limitedBuffer { return limitedBuffer{limit: limit} }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func extractStatements(raw string, workType WorkType) string {
	if workType == WorkTypeRollback {
		return extractRollbackStatements(raw)
	}
	return extractForwardStatements(raw)
}

func extractForwardStatements(raw string) string {
	var groups []string
	var statements []string
	var current strings.Builder
	metadata := ""
	inStatement := false
	flushGroup := func() {
		if len(statements) == 0 {
			return
		}
		var group strings.Builder
		if metadata != "" {
			group.WriteString(metadata)
			group.WriteString("\n")
		}
		group.WriteString(strings.Join(statements, "\n"))
		groups = append(groups, group.String())
		statements = nil
	}

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !inStatement && strings.HasPrefix(trimmed, "# datetime=") {
			flushGroup()
			metadata = trimmed
			continue
		}
		if !inStatement && !isStatementStart(trimmed) {
			continue
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(trimmed)
		inStatement = true
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, current.String())
			current.Reset()
			inStatement = false
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		statements = append(statements, strings.TrimSpace(current.String()))
	}
	flushGroup()
	return strings.Join(groups, "\n\n")
}

func extractRollbackStatements(raw string) string {
	var groups []string
	var statements []string
	var current strings.Builder
	inStatement := false
	flushGroup := func(metadata string) {
		if len(statements) == 0 {
			return
		}
		var group strings.Builder
		if metadata != "" {
			group.WriteString(metadata)
			group.WriteString("\n")
		}
		group.WriteString(strings.Join(statements, "\n"))
		groups = append(groups, group.String())
		statements = nil
	}

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !inStatement && strings.HasPrefix(trimmed, "# datetime=") {
			flushGroup(trimmed)
			continue
		}
		if !inStatement && !isStatementStart(trimmed) {
			continue
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(trimmed)
		inStatement = true
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, strings.TrimSpace(current.String()))
			current.Reset()
			inStatement = false
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		statements = append(statements, strings.TrimSpace(current.String()))
	}
	flushGroup("")
	return strings.Join(groups, "\n\n")
}

func isStatementStart(line string) bool {
	upper := strings.ToUpper(strings.TrimSpace(line))
	return strings.HasPrefix(upper, "INSERT ") ||
		strings.HasPrefix(upper, "UPDATE ") ||
		strings.HasPrefix(upper, "DELETE ") ||
		strings.HasPrefix(upper, "REPLACE ")
}

func version(ctx context.Context, path string) string {
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, path, "-v").CombinedOutput()
	if err != nil {
		return ""
	}
	return truncate(strings.TrimSpace(string(out)), 120)
}

func countStatements(sqlText string) int {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, sqlText)
	if err == nil && len(parsed.Statements) > 0 {
		return len(parsed.Statements)
	}
	count := 0
	for _, part := range strings.Split(sqlText, ";") {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func workTypeLabel(workType WorkType) string {
	if workType == WorkTypeForward {
		return "forward"
	}
	return "rollback"
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
