package onlineddl

import (
	"context"
	"database/sql"
	"strings"
)

type TerminalError struct {
	Status, Code, Confidence string
	Detail                   string
	Artifacts                ArtifactSummary
}

func (e *TerminalError) Error() string          { return e.Code }
func TerminalOutcome(status, code string) error { return &TerminalError{Status: status, Code: code} }
func VerifiedTerminalOutcome(status, code string, artifacts ArtifactSummary) error {
	return VerifiedTerminalOutcomeWithDetail(status, code, "", artifacts)
}
func VerifiedTerminalOutcomeWithDetail(status, code, detail string, artifacts ArtifactSummary) error {
	confidence := "verified"
	if status == StatusOutcomeUnknown {
		confidence = StatusOutcomeUnknown
	}
	return &TerminalError{Status: status, Code: code, Confidence: confidence, Detail: detail, Artifacts: artifacts}
}

type Artifact struct {
	Kind, Name string
	Exists     bool
}
type ArtifactSummary struct {
	Items []Artifact `json:"items"`
}

type artifactProbe interface {
	TableExists(context.Context, string, string) (bool, error)
	TriggerExists(context.Context, string, string) (bool, error)
}

type SQLArtifactProbe struct{ DB *sql.DB }

func (p SQLArtifactProbe) TableExists(ctx context.Context, database, name string) (bool, error) {
	var exists bool
	err := p.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?)`, database, name).Scan(&exists)
	return exists, err
}
func (p SQLArtifactProbe) TriggerExists(ctx context.Context, database, name string) (bool, error) {
	var exists bool
	err := p.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ? AND TRIGGER_NAME = ?)`, database, name).Scan(&exists)
	return exists, err
}

func DiscoverArtifacts(ctx context.Context, probe artifactProbe, mode, database, table string) (ArtifactSummary, error) {
	var candidates []Artifact
	if mode == ModeGhost {
		candidates = []Artifact{{Kind: "ghost_table", Name: toolObjectName(table, "_gho")}, {Kind: "old_table", Name: toolObjectName(table, "_del")}, {Kind: "metadata_table", Name: toolObjectName(table, "_ghc")}}
	} else if mode == ModePTOSC {
		candidates = []Artifact{{Kind: "new_table", Name: toolObjectName(table, "_new")}, {Kind: "old_table", Name: toolObjectName(table, "_old")}, {Kind: "insert_trigger", Name: boundedObjectName("pt_osc_"+database+"_"+table, "_ins")}, {Kind: "update_trigger", Name: boundedObjectName("pt_osc_"+database+"_"+table, "_upd")}, {Kind: "delete_trigger", Name: boundedObjectName("pt_osc_"+database+"_"+table, "_del")}}
	} else {
		return ArtifactSummary{}, ErrInvalidParameters
	}
	for i := range candidates {
		var err error
		if strings.HasSuffix(candidates[i].Kind, "trigger") {
			candidates[i].Exists, err = probe.TriggerExists(ctx, database, candidates[i].Name)
		} else {
			candidates[i].Exists, err = probe.TableExists(ctx, database, candidates[i].Name)
		}
		if err != nil {
			return ArtifactSummary{}, err
		}
	}
	return ArtifactSummary{Items: candidates}, nil
}

func toolObjectName(base, suffix string) string {
	return boundedObjectName("_"+strings.TrimPrefix(base, "_"), suffix)
}
func boundedObjectName(base, suffix string) string {
	const max = 64
	baseRunes, suffixRunes := []rune(base), []rune(suffix)
	if len(baseRunes)+len(suffixRunes) > max {
		baseRunes = baseRunes[:max-len(suffixRunes)]
	}
	return string(baseRunes) + suffix
}
func (s ArtifactSummary) HasArtifacts() bool {
	for _, item := range s.Items {
		if item.Exists {
			return true
		}
	}
	return false
}

type OutcomeEvidence struct {
	CancelRequested, ProcessSucceeded, VerificationComplete, TargetDDLApplied, OriginalUnchanged bool
	Artifacts                                                                                    ArtifactSummary
}

func ClassifyOutcome(e OutcomeEvidence) (string, string) {
	if e.CancelRequested {
		if e.VerificationComplete && e.TargetDDLApplied {
			return StatusCompletedAfterCancel, ""
		}
		if !e.VerificationComplete || !e.OriginalUnchanged {
			return StatusOutcomeUnknown, ErrOutcomeUnknown.Error()
		}
		if e.Artifacts.HasArtifacts() {
			return StatusCancelledArtifacts, ErrCancelledArtifacts.Error()
		}
		return StatusCancelled, ""
	}
	if e.VerificationComplete && e.TargetDDLApplied {
		return StatusCompleted, ""
	}
	if e.ProcessSucceeded || !e.VerificationComplete || !e.OriginalUnchanged || e.Artifacts.HasArtifacts() {
		return StatusOutcomeUnknown, ErrOutcomeUnknown.Error()
	}
	return StatusFailed, ErrToolProcessFailed.Error()
}
