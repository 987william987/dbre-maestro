package onlineddl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultToolOutputBytes = 64 * 1024
	maxToolOutputBytes     = 1024 * 1024
	toolVersionTimeout     = 10 * time.Second
)

type ToolRequest struct {
	Statement      Statement
	Parameters     Parameters
	Host           string
	Port           uint16
	Username       string
	Password       string
	Timeout        time.Duration
	MaxOutputBytes int
	DryRun         bool
}
type ToolResult struct {
	Stdout, Stderr  string
	OutputTruncated bool
}
type ToolProcess interface {
	PID() int
	Output() ToolResult
	Pause() error
	Resume() error
	CancelGracefully() error
	Tune(string) error
	Wait() (ToolResult, error)
	Terminate() error
}

type CommandAdapter struct{ mode, path string }

func NewGhostAdapter(path string) *CommandAdapter {
	return &CommandAdapter{mode: ModeGhost, path: strings.TrimSpace(path)}
}
func NewPTOSCAdapter(path string) *CommandAdapter {
	return &CommandAdapter{mode: ModePTOSC, path: strings.TrimSpace(path)}
}
func (a *CommandAdapter) Mode() string { return a.mode }

func (a *CommandAdapter) Version(ctx context.Context) (string, error) {
	if a.path == "" {
		return "", ErrToolUnavailable
	}
	versionCtx, cancel := context.WithTimeout(ctx, toolVersionTimeout)
	defer cancel()
	buffer := &limitedBuffer{limit: 4096}
	cmd := exec.CommandContext(versionCtx, a.path, "--version")
	cmd.Env = sanitizedEnvironment()
	cmd.Stdout, cmd.Stderr = buffer, buffer
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: version detection failed", ErrToolUnavailable)
	}
	version := strings.TrimSpace(buffer.String())
	if version == "" {
		return "", fmt.Errorf("%w: empty version", ErrToolUnavailable)
	}
	return version, nil
}

