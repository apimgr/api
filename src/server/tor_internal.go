package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/apimgr/api/src/tor"
)

// torVanityDefaultTimeout is how long a single /server/tor/vanity/start
// request is allowed to search before giving up, when the caller does not
// specify timeout_seconds. Vanity search is a synchronous, blocking
// request/response - there is no background job/poll mechanism.
const torVanityDefaultTimeout = 60 * time.Second

// torVanityMaxTimeout caps the caller-supplied timeout_seconds so a single
// loopback-only request cannot tie up the server indefinitely.
const torVanityMaxTimeout = 10 * time.Minute

// isLoopbackPeer reports whether r's true (pre-proxy-header-rewrite) TCP
// peer is 127.0.0.1 or ::1. Per AI.md PART 31.1, the /server/tor/* control
// channel is loopback-gated using the same "trusted" address concept as
// PART 12's trusted proxies, but narrower: only the literal loopback
// addresses qualify, never a private/RFC1918 range.
func isLoopbackPeer(r *http.Request) bool {
	addr := originalPeerAddr(r)
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackOnlyMiddleware rejects any request whose true TCP peer is not
// loopback with a 404 (never 403 - the endpoint must not be discoverable
// from a non-loopback caller, per AI.md PART 31.1).
func loopbackOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackPeer(r) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// torStatusHandler implements GET /server/tor/status (INTERNAL, loopback
// only). It reports the live state of the process-wide Tor Manager.
func torStatusHandler(w http.ResponseWriter, r *http.Request) {
	m := tor.Get()
	if m == nil {
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"running": false,
			"address": "",
		})
		return
	}

	cfg := m.ConfigSnapshot()
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"running":      m.Running(),
		"address":      m.OnionAddress(),
		"virtual_port": cfg.VirtualPort,
		"use_network":  cfg.UseNetwork,
	})
}

// torValidateHandler implements POST /server/tor/validate (INTERNAL,
// loopback only). It sanity-checks the live Manager's configuration and,
// if running, its control-port connectivity.
func torValidateHandler(w http.ResponseWriter, r *http.Request) {
	m := tor.Get()
	if m == nil {
		writeEnvelopeError(w, http.StatusServiceUnavailable, "TOR_NOT_CONFIGURED", "Tor is not configured on this server", nil)
		return
	}

	var issues []string
	cfg := m.ConfigSnapshot()
	if cfg.VirtualPort < 1 || cfg.VirtualPort > 65535 {
		issues = append(issues, "virtual_port must be between 1 and 65535")
	}
	if m.Running() {
		if err := m.Ping(); err != nil {
			issues = append(issues, "control connection unresponsive: "+err.Error())
		}
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"valid":  len(issues) == 0,
		"issues": issues,
	})
}

// torRestartHandler implements POST /server/tor/restart (INTERNAL,
// loopback only).
func torRestartHandler(w http.ResponseWriter, r *http.Request) {
	m := tor.Get()
	if m == nil {
		writeEnvelopeError(w, http.StatusServiceUnavailable, "TOR_NOT_CONFIGURED", "Tor is not configured on this server", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := m.Restart(ctx); err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "TOR_RESTART_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"address": m.OnionAddress(),
	})
}

// torRegenerateHandler implements POST /server/tor/regenerate (INTERNAL,
// loopback only).
func torRegenerateHandler(w http.ResponseWriter, r *http.Request) {
	m := tor.Get()
	if m == nil {
		writeEnvelopeError(w, http.StatusServiceUnavailable, "TOR_NOT_CONFIGURED", "Tor is not configured on this server", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	address, err := m.RegenerateAddress(ctx)
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "TOR_REGENERATE_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"address": address,
	})
}

// torVanityStartRequest is the POST /server/tor/vanity/start body.
type torVanityStartRequest struct {
	Prefix         string `json:"prefix"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxAttempts    int64  `json:"max_attempts"`
}

// torVanityStartHandler implements POST /server/tor/vanity/start (INTERNAL,
// loopback only). It runs a synchronous, bounded-time vanity search and, on
// success, records the candidate on the Manager for a later
// /server/tor/vanity/apply call - the private key blob is never sent back
// over the loopback channel a second time.
func torVanityStartHandler(w http.ResponseWriter, r *http.Request) {
	m := tor.Get()
	if m == nil {
		writeEnvelopeError(w, http.StatusServiceUnavailable, "TOR_NOT_CONFIGURED", "Tor is not configured on this server", nil)
		return
	}

	var req torVanityStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", nil)
		return
	}

	if err := tor.ValidateVanityPrefix(req.Prefix); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
		return
	}

	timeout := torVanityDefaultTimeout
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
		if timeout > torVanityMaxTimeout {
			timeout = torVanityMaxTimeout
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := tor.GenerateVanityAddress(ctx, req.Prefix, req.MaxAttempts)
	if err != nil {
		writeEnvelopeError(w, http.StatusRequestTimeout, "VANITY_SEARCH_FAILED", err.Error(), nil)
		return
	}

	m.SetLastVanity(result)

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"address":         result.Address,
		"attempts":        result.Attempts,
		"elapsed_seconds": result.Elapsed.Seconds(),
	})
}

// torVanityApplyHandler implements POST /server/tor/vanity/apply (INTERNAL,
// loopback only). It applies the candidate found by the most recent
// /server/tor/vanity/start call.
func torVanityApplyHandler(w http.ResponseWriter, r *http.Request) {
	m := tor.Get()
	if m == nil {
		writeEnvelopeError(w, http.StatusServiceUnavailable, "TOR_NOT_CONFIGURED", "Tor is not configured on this server", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	address, err := m.ApplyLastVanity(ctx)
	if err != nil {
		writeEnvelopeError(w, http.StatusConflict, "NO_PENDING_VANITY", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"address": address,
	})
}

// torImportKeysRequest is the POST /server/tor/import-keys body. KeyBlob is
// the exact base64 text the CLI read from the operator-supplied key file -
// the same base64(raw 64-byte ed25519 private key) format this project
// persists to hs_ed25519_secret_key.
type torImportKeysRequest struct {
	KeyBlob string `json:"key_blob"`
}

// torImportKeysHandler implements POST /server/tor/import-keys (INTERNAL,
// loopback only).
func torImportKeysHandler(w http.ResponseWriter, r *http.Request) {
	m := tor.Get()
	if m == nil {
		writeEnvelopeError(w, http.StatusServiceUnavailable, "TOR_NOT_CONFIGURED", "Tor is not configured on this server", nil)
		return
	}

	var req torImportKeysRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", nil)
		return
	}

	raw, err := base64.StdEncoding.DecodeString(req.KeyBlob)
	if err != nil || len(raw) != 64 {
		writeEnvelopeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "key_blob must be base64 of a raw 64-byte ed25519 private key", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	address, err := m.ApplyKeys(ctx, []byte(req.KeyBlob))
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "TOR_IMPORT_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"address": address,
	})
}
