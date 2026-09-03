package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apimgr/api/src/config"
)

// newGeoTestHandler wraps a always-200 handler in the geoip middleware for
// the supplied config.
func newGeoTestHandler(cfg *config.Config) http.Handler {
	return geoIPMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

// geoTestConfig builds a config with GeoIP enabled or disabled and the given
// deny list.
func geoTestConfig(enabled bool, deny []string) *config.Config {
	cfg := &config.Config{}
	cfg.Server.GeoIP.Enabled = enabled
	cfg.Server.GeoIP.DenyCountries = deny
	cfg.Server.GeoIP.Databases.Country = true
	return cfg
}

func TestGeoIPMiddleware_DisabledPassesThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"

	newGeoTestHandler(geoTestConfig(false, []string{"CN"})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when GeoIP is disabled, got %d", rec.Code)
	}
}

func TestGeoIPMiddleware_FailsOpenWithoutDatabase(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"

	newGeoTestHandler(geoTestConfig(true, []string{"CN"})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected fail-open 200 with no country database, got %d", rec.Code)
	}
}

func TestGeoIPMiddleware_PrivateAddressIsNeverBlocked(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"

	newGeoTestHandler(geoTestConfig(true, []string{"CN"})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected private addresses to pass, got %d", rec.Code)
	}
}
