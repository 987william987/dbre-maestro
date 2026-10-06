package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/onlineddl"
)

type settingsToolAdapter struct {
	mode, version string
	err           error
}

func (a settingsToolAdapter) Mode() string                            { return a.mode }
func (a settingsToolAdapter) Version(context.Context) (string, error) { return a.version, a.err }
func (a settingsToolAdapter) Start(context.Context, onlineddl.ToolRequest) (onlineddl.ToolProcess, error) {
	return nil, errors.New("not used")
}

func TestOnlineDDLReadinessExposesCanonicalVersionWithoutImplementationDetails(t *testing.T) {
	readiness := onlineDDLReadiness(context.Background(), map[string]onlineddl.Adapter{
		onlineddl.ModeGhost: settingsToolAdapter{mode: onlineddl.ModeGhost, version: "gh-ost version 1.1.6"},
		onlineddl.ModePTOSC: settingsToolAdapter{mode: onlineddl.ModePTOSC, err: onlineddl.ErrToolUnavailable},
	})
	if !readiness[onlineddl.ModeGhost].Available || readiness[onlineddl.ModeGhost].Version != "1.1.6" {
		t.Fatalf("unexpected gh-ost readiness: %#v", readiness[onlineddl.ModeGhost])
	}
	if readiness[onlineddl.ModePTOSC].Available || readiness[onlineddl.ModePTOSC].Version != "" {
		t.Fatalf("unexpected pt-osc readiness: %#v", readiness[onlineddl.ModePTOSC])
	}
}

func TestSettingsResponseKeepsOnlineDDLReadinessAfterMutation(t *testing.T) {
	handler := &SettingsHandler{
		appEnv: "development",
		onlineDDLAdapters: map[string]onlineddl.Adapter{
			onlineddl.ModeGhost: settingsToolAdapter{mode: onlineddl.ModeGhost, version: "gh-ost version 1.1.6"},
			onlineddl.ModePTOSC: settingsToolAdapter{mode: onlineddl.ModePTOSC, version: "pt-online-schema-change 3.7.1"},
		},
	}
	recorder := httptest.NewRecorder()
	handler.writeSettingsResponse(recorder, context.Background(), &model.PlatformSettings{})

	var response settingsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.AppEnv != "development" || !response.OnlineDDLTools[onlineddl.ModeGhost].Available || !response.OnlineDDLTools[onlineddl.ModePTOSC].Available {
		t.Fatalf("mutation response lost runtime readiness: %#v", response)
	}
	if response.OnlineDDLTools[onlineddl.ModeGhost].Version != "1.1.6" || response.OnlineDDLTools[onlineddl.ModePTOSC].Version != "3.7.1" {
		t.Fatalf("unexpected versions: %#v", response.OnlineDDLTools)
	}
}

func TestResolveSecretStateAllowsSavingWhenCurrentSecretExists(t *testing.T) {
	configured, required := resolveSecretState("cli_existing", "", false, true)

	if required {
		t.Fatal("secret should not be required when an existing configured secret is present")
	}
	if !configured {
		t.Fatal("configured should be true when the current settings already have a secret")
	}
}

func TestResolveSecretStateRequiresSecretForFirstTimeConfiguration(t *testing.T) {
	configured, required := resolveSecretState("cli_new", "", false, false)

	if !required {
		t.Fatal("secret should be required when configuring Lark for the first time")
	}
	if configured {
		t.Fatal("configured should be false without a request or current secret")
	}
}

func TestResolveSecretStateMarksConfiguredWhenSecretProvided(t *testing.T) {
	configured, required := resolveSecretState("cli_new", "secret", false, false)

	if required {
		t.Fatal("secret should not be required when the request provides one")
	}
	if !configured {
		t.Fatal("configured should be true when the request provides a secret")
	}
}

func TestResolveSecretStateDoesNotRequireSecretWhenAppIDIsEmpty(t *testing.T) {
	configured, required := resolveSecretState("", "", false, true)

	if required {
		t.Fatal("secret should not be required when Lark App ID is empty")
	}
	if !configured {
		t.Fatal("configured should preserve the current configured state")
	}
}

func TestValidateWorkflowRuleShapeEnforcesProductionApproval(t *testing.T) {
	handler := &SettingsHandler{appEnv: "production"}
	rule := model.WorkflowRule{
		RuleName:        "Production DDL",
		TicketType:      model.TicketTypeDDL,
		ApprovalEnabled: false,
		ExecutionMode:   workflowExecutionModeManual,
	}

	err := handler.validateWorkflowRuleShape(context.Background(), rule)
	if err == nil || !strings.Contains(err.Error(), "approval_enabled cannot be disabled in production") {
		t.Fatalf("expected production approval enforcement error, got %v", err)
	}
}

func TestValidateWorkflowRuleShapeAllowsNonProductionAutoExecuteWithoutApproval(t *testing.T) {
	handler := &SettingsHandler{appEnv: "staging"}
	cases := []struct {
		name       string
		ticketType model.TicketType
	}{
		{name: "dml", ticketType: model.TicketTypeDML},
		{name: "redis command", ticketType: model.TicketTypeRedisCommand},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := model.WorkflowRule{
				RuleName:        "Staging " + tc.name,
				TicketType:      tc.ticketType,
				ApprovalEnabled: false,
				ExecutionMode:   workflowExecutionModeAutoApproval,
			}

			if err := handler.validateWorkflowRuleShape(context.Background(), rule); err != nil {
				t.Fatalf("expected staging no-approval auto-execute rule to be valid, got %v", err)
			}
		})
	}
}

func TestValidateTicketDBType(t *testing.T) {
	cases := []struct {
		name       string
		ticketType model.TicketType
		dbType     string
		wantErr    bool
	}{
		{name: "ddl mysql", ticketType: model.TicketTypeDDL, dbType: "mysql"},
		{name: "ddl postgres", ticketType: model.TicketTypeDDL, dbType: "postgres"},
		{name: "ddl rejects redis", ticketType: model.TicketTypeDDL, dbType: "redis", wantErr: true},
		{name: "redis command accepts redis", ticketType: model.TicketTypeRedisCommand, dbType: "redis"},
		{name: "redis command rejects mysql", ticketType: model.TicketTypeRedisCommand, dbType: "mysql", wantErr: true},
		{name: "query access accepts redis", ticketType: model.TicketTypeQueryAccess, dbType: "redis"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTicketDBType(tc.ticketType, tc.dbType)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
