package tor

import "testing"

// TestDefaultConfig verifies AI.md PART 31's documented defaults.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	cases := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"Binary", cfg.Binary, ""},
		{"UseNetwork", cfg.UseNetwork, false},
		{"MaxCircuits", cfg.MaxCircuits, 32},
		{"CircuitTimeout", cfg.CircuitTimeout, 60},
		{"BootstrapTimeout", cfg.BootstrapTimeout, 180},
		{"SafeLogging", cfg.SafeLogging, true},
		{"MaxStreamsPerCircuit", cfg.MaxStreamsPerCircuit, 100},
		{"CloseCircuitOnStreamLimit", cfg.CloseCircuitOnStreamLimit, true},
		{"BandwidthRate", cfg.BandwidthRate, "1 MB"},
		{"BandwidthBurst", cfg.BandwidthBurst, "2 MB"},
		{"MaxMonthlyBandwidth", cfg.MaxMonthlyBandwidth, "100 GB"},
		{"NumIntroPoints", cfg.NumIntroPoints, 3},
		{"VirtualPort", cfg.VirtualPort, 80},
	}

	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("DefaultConfig().%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}
