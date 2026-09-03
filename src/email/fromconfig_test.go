package email

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/apimgr/api/src/config"
)

func TestEventTogglesFromConfig(t *testing.T) {
	toggles := EventTogglesFromConfig(config.DefaultEmailEventsConfig())
	assert.True(t, toggles.BackupFailed)
	assert.True(t, toggles.SSLExpiring)
	assert.True(t, toggles.SecurityAlert)
	assert.True(t, toggles.SchedulerError)
	assert.True(t, toggles.UpdateInstalled)
	assert.False(t, toggles.Startup)
	assert.False(t, toggles.Shutdown)
	assert.False(t, toggles.BackupComplete)
	assert.False(t, toggles.SSLRenewed)
	assert.False(t, toggles.UpdateAvailable)
}

// TestClientConfigFromConfig_DefaultSender covers AI.md PART 17 "Default
// Sender": From Name falls back to the app title and From Address to
// no-reply@{fqdn}.
func TestClientConfigFromConfig_DefaultSender(t *testing.T) {
	var cfg config.EmailConfig
	cfg.SMTP.Host = "smtp.example.com"
	cfg.SMTP.Port = 587
	cfg.SMTP.TLS = "starttls"
	cfg.ReplyTo = "ops@example.com"

	got := ClientConfigFromConfig(cfg, "My App", "example.com", true)
	assert.True(t, got.Enabled)
	assert.Equal(t, "smtp.example.com", got.SMTPHost)
	assert.Equal(t, 587, got.SMTPPort)
	assert.Equal(t, "starttls", got.TLS)
	assert.Equal(t, "My App", got.FromName)
	assert.Equal(t, "no-reply@example.com", got.FromEmail)
	assert.Equal(t, "ops@example.com", got.ReplyTo)
}

func TestClientConfigFromConfig_NoFQDNFallsBackToLocalhost(t *testing.T) {
	var cfg config.EmailConfig
	got := ClientConfigFromConfig(cfg, "My App", "", false)
	assert.Equal(t, "no-reply@localhost", got.FromEmail)
	assert.False(t, got.Enabled)
}

func TestClientConfigFromConfig_ExplicitSenderWins(t *testing.T) {
	var cfg config.EmailConfig
	cfg.From.Name = "Ops Bot"
	cfg.From.Email = "bot@example.org"

	got := ClientConfigFromConfig(cfg, "My App", "example.com", true)
	assert.Equal(t, "Ops Bot", got.FromName)
	assert.Equal(t, "bot@example.org", got.FromEmail)
}

func TestParseHexIPv4(t *testing.T) {
	assert.Equal(t, "192.168.1.1", parseHexIPv4("0101A8C0"))
	assert.Equal(t, "0.0.0.0", parseHexIPv4("00000000"))
	assert.Equal(t, "", parseHexIPv4("nothex"))
}

func TestDefaultGatewayIP_ParsesRouteTable(t *testing.T) {
	original := procNetRoute
	defer func() { procNetRoute = original }()

	dir := t.TempDir()
	path := dir + "/route"
	content := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t0000A8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n" +
		"eth0\t00000000\t0101A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n"
	assert.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	procNetRoute = path
	assert.Equal(t, "192.168.1.1", defaultGatewayIP())

	procNetRoute = dir + "/does-not-exist"
	assert.Equal(t, "", defaultGatewayIP())
}

// TestDetectionHosts_PriorityOrder covers the SMTP auto-detection host
// order in AI.md PART 17: loopback, docker bridge, gateway, detected
// FQDN, global IPv4, then mail./smtp. prefixes of the FQDN.
func TestDetectionHosts_PriorityOrder(t *testing.T) {
	hosts := detectionHosts("example.com")
	assert.Equal(t, "127.0.0.1", hosts[0])
	assert.Equal(t, "172.17.0.1", hosts[1])
	assert.Contains(t, hosts, "example.com")
	assert.Contains(t, hosts, "mail.example.com")
	assert.Contains(t, hosts, "smtp.example.com")

	seen := map[string]bool{}
	for _, h := range hosts {
		assert.NotEmpty(t, h)
		assert.False(t, seen[h], "duplicate candidate %q", h)
		seen[h] = true
	}
	assert.Less(t, indexOf(hosts, "example.com"), indexOf(hosts, "mail.example.com"))
	assert.Less(t, indexOf(hosts, "mail.example.com"), indexOf(hosts, "smtp.example.com"))
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}
