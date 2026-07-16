package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
)

func TestResolveLarkSecretStateAllowsSavingWhenCurrentSecretExists(t *testing.T) {
	configured, required := resolveLarkSecretState("cli_existing", "", false, true)

	if required {
		t.Fatal("secret should not be required when an existing configured secret is present")
	}
	if !configured {
		t.Fatal("configured should be true when the current settings already have a secret")
	}
}

func TestResolveLarkSecretStateRequiresSecretForFirstTimeConfiguration(t *testing.T) {
	configured, required := resolveLarkSecretState("cli_new", "", false, false)

	if !required {
		t.Fatal("secret should be required when configuring Lark for the first time")
	}
	if configured {
		t.Fatal("configured should be false without a request or current secret")
	}
}

func TestResolveLarkSecretStateMarksConfiguredWhenSecretProvided(t *testing.T) {
	configured, required := resolveLarkSecretState("cli_new", "secret", false, false)

	if required {
		t.Fatal("secret should not be required when the request provides one")
	}
	if !configured {
		t.Fatal("configured should be true when the request provides a secret")
	}
}

func TestResolveLarkSecretStateDoesNotRequireSecretWhenAppIDIsEmpty(t *testing.T) {
	configured, required := resolveLarkSecretState("", "", false, true)

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
	rule := model.WorkflowRule{
		RuleName:        "Staging DML",
		TicketType:      model.TicketTypeDML,
		ApprovalEnabled: false,
		ExecutionMode:   workflowExecutionModeAutoApproval,
	}

	if err := handler.validateWorkflowRuleShape(context.Background(), rule); err != nil {
		t.Fatalf("expected staging no-approval auto-execute rule to be valid, got %v", err)
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
