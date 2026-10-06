package onlineddl

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/dbre-maestro/maestro/internal/model"
)

type RuntimeTuning struct {
	MaxLoadThreadsRunning      *int     `json:"max_load_threads_running,omitempty"`
	CriticalLoadThreadsRunning *int     `json:"critical_load_threads_running,omitempty"`
	ChunkSize                  *int     `json:"chunk_size,omitempty"`
	DMLBatchSize               *int     `json:"dml_batch_size,omitempty"`
	NiceRatio                  *float64 `json:"nice_ratio,omitempty"`
	MaxLagMillis               *int     `json:"max_lag_millis,omitempty"`
}

func ApplyRuntimeTuning(mode string, current Parameters, patch RuntimeTuning) (Parameters, string, string, error) {
	if mode != ModeGhost {
		return Parameters{}, "", "", ErrControlNotSupported
	}
	if err := current.Validate(ModeGhost); err != nil {
		return Parameters{}, "", "", err
	}
	count := 0
	for _, set := range []bool{patch.MaxLoadThreadsRunning != nil, patch.CriticalLoadThreadsRunning != nil, patch.ChunkSize != nil, patch.DMLBatchSize != nil, patch.NiceRatio != nil, patch.MaxLagMillis != nil} {
		if set {
			count++
		}
	}
	if count != 1 {
		return Parameters{}, "", "", ErrInvalidParameters
	}
	before := *current.Ghost
	after := before
	command, rollback := "", ""
	switch {
	case patch.MaxLoadThreadsRunning != nil:
		after.MaxLoadThreadsRunning = *patch.MaxLoadThreadsRunning
		command = "max-load=Threads_running=" + strconv.Itoa(after.MaxLoadThreadsRunning)
		rollback = "max-load=Threads_running=" + strconv.Itoa(before.MaxLoadThreadsRunning)
	case patch.CriticalLoadThreadsRunning != nil:
		after.CriticalLoadThreadsRunning = *patch.CriticalLoadThreadsRunning
		command = "critical-load=Threads_running=" + strconv.Itoa(after.CriticalLoadThreadsRunning)
		rollback = "critical-load=Threads_running=" + strconv.Itoa(before.CriticalLoadThreadsRunning)
	case patch.ChunkSize != nil:
		after.ChunkSize = *patch.ChunkSize
		command = "chunk-size=" + strconv.Itoa(after.ChunkSize)
		rollback = "chunk-size=" + strconv.Itoa(before.ChunkSize)
	case patch.DMLBatchSize != nil:
		after.DMLBatchSize = *patch.DMLBatchSize
		command = "dml-batch-size=" + strconv.Itoa(after.DMLBatchSize)
		rollback = "dml-batch-size=" + strconv.Itoa(before.DMLBatchSize)
	case patch.NiceRatio != nil:
		after.NiceRatio = *patch.NiceRatio
		command = "nice-ratio=" + strconv.FormatFloat(after.NiceRatio, 'f', -1, 64)
		rollback = "nice-ratio=" + strconv.FormatFloat(before.NiceRatio, 'f', -1, 64)
	case patch.MaxLagMillis != nil:
		after.MaxLagMillis = *patch.MaxLagMillis
		command = "max-lag-millis=" + strconv.Itoa(after.MaxLagMillis)
		rollback = "max-lag-millis=" + strconv.Itoa(before.MaxLagMillis)
	}
	next := Parameters{SchemaVersion: current.SchemaVersion, Ghost: &after}
	if err := next.Validate(ModeGhost); err != nil {
		return Parameters{}, "", "", err
	}
	return next, command, rollback, nil
}

type tuningStore interface {
	GetByID(context.Context, uint64) (*model.OnlineDDLRun, error)
	UpdateEffectiveParameters(context.Context, uint64, uint64, *uint64, json.RawMessage, json.RawMessage) (bool, error)
}
type TuningAudit func(context.Context, *model.OnlineDDLRun, uint64, Parameters, Parameters, string) error
type TuningService struct {
	Store    tuningStore
	Controls *Controller
	Audit    TuningAudit
}

func (s *TuningService) Tune(ctx context.Context, runID, actorID, expectedVersion uint64, patch RuntimeTuning) (*model.OnlineDDLRun, error) {
	if s.Store == nil || s.Controls == nil {
		return nil, ErrInvalidTransition
	}
	var updated *model.OnlineDDLRun
	err := s.Controls.WithTuning(runID, func(process ToolProcess) error {
		run, err := s.Store.GetByID(ctx, runID)
		if err != nil {
			return err
		}
		if run == nil {
			return ErrInvalidTransition
		}
		if run.Mode != ModeGhost {
			return ErrControlNotSupported
		}
		if run.Status != StatusRunning && run.Status != StatusPaused {
			return ErrInvalidTransition
		}
		if run.Version != expectedVersion {
			return ErrStaleVersion
		}
		var current Parameters
		if err := json.Unmarshal(run.EffectiveParameters, &current); err != nil {
			return ErrInvalidParameters
		}
		next, command, rollback, err := ApplyRuntimeTuning(run.Mode, current, patch)
		if err != nil {
			return err
		}
		beforeJSON, err := json.Marshal(current)
		if err != nil {
			return err
		}
		if err := process.Tune(command); err != nil {
			return err
		}
		afterJSON, err := json.Marshal(next)
		if err != nil {
			return err
		}
		ok, err := s.Store.UpdateEffectiveParameters(ctx, runID, expectedVersion, &actorID, beforeJSON, afterJSON)
		if err != nil || !ok {
			if rollbackErr := process.Tune(rollback); rollbackErr != nil {
				return ErrOutcomeUnknown
			}
			if err != nil {
				return err
			}
			return ErrStaleVersion
		}
		original := *run
		updated = run
		updated.Version++
		updated.EffectiveParameters = afterJSON
		if s.Audit != nil {
			_ = s.Audit(context.WithoutCancel(ctx), &original, actorID, current, next, "succeeded")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
