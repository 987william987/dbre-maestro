package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type MaskingRuleHandler struct {
	rules *repository.MaskingRuleRepo
	audit *repository.AuditRepo
	cache *masking.RuleCache
}

func NewMaskingRuleHandler(rules *repository.MaskingRuleRepo, audit *repository.AuditRepo, cache *masking.RuleCache) *MaskingRuleHandler {
	return &MaskingRuleHandler{rules: rules, audit: audit, cache: cache}
}

// GET /masking-rules
func (h *MaskingRuleHandler) List(w http.ResponseWriter, r *http.Request) {
	rules, err := h.rules.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list masking rules failed")
		return
	}
	if rules == nil {
		rules = []model.MaskingRule{}
	}
	jsonOK(w, map[string]any{"rules": rules})
}

// POST /masking-rules
func (h *MaskingRuleHandler) Create(w http.ResponseWriter, r *http.Request) {
	req, ok := parseMaskingRulePayload(w, r)
	if !ok {
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	rule := &model.MaskingRule{
		ColumnName: req.ColumnName,
		MatchType:  req.MatchType,
		MaskMode:   req.MaskMode,
		MaskConfig: req.MaskConfig,
		CreatedBy:  userID,
	}

	created, err := h.rules.Create(r.Context(), rule)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create masking rule failed")
		return
	}

	h.cache.Invalidate()
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "masking_rule",
		ResourceID:   &created.ID,
		Details:      map[string]string{"column": created.ColumnName, "mode": created.MaskMode},
	})

	jsonCreated(w, created)
}

// PATCH /masking-rules/{id}
func (h *MaskingRuleHandler) Patch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	existing, err := h.rules.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		jsonErr(w, http.StatusNotFound, "masking rule not found")
		return
	}

	req, ok := parseMaskingRulePayload(w, r)
	if !ok {
		return
	}
	existing.ColumnName = req.ColumnName
	existing.MatchType = req.MatchType
	existing.MaskMode = req.MaskMode
	existing.MaskConfig = req.MaskConfig

	updated, err := h.rules.Patch(r.Context(), existing)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "patch masking rule failed")
		return
	}

	h.cache.Invalidate()
	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "masking_rule",
		ResourceID:   &id,
		Details:      map[string]string{"column": updated.ColumnName, "mode": updated.MaskMode, "action": "update"},
	})

	jsonOK(w, updated)
}

// DELETE /masking-rules/{id}
func (h *MaskingRuleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	existing, err := h.rules.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		jsonErr(w, http.StatusNotFound, "masking rule not found")
		return
	}

	if err := h.rules.Delete(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete masking rule failed")
		return
	}

	h.cache.Invalidate()
	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "masking_rule",
		ResourceID:   &id,
		Details:      map[string]string{"action": "delete"},
	})

	w.WriteHeader(http.StatusNoContent)
}

