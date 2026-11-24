package server

import (
	"net/http"

	"github.com/apimgr/api/src/i2p"
)

func i2pStatusHandler(w http.ResponseWriter, r *http.Request) {
	manager := i2p.Get()
	if manager == nil {
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{"enabled": false, "running": false, "state": "disabled", "provider": "none", "address": ""})
		return
	}
	snapshot := manager.Snapshot()
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"enabled":  snapshot.Enabled,
		"running":  snapshot.Running,
		"state":    snapshot.State,
		"provider": snapshot.Provider,
		"address":  snapshot.Address,
	})
}
