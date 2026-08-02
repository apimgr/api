package server

import (
	"encoding/json"
	"net/http"
	"strconv"

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

// networkSubnetParams validates apiNetworkSubnetHandler's ?cidr= input.
type networkSubnetParams struct {
	CIDR string `validate:"required"`
}

// apiNetworkSubnetHandler computes network/broadcast/host details for a
// CIDR block passed as ?cidr=.
func apiNetworkSubnetHandler(w http.ResponseWriter, r *http.Request) {
	params := networkSubnetParams{CIDR: r.URL.Query().Get("cidr")}
	if !validateStruct(w, params) {
		return
	}
	info, err := networkService.SubnetCalculate(params.CIDR)
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

// networkPingParams validates apiNetworkPingHandler's ?host= and ?count=
// input.
type networkPingParams struct {
	Host  string `validate:"required"`
	Count int    `validate:"gte=1,lte=20"`
}

// apiNetworkPingHandler measures TCP connect round-trip latency to ?host=
// (optionally ?count=, default 4, max 20) using network.Service.Ping.
func apiNetworkPingHandler(w http.ResponseWriter, r *http.Request) {
	params := networkPingParams{Host: r.URL.Query().Get("host"), Count: 4}
	if countParam := r.URL.Query().Get("count"); countParam != "" {
		parsed, err := strconv.Atoi(countParam)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COUNT", "count must be a positive integer", nil)
			return
		}
		params.Count = parsed
	}
	if !validateStruct(w, params) {
		return
	}

	result, err := networkService.Ping(params.Host, params.Count)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "PING_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// networkSSLParams validates apiNetworkSSLHandler's ?host= input.
type networkSSLParams struct {
	Host string `validate:"required"`
}

// apiNetworkSSLHandler reports the leaf TLS certificate details for ?host=
// using network.Service.SSLInfo.
func apiNetworkSSLHandler(w http.ResponseWriter, r *http.Request) {
	params := networkSSLParams{Host: r.URL.Query().Get("host")}
	if !validateStruct(w, params) {
		return
	}

	result, err := networkService.SSLInfo(params.Host)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "SSL_LOOKUP_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// networkURLParams validates apiNetworkURLHandler's ?url= input.
type networkURLParams struct {
	URL string `validate:"required"`
}

// apiNetworkURLHandler parses ?url= into its component parts using
// network.Service.ParseURL.
func apiNetworkURLHandler(w http.ResponseWriter, r *http.Request) {
	params := networkURLParams{URL: r.URL.Query().Get("url")}
	if !validateStruct(w, params) {
		return
	}

	result, err := networkService.ParseURL(params.URL)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_URL", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// networkWhoisParams validates apiNetworkWhoisHandler's ?domain= input.
type networkWhoisParams struct {
	Domain string `validate:"required"`
}

// apiNetworkWhoisHandler looks up WHOIS information for ?domain= using
// network.Service.Whois.
func apiNetworkWhoisHandler(w http.ResponseWriter, r *http.Request) {
	params := networkWhoisParams{Domain: r.URL.Query().Get("domain")}
	if !validateStruct(w, params) {
		return
	}

	result, err := networkService.Whois(params.Domain)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "WHOIS_LOOKUP_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"domain": params.Domain,
		"raw":    result,
	})
}

// apiNetworkTracerouteHandler honestly reports 501 NOT_SUPPORTED: a real
// traceroute requires sending TTL-limited probes and receiving ICMP
// time-exceeded replies, which needs a raw ICMP socket (CAP_NET_RAW or
// root). This project ships as an unprivileged, non-root, self-contained
// binary with no guarantee of that capability on the host it runs on, so
// this honestly reports unsupported rather than shipping a fake
// TCP-connect approximation mislabeled as "traceroute". See TODO.AI.md
// "Known permanent API gaps" for the same pattern applied to generate/qr,
// language/detect, and research/extract.
func apiNetworkTracerouteHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "traceroute requires a raw ICMP socket (CAP_NET_RAW/root), which this unprivileged self-contained binary cannot assume it has", nil)
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
