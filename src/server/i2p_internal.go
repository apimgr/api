package server

import (
	"net/http"

	"github.com/apimgr/api/src/i2p"
)

func i2pStatusHandler(w http.ResponseWriter, r *http.Request) {
	manager := i2p.Get()
	if manager == nil {
		writeEnvelopeOK(w, http.StatusOK, responseObject(responseField("enabled", false), responseField("running", false), responseField("state", "disabled"), responseField("provider", "none"), responseField("address", "")))
		return
	}
	snapshot := manager.Snapshot()
	writeEnvelopeOK(w, http.StatusOK, responseObject(responseField("enabled", snapshot.Enabled), responseField("running", snapshot.Running), responseField("state", snapshot.I2PState), responseField("provider", snapshot.Provider), responseField("address", snapshot.Address)))
}
