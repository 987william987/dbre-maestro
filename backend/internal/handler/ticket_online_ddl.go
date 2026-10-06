package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/onlineddl"
	"github.com/go-chi/chi/v5"
)

type TicketOnlineDDLService interface {
	DryRun(context.Context, *model.Ticket, *model.TicketExecution, uint64, string, onlineddl.Parameters) (any, error)
	Get(context.Context, uint64, uint64) (*model.OnlineDDLRun, error)
	Pause(context.Context, *model.OnlineDDLRun, uint64, uint64) (*model.OnlineDDLRun, error)
	Resume(context.Context, *model.OnlineDDLRun, uint64, uint64) (*model.OnlineDDLRun, error)
	Cancel(context.Context, *model.OnlineDDLRun, uint64, uint64) (*model.OnlineDDLRun, error)
	Tune(context.Context, *model.OnlineDDLRun, uint64, uint64, onlineddl.RuntimeTuning) (*model.OnlineDDLRun, error)
}

type ticketOnlineDDLRunLister interface {
	ListByTicket(context.Context, uint64) ([]model.OnlineDDLRun, error)
}

type ticketOnlineDDLQueue interface {
	Queue(context.Context, *model.Ticket, *model.TicketExecution, uint64, string, onlineddl.Parameters) (*model.OnlineDDLRun, error)
}

type onlineDDLPlanRequest struct {
	ExecutionID uint64               `json:"execution_id,omitempty"`
	Mode        string               `json:"mode"`
	Parameters  onlineddl.Parameters `json:"parameters"`
}

func (h *TicketHandler) OnlineDDLDryRun(w http.ResponseWriter, r *http.Request) {
	var req onlineDDLPlanRequest
	if bindJSON(r, &req) != nil || req.ExecutionID == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, onlineddl.ErrInvalidParameters.Error())
		return
	}
	ticket, execution, actor, ok := h.onlineDDLExecutionByID(w, r, req.ExecutionID, true, false, true)
	if !ok {
		return
	}
	result, err := h.onlineDDL.DryRun(r.Context(), ticket, execution, actor, req.Mode, req.Parameters)
	if err != nil {
		writeOnlineDDLError(w, err)
		return
	}
	jsonOK(w, result)
}

func (h *TicketHandler) GetOnlineDDL(w http.ResponseWriter, r *http.Request) {
	ticket, execution, _, ok := h.onlineDDLExecution(w, r, false, false, false)
	if !ok {
		return
	}
	run, err := h.onlineDDL.Get(r.Context(), ticket.ID, execution.ID)
	if err != nil {
		writeOnlineDDLError(w, err)
		return
	}
	if run == nil {
		jsonErr(w, http.StatusNotFound, "online_ddl_run_not_found")
		return
	}
	jsonOK(w, run)
}

func (h *TicketHandler) PauseOnlineDDL(w http.ResponseWriter, r *http.Request) {
	h.onlineDDLControl(w, r, "pause")
}
func (h *TicketHandler) ResumeOnlineDDL(w http.ResponseWriter, r *http.Request) {
	h.onlineDDLControl(w, r, "resume")
}
func (h *TicketHandler) CancelOnlineDDL(w http.ResponseWriter, r *http.Request) {
	h.onlineDDLControl(w, r, "cancel")
}

func (h *TicketHandler) TuneOnlineDDL(w http.ResponseWriter, r *http.Request) {
	_, execution, actor, ok := h.onlineDDLExecution(w, r, true, false, false)
	if !ok {
		return
	}
	run, ok := h.loadOnlineDDLRun(w, r, execution)
	if !ok {
		return
	}
	if run.ExecutorID != actor {
		h.forbidTicketAccess(w, r, nil, "online_ddl_tune", "not_execution_owner")
		return
	}
	var req struct {
		Version    uint64                  `json:"version"`
		Parameters onlineddl.RuntimeTuning `json:"parameters"`
	}
	if bindJSON(r, &req) != nil || req.Version == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, onlineddl.ErrInvalidParameters.Error())
		return
	}
	updated, err := h.onlineDDL.Tune(r.Context(), run, actor, req.Version, req.Parameters)
	if err != nil {
		writeOnlineDDLError(w, err)
		return
	}
	jsonOK(w, updated)
}

func (h *TicketHandler) onlineDDLControl(w http.ResponseWriter, r *http.Request, action string) {
	requireExecute := action == "resume"
	_, execution, actor, ok := h.onlineDDLExecution(w, r, requireExecute, !requireExecute, false)
	if !ok {
		return
	}
	run, ok := h.loadOnlineDDLRun(w, r, execution)
	if !ok {
		return
	}
	if action == "resume" && run.ExecutorID != actor {
		h.forbidTicketAccess(w, r, nil, "online_ddl_resume", "not_execution_owner")
		return
	}
	var req struct {
		Version uint64 `json:"version"`
	}
	if bindJSON(r, &req) != nil || req.Version == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, onlineddl.ErrInvalidParameters.Error())
		return
	}
	var updated *model.OnlineDDLRun
	var err error
	switch action {
	case "pause":
		updated, err = h.onlineDDL.Pause(r.Context(), run, actor, req.Version)
	case "resume":
		updated, err = h.onlineDDL.Resume(r.Context(), run, actor, req.Version)
	case "cancel":
		updated, err = h.onlineDDL.Cancel(r.Context(), run, actor, req.Version)
	}
	if err != nil {
		writeOnlineDDLError(w, err)
		return
	}
	jsonOK(w, updated)
}

