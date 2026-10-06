package onlineddl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
)

func TestParseProgressFixturesAndRejectMalformedOutput(t *testing.T) {
	ghost, ok := ParseProgress(ModeGhost, "noise\nCopy: 125/1000 12.5%; Applied: 4; Backlog: 0/100; Time: 1m; ETA: 42s\n")
	if !ok || *ghost.CopiedRows != 125 || *ghost.ProgressPercent != 12.5 || *ghost.ETASeconds != 42 {
		t.Fatalf("ghost=%+v ok=%v", ghost, ok)
	}
	ptosc, ok := ParseProgress(ModePTOSC, "500 rows copied; 25.0% done; 00:01:30 remain")
	if !ok || *ptosc.CopiedRows != 500 || *ptosc.ETASeconds != 90 {
		t.Fatalf("ptosc=%+v ok=%v", ptosc, ok)
	}
	common, ok := ParseProgress(ModePTOSC, "Copying `app`.`orders`:  95% 02:03 remain")
	if !ok || *common.ProgressPercent != 95 || *common.ETASeconds != 123 {
		t.Fatalf("common ptosc=%+v ok=%v", common, ok)
	}
	enriched, ok := ParseProgress(ModeGhost, "MySQL load: Threads_running=7\nCopy: 5/10 50.0%; ETA: 9s; State: migrating; LAG: 0.25s; throttled by replica")
	if !ok || enriched.Phase != "migrating" || *enriched.ReplicationLagMs != 250 || *enriched.ThreadsRunning != 7 || enriched.ThrottleReason == "" {
		t.Fatalf("enriched=%+v", enriched)
	}
	multiETA, ok := ParseProgress(ModeGhost, "Copy: 5/10 50.0%; ETA: 2m3s")
	if !ok || *multiETA.ETASeconds != 123 {
		t.Fatalf("multi eta=%+v ok=%v", multiETA, ok)
	}
	for _, marker := range []string{"N/A", "due"} {
		progress, ok := ParseProgress(ModeGhost, "Copy: 0/0 100.0%; Applied: 0; ETA: "+marker)
		if !ok || progress.ProgressPercent == nil || *progress.ProgressPercent != 100 || progress.CopiedRows == nil || *progress.CopiedRows != 0 || progress.ETASeconds != nil {
			t.Fatalf("marker=%s progress=%+v ok=%v", marker, progress, ok)
		}
	}
	withoutRuntimeLoad, ok := ParseProgress(ModeGhost, "# max-load: Threads_running=10\nCopy: 1/2 50.0%; ETA: N/A")
	if !ok || withoutRuntimeLoad.ThreadsRunning != nil {
		t.Fatalf("configured max load was treated as runtime load: %+v", withoutRuntimeLoad)
	}
	ghostRecovered, ok := ParseProgress(ModeGhost, "MySQL load: Threads_running=12\nCopy: 10/100 10.0%; ETA: 9s; State: migrating; throttled, max-load Threads_running=12 >= 10\nCopy: 20/100 20.0%; ETA: 8s; State: migrating")
	if !ok || *ghostRecovered.CopiedRows != 20 || ghostRecovered.ThrottleReason != "" || ghostRecovered.ThreadsRunning != nil {
		t.Fatalf("latest gh-ost snapshot retained stale metrics: %+v", ghostRecovered)
	}
	ghostThrottled, ok := ParseProgress(ModeGhost, "Copy: 10/100 10.0%; ETA: 9s\nCopy: 20/100 20.0%; ETA: 8s\nMySQL load: Threads_running=12\nthrottled, max-load Threads_running=12 >= 10")
	if !ok || ghostThrottled.ThrottleReason == "" || ghostThrottled.ThreadsRunning == nil || *ghostThrottled.ThreadsRunning != 12 {
		t.Fatalf("latest gh-ost snapshot omitted trailing metrics: %+v", ghostThrottled)
	}
	ptoscRecovered, ok := ParseProgress(ModePTOSC, "MySQL load: Threads_running=30\nthrottling because Threads_running=30\n100 rows copied; 10.0% done; 00:00:09 remain\n200 rows copied; 20.0% done; 00:00:08 remain")
	if !ok || *ptoscRecovered.CopiedRows != 200 || ptoscRecovered.ThrottleReason != "" || ptoscRecovered.ThreadsRunning != nil {
		t.Fatalf("latest pt-osc snapshot retained stale metrics: %+v", ptoscRecovered)
	}
	for _, output := range []string{"", "Copy: broken", "500 rows copied; 25%"} {
		if _, ok := ParseProgress(ModeGhost, output); ok {
			t.Fatalf("malformed output parsed: %q", output)
		}
	}
}

