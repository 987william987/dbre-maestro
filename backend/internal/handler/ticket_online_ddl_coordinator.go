package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/onlineddl"
	"github.com/dbre-maestro/maestro/internal/repository"
)

const (
	ghostBinaryPath = "/usr/local/bin/gh-ost"
	ptoscBinaryPath = "/usr/local/bin/pt-online-schema-change"
)

type ticketOnlineDDLCoordinator struct {
	handler  *TicketHandler
	runs     *repository.OnlineDDLRepo
	settings interface {
		Get(context.Context) (*model.PlatformSettings, error)
	}
	controls *onlineddl.Controller
	adapters map[string]onlineddl.Adapter
	locks    *onlineddl.TargetLocks
}

func (h *TicketHandler) ConfigureOnlineDDL(repo *repository.OnlineDDLRepo, controls *onlineddl.Controller) *onlineddl.Runner {
	if h == nil || repo == nil {
		return nil
	}
	if controls == nil {
		controls = onlineddl.NewController()
	}
	h.onlineDDL = &ticketOnlineDDLCoordinator{
		handler: h, runs: repo, settings: h.settings, controls: controls,
		adapters: OnlineDDLToolAdapters(), locks: onlineddl.NewTargetLocks(),
	}
	coordinator := h.onlineDDL.(*ticketOnlineDDLCoordinator)
	managed := &onlineddl.ManagedExecutor{Resolve: coordinator.ResolveExecution, Controls: controls, Progress: onlineddl.NewProgressSampler(repo.SaveProgress)}
	runner := onlineddl.NewRunner(repo, managed.Execute, coordinator.HandleTerminal, slog.Default())
	runner.Recover = coordinator.RecoverInterrupted
	return runner
}

func OnlineDDLToolAdapters() map[string]onlineddl.Adapter {
	return map[string]onlineddl.Adapter{
		onlineddl.ModeGhost: onlineddl.NewGhostAdapter(ghostBinaryPath),
		onlineddl.ModePTOSC: onlineddl.NewPTOSCAdapter(ptoscBinaryPath),
	}
}

type onlineDDLDryRunResult struct {
	Success         bool   `json:"success"`
	Stdout          string `json:"stdout,omitempty"`
	Stderr          string `json:"stderr,omitempty"`
	OutputTruncated bool   `json:"output_truncated"`
	ErrorCode       string `json:"error_code,omitempty"`
}

func (c *ticketOnlineDDLCoordinator) DryRun(ctx context.Context, ticket *model.Ticket, execution *model.TicketExecution, actorID uint64, mode string, parameters onlineddl.Parameters) (any, error) {
	statement, _, _, adapter, err := c.preparePlan(ctx, ticket, execution, actorID, mode, parameters)
	if err != nil {
		return nil, err
	}
	conn, err := c.handler.dbConns.GetByID(ctx, *ticket.DBConnectionID)
	if err != nil || conn == nil {
		return nil, onlineddl.ErrToolUnavailable
	}
	resolved, password, err := c.handler.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadwrite)
	if err != nil {
		return nil, err
	}
	release, acquired := c.locks.TryAcquire(resolved.Host, resolved.Port)
	if !acquired {
		return nil, onlineddl.ErrConflict
	}
	defer release()
	process, err := adapter.Start(ctx, onlineddl.ToolRequest{Statement: statement, Parameters: parameters, Host: resolved.Host, Port: resolved.Port, Username: resolved.Username, Password: password, Timeout: 5 * time.Minute, DryRun: true})
	if err != nil {
		return nil, err
	}
	output, waitErr := process.Wait()
	result := onlineDDLDryRunResult{Success: waitErr == nil, Stdout: output.Stdout, Stderr: output.Stderr, OutputTruncated: output.OutputTruncated}
	if waitErr != nil {
		result.ErrorCode = waitErr.Error()
	}
	if c.handler.audit != nil {
		_ = c.handler.audit.Log(ctx, repository.AuditEntry{ActorID: &actorID, ActorName: c.handler.auditActorName(ctx, &actorID, ""), ActionType: "ticket_online_ddl_dry_run", ResourceType: "ticket", ResourceID: &ticket.ID, Details: map[string]any{"execution_id": execution.ID, "mode": mode, "success": result.Success}})
	}
	return result, nil
}

