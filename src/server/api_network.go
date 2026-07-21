package server

import (
	"encoding/json"
	"net/http"

	"github.com/apimgr/api/src/service/network"
	"github.com/go-chi/chi/v5"
)

// networkService is the shared instance backing all /api/{version}/network
// routes.
var networkService = network.New()

// apiNetworkCallerHandler returns the caller's resolved IP/port and the
// caller-identifying request headers.
func apiNetworkCallerHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeOK(w, http.StatusOK, networkService.CallerInfo(r))
}

// apiNetworkUserAgentHandler parses the caller's User-Agent header (or an
// explicit ?ua= override) into browser/OS/device components.
func apiNetworkUserAgentHandler(w http.ResponseWriter, r *http.Request) {
	ua := r.URL.Query().Get("ua")
	if ua == "" {
		ua = r.Header.Get("User-Agent")
	}
	writeEnvelopeOK(w, http.StatusOK, networkService.ParseUserAgent(ua))
}

// apiNetworkMACVendorHandler looks up the vendor for a MAC address's OUI
// prefix.
func apiNetworkMACVendorHandler(w http.ResponseWriter, r *http.Request) {
	mac := chi.URLParam(r, "mac")
	vendor, err := networkService.MACVendor(mac)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MAC", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"mac": mac, "vendor": vendor})
}

// apiNetworkSubnetHandler computes network/broadcast/host details for a
// CIDR block passed as ?cidr=.
func apiNetworkSubnetHandler(w http.ResponseWriter, r *http.Request) {
	cidr := r.URL.Query().Get("cidr")
	if cidr == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_CIDR", "cidr query parameter is required", nil)
		return
	}
	info, err := networkService.SubnetCalculate(cidr)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_CIDR", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, info)
}

// apiNetworkULAHandler generates an RFC 4193 IPv6 unique-local-address
// prefix.
func apiNetworkULAHandler(w http.ResponseWriter, r *http.Request) {
	ula, err := networkService.GenerateULA()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "ULA_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"ula": ula})
}

// apiNetworkPortHandler suggests a random unprivileged port.
func apiNetworkPortHandler(w http.ResponseWriter, r *http.Request) {
	port, err := networkService.RandomPort()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "PORT_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]int{"port": port})
}

// writeEnvelopeOK writes a PART 14 success envelope: {"ok":true,"data":...}.
func writeEnvelopeOK(w http.ResponseWriter, status int, data interface{}) {
	writeJSONEnvelope(w, status, map[string]interface{}{
		"ok":   true,
		"data": data,
	})
}

// writeEnvelopeError writes a PART 14 error envelope:
// {"ok":false,"error":CODE,"message":"...","details":{}}.
func writeEnvelopeError(w http.ResponseWriter, status int, code, message string, details map[string]interface{}) {
	body := map[string]interface{}{
		"ok":      false,
		"error":   code,
		"message": message,
	}
	if details != nil {
		body["details"] = details
	}
	writeJSONEnvelope(w, status, body)
}

// writeJSONEnvelope marshals with 2-space indentation and a single
// trailing newline, per PART 14 JSON formatting rules.
func writeJSONEnvelope(w http.ResponseWriter, status int, body interface{}) {
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"ok":false,"error":"INTERNAL","message":"failed to encode response"}` + "\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
	w.Write([]byte("\n"))
}