func (a *CommandAdapter) Start(ctx context.Context, req ToolRequest) (ToolProcess, error) {
	if a.path == "" {
		return nil, ErrToolUnavailable
	}
	if err := req.Parameters.Validate(a.mode); err != nil {
		return nil, err
	}
	if req.Statement.Database == "" || req.Statement.Table == "" || req.Statement.AlterClause == "" || req.Host == "" || req.Port == 0 || req.Username == "" {
		return nil, ErrInvalidParameters
	}
	workDir, err := os.MkdirTemp("", "maestro-online-ddl-*")
	if err != nil {
		return nil, fmt.Errorf("create online ddl work directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(workDir) }
	if err := os.Chmod(workDir, 0o700); err != nil {
		cleanup()
		return nil, err
	}
	configPath := filepath.Join(workDir, "client.cnf")
	if err := os.WriteFile(configPath, []byte(renderClientConfig(req)), 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("write online ddl client config: %w", err)
	}
	args := a.buildArgs(req, workDir, configPath)
	cmd := exec.Command(a.path, args...)
	cmd.Env = sanitizedEnvironment()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	limit := req.MaxOutputBytes
	if limit <= 0 {
		limit = defaultToolOutputBytes
	} else if limit > maxToolOutputBytes {
		limit = maxToolOutputBytes
	}
	stdout, stderr := &limitedBuffer{limit: limit}, &limitedBuffer{limit: limit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		cleanup()
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrToolUnavailable
		}
		return nil, fmt.Errorf("start %s: %w", a.mode, err)
	}
	p := &commandProcess{cmd: cmd, mode: a.mode, socketPath: filepath.Join(workDir, "gh-ost.sock"), pausePath: filepath.Join(workDir, "pt-osc.pause"), panicPath: filepath.Join(workDir, "gh-ost.panic"), stdout: stdout, stderr: stderr, password: req.Password, cleanup: cleanup, done: make(chan struct{})}
	go p.collect()
	go func() {
		select {
		case <-ctx.Done():
			p.setContextErr(ctx.Err())
			_ = p.Terminate()
		case <-p.done:
		}
	}()
	if req.Timeout > 0 {
		go func() {
			timer := time.NewTimer(req.Timeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				p.setContextErr(context.DeadlineExceeded)
				_ = p.Terminate()
			case <-p.done:
			}
		}()
	}
	return p, nil
}

func (a *CommandAdapter) buildArgs(req ToolRequest, workDir, configPath string) []string {
	if a.mode == ModeGhost {
		p := req.Parameters.Ghost
		args := []string{"--conf=" + configPath, "--host=" + req.Host, "--port=" + strconv.Itoa(int(req.Port)), "--database=" + req.Statement.Database, "--table=" + req.Statement.Table, "--alter=" + req.Statement.AlterClause, "--max-load=Threads_running=" + strconv.Itoa(p.MaxLoadThreadsRunning), "--critical-load=Threads_running=" + strconv.Itoa(p.CriticalLoadThreadsRunning), "--chunk-size=" + strconv.Itoa(p.ChunkSize), "--dml-batch-size=" + strconv.Itoa(p.DMLBatchSize), "--nice-ratio=" + strconv.FormatFloat(p.NiceRatio, 'f', -1, 64), "--max-lag-millis=" + strconv.Itoa(p.MaxLagMillis), "--cut-over-lock-timeout-seconds=" + strconv.Itoa(p.CutOverLockTimeoutSeconds), "--serve-socket-file=" + filepath.Join(workDir, "gh-ost.sock"), "--panic-flag-file=" + filepath.Join(workDir, "gh-ost.panic"), "--throttle-additional-flag-file=" + filepath.Join(workDir, "gh-ost.throttle"), "--allow-on-master", "--assume-rbr"}
		if !req.DryRun {
			args = append(args, "--execute", "--ok-to-drop-table")
		}
		return args
	}
	p := req.Parameters.PTOSC
	args := []string{"--defaults-file", configPath, "--alter", req.Statement.AlterClause, "--max-load", "Threads_running=" + strconv.Itoa(p.MaxLoadThreadsRunning), "--critical-load", "Threads_running=" + strconv.Itoa(p.CriticalLoadThreadsRunning), "--max-lag", strconv.Itoa(p.MaxLagSeconds), "--check-interval", strconv.Itoa(p.CheckIntervalSeconds), "--alter-foreign-keys-method", p.AlterForeignKeysMethod, "--recursion-method", "none", "--pause-file", filepath.Join(workDir, "pt-osc.pause"), "--force"}
	if p.ChunkSize != nil {
		args = append(args, "--chunk-size", strconv.Itoa(*p.ChunkSize))
	} else {
		args = append(args, "--chunk-time", strconv.FormatFloat(*p.ChunkTime, 'f', -1, 64))
	}
	if req.DryRun {
		args = append(args, "--dry-run")
	} else {
		args = append(args, "--execute", "--drop-old-table", "--drop-triggers", "--no-drop-new-table")
	}
	return append(args, "D="+req.Statement.Database+",t="+req.Statement.Table)
}

type commandProcess struct {
	cmd            *exec.Cmd
	mode           string
	socketPath     string
	pausePath      string
	panicPath      string
	ghostCommand   func(string) error
	stdout, stderr *limitedBuffer
	password       string
	cleanup        func()
	done           chan struct{}
	waitOnce       sync.Once
	result         ToolResult
	err            error
	contextErr     error
	terminateOnce  sync.Once
	mu             sync.Mutex
}

func (p *commandProcess) PID() int { return p.cmd.Process.Pid }
func (p *commandProcess) Output() ToolResult {
	return ToolResult{Stdout: redact(p.stdout.String(), p.password), Stderr: redact(p.stderr.String(), p.password), OutputTruncated: p.stdout.Truncated() || p.stderr.Truncated()}
}
func (p *commandProcess) Pause() error {
	if p.mode == ModeGhost {
		return p.sendGhost("throttle")
	}
	if p.mode != ModePTOSC {
		return ErrControlNotSupported
	}
	file, err := os.OpenFile(p.pausePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return file.Close()
}
func (p *commandProcess) Resume() error {
	if p.mode == ModeGhost {
		return p.sendGhost("no-throttle")
	}
	if p.mode != ModePTOSC {
		return ErrControlNotSupported
	}
	err := os.Remove(p.pausePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (p *commandProcess) sendGhost(command string) error {
	if p.ghostCommand != nil {
		return p.ghostCommand(command)
	}
	return sendGhostCommand(p.socketPath, command)
}
func (p *commandProcess) CancelGracefully() error {
	if p.mode == ModeGhost {
		file, err := os.OpenFile(p.panicPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return file.Close()
	}
	return p.Terminate()
}
func (p *commandProcess) Tune(command string) error {
	if p.mode != ModeGhost {
		return ErrControlNotSupported
	}
	return p.sendGhost(command)
}
func (p *commandProcess) collect() {
	p.waitOnce.Do(func() {
		err := p.cmd.Wait()
		p.result = p.Output()
		p.mu.Lock()
		contextErr := p.contextErr
		p.mu.Unlock()
		if contextErr != nil {
			p.err = contextErr
		} else if err != nil {
			p.err = ErrToolProcessFailed
		}
		p.cleanup()
		close(p.done)
	})
}
func (p *commandProcess) Wait() (ToolResult, error) { <-p.done; return p.result, p.err }
func (p *commandProcess) Terminate() error {
	var err error
	p.terminateOnce.Do(func() {
		if p.cmd.Process != nil {
			err = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
			if errors.Is(err, syscall.ESRCH) {
				err = nil
			}
			go func() {
				select {
				case <-p.done:
				case <-time.After(2 * time.Second):
					_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
				}
			}()
		}
	})
	return err
}
func (p *commandProcess) setContextErr(err error) {
	p.mu.Lock()
	if p.contextErr == nil {
		p.contextErr = err
	}
	p.mu.Unlock()
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
	mu        sync.Mutex
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(p)
	if b.limit <= 0 {
		b.truncated = b.truncated || original > 0
		return original, nil
	}
	if len(p) >= b.limit {
		b.buffer.Reset()
		_, _ = b.buffer.Write(p[len(p)-b.limit:])
		b.truncated = b.truncated || original > b.limit
		return original, nil
	}
	overflow := b.buffer.Len() + len(p) - b.limit
	if overflow > 0 {
		retained := append([]byte(nil), b.buffer.Bytes()[overflow:]...)
		b.buffer.Reset()
		_, _ = b.buffer.Write(retained)
		b.truncated = true
	}
	_, _ = b.buffer.Write(p)
	return original, nil
}
func (b *limitedBuffer) String() string  { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }
func (b *limitedBuffer) Truncated() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.truncated }
func sendGhostCommand(path, command string) error {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, err = fmt.Fprintln(conn, command)
	return err
}
func renderClientConfig(req ToolRequest) string {
	return "[client]\nhost=" + quoteConfig(req.Host) + "\nport=" + strconv.Itoa(int(req.Port)) + "\nuser=" + quoteConfig(req.Username) + "\npassword=" + quoteConfig(req.Password) + "\n"
}
func quoteConfig(value string) string {
	value = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r").Replace(value)
	return "\"" + value + "\""
}
func sanitizedEnvironment() []string {
	result := []string{}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "TOKEN") {
			continue
		}
		result = append(result, item)
	}
	return result
}
func redact(value, password string) string {
	if password == "" {
		return value
	}
	return strings.ReplaceAll(value, password, "[REDACTED]")
}