func (c *ticketOnlineDDLCoordinator) Get(ctx context.Context, ticketID, executionID uint64) (*model.OnlineDDLRun, error) {
	return c.runs.GetByExecution(ctx, ticketID, executionID)
}

func (c *ticketOnlineDDLCoordinator) ListByTicket(ctx context.Context, ticketID uint64) ([]model.OnlineDDLRun, error) {
	return c.runs.ListByTicket(ctx, ticketID)
}

func (c *ticketOnlineDDLCoordinator) Queue(ctx context.Context, ticket *model.Ticket, execution *model.TicketExecution, executorID uint64, mode string, parameters onlineddl.Parameters) (*model.OnlineDDLRun, error) {
	statement, fingerprint, version, _, err := c.preparePlan(ctx, ticket, execution, executorID, mode, parameters)
	if err != nil {
		return nil, err
	}
	run, err := c.runs.CreateQueued(ctx, repository.OnlineDDLCreateInput{TicketID: ticket.ID, ExecutionID: execution.ID, ConnectionID: *ticket.DBConnectionID, ExecutorID: executorID, Mode: mode, SQLSHA256: statement.SQLSHA256, PreflightSHA256: fingerprint, ToolVersion: version, Parameters: parameters})
	if err == nil && c.handler.audit != nil {
		_ = c.handler.audit.Log(ctx, repository.AuditEntry{ActorID: &executorID, ActorName: c.handler.auditActorName(ctx, &executorID, ""), ActionType: "ticket_online_ddl_queued", ResourceType: "ticket", ResourceID: &ticket.ID, Details: map[string]any{"execution_id": execution.ID, "mode": mode, "parameters": parameters}})
	}
	return run, err
}

func (c *ticketOnlineDDLCoordinator) preparePlan(ctx context.Context, ticket *model.Ticket, execution *model.TicketExecution, actorID uint64, mode string, parameters onlineddl.Parameters) (onlineddl.Statement, string, string, onlineddl.Adapter, error) {
	if c == nil || c.handler == nil || ticket == nil || execution == nil || ticket.DBConnectionID == nil || ticket.DatabaseName == nil || c.settings == nil {
		return onlineddl.Statement{}, "", "", nil, onlineddl.ErrInvalidParameters
	}
	if err := parameters.Validate(mode); err != nil {
		return onlineddl.Statement{}, "", "", nil, err
	}
	conn, err := c.handler.dbConns.GetByID(ctx, *ticket.DBConnectionID)
	if err != nil {
		return onlineddl.Statement{}, "", "", nil, err
	}
	if !onlineDDLSupportsConnection(conn) {
		return onlineddl.Statement{}, "", "", nil, onlineddl.ErrUnsupportedStatement
	}
	settings, err := c.settings.Get(ctx)
	if err != nil {
		return onlineddl.Statement{}, "", "", nil, err
	}
	if settings == nil || (mode == onlineddl.ModeGhost && !settings.DDLGhostEnabled) || (mode == onlineddl.ModePTOSC && !settings.DDLPTOSCEnabled) {
		return onlineddl.Statement{}, "", "", nil, onlineddl.ErrModeDisabled
	}
	adapter := c.adapters[mode]
	if adapter == nil {
		return onlineddl.Statement{}, "", "", nil, onlineddl.ErrInvalidParameters
	}
	statement, err := onlineddl.ParseApprovedAlter(execution.SQLStmt, strings.TrimSpace(*ticket.DatabaseName))
	if err != nil {
		return onlineddl.Statement{}, "", "", nil, err
	}
	rawVersion, err := adapter.Version(ctx)
	if err != nil {
		return onlineddl.Statement{}, "", "", nil, err
	}
	version := canonicalOnlineDDLToolVersion(mode, rawVersion)
	raw, err := json.Marshal([]any{actorID, ticket.ID, execution.ID, *ticket.DBConnectionID, statement, mode, parameters, version})
	if err != nil {
		return onlineddl.Statement{}, "", "", nil, err
	}
	sum := sha256.Sum256(raw)
	return statement, fmt.Sprintf("%x", sum[:]), version, adapter, nil
}

