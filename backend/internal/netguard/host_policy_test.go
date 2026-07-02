package netguard

import (
	"context"
	"net"
	"testing"
)

type staticResolver map[string][]net.IPAddr

func (r staticResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	return r[host], nil
}

func TestPolicyAllowsMatchingHostAndCIDR(t *testing.T) {
	policy, err := NewPolicy(Config{
		Enforcement:   EnforcementEnforce,
		HostAllowlist: []string{"*.rds.amazonaws.com"},
		CIDRAllowlist: []string{"10.183.0.0/16"},
		CIDRDenylist:  []string{"169.254.0.0/16"},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	policy = policy.WithResolver(staticResolver{
		"maestro.abc.ap-northeast-1.rds.amazonaws.com": {{IP: net.ParseIP("10.183.9.183")}},
	})

	report, err := policy.Check(context.Background(), "readonly", "maestro.abc.ap-northeast-1.rds.amazonaws.com", 3306)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(report.Violations) != 0 {
		t.Fatalf("Violations = %v, want empty", report.Violations)
	}
}

func TestPolicyBlocksDeniedCIDRInEnforceMode(t *testing.T) {
	policy, err := NewPolicy(Config{
		Enforcement:   EnforcementEnforce,
		HostAllowlist: []string{"*.internal"},
		CIDRDenylist:  []string{"169.254.0.0/16"},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	policy = policy.WithResolver(staticResolver{
		"metadata.internal": {{IP: net.ParseIP("169.254.169.254")}},
	})

	report, err := policy.Check(context.Background(), "readonly", "metadata.internal", 3306)
	if err == nil {
		t.Fatal("Check() expected enforce error")
	}
	if len(report.Violations) == 0 {
		t.Fatal("expected violation report")
	}
}

func TestPolicyWarnsWithoutBlocking(t *testing.T) {
	policy, err := NewPolicy(Config{
		Enforcement:   EnforcementWarn,
		HostAllowlist: []string{"*.rds.amazonaws.com"},
		CIDRAllowlist: []string{"10.183.0.0/16"},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	policy = policy.WithResolver(staticResolver{
		"unexpected.internal": {{IP: net.ParseIP("10.222.38.39")}},
	})

	report, err := policy.Check(context.Background(), "readonly", "unexpected.internal", 3306)
	if err != nil {
		t.Fatalf("Check() error = %v, want warn-only", err)
	}
	if len(report.Violations) == 0 {
		t.Fatal("expected warning violations")
	}
}

func TestPolicyOffSkipsValidation(t *testing.T) {
	policy, err := NewPolicy(Config{Enforcement: EnforcementOff})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	report, err := policy.Check(context.Background(), "readonly", "http://169.254.169.254/latest", 80)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(report.Violations) != 0 {
		t.Fatalf("Violations = %v, want empty", report.Violations)
	}
}
