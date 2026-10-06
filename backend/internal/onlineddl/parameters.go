package onlineddl

import (
	"fmt"
	"math"
)

const ParameterSchemaV1 = "v1"

type Parameters struct {
	SchemaVersion string           `json:"schema_version"`
	Ghost         *GhostParameters `json:"ghost,omitempty"`
	PTOSC         *PTOSCParameters `json:"ptosc,omitempty"`
}
type GhostParameters struct {
	MaxLoadThreadsRunning      int     `json:"max_load_threads_running"`
	CriticalLoadThreadsRunning int     `json:"critical_load_threads_running"`
	ChunkSize                  int     `json:"chunk_size"`
	DMLBatchSize               int     `json:"dml_batch_size"`
	NiceRatio                  float64 `json:"nice_ratio"`
	MaxLagMillis               int     `json:"max_lag_millis"`
	CutOverLockTimeoutSeconds  int     `json:"cut_over_lock_timeout_seconds"`
}
type PTOSCParameters struct {
	ChunkSize                  *int     `json:"chunk_size,omitempty"`
	ChunkTime                  *float64 `json:"chunk_time,omitempty"`
	MaxLoadThreadsRunning      int      `json:"max_load_threads_running"`
	CriticalLoadThreadsRunning int      `json:"critical_load_threads_running"`
	MaxLagSeconds              int      `json:"max_lag_seconds"`
	CheckIntervalSeconds       int      `json:"check_interval_seconds"`
	AlterForeignKeysMethod     string   `json:"alter_foreign_keys_method"`
}

func DefaultParameters(mode string) Parameters {
	switch mode {
	case ModeGhost:
		return Parameters{SchemaVersion: ParameterSchemaV1, Ghost: &GhostParameters{MaxLoadThreadsRunning: 10, CriticalLoadThreadsRunning: 20, ChunkSize: 1000, DMLBatchSize: 50, NiceRatio: .2, MaxLagMillis: 1500, CutOverLockTimeoutSeconds: 3}}
	case ModePTOSC:
		chunk := 1000
		return Parameters{SchemaVersion: ParameterSchemaV1, PTOSC: &PTOSCParameters{ChunkSize: &chunk, MaxLoadThreadsRunning: 25, CriticalLoadThreadsRunning: 50, MaxLagSeconds: 1, CheckIntervalSeconds: 1, AlterForeignKeysMethod: "none"}}
	default:
		return Parameters{}
	}
}

func (p Parameters) Validate(mode string) error {
	if p.SchemaVersion != ParameterSchemaV1 {
		return fmt.Errorf("%w: unsupported schema version", ErrInvalidParameters)
	}
	switch mode {
	case ModeGhost:
		if p.Ghost == nil || p.PTOSC != nil {
			return ErrInvalidParameters
		}
		v := p.Ghost
		if !between(v.MaxLoadThreadsRunning, 1, 10000) || !between(v.CriticalLoadThreadsRunning, 2, 100000) || v.CriticalLoadThreadsRunning <= v.MaxLoadThreadsRunning || !between(v.ChunkSize, 100, 100000) || !between(v.DMLBatchSize, 1, 100) || math.IsNaN(v.NiceRatio) || math.IsInf(v.NiceRatio, 0) || v.NiceRatio < 0 || v.NiceRatio > 100 || !between(v.MaxLagMillis, 100, 60000) || !between(v.CutOverLockTimeoutSeconds, 1, 60) {
			return ErrInvalidParameters
		}
	case ModePTOSC:
		if p.PTOSC == nil || p.Ghost != nil {
			return ErrInvalidParameters
		}
		v := p.PTOSC
		if (v.ChunkSize == nil) == (v.ChunkTime == nil) {
			return ErrInvalidParameters
		}
		if v.ChunkSize != nil && !between(*v.ChunkSize, 100, 100000) {
			return ErrInvalidParameters
		}
		if v.ChunkTime != nil && (*v.ChunkTime < .1 || *v.ChunkTime > 10) {
			return ErrInvalidParameters
		}
		if !between(v.MaxLoadThreadsRunning, 1, 10000) || !between(v.CriticalLoadThreadsRunning, 2, 100000) || v.CriticalLoadThreadsRunning <= v.MaxLoadThreadsRunning || !between(v.MaxLagSeconds, 1, 60) || !between(v.CheckIntervalSeconds, 1, 60) {
			return ErrInvalidParameters
		}
		if v.AlterForeignKeysMethod != "none" && v.AlterForeignKeysMethod != "auto" && v.AlterForeignKeysMethod != "rebuild_constraints" && v.AlterForeignKeysMethod != "drop_swap" {
			return ErrInvalidParameters
		}
	default:
		return ErrInvalidParameters
	}
	return nil
}
func between(v, min, max int) bool { return v >= min && v <= max }
