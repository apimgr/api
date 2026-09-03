package tor

import (
	"errors"

	"github.com/apimgr/api/src/metrics"
)

// recordStarted publishes the PART 20 Tor gauges for a successfully started
// hidden service: the binary was found (enabled), the owned child process is
// up (running), and bootstrap plus ADD_ONION both completed, which means a
// circuit to the network is established.
func recordStarted() {
	m := metrics.Get()
	m.SetTorEnabled(true)
	m.SetTorRunning(true)
	m.SetTorCircuitEstablished(true)
}

// recordStartFailed publishes the PART 20 Tor gauges for a failed start. A
// missing binary means Tor support is not enabled on this host at all; any
// other failure means Tor is enabled but not currently running.
func recordStartFailed(err error) {
	m := metrics.Get()
	m.SetTorEnabled(!errors.Is(err, ErrBinaryNotFound))
	m.SetTorRunning(false)
	m.SetTorCircuitEstablished(false)
}

// recordStopped publishes the PART 20 Tor gauges for a stopped process. The
// enabled gauge is left untouched: stopping the process does not remove the
// Tor binary from the host.
func recordStopped() {
	m := metrics.Get()
	m.SetTorRunning(false)
	m.SetTorCircuitEstablished(false)
}

// recordHealth publishes the PART 20 circuit gauge from a health probe
// result. A responsive control connection means the hidden service still has
// a working circuit; a probe error means it does not.
func recordHealth(err error) {
	metrics.Get().SetTorCircuitEstablished(err == nil)
}
