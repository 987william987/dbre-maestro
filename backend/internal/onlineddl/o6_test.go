package onlineddl

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
)

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

func TestApplyRuntimeTuningBuildsTypedGhostCommands(t *testing.T) {
	tests := []struct {
		name, command, rollback string
		patch                   RuntimeTuning
	}{
		{"max load", "max-load=Threads_running=11", "max-load=Threads_running=10", RuntimeTuning{MaxLoadThreadsRunning: intPtr(11)}},
		{"critical load", "critical-load=Threads_running=21", "critical-load=Threads_running=20", RuntimeTuning{CriticalLoadThreadsRunning: intPtr(21)}},
		{"chunk size", "chunk-size=2000", "chunk-size=1000", RuntimeTuning{ChunkSize: intPtr(2000)}},
		{"dml batch", "dml-batch-size=60", "dml-batch-size=50", RuntimeTuning{DMLBatchSize: intPtr(60)}},
		{"nice ratio", "nice-ratio=0.5", "nice-ratio=0.2", RuntimeTuning{NiceRatio: floatPtr(.5)}},
		{"max lag", "max-lag-millis=2000", "max-lag-millis=1500", RuntimeTuning{MaxLagMillis: intPtr(2000)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, command, rollback, err := ApplyRuntimeTuning(ModeGhost, DefaultParameters(ModeGhost), tt.patch)
			if err != nil || command != tt.command || rollback != tt.rollback {
				t.Fatalf("command=%q rollback=%q err=%v", command, rollback, err)
			}
		})
	}
}

func TestApplyRuntimeTuningRejectsUnsafePatches(t *testing.T) {
	current := DefaultParameters(ModeGhost)
	invalid := []RuntimeTuning{
		{},
		{ChunkSize: intPtr(100), DMLBatchSize: intPtr(10)},
		{ChunkSize: intPtr(99)},
		{ChunkSize: intPtr(100001)},
		{MaxLoadThreadsRunning: intPtr(20)},
		{CriticalLoadThreadsRunning: intPtr(10)},
		{NiceRatio: floatPtr(-.1)},
		{NiceRatio: floatPtr(math.NaN())},
		{NiceRatio: floatPtr(math.Inf(1))},
		{MaxLagMillis: intPtr(60001)},
	}
	for _, patch := range invalid {
		if _, _, _, err := ApplyRuntimeTuning(ModeGhost, current, patch); !errors.Is(err, ErrInvalidParameters) {
			t.Fatalf("patch=%+v err=%v", patch, err)
		}
	}
	if _, _, _, err := ApplyRuntimeTuning(ModePTOSC, DefaultParameters(ModePTOSC), RuntimeTuning{ChunkSize: intPtr(1000)}); !errors.Is(err, ErrControlNotSupported) {
		t.Fatalf("pt-osc err=%v", err)
	}
	for _, patch := range []RuntimeTuning{{ChunkSize: intPtr(100)}, {ChunkSize: intPtr(100000)}, {NiceRatio: floatPtr(0)}, {NiceRatio: floatPtr(100)}, {MaxLagMillis: intPtr(100)}, {MaxLagMillis: intPtr(60000)}} {
		if _, _, _, err := ApplyRuntimeTuning(ModeGhost, current, patch); err != nil {
			t.Fatalf("boundary patch=%+v err=%v", patch, err)
		}
	}
}

type tuningStoreStub struct {
	run    *model.OnlineDDLRun
	ok     bool
	err    error
	writes int
}

func (s *tuningStoreStub) GetByID(context.Context, uint64) (*model.OnlineDDLRun, error) {
	return s.run, nil
}
func (s *tuningStoreStub) UpdateEffectiveParameters(_ context.Context, _ uint64, _ uint64, _ *uint64, _, _ json.RawMessage) (bool, error) {
	s.writes++
	return s.ok, s.err
}

type tuningProcess struct {
	mu       sync.Mutex
	commands []string
	errors   []error
}

func (p *tuningProcess) PID() int                  { return 1 }
func (p *tuningProcess) Output() ToolResult        { return ToolResult{} }
func (p *tuningProcess) Pause() error              { return nil }
func (p *tuningProcess) Resume() error             { return nil }
func (p *tuningProcess) CancelGracefully() error   { return nil }
func (p *tuningProcess) Wait() (ToolResult, error) { return ToolResult{}, nil }
func (p *tuningProcess) Terminate() error          { return nil }
func (p *tuningProcess) Tune(command string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.commands = append(p.commands, command)
	if len(p.errors) == 0 {
		return nil
	}
	err := p.errors[0]
	p.errors = p.errors[1:]
	return err
}

func newTuningService(t *testing.T, status, mode string, version uint64, process *tuningProcess) (*TuningService, *tuningStoreStub) {
	t.Helper()
	params, err := json.Marshal(DefaultParameters(mode))
	if err != nil {
		t.Fatal(err)
	}
	store := &tuningStoreStub{run: &model.OnlineDDLRun{ID: 7, Mode: mode, Status: status, Version: version, EffectiveParameters: params}, ok: true}
	controls := NewController()
	if !controls.Register(7, process) {
		t.Fatal("register process")
	}
	return &TuningService{Store: store, Controls: controls}, store
}

