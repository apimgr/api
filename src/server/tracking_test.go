package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackingOutputProviderMarkupAndHosts(t *testing.T) {
	tests := []struct {
		name     string
		config   config.TrackingConfig
		wantTag  string
		wantHost string
		wantBody bool
	}{
		{"google", config.TrackingConfig{Type: "google", ID: "G-ABC123"}, "googletagmanager.com", "https://www.googletagmanager.com", true},
		{"matomo", config.TrackingConfig{Type: "matomo", ID: "1", URL: "https://analytics.example"}, "matomo.js", "https://analytics.example", true},
		{"fathom", config.TrackingConfig{Type: "fathom", ID: "site-id"}, "cdn.usefathom.com", "https://cdn.usefathom.com", false},
		{"simple", config.TrackingConfig{Type: "simple"}, "simpleanalyticscdn.com", "https://scripts.simpleanalyticscdn.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := buildTrackingOutput(tt.config)
			assert.Contains(t, output.Tags+output.Body, tt.wantTag)
			assert.Equal(t, tt.wantHost, output.ScriptHost)
			assert.Equal(t, tt.wantBody, output.Body != "")
		})
	}
}

func TestTrackingRequiresConsentAndSuppressesOverlayRequests(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Tracking = config.TrackingConfig{Type: "google", ID: "G-ABC123"}
	cfg.Server.Privacy.Cookies.Analytics.Enabled = true
	request := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	assert.False(t, trackingAllowed(cfg, request))
	request.AddCookie(&http.Cookie{Name: "cookie_consent", Value: url.PathEscape(`{"essential":true,"preferences":false,"analytics":true,"timestamp":1}`)})
	assert.True(t, trackingAllowed(cfg, request))
	request.Host = "service.b32.i2p"
	assert.False(t, trackingAllowed(cfg, request))
	request.Host = "example.test"
	request = request.WithContext(context.WithValue(request.Context(), torCircuitContextKey, "tor:1234"))
	assert.False(t, trackingAllowed(cfg, request))
}

func TestTrackingScriptHandlerIsNoCacheAndConsentGated(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Tracking = config.TrackingConfig{Type: "matomo", ID: "1", URL: "https://analytics.example"}
	cfg.Server.Privacy.Cookies.Analytics.Enabled = true
	handler := trackingScriptHandler(cfg)

	withoutConsent := httptest.NewRecorder()
	handler.ServeHTTP(withoutConsent, httptest.NewRequest(http.MethodGet, "/server/tracking.js", nil))
	require.Equal(t, http.StatusNotFound, withoutConsent.Code)

	request := httptest.NewRequest(http.MethodGet, "/server/tracking.js", nil)
	request.AddCookie(&http.Cookie{Name: "cookie_consent", Value: url.PathEscape(`{"essential":true,"preferences":false,"analytics":true,"timestamp":1}`)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "no-cache, no-store, must-revalidate", response.Header().Get("Cache-Control"))
	assert.True(t, strings.Contains(response.Body.String(), "analytics.example"))
}

func TestTrackingCSPAddsProviderOnlyAfterConsent(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Tracking = config.TrackingConfig{Type: "google", ID: "G-ABC123"}
	cfg.Server.Privacy.Cookies.Analytics.Enabled = true
	cfg.Web.CSP.Enabled = true
	serve := securityHeadersMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	withoutConsent := httptest.NewRecorder()
	serve.ServeHTTP(withoutConsent, httptest.NewRequest(http.MethodGet, "https://example.test/", nil))
	assert.NotContains(t, withoutConsent.Header().Get("Content-Security-Policy"), "googletagmanager.com")

	request := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	request.AddCookie(&http.Cookie{Name: "cookie_consent", Value: url.PathEscape(`{"essential":true,"preferences":false,"analytics":true,"timestamp":1}`)})
	withConsent := httptest.NewRecorder()
	serve.ServeHTTP(withConsent, request)
	assert.Contains(t, withConsent.Header().Get("Content-Security-Policy"), "googletagmanager.com")
}