func TestProgressSamplerLimitsLatestAndHistoryWrites(t *testing.T) {
	now := time.Unix(100, 0)
	writes, histories := 0, 0
	sampler := NewProgressSampler(func(_ context.Context, _ uint64, _ ProgressSnapshot, history bool) (bool, error) {
		writes++
		if history {
			histories++
		}
		return true, nil
	})
	sampler.now = func() time.Time { return now }
	fixture := "Copy: 1/10 10.0%; Applied: 0; ETA: 9s"
	if ok, _ := sampler.Sample(context.Background(), 1, ModeGhost, fixture); !ok {
		t.Fatal("first sample not written")
	}
	now = now.Add(time.Second)
	if ok, _ := sampler.Sample(context.Background(), 1, ModeGhost, fixture); ok {
		t.Fatal("latest progress exceeded two-second rate")
	}
	now = now.Add(time.Second)
	_, _ = sampler.Sample(context.Background(), 1, ModeGhost, fixture)
	now = now.Add(8 * time.Second)
	_, _ = sampler.Sample(context.Background(), 1, ModeGhost, fixture)
	if writes != 3 || histories != 2 {
		t.Fatalf("writes=%d histories=%d", writes, histories)
	}
	if ok, _ := sampler.SampleFinal(context.Background(), 1, ModeGhost, fixture); !ok || writes != 4 {
		t.Fatalf("final sample was throttled: ok=%v writes=%d", ok, writes)
	}
}

func TestCommandProcessPauseResumeAndGracefulCancelAreIdempotent(t *testing.T) {
	dir := t.TempDir()
	pt := &commandProcess{mode: ModePTOSC, pausePath: filepath.Join(dir, "pause")}
	if err := pt.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := pt.Pause(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pt.pausePath); err != nil {
		t.Fatal(err)
	}
	if err := pt.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := pt.Resume(); err != nil {
		t.Fatal(err)
	}
	ghost := &commandProcess{mode: ModeGhost, panicPath: filepath.Join(dir, "panic")}
	if err := ghost.CancelGracefully(); err != nil {
		t.Fatal(err)
	}
	if err := ghost.CancelGracefully(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ghost.panicPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("panic flag info=%v err=%v", info, err)
	}
}

func TestGhostPauseResumeUseSocketCommands(t *testing.T) {
	var commands []string
	p := &commandProcess{mode: ModeGhost, ghostCommand: func(command string) error { commands = append(commands, command); return nil }}
	if err := p.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := p.Resume(); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[0] != "throttle" || commands[1] != "no-throttle" {
		t.Fatalf("commands=%v", commands)
	}
}

type fakeControlledProcess struct {
	mu                                    sync.Mutex
	done                                  chan struct{}
	result                                ToolResult
	waitErr                               error
	graceful, terminated, paused, resumed int
}

type fakeManagedAdapter struct{ process ToolProcess }

func (a fakeManagedAdapter) Mode() string                            { return ModeGhost }
func (a fakeManagedAdapter) Version(context.Context) (string, error) { return "1.1.6", nil }
func (a fakeManagedAdapter) Start(context.Context, ToolRequest) (ToolProcess, error) {
	return a.process, nil
}

type failingManagedAdapter struct{}

func (failingManagedAdapter) Mode() string                            { return ModeGhost }
func (failingManagedAdapter) Version(context.Context) (string, error) { return "1.1.6", nil }
func (failingManagedAdapter) Start(context.Context, ToolRequest) (ToolProcess, error) {
	return nil, ErrToolUnavailable
}