func (h *TicketHandler) loadOnlineDDLRun(w http.ResponseWriter, r *http.Request, execution *model.TicketExecution) (*model.OnlineDDLRun, bool) {
	run, err := h.onlineDDL.Get(r.Context(), execution.TicketID, execution.ID)
	if err != nil {
		writeOnlineDDLError(w, err)
		return nil, false
	}
	if run == nil {
		jsonErr(w, http.StatusNotFound, "online_ddl_run_not_found")
		return nil, false
	}
	return run, true
}

func (h *TicketHandler) onlineDDLExecution(w http.ResponseWriter, r *http.Request, requireExecute, requireStop, requirePending bool) (*model.Ticket, *model.TicketExecution, uint64, bool) {
	executionID, err := strconv.ParseUint(chi.URLParam(r, "executionID"), 10, 64)
	if err != nil || executionID == 0 {
		jsonErr(w, http.StatusBadRequest, "invalid execution id")
		return nil, nil, 0, false
	}
	return h.onlineDDLExecutionByID(w, r, executionID, requireExecute, requireStop, requirePending)
}

func (h *TicketHandler) onlineDDLExecutionByID(w http.ResponseWriter, r *http.Request, executionID uint64, requireExecute, requireStop, requirePending bool) (*model.Ticket, *model.TicketExecution, uint64, bool) {
	if h.onlineDDL == nil {
		jsonErr(w, http.StatusServiceUnavailable, onlineddl.ErrToolUnavailable.Error())
		return nil, nil, 0, false
	}
	ticket, resolved := h.resolveTicketRef(w, r)
	if !resolved || ticket == nil {
		if resolved {
			jsonErr(w, http.StatusNotFound, "ticket not found")
		}
		return nil, nil, 0, false
	}
	if ticket.TicketType != model.TicketTypeDDL || ticket.DBConnectionID == nil {
		jsonErr(w, http.StatusUnprocessableEntity, onlineddl.ErrUnsupportedStatement.Error())
		return nil, nil, 0, false
	}
	execution, err := h.tickets.GetExecution(r.Context(), ticket.ID, executionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load statement execution failed")
		return nil, nil, 0, false
	}
	if execution == nil {
		jsonErr(w, http.StatusNotFound, "statement execution not found")
		return nil, nil, 0, false
	}
	actor := middleware.UserIDFromCtx(r.Context())
	allowed := false
	if requireExecute {
		allowed, err = h.canExecuteTicket(r.Context(), ticket, actor)
	} else if requireStop {
		allowed, err = h.canStopTicket(r.Context(), ticket, actor)
	} else {
		allowed, err = h.canViewTicket(r.Context(), ticket, actor)
	}
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket authorization failed")
		return nil, nil, 0, false
	}
	if !allowed {
		h.forbidTicketAccess(w, r, ticket, "online_ddl", "not_authorized")
		return nil, nil, 0, false
	}
	if requireExecute || requireStop {
		hasScope, scopeErr := userCanAccessConnection(r.Context(), h.users, actor, *ticket.DBConnectionID)
		if scopeErr != nil {
			jsonErr(w, http.StatusInternalServerError, "ticket db scope check failed")
			return nil, nil, 0, false
		}
		if !hasScope {
			h.forbidTicketAccess(w, r, ticket, "online_ddl", "db_connection_scope_denied")
			return nil, nil, 0, false
		}
	}
	if requirePending && execution.Status != "pending" {
		jsonErr(w, http.StatusConflict, onlineddl.ErrInvalidTransition.Error())
		return nil, nil, 0, false
	}
	return ticket, execution, actor, true
}

func writeOnlineDDLError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, onlineddl.ErrStaleVersion), errors.Is(err, onlineddl.ErrConflict), errors.Is(err, onlineddl.ErrInvalidTransition):
		status = http.StatusConflict
	case errors.Is(err, onlineddl.ErrInvalidParameters), errors.Is(err, onlineddl.ErrUnsupportedStatement), errors.Is(err, onlineddl.ErrPreflightChanged), errors.Is(err, onlineddl.ErrModeDisabled), errors.Is(err, onlineddl.ErrControlNotSupported):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, onlineddl.ErrToolUnavailable):
		status = http.StatusServiceUnavailable
	}
	jsonErr(w, status, err.Error())
}
