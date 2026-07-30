// Package tor implements the dedicated Tor hidden-service integration
// described in AI.md PART 31. The server never uses system Tor or a shared
// Tor port - it starts and owns its own Tor child process, generates a
// persistent v3 (ed25519) onion address via the control protocol's
// ADD_ONION command, and optionally exposes outbound-via-Tor networking.
//
// Tor support is always optional: a missing Tor binary or any Tor failure
// must never prevent the server from starting or keep it from running.
package tor

// Config holds the operator-configurable Tor settings. It mirrors
// AI.md PART 31's TorConfig field set and is decoupled from
// src/config.Config (the YAML-tagged config struct) the same way
// src/ssl.Config is decoupled from src/config's SSL settings - the caller
// (src/main.go) maps config.TorConfig fields into this struct.
type Config struct {
	// Binary is the path to the tor executable. Empty means auto-detect via
	// config > PATH > common per-OS install locations.
	Binary string

	// UseNetwork enables outbound-via-Tor networking (a SOCKS dialer/HTTP
	// client for the server's own outbound requests). Defaults to false -
	// the hidden service itself works regardless of this setting.
	UseNetwork bool

	// MaxCircuits is the maximum number of circuits Tor may build.
	MaxCircuits int

	// CircuitTimeout is the circuit build timeout, in seconds.
	CircuitTimeout int

	// BootstrapTimeout is the maximum time, in seconds, to wait for Tor to
	// finish bootstrapping before giving up and disabling Tor for this run.
	BootstrapTimeout int

	// SafeLogging, when true, scrubs sensitive data (addresses) from Tor's
	// own logs.
	SafeLogging bool

	// MaxStreamsPerCircuit limits streams per circuit.
	MaxStreamsPerCircuit int

	// CloseCircuitOnStreamLimit closes a circuit once MaxStreamsPerCircuit
	// is reached instead of reusing it.
	CloseCircuitOnStreamLimit bool

	// BandwidthRate is Tor's sustained bandwidth rate (e.g. "1 MB").
	BandwidthRate string

	// BandwidthBurst is Tor's burst bandwidth allowance (e.g. "2 MB").
	BandwidthBurst string

	// MaxMonthlyBandwidth caps monthly bandwidth (e.g. "100 GB"), or
	// "unlimited" to disable the cap.
	MaxMonthlyBandwidth string

	// NumIntroPoints is the number of hidden-service introduction points.
	NumIntroPoints int

	// VirtualPort is the port the .onion address listens on (the port
	// clients connect to over Tor); it is mapped to the server's real
	// listening port on 127.0.0.1.
	VirtualPort int
}

// DefaultConfig returns AI.md PART 31's documented default Tor settings.
func DefaultConfig() Config {
	return Config{
		Binary:                    "",
		UseNetwork:                false,
		MaxCircuits:               32,
		CircuitTimeout:            60,
		BootstrapTimeout:          180,
		SafeLogging:               true,
		MaxStreamsPerCircuit:      100,
		CloseCircuitOnStreamLimit: true,
		BandwidthRate:             "1 MB",
		BandwidthBurst:            "2 MB",
		MaxMonthlyBandwidth:       "100 GB",
		NumIntroPoints:            3,
		VirtualPort:               80,
	}
}