func (c *ticketOnlineDDLCoordinator) Pause(ctx context.Context, run *model.OnlineDDLRun, _ uint64, version uint64) (*model.OnlineDDLRun, error) {
	if run.Version != version {
		return nil, onlineddl.ErrStaleVersion
	}
	if err := c.controls.Pause(run.ID); err != nil {
		return nil, err
	}
	ok, err := c.runs.SetPaused(ctx, run.ID, version, true)
	if err != nil || !ok {
		_ = c.controls.Resume(run.ID)
		if err != nil {
			return nil, err
		}
		return nil, onlineddl.ErrStaleVersion
	}
	return c.runs.GetByID(ctx, run.ID)
}

func (c *ticketOnlineDDLCoordinator) Resume(ctx context.Context, run *model.OnlineDDLRun, _ uint64, version uint64) (*model.OnlineDDLRun, error) {
	if run.Version != version {
		return nil, onlineddl.ErrStaleVersion
	}
	if err := c.controls.Resume(run.ID); err != nil {
		return nil, err
	}
	ok, err := c.runs.SetPaused(ctx, run.ID, version, false)
	if err != nil || !ok {
		_ = c.controls.Pause(run.ID)
		if err != nil {
			return nil, err
		}
		return nil, onlineddl.ErrStaleVersion
	}
	return c.runs.GetByID(ctx, run.ID)
}

func (c *ticketOnlineDDLCoordinator) Cancel(ctx context.Context, run *model.OnlineDDLRun, _ uint64, version uint64) (*model.OnlineDDLRun, error) {
	if run.Version != version {
		return nil, onlineddl.ErrStaleVersion
	}
	ok, err := c.runs.RequestCancel(ctx, run.ID, version)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, onlineddl.ErrStaleVersion
	}
	if err := c.controls.Cancel(ctx, run.ID, 5*time.Second); err != nil {
		return nil, err
	}
	return c.runs.GetByID(ctx, run.ID)
}

func (c *ticketOnlineDDLCoordinator) Tune(ctx context.Context, run *model.OnlineDDLRun, actorID, version uint64, patch onlineddl.RuntimeTuning) (*model.OnlineDDLRun, error) {
	return (&onlineddl.TuningService{Store: c.runs, Controls: c.controls}).Tune(ctx, run.ID, actorID, version, patch)
}

func canonicalOnlineDDLToolVersion(mode, raw string) string {
	want := "1.1.6"
	if mode == onlineddl.ModePTOSC {
		want = "3.7.1"
	}
	if strings.Contains(raw, want) {
		return want
	}
	return strings.TrimSpace(raw)
}

