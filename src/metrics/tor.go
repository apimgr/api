package metrics

// SetTorEnabled records whether Tor support is enabled (a Tor binary was
// detected on the host), as a 0/1 gauge.
func (m *Metrics) SetTorEnabled(enabled bool) {
	m.torEnabled.Set(boolToFloat(enabled))
}

// SetTorRunning records whether the owned Tor child process is currently
// running, as a 0/1 gauge.
func (m *Metrics) SetTorRunning(running bool) {
	m.torRunning.Set(boolToFloat(running))
}

// SetTorCircuitEstablished records whether the hidden service currently has
// an established circuit, as a 0/1 gauge.
func (m *Metrics) SetTorCircuitEstablished(established bool) {
	m.torCircuitEstablished.Set(boolToFloat(established))
}

// IncTorRequests records one request served via the Tor hidden service.
func (m *Metrics) IncTorRequests() {
	m.torRequestsTotal.Inc()
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