func TestTuningServiceStatusOCCPersistenceAndAudit(t *testing.T) {
	for _, status := range []string{StatusRunning, StatusPaused} {
		process := &tuningProcess{}
		service, store := newTuningService(t, status, ModeGhost, 3, process)
		var auditBefore, auditAfter Parameters
		var auditRunVersion uint64
		service.Audit = func(_ context.Context, run *model.OnlineDDLRun, _ uint64, before, after Parameters, result string) error {
			auditRunVersion, auditBefore, auditAfter = run.Version, before, after
			return errors.New("best effort")
		}
		run, err := service.Tune(context.Background(), 7, 9, 3, RuntimeTuning{ChunkSize: intPtr(2000)})
		if err != nil || run.Version != 4 || store.writes != 1 || !reflect.DeepEqual(process.commands, []string{"chunk-size=2000"}) {
			t.Fatalf("status=%s run=%+v writes=%d commands=%v err=%v", status, run, store.writes, process.commands, err)
		}
		if auditRunVersion != 3 || auditBefore.Ghost.ChunkSize != 1000 || auditAfter.Ghost.ChunkSize != 2000 {
			t.Fatalf("audit leaked mutated state: version=%d before=%+v after=%+v", auditRunVersion, auditBefore, auditAfter)
		}
	}
}

func TestTuningServiceRejectsBeforeSocketAndCompensatesPersistenceFailure(t *testing.T) {
	for _, tc := range []struct {
		name, status, mode string
		expected           uint64
		want               error
	}{
		{"queued", StatusQueued, ModeGhost, 3, ErrInvalidTransition},
		{"cancel requested", StatusCancelRequested, ModeGhost, 3, ErrInvalidTransition},
		{"terminal", StatusCompleted, ModeGhost, 3, ErrInvalidTransition},
		{"stale", StatusRunning, ModeGhost, 2, ErrStaleVersion},
		{"ptosc", StatusRunning, ModePTOSC, 3, ErrControlNotSupported},
	} {
		process := &tuningProcess{}
		service, store := newTuningService(t, tc.status, tc.mode, 3, process)
		_, err := service.Tune(context.Background(), 7, 9, tc.expected, RuntimeTuning{ChunkSize: intPtr(2000)})
		if !errors.Is(err, tc.want) || len(process.commands) != 0 || store.writes != 0 {
			t.Fatalf("%s err=%v commands=%v writes=%d", tc.name, err, process.commands, store.writes)
		}
	}

	process := &tuningProcess{}
	service, store := newTuningService(t, StatusRunning, ModeGhost, 3, process)
	store.ok = false
	_, err := service.Tune(context.Background(), 7, 9, 3, RuntimeTuning{ChunkSize: intPtr(2000)})
	if !errors.Is(err, ErrStaleVersion) || !reflect.DeepEqual(process.commands, []string{"chunk-size=2000", "chunk-size=1000"}) {
		t.Fatalf("err=%v commands=%v", err, process.commands)
	}

	process = &tuningProcess{errors: []error{nil, errors.New("rollback failed")}}
	service, store = newTuningService(t, StatusRunning, ModeGhost, 3, process)
	store.err = errors.New("db failed")
	_, err = service.Tune(context.Background(), 7, 9, 3, RuntimeTuning{ChunkSize: intPtr(2000)})
	if !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("err=%v", err)
	}
}

func TestTuningServiceDoesNotPersistRejectedSocketCommand(t *testing.T) {
	process := &tuningProcess{errors: []error{errors.New("socket timeout")}}
	service, store := newTuningService(t, StatusRunning, ModeGhost, 3, process)
	_, err := service.Tune(context.Background(), 7, 9, 3, RuntimeTuning{ChunkSize: intPtr(2000)})
	if err == nil || store.writes != 0 {
		t.Fatalf("err=%v writes=%d", err, store.writes)
	}
}

func TestControllerRejectsTuningWhileCancelling(t *testing.T) {
	controller := NewController()
	process := &tuningProcess{}
	if !controller.Register(7, process) {
		t.Fatal("register process")
	}
	controller.active[7].cancelling = true
	if err := controller.WithTuning(7, func(ToolProcess) error { t.Fatal("tuning callback called"); return nil }); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err=%v", err)
	}
}

func TestControllerSerializesConcurrentTuning(t *testing.T) {
	controller := NewController()
	if !controller.Register(7, &tuningProcess{}) {
		t.Fatal("register process")
	}
	var active, maximum atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := controller.WithTuning(7, func(ToolProcess) error {
				current := active.Add(1)
				for {
					previous := maximum.Load()
					if current <= previous || maximum.CompareAndSwap(previous, current) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				active.Add(-1)
				return nil
			}); err != nil {
				t.Errorf("tune: %v", err)
			}
		}()
	}
	wg.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent tuning = %d", maximum.Load())
	}
}