func (c *ticketOnlineDDLCoordinator) ResolveExecution(ctx context.Context, run *model.OnlineDDLRun) (*onlineddl.ExecutionPlan, error) {
	if run == nil || run.ToolVersion == nil {
		return nil, onlineddl.ErrPreflightChanged
	}
	ticket, err := c.handler.tickets.GetByID(ctx, run.TicketID)
	if err != nil || ticket == nil || ticket.DBConnectionID == nil || *ticket.DBConnectionID != run.ConnectionID {
		return nil, onlineddl.ErrPreflightChanged
	}
	execution, err := c.handler.tickets.GetExecution(ctx, run.TicketID, run.ExecutionID)
	if err != nil || execution == nil {
		return nil, onlineddl.ErrPreflightChanged
	}
	statement, err := onlineddl.ParseApprovedAlter(execution.SQLStmt, nullableStringValue(ticket.DatabaseName))
	if err != nil || statement.SQLSHA256 != run.SQLSHA256 {
		return nil, onlineddl.ErrPreflightChanged
	}
	var parameters onlineddl.Parameters
	if json.Unmarshal(run.EffectiveParameters, &parameters) != nil || parameters.Validate(run.Mode) != nil {
		return nil, onlineddl.ErrInvalidParameters
	}
	adapter := c.adapters[run.Mode]
	if adapter == nil {
		return nil, onlineddl.ErrToolUnavailable
	}
	version, err := adapter.Version(ctx)
	if err != nil || canonicalOnlineDDLToolVersion(run.Mode, version) != *run.ToolVersion {
		return nil, onlineddl.ErrPreflightChanged
	}
	conn, err := c.handler.dbConns.GetByID(ctx, run.ConnectionID)
	if err != nil || conn == nil {
		return nil, onlineddl.ErrPreflightChanged
	}
	if !onlineDDLSupportsConnection(conn) {
		return nil, onlineddl.ErrUnsupportedStatement
	}
	resolved, password, err := c.handler.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadwrite)
	if err != nil {
		return nil, err
	}
	release, err := c.locks.Acquire(ctx, resolved.Host, resolved.Port)
	if err != nil {
		return nil, err
	}
	database := statement.Database
	resolved.DatabaseName = &database
	db, cleanup, err := openResolvedSQLDB(ctx, resolved, password)
	if err != nil {
		release()
		return nil, err
	}
	cleanupAll := func() { cleanup(); release() }
	baseline, err := onlineDDLCreateHash(ctx, db, statement.Database, statement.Table)
	if err != nil {
		cleanupAll()
		return nil, err
	}
	verify := func(verifyCtx context.Context, _ onlineddl.ToolResult, processErr error, cancelling bool) (onlineddl.OutcomeEvidence, error) {
		if processErr == nil && !cancelling {
			return onlineddl.OutcomeEvidence{ProcessSucceeded: true, VerificationComplete: true, TargetDDLApplied: true}, nil
		}
		current, err := onlineDDLCreateHash(verifyCtx, db, statement.Database, statement.Table)
		if err != nil {
			return onlineddl.OutcomeEvidence{}, err
		}
		artifacts, err := onlineddl.DiscoverArtifacts(verifyCtx, onlineddl.SQLArtifactProbe{DB: db}, run.Mode, statement.Database, statement.Table)
		if err != nil {
			return onlineddl.OutcomeEvidence{}, err
		}
		return onlineDDLOutcomeEvidence(processErr, cancelling, baseline, current, artifacts), nil
	}
	return &onlineddl.ExecutionPlan{Adapter: adapter, Request: onlineddl.ToolRequest{Statement: statement, Parameters: parameters, Host: resolved.Host, Port: resolved.Port, Username: resolved.Username, Password: password, Timeout: 24 * time.Hour}, Verify: verify, Cleanup: cleanupAll}, nil
}

func onlineDDLOutcomeEvidence(processErr error, cancelling bool, baseline, current [32]byte, artifacts onlineddl.ArtifactSummary) onlineddl.OutcomeEvidence {
	return onlineddl.OutcomeEvidence{CancelRequested: cancelling, ProcessSucceeded: processErr == nil, VerificationComplete: true, TargetDDLApplied: current != baseline, OriginalUnchanged: current == baseline, Artifacts: artifacts}
}

func (c *ticketOnlineDDLCoordinator) RecoverInterrupted(ctx context.Context, run *model.OnlineDDLRun) (onlineddl.OutcomeEvidence, error) {
	ticket, err := c.handler.tickets.GetByID(ctx, run.TicketID)
	if err != nil || ticket == nil {
		return onlineddl.OutcomeEvidence{}, err
	}
	execution, err := c.handler.tickets.GetExecution(ctx, run.TicketID, run.ExecutionID)
	if err != nil || execution == nil {
		return onlineddl.OutcomeEvidence{}, err
	}
	statement, err := onlineddl.ParseApprovedAlter(execution.SQLStmt, nullableStringValue(ticket.DatabaseName))
	if err != nil {
		return onlineddl.OutcomeEvidence{}, err
	}
	db, cleanup, _, err := c.handler.openTicketSQLDBWithConnection(ctx, run.ConnectionID, model.DBCredentialRoleReadwrite, ticket.DatabaseName)
	if err != nil {
		return onlineddl.OutcomeEvidence{}, err
	}
	defer cleanup()
	artifacts, err := onlineddl.DiscoverArtifacts(ctx, onlineddl.SQLArtifactProbe{DB: db}, run.Mode, statement.Database, statement.Table)
	return onlineddl.OutcomeEvidence{Artifacts: artifacts}, err
}