func TestManagedExecutorCleansResolverResourcesWhenStartFails(t *testing.T) {
	cleaned := 0
	executor := &ManagedExecutor{Resolve: func(context.Context, *model.OnlineDDLRun) (*ExecutionPlan, error) {
		return &ExecutionPlan{Adapter: failingManagedAdapter{}, Verify: func(context.Context, ToolResult, error, bool) (OutcomeEvidence, error) { return OutcomeEvidence{}, nil }, Cleanup: func() { cleaned++ }}, nil
	}}
	if err := executor.Execute(context.Background(), &model.OnlineDDLRun{}); !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if cleaned != 1 {
		t.Fatalf("cleanup calls=%d", cleaned)
	}
}

func newFakeControlledProcess(done bool) *fakeControlledProcess {
	p := &fakeControlledProcess{done: make(chan struct{})}
	if done {
		close(p.done)
	}
	return p
}
func (p *fakeControlledProcess) PID() int           { return 1 }
func (p *fakeControlledProcess) Output() ToolResult { return p.result }
func (p *fakeControlledProcess) Pause() error       { p.mu.Lock(); p.paused++; p.mu.Unlock(); return nil }
func (p *fakeControlledProcess) Resume() error      { p.mu.Lock(); p.resumed++; p.mu.Unlock(); return nil }
func (p *fakeControlledProcess) CancelGracefully() error {
	p.mu.Lock()
	p.graceful++
	p.mu.Unlock()
	return nil
}
func (p *fakeControlledProcess) Tune(string) error         { return nil }
func (p *fakeControlledProcess) Wait() (ToolResult, error) { <-p.done; return p.result, p.waitErr }
func (p *fakeControlledProcess) Terminate() error {
	p.mu.Lock()
	p.terminated++
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	p.mu.Unlock()
	return nil
}

func TestControllerIdempotencyExitRaceAndForcedCancel(t *testing.T) {
	controller := NewController()
	process := newFakeControlledProcess(false)
	if !controller.Register(1, process) {
		t.Fatal("register failed")
	}
	if err := controller.Pause(1); err != nil {
		t.Fatal(err)
	}
	if err := controller.Pause(1); err != nil {
		t.Fatal(err)
	}
	if err := controller.Resume(1); err != nil {
		t.Fatal(err)
	}
	if err := controller.Resume(1); err != nil {
		t.Fatal(err)
	}
	if err := controller.Cancel(context.Background(), 1, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.paused != 1 || process.resumed != 1 || process.graceful != 1 || process.terminated != 1 {
		t.Fatalf("process=%+v", process)
	}
	exited := newFakeControlledProcess(true)
	if !controller.Register(2, exited) {
		t.Fatal("register failed")
	}
	if err := controller.Cancel(context.Background(), 2, time.Second); err != nil {
		t.Fatal(err)
	}
	controller.Unregister(2)
	if !errors.Is(controller.Cancel(context.Background(), 2, time.Second), ErrInvalidTransition) {
		t.Fatal("inactive process accepted cancel")
	}
}

type artifactProbeStub struct {
	tables, triggers map[string]bool
	calls            int
}

func (p *artifactProbeStub) TableExists(_ context.Context, _, name string) (bool, error) {
	p.calls++
	return p.tables[name], nil
}
func (p *artifactProbeStub) TriggerExists(_ context.Context, _, name string) (bool, error) {
	p.calls++
	return p.triggers[name], nil
}

func TestArtifactDiscoveryAndConservativeOutcomeClassification(t *testing.T) {
	probe := &artifactProbeStub{tables: map[string]bool{"_orders_gho": true}, triggers: map[string]bool{}}
	artifacts, err := DiscoverArtifacts(context.Background(), probe, ModeGhost, "app", "orders")
	if err != nil || !artifacts.HasArtifacts() || probe.calls != 3 {
		t.Fatalf("artifacts=%+v calls=%d err=%v", artifacts, probe.calls, err)
	}
	tests := []struct {
		name     string
		evidence OutcomeEvidence
		status   string
	}{
		{"completed", OutcomeEvidence{ProcessSucceeded: true, VerificationComplete: true, TargetDDLApplied: true}, StatusCompleted},
		{"failed", OutcomeEvidence{VerificationComplete: true, OriginalUnchanged: true}, StatusFailed},
		{"completed after cancel", OutcomeEvidence{CancelRequested: true, VerificationComplete: true, TargetDDLApplied: true}, StatusCompletedAfterCancel},
		{"cancelled", OutcomeEvidence{CancelRequested: true, VerificationComplete: true, OriginalUnchanged: true}, StatusCancelled},
		{"cancelled artifacts", OutcomeEvidence{CancelRequested: true, VerificationComplete: true, OriginalUnchanged: true, Artifacts: artifacts}, StatusCancelledArtifacts},
		{"unknown verification", OutcomeEvidence{CancelRequested: true}, StatusOutcomeUnknown},
		{"unknown cutover race", OutcomeEvidence{CancelRequested: true, VerificationComplete: true}, StatusOutcomeUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := ClassifyOutcome(tc.evidence)
			if status != tc.status {
				t.Fatalf("status=%q", status)
			}
		})
	}
	if len(toolObjectName(strings.Repeat("a", 80), "_new")) > 64 {
		t.Fatal("artifact name exceeds MySQL identifier limit")
	}
}