func parseMaskingRulePayload(w http.ResponseWriter, r *http.Request) (*struct {
	ColumnName string `json:"column_name"`
	MatchType  string `json:"match_type"`
	MaskMode   string `json:"mask_mode"`
	MaskConfig json.RawMessage `json:"mask_config"`
}, bool) {
	var req struct {
		DBConnectionID *uint64 `json:"db_connection_id"`
		DatabaseName   string  `json:"database_name"`
		SchemaName     string  `json:"schema_name"`
		TableName      string  `json:"table_name"`
		ColumnName     string  `json:"column_name"`
		MatchType      string  `json:"match_type"`
		MaskMode       string  `json:"mask_mode"`
		MaskConfig     json.RawMessage `json:"mask_config"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	req.ColumnName = normalizeRuleIdentifier(req.ColumnName)

	if req.DBConnectionID != nil || strings.TrimSpace(req.DatabaseName) != "" || strings.TrimSpace(req.SchemaName) != "" || strings.TrimSpace(req.TableName) != "" {
		jsonErr(w, http.StatusUnprocessableEntity, "only global column_name + mask_mode is supported")
		return nil, false
	}
	if req.ColumnName == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "column_name is required")
		return nil, false
	}
	switch masking.MatchType(req.MatchType) {
	case "", masking.MatchTypeExact:
		req.MatchType = string(masking.MatchTypeExact)
	case masking.MatchTypeRegex:
		if _, err := masking.MatchColumnPattern(masking.Rule{
			Column: req.ColumnName,
			Match:  masking.MatchTypeRegex,
		}, "validation_probe"); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, "column_name regex is invalid")
			return nil, false
		}
	default:
		jsonErr(w, http.StatusUnprocessableEntity, "match_type must be exact or regex")
		return nil, false
	}
	switch masking.MaskMode(req.MaskMode) {
	case masking.MaskModeFull,
		masking.MaskModePartial,
		masking.MaskModeHash,
		masking.MaskModeEmail,
		masking.MaskModeFixed,
		masking.MaskModeNumeric,
		masking.MaskModeDateTime,
		masking.MaskModeIP:
	case "":
		req.MaskMode = string(masking.MaskModeFull)
	default:
		jsonErr(w, http.StatusUnprocessableEntity, "mask_mode is not supported")
		return nil, false
	}
	if normalized, err := normalizeMaskConfig(masking.MaskMode(req.MaskMode), req.MaskConfig); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return nil, false
	} else {
		req.MaskConfig = normalized
	}
	return &struct {
		ColumnName string `json:"column_name"`
		MatchType  string `json:"match_type"`
		MaskMode   string `json:"mask_mode"`
		MaskConfig json.RawMessage `json:"mask_config"`
	}{
		ColumnName: req.ColumnName,
		MatchType:  req.MatchType,
		MaskMode:   req.MaskMode,
		MaskConfig: req.MaskConfig,
	}, true
}

func normalizeRuleIdentifier(value string) string {
	return strings.TrimSpace(value)
}

func normalizeMaskConfig(mode masking.MaskMode, raw json.RawMessage) (json.RawMessage, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = json.RawMessage(`{}`)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	switch mode {
	case masking.MaskModeFull, masking.MaskModeHash:
		return json.RawMessage(`{}`), nil
	case masking.MaskModePartial:
		if err := requireNonNegativeInteger(payload, "keep_prefix"); err != nil {
			return nil, err
		}
		if err := requireNonNegativeInteger(payload, "keep_suffix"); err != nil {
			return nil, err
		}
		if err := requireOptionalNonNegativeInteger(payload, "fixed_mask_length"); err != nil {
			return nil, err
		}
	case masking.MaskModeEmail:
		if err := requireOptionalNonNegativeInteger(payload, "keep_local_prefix"); err != nil {
			return nil, err
		}
	case masking.MaskModeFixed:
		if strings.TrimSpace(stringValue(payload["value"])) == "" {
			return nil, &fieldValidationError{message: "mask_config.value is required for fixed mode"}
		}
	case masking.MaskModeNumeric:
		op := stringValue(payload["operation"])
		if op != "" && op != "round" && op != "zero" {
			return nil, &fieldValidationError{message: "mask_config.operation must be round or zero"}
		}
		if err := requireOptionalNonNegativeInteger(payload, "decimals"); err != nil {
			return nil, err
		}
	case masking.MaskModeDateTime:
		granularity := stringValue(payload["granularity"])
		if granularity != "" && granularity != "day" && granularity != "hour" {
			return nil, &fieldValidationError{message: "mask_config.granularity must be day or hour"}
		}
	case masking.MaskModeIP:
		if err := requireOptionalNonNegativeInteger(payload, "keep_segments"); err != nil {
			return nil, err
		}
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

type fieldValidationError struct {
	message string
}

func (e *fieldValidationError) Error() string { return e.message }

func requireNonNegativeInteger(payload map[string]any, field string) error {
	if payload[field] == nil {
		return &fieldValidationError{message: "mask_config." + field + " is required"}
	}
	return requireOptionalNonNegativeInteger(payload, field)
}

func requireOptionalNonNegativeInteger(payload map[string]any, field string) error {
	if payload[field] == nil {
		return nil
	}
	number, ok := payload[field].(float64)
	if !ok || number < 0 || number != float64(int(number)) {
		return &fieldValidationError{message: "mask_config." + field + " must be a non-negative integer"}
	}
	return nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
