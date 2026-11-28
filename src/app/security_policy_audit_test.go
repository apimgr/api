package app

import (
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityPolicyChangesAuditAllowlistAndPermanentBlockTransitions(t *testing.T) {
	previous := &config.ServerConfig{}
	previous.Server.Security.Allowlist = []config.AllowlistEntry{{CIDR: "192.0.2.0/24", Description: "office"}}
	previous.Server.Security.BlockedIPs = []config.BlockedIPEntry{{CIDR: "198.51.100.8/32", Description: "abuse"}}
	current := &config.ServerConfig{}
	current.Server.Security.Allowlist = []config.AllowlistEntry{{CIDR: "2001:db8::/32", Description: "monitoring"}}
	current.Server.Security.BlockedIPs = []config.BlockedIPEntry{{CIDR: "203.0.113.9/32", Description: "abuse"}}

	events := securityPolicyChanges(previous, current)
	require.Len(t, events, 4)
	assert.Equal(t, "security.ip_allowlisted", events[0].name)
	assert.Equal(t, "2001:db8::/32", events[0].detail["cidr"])
	assert.Equal(t, "monitoring", events[0].detail["description"])
	assert.Equal(t, "security.ip_allowlist_removed", events[1].name)
	assert.Equal(t, "192.0.2.0/24", events[1].detail["cidr"])
	assert.Equal(t, "security.ip_blocked", events[2].name)
	assert.Equal(t, "permanent", events[2].detail["duration"])
	assert.Equal(t, "security.ip_unblocked", events[3].name)
	assert.Equal(t, "configuration_removed", events[3].detail["reason"])
}

func TestSecurityPolicyChangesIgnoreDescriptionOnlyChanges(t *testing.T) {
	previous := &config.ServerConfig{}
	previous.Server.Security.Allowlist = []config.AllowlistEntry{{CIDR: "192.0.2.4/32", Description: "old"}}
	current := &config.ServerConfig{}
	current.Server.Security.Allowlist = []config.AllowlistEntry{{CIDR: "192.0.2.4/32", Description: "new"}}
	assert.Empty(t, securityPolicyChanges(previous, current))
}
