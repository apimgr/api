package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNormalizedBlockedIPsSkipsInvalidEntriesWithIndexedWarnings(t *testing.T) {
	security := ServerSecurityConfig{BlockedIPs: []BlockedIPEntry{
		{CIDR: "198.51.100.7", Description: "single address"},
		{CIDR: "not-an-address"},
		{CIDR: "2001:db8::/32", Description: "network"},
	}}

	entries, warnings := security.NormalizedBlockedIPs()

	require.Len(t, entries, 2)
	assert.Equal(t, "198.51.100.7/32", entries[0].CIDR)
	assert.Equal(t, "single address", entries[0].Description)
	assert.Equal(t, "2001:db8::/32", entries[1].CIDR)
	assert.Equal(t, []string{"server.security.blocked_ips[1].cidr is invalid; skipping entry"}, warnings)
}

func TestNormalizedAllowlistSkipsInvalidAndOverlyBroadEntries(t *testing.T) {
	security := ServerSecurityConfig{Allowlist: []AllowlistEntry{
		{CIDR: "192.0.2.9", Description: "single address"},
		{CIDR: "10.0.0.0/8", Description: "internal"},
		{CIDR: "0.0.0.0/7"},
		{CIDR: "::/15"},
		{CIDR: "invalid"},
	}}

	entries, warnings := security.NormalizedAllowlist()

	require.Len(t, entries, 2)
	assert.Equal(t, "192.0.2.9/32", entries[0].CIDR)
	assert.Equal(t, "10.0.0.0/8", entries[1].CIDR)
	assert.Equal(t, []string{
		"server.security.allowlist[2].cidr is overly broad; skipping entry",
		"server.security.allowlist[3].cidr is overly broad; skipping entry",
		"server.security.allowlist[4].cidr is invalid; skipping entry",
	}, warnings)
}

func TestComplianceStandardsAreIndependentAndDisabledByDefault(t *testing.T) {
	cfg := defaultConfig()
	assert.False(t, cfg.Server.Compliance.GDPR)
	assert.False(t, cfg.Server.Compliance.CCPA)
	assert.False(t, cfg.Server.Compliance.HIPAA)
	assert.False(t, cfg.Server.Compliance.SOC2)
	assert.False(t, cfg.Server.Compliance.PCIDSS)
	assert.False(t, cfg.Server.Compliance.ISO27001)
	assert.False(t, cfg.Server.Compliance.FedRAMP)
	assert.False(t, cfg.Server.Compliance.LGPD)
	assert.False(t, cfg.Server.Compliance.PIPEDA)
	assert.False(t, cfg.Server.Compliance.APPI)
	assert.False(t, cfg.Server.Compliance.PDPA)

	var loaded ServerConfig
	require.NoError(t, yaml.Unmarshal([]byte("server:\n  compliance:\n    gdpr: true\n    hipaa: true\n"), &loaded))
	assert.True(t, loaded.Server.Compliance.GDPR)
	assert.True(t, loaded.Server.Compliance.HIPAA)
	assert.False(t, loaded.Server.Compliance.CCPA)
}

func TestEffectiveAuditRetentionUsesStrictestEnabledMinimum(t *testing.T) {
	cfg := ComplianceConfig{GDPR: true, HIPAA: true, ISO27001: true}
	assert.Equal(t, 2190, cfg.MinimumAuditRetentionDays())
	assert.Equal(t, "2190d", cfg.EffectiveAuditRetention("none"))
	assert.Equal(t, "2190d", cfg.EffectiveAuditRetention("90d"))
	assert.Equal(t, "forever", cfg.EffectiveAuditRetention("forever"))
	assert.Equal(t, "2500d", cfg.EffectiveAuditRetention("2500d"))
}