func TestManagedExecutorAlwaysVerifiesOutcomeAndExposesControl(t *testing.T) {
	t.Run("successful process still requires verified ddl", func(t *testing.T) {
		process := newFakeControlledProcess(true)
		verified := false
		executor := &ManagedExecutor{Resolve: func(context.Context, *model.OnlineDDLRun) (*ExecutionPlan, error) {
			return &ExecutionPlan{Adapter: fakeManagedAdapter{process}, Verify: func(context.Context, ToolResult, error, bool) (OutcomeEvidence, error) {
				verified = true
				return OutcomeEvidence{ProcessSucceeded: true, VerificationComplete: true, TargetDDLApplied: true}, nil
			}}, nil
		}}
		if err := executor.Execute(context.Background(), &model.OnlineDDLRun{ID: 1, Mode: ModeGhost}); !IsTerminalOutcome(err, StatusCompleted) || !verified {
			t.Fatalf("verified=%v err=%v", verified, err)
		}
	})
	t.Run("cancel during copy uses post-process evidence", func(t *testing.T) {
		process := newFakeControlledProcess(false)
		controls := NewController()
		executor := &ManagedExecutor{Controls: controls, Resolve: func(context.Context, *model.OnlineDDLRun) (*ExecutionPlan, error) {
			return &ExecutionPlan{Adapter: fakeManagedAdapter{process}, Verify: func(_ context.Context, _ ToolResult, _ error, cancelling bool) (OutcomeEvidence, error) {
				return OutcomeEvidence{CancelRequested: cancelling, VerificationComplete: true, OriginalUnchanged: true}, nil
			}}, nil
		}}
		done := make(chan error, 1)
		go func() { done <- executor.Execute(context.Background(), &model.OnlineDDLRun{ID: 2, Mode: ModeGhost}) }()
		waitControl := time.Now().Add(time.Second)
		for {
			if err := controls.Pause(2); err == nil {
				_ = controls.Resume(2)
				break
			}
			if time.Now().After(waitControl) {
				t.Fatal("process was not registered")
			}
			time.Sleep(time.Millisecond)
		}
		if err := controls.Cancel(context.Background(), 2, 5*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !IsTerminalOutcome(err, StatusCancelled) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("verification failure is outcome unknown", func(t *testing.T) {
		process := newFakeControlledProcess(true)
		process.result = ToolResult{Stderr: "DBD::mysql::db do failed: Error 1419: SUPER privilege is required"}
		process.waitErr = ErrToolProcessFailed
		executor := &ManagedExecutor{Resolve: func(context.Context, *model.OnlineDDLRun) (*ExecutionPlan, error) {
			return &ExecutionPlan{Adapter: fakeManagedAdapter{process}, Verify: func(context.Context, ToolResult, error, bool) (OutcomeEvidence, error) {
				return OutcomeEvidence{}, errors.New("database unavailable")
			}}, nil
		}}
		err := executor.Execute(context.Background(), &model.OnlineDDLRun{ID: 3, Mode: ModeGhost})
		var terminal *TerminalError
		if !IsTerminalOutcome(err, StatusOutcomeUnknown) || !errors.As(err, &terminal) || !strings.Contains(terminal.Detail, "Error 1419") {
			t.Fatalf("err=%v", err)
		}
	})
}
