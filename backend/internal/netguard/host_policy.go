package netguard

import (
	"context"
	"fmt"
	"net"
	"strings"
)

const (
	EnforcementOff     = "off"
	EnforcementWarn    = "warn"
	EnforcementEnforce = "enforce"
)

type Config struct {
	Enforcement   string
	HostAllowlist []string
	CIDRAllowlist []string
	CIDRDenylist  []string
}

type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type Policy struct {
	enforcement   string
	hostAllowlist []string
	cidrAllowlist []*net.IPNet
	cidrDenylist  []*net.IPNet
	resolver      Resolver
}

type CheckReport struct {
	Endpoint    string   `json:"endpoint"`
	Host        string   `json:"host"`
	Port        uint16   `json:"port"`
	IPs         []string `json:"ips,omitempty"`
	Violations  []string `json:"violations,omitempty"`
	Enforcement string   `json:"enforcement"`
}

func NewPolicy(cfg Config) (*Policy, error) {
	enforcement := NormalizeEnforcement(cfg.Enforcement)
	if enforcement == "" {
		return nil, fmt.Errorf("DB_CONNECTION_HOST_POLICY_ENFORCEMENT must be off, warn, or enforce")
	}
	p := &Policy{
		enforcement:   enforcement,
		hostAllowlist: normalizeList(cfg.HostAllowlist),
		resolver:      net.DefaultResolver,
	}
	for _, raw := range normalizeList(cfg.CIDRAllowlist) {
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid DB_CONNECTION_CIDR_ALLOWLIST CIDR %q: %w", raw, err)
		}
		p.cidrAllowlist = append(p.cidrAllowlist, network)
	}
	for _, raw := range normalizeList(cfg.CIDRDenylist) {
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid DB_CONNECTION_CIDR_DENYLIST CIDR %q: %w", raw, err)
		}
		p.cidrDenylist = append(p.cidrDenylist, network)
	}
	return p, nil
}

func NormalizeEnforcement(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "":
		return EnforcementOff
	case "disabled", "disable", "off", "false":
		return EnforcementOff
	case EnforcementWarn:
		return EnforcementWarn
	case EnforcementEnforce:
		return EnforcementEnforce
	default:
		return ""
	}
}

func SplitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func (p *Policy) Enabled() bool {
	return p != nil && p.enforcement != EnforcementOff
}

func (p *Policy) Enforcement() string {
	if p == nil {
		return EnforcementOff
	}
	return p.enforcement
}

func (p *Policy) WithResolver(resolver Resolver) *Policy {
	if p == nil || resolver == nil {
		return p
	}
	cloned := *p
	cloned.resolver = resolver
	return &cloned
}

func (p *Policy) Check(ctx context.Context, endpoint string, host string, port uint16) (CheckReport, error) {
	report := CheckReport{
		Endpoint:    strings.TrimSpace(endpoint),
		Host:        strings.TrimSpace(host),
		Port:        port,
		Enforcement: p.Enforcement(),
	}
	if !p.Enabled() {
		return report, nil
	}
	if report.Host == "" {
		report.Violations = append(report.Violations, "host is required")
		return report, p.enforcementError(report)
	}
	if invalidHostLiteral(report.Host) {
		report.Violations = append(report.Violations, "host must not include scheme, path, query, or userinfo")
		return report, p.enforcementError(report)
	}
	if len(p.hostAllowlist) > 0 && !hostAllowed(report.Host, p.hostAllowlist) {
		report.Violations = append(report.Violations, "host does not match DB_CONNECTION_HOST_ALLOWLIST")
	}

	ips, err := p.resolve(ctx, report.Host)
	if err != nil {
		report.Violations = append(report.Violations, "host DNS resolution failed")
		return report, p.enforcementError(report)
	}
	report.IPs = stringifyIPs(ips)

	for _, ip := range ips {
		if containsIP(p.cidrDenylist, ip) {
			report.Violations = append(report.Violations, fmt.Sprintf("resolved IP %s matches DB_CONNECTION_CIDR_DENYLIST", ip.String()))
		}
	}
	if len(p.cidrAllowlist) > 0 && !anyIPAllowed(p.cidrAllowlist, ips) {
		report.Violations = append(report.Violations, "no resolved IP matches DB_CONNECTION_CIDR_ALLOWLIST")
	}
	return report, p.enforcementError(report)
}

func (p *Policy) enforcementError(report CheckReport) error {
	if len(report.Violations) == 0 || p.enforcement != EnforcementEnforce {
		return nil
	}
	endpoint := report.Endpoint
	if endpoint == "" {
		endpoint = "endpoint"
	}
	return fmt.Errorf("db connection host policy blocked %s %s:%d: %s", endpoint, report.Host, report.Port, strings.Join(report.Violations, "; "))
}

func (p *Policy) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	resolver := p.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP != nil {
			ips = append(ips, addr.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses resolved")
	}
	return ips, nil
}

func normalizeList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
	}
	return result
}

func invalidHostLiteral(host string) bool {
	return strings.Contains(host, "://") ||
		strings.ContainsAny(host, "/?#@") ||
		strings.Contains(host, " ")
}

func hostAllowed(host string, patterns []string) bool {
	normalizedHost := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	for _, pattern := range patterns {
		normalizedPattern := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(pattern), "."))
		if normalizedPattern == "" {
			continue
		}
		if strings.HasPrefix(normalizedPattern, "*.") {
			suffix := strings.TrimPrefix(normalizedPattern, "*")
			if strings.HasSuffix(normalizedHost, suffix) && normalizedHost != strings.TrimPrefix(suffix, ".") {
				return true
			}
			continue
		}
		if normalizedHost == normalizedPattern {
			return true
		}
	}
	return false
}

func containsIP(networks []*net.IPNet, ip net.IP) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func anyIPAllowed(networks []*net.IPNet, ips []net.IP) bool {
	for _, ip := range ips {
		if containsIP(networks, ip) {
			return true
		}
	}
	return false
}

func stringifyIPs(ips []net.IP) []string {
	result := make([]string, 0, len(ips))
	for _, ip := range ips {
		result = append(result, ip.String())
	}
	return result
}