func onlineDDLCreateHash(ctx context.Context, db *sql.DB, database, table string) ([32]byte, error) {
	var name, ddl string
	query := fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", strings.ReplaceAll(database, "`", "``"), strings.ReplaceAll(table, "`", "``"))
	if err := db.QueryRowContext(ctx, query).Scan(&name, &ddl); err != nil {
		return [32]byte{}, err
	}
	ddl = onlineDDLAutoIncrement.ReplaceAllString(ddl, " AUTO_INCREMENT=?")
	return sha256.Sum256([]byte(ddl)), nil
}

var onlineDDLAutoIncrement = regexp.MustCompile(`(?i)\s+AUTO_INCREMENT=\d+`)

func onlineDDLSupportsConnection(connection *model.DBConnection) bool {
	return connection != nil && strings.EqualFold(strings.TrimSpace(connection.DBType), "mysql")
}

func (c *ticketOnlineDDLCoordinator) HandleTerminal(ctx context.Context, run *model.OnlineDDLRun, status, code, detail string, duration time.Duration) error {
	durationMs := duration.Milliseconds()
	if status == onlineddl.StatusCompleted || status == onlineddl.StatusCompletedAfterCancel {
		if err := c.handler.tickets.MarkExecutionDone(ctx, run.ExecutionID, nil, &durationMs, nil); err != nil {
			return err
		}
	} else {
		if code == "" {
			code = status
		}
		if strings.TrimSpace(detail) == "" {
			detail = code
		}
		if err := c.handler.tickets.MarkExecutionFailedWithOutcome(ctx, run.ExecutionID, &durationMs, detail, code, status); err != nil {
			return err
		}
	}
	ticket, err := c.handler.tickets.GetByID(ctx, run.TicketID)
	if err != nil || ticket == nil {
		return err
	}
	if c.handler.audit != nil {
		actor := run.ExecutorID
		_ = c.handler.audit.Log(ctx, repository.AuditEntry{ActorID: &actor, ActorName: c.handler.auditActorName(ctx, &actor, ""), ActionType: "ticket_online_ddl_terminal", ResourceType: "ticket", ResourceID: &run.TicketID, Details: map[string]any{"execution_id": run.ExecutionID, "run_id": run.ID, "mode": run.Mode, "status": status, "error_code": code, "duration_ms": durationMs}})
	}
	if ticket.ExecutionRunMode != nil && *ticket.ExecutionRunMode == model.TicketExecutionRunModeBatch {
		c.handler.runTicketExecutionWithOptions(ticket, run.ExecutorID, ticketExecutionRunOptions{})
	} else {
		c.handler.refreshTicketStatusFromExecutions(ctx, run.TicketID)
		c.handler.publishTicketUpdateByID(ctx, run.TicketID, ticket, &run.ExecutorID)
	}
	return nil
}

func (h *TicketHandler) queueOnlineDDL(ctx context.Context, ticket *model.Ticket, execution *model.TicketExecution, executorID uint64, mode string, parameters onlineddl.Parameters) (bool, error) {
	queue, ok := h.onlineDDL.(ticketOnlineDDLQueue)
	if !ok {
		return false, nil
	}
	_, err := queue.Queue(ctx, ticket, execution, executorID, mode, parameters)
	return err == nil, err
}

func onlineDDLQueueErrorCode(err error) string {
	for _, candidate := range []error{onlineddl.ErrModeDisabled, onlineddl.ErrUnsupportedStatement, onlineddl.ErrPreflightChanged, onlineddl.ErrToolUnavailable, onlineddl.ErrConflict, onlineddl.ErrInvalidParameters, onlineddl.ErrStaleVersion} {
		if errors.Is(err, candidate) {
			return candidate.Error()
		}
	}
	return "online_ddl_queue_failed"
}

var _ TicketOnlineDDLService = (*ticketOnlineDDLCoordinator)(nil)
var _ ticketOnlineDDLQueue = (*ticketOnlineDDLCoordinator)(nil)
