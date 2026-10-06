package onlineddl

import (
	"context"
	"errors"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
)

const outcomeVerificationTimeout = 10 * time.Second

type ExecutionPlan struct {
	Adapter Adapter
	Request ToolRequest
	Verify  func(context.Context, ToolResult, error, bool) (OutcomeEvidence, error)
	Cleanup func()
}
type ExecutionResolver func(context.Context, *model.OnlineDDLRun) (*ExecutionPlan, error)

type ManagedExecutor struct {
	Resolve        ExecutionResolver
	Controls       *Controller
	Progress       *ProgressSampler
	SampleInterval time.Duration
}

func (e *ManagedExecutor) Execute(ctx context.Context, run *model.OnlineDDLRun) error {
	if e.Resolve == nil {
		return ErrToolUnavailable
	}
	plan, err := e.Resolve(ctx, run)
	if err != nil {
		return err
	}
	if plan == nil || plan.Adapter == nil || plan.Verify == nil {
		return ErrToolUnavailable
	}
	if plan.Cleanup != nil {
		defer plan.Cleanup()
	}
	process, err := plan.Adapter.Start(ctx, plan.Request)
	if err != nil {
		return err
	}
	controls := e.Controls
	if controls == nil {
		controls = NewController()
	}
	if !controls.Register(run.ID, process) {
		_ = process.Terminate()
		_, _ = process.Wait()
		return ErrConflict
	}
	defer controls.Unregister(run.ID)
	interval := e.SampleInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	done := make(chan struct{})
	var result ToolResult
	var waitErr error
	go func() { result, waitErr = process.Wait(); close(done) }()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			if e.Progress != nil {
				_, _ = e.Progress.SampleFinal(context.WithoutCancel(ctx), run.ID, run.Mode, result.Stdout+"\n"+result.Stderr)
				e.Progress.Forget(run.ID)
			}
			verifyCtx, cancelVerify := context.WithTimeout(context.WithoutCancel(ctx), outcomeVerificationTimeout)
			evidence, verifyErr := plan.Verify(verifyCtx, result, waitErr, controls.IsCancelling(run.ID))
			cancelVerify()
			if verifyErr != nil {
				return VerifiedTerminalOutcomeWithDetail(StatusOutcomeUnknown, ErrOutcomeUnknown.Error(), FailureSummary(result), ArtifactSummary{})
			}
			status, code := ClassifyOutcome(evidence)
			detail := ""
			if waitErr != nil {
				detail = FailureSummary(result)
			}
			return VerifiedTerminalOutcomeWithDetail(status, code, detail, evidence.Artifacts)
		case <-ticker.C:
			if e.Progress != nil {
				output := process.Output()
				_, _ = e.Progress.Sample(ctx, run.ID, run.Mode, output.Stdout+"\n"+output.Stderr)
			}
		case <-ctx.Done():
			_ = process.Terminate()
			<-done
			verifyCtx, cancelVerify := context.WithTimeout(context.WithoutCancel(ctx), outcomeVerificationTimeout)
			evidence, verifyErr := plan.Verify(verifyCtx, result, waitErr, controls.IsCancelling(run.ID))
			cancelVerify()
			if verifyErr != nil {
				return VerifiedTerminalOutcome(StatusInterrupted, "server_shutdown", ArtifactSummary{})
			}
			return VerifiedTerminalOutcome(StatusInterrupted, "server_shutdown", evidence.Artifacts)
		}
	}
}

func IsTerminalOutcome(err error, status string) bool {
	var terminal *TerminalError
	return errors.As(err, &terminal) && terminal.Status == status
}
