package tor

import (
	"strings"
	"testing"
)

// TestGenerateTorrcOutboundDisabled verifies the default (no outbound-via-Tor)
// case never enables SocksPort, per PART 31's "NEVER use default Tor ports".
func TestGenerateTorrcOutboundDisabled(t *testing.T) {
	cfg := DefaultConfig()
	out := generateTorrc(cfg)

	if !strings.Contains(out, "SocksPort 0") {
		t.Errorf("expected SocksPort 0 when UseNetwork is false, got:\n%s", out)
	}
	if strings.Contains(out, "SocksPort auto") {
		t.Errorf("did not expect SocksPort auto when UseNetwork is false, got:\n%s", out)
	}
}

// TestGenerateTorrcOutboundEnabled verifies SocksPort auto is emitted when
// outbound-via-Tor is enabled.
func TestGenerateTorrcOutboundEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UseNetwork = true
	out := generateTorrc(cfg)

	if !strings.Contains(out, "SocksPort auto") {
		t.Errorf("expected SocksPort auto when UseNetwork is true, got:\n%s", out)
	}
	if strings.Contains(out, "SocksPort 0") {
		t.Errorf("did not expect SocksPort 0 when UseNetwork is true, got:\n%s", out)
	}
}

// TestGenerateTorrcControlPort verifies the control port is always the
// runtime-selected localhost port, never a hardcoded default.
func TestGenerateTorrcControlPort(t *testing.T) {
	out := generateTorrc(DefaultConfig())

	if !strings.Contains(out, "ControlPort 127.0.0.1:auto") {
		t.Errorf("expected ControlPort 127.0.0.1:auto, got:\n%s", out)
	}
	if strings.Contains(out, "ControlPort 9051") || strings.Contains(out, "SocksPort 9050") {
		t.Errorf("must never set a system Tor default port as a directive, got:\n%s", out)
	}
}

// TestGenerateTorrcSafeLogging verifies SafeLogging follows the config value.
func TestGenerateTorrcSafeLogging(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SafeLogging = true
	out := generateTorrc(cfg)
	if !strings.Contains(out, "SafeLogging 1") {
		t.Errorf("expected SafeLogging 1, got:\n%s", out)
	}

	cfg.SafeLogging = false
	out = generateTorrc(cfg)
	if !strings.Contains(out, "SafeLogging 0") {
		t.Errorf("expected SafeLogging 0, got:\n%s", out)
	}
}

// TestGenerateTorrcBandwidth verifies bandwidth rate/burst lines are rendered
// from config.
func TestGenerateTorrcBandwidth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BandwidthRate = "500 KB"
	cfg.BandwidthBurst = "1 MB"
	out := generateTorrc(cfg)

	if !strings.Contains(out, "BandwidthRate 500 KB") {
		t.Errorf("expected BandwidthRate 500 KB, got:\n%s", out)
	}
	if !strings.Contains(out, "BandwidthBurst 1 MB") {
		t.Errorf("expected BandwidthBurst 1 MB, got:\n%s", out)
	}
}

// TestGenerateTorrcAccounting verifies the monthly-bandwidth accounting
// block only appears when a real cap is configured, never for "unlimited"
// or empty.
func TestGenerateTorrcAccounting(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxMonthlyBandwidth = "100 GB"
	out := generateTorrc(cfg)
	if !strings.Contains(out, "AccountingMax 100 GB") {
		t.Errorf("expected AccountingMax 100 GB, got:\n%s", out)
	}
	if !strings.Contains(out, "AccountingStart month 1 00:00") {
		t.Errorf("expected AccountingStart line, got:\n%s", out)
	}

	cfg.MaxMonthlyBandwidth = "unlimited"
	out = generateTorrc(cfg)
	if strings.Contains(out, "AccountingMax") {
		t.Errorf("did not expect accounting block for unlimited, got:\n%s", out)
	}

	cfg.MaxMonthlyBandwidth = ""
	out = generateTorrc(cfg)
	if strings.Contains(out, "AccountingMax") {
		t.Errorf("did not expect accounting block for empty cap, got:\n%s", out)
	}
}

// TestGenerateTorrcNoRelayNoExit verifies the daemon is never configured as
// a relay or exit node.
func TestGenerateTorrcNoRelayNoExit(t *testing.T) {
	out := generateTorrc(DefaultConfig())

	if !strings.Contains(out, "ExitRelay 0") {
		t.Errorf("expected ExitRelay 0, got:\n%s", out)
	}
	if !strings.Contains(out, "ORPort 0") {
		t.Errorf("expected ORPort 0, got:\n%s", out)
	}
	if !strings.Contains(out, "DirPort 0") {
		t.Errorf("expected DirPort 0, got:\n%s", out)
	}
}
