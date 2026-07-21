package ssl

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ParseChallenge must normalize the various spellings for each challenge
// type and default unknown values to http-01.
func TestParseChallenge(t *testing.T) {
	cases := map[string]string{
		"http-01":     "http-01",
		"HTTP01":      "http-01",
		"http":        "http-01",
		"tls-alpn-01": "tls-alpn-01",
		"tlsalpn01":   "tls-alpn-01",
		"tls-alpn":    "tls-alpn-01",
		"tls":         "tls-alpn-01",
		"dns-01":      "dns-01",
		"dns01":       "dns-01",
		"dns":         "dns-01",
		"":            "http-01",
		"bogus":       "http-01",
		"  DNS  ":     "dns-01",
	}
	for in, want := range cases {
		assert.Equal(t, want, ParseChallenge(in), "ParseChallenge(%q)", in)
	}
}

// fileExists must reflect actual filesystem state.
func TestFileExists(t *testing.T) {
	tmp := t.TempDir()
	present := filepath.Join(tmp, "present.txt")
	require.NoError(t, os.WriteFile(present, []byte("x"), 0644))

	assert.True(t, fileExists(present))
	assert.False(t, fileExists(filepath.Join(tmp, "missing.txt")))
}

// GetTLSConfig must return (nil, nil) when SSL is disabled, without
// touching the filesystem or attempting certificate generation.
func TestGetTLSConfigDisabled(t *testing.T) {
	m := NewManager(Config{Enabled: false})
	cfg, err := m.GetTLSConfig([]string{"example.com"})
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

// GetTLSConfig must fall back to a self-signed certificate when enabled
// with no Let's Encrypt config and no existing/manual certs on disk.
func TestGetTLSConfigSelfSignedFallback(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(Config{Enabled: true, CertPath: tmp})
	cfg, err := m.GetTLSConfig([]string{"selfsigned.example.com"})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Certificates, 1)

	// The self-signed cert must have been written under CertPath/local/<fqdn>.
	assert.True(t, fileExists(filepath.Join(tmp, "local", "selfsigned.example.com", "cert.pem")))
}

// GetTLSConfig with no domains and no manual/LE cert must error out of the
// self-signed fallback rather than panicking.
func TestGetTLSConfigNoDomains(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(Config{Enabled: true, CertPath: tmp})
	_, err := m.GetTLSConfig(nil)
	assert.Error(t, err)
}

// GetTLSConfig must prefer a manually placed cert.pem/key.pem pair over
// generating a self-signed one.
func TestGetTLSConfigManualCert(t *testing.T) {
	tmp := t.TempDir()
	domain := "manual.example.com"

	// Reuse GenerateSelfSigned to produce a valid cert/key pair, then move
	// it into the "manual" tier layout that findManualCerts looks for.
	genCertPath, genKeyPath, err := GenerateSelfSigned(t.TempDir(), domain)
	require.NoError(t, err)

	manualDir := filepath.Join(tmp, "local", domain)
	require.NoError(t, os.MkdirAll(manualDir, 0700))
	certBytes, err := os.ReadFile(genCertPath)
	require.NoError(t, err)
	keyBytes, err := os.ReadFile(genKeyPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(manualDir, "cert.pem"), certBytes, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(manualDir, "key.pem"), keyBytes, 0600))

	m := NewManager(Config{Enabled: true, CertPath: tmp})
	cfg, err := m.GetTLSConfig([]string{domain})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Certificates, 1)
}

// findManualCerts must also recognize the legacy flat "<domain>.crt" /
// "<domain>.key" naming, and the fullchain.pem/privkey.pem naming.
func TestFindManualCertsLegacyNaming(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(Config{Enabled: true, CertPath: tmp})

	domain := "legacy.example.com"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, domain+".crt"), []byte("cert"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, domain+".key"), []byte("key"), 0600))

	cert, key := m.findManualCerts([]string{domain})
	assert.Equal(t, filepath.Join(tmp, domain+".crt"), cert)
	assert.Equal(t, filepath.Join(tmp, domain+".key"), key)
}

// findManualCerts with an empty CertPath must never match, avoiding a
// join against the current working directory.
func TestFindManualCertsEmptyCertPath(t *testing.T) {
	m := NewManager(Config{Enabled: true, CertPath: ""})
	cert, key := m.findManualCerts([]string{"example.com"})
	assert.Empty(t, cert)
	assert.Empty(t, key)
}

// findExistingCerts must return empty when nothing is present under
// /etc/letsencrypt/live or CertPath/letsencrypt.
func TestFindExistingCertsNoneFound(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(Config{Enabled: true, CertPath: tmp})
	cert, key := m.findExistingCerts([]string{"nowhere.example.com"})
	assert.Empty(t, cert)
	assert.Empty(t, key)
}

// GetHTTPHandler must return the fallback handler untouched until a
// certManager has actually been set up (i.e. before any Let's Encrypt
// config path has run).
func TestGetHTTPHandlerNoCertManager(t *testing.T) {
	m := NewManager(Config{Enabled: false})
	called := false
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/token", nil)
	rec := httptest.NewRecorder()
	m.GetHTTPHandler(fallback).ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

// ChallengeServer must serve a registered token's key authorization on the
// well-known ACME path and 404 on an unregistered token, while leaving
// unrelated paths unhandled.
func TestChallengeServer(t *testing.T) {
	cs := NewChallengeServer()
	cs.SetToken("tok123", "tok123.keyauth")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/tok123", nil)
	rec := httptest.NewRecorder()
	handled := cs.ServeHTTP(rec, req)
	assert.True(t, handled)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "tok123.keyauth", rec.Body.String())

	// Unknown token on the ACME path: handled, but 404.
	req2 := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/unknown", nil)
	rec2 := httptest.NewRecorder()
	handled2 := cs.ServeHTTP(rec2, req2)
	assert.True(t, handled2)
	assert.Equal(t, http.StatusNotFound, rec2.Code)

	// A cleared token must revert to 404.
	cs.ClearToken("tok123")
	req3 := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/tok123", nil)
	rec3 := httptest.NewRecorder()
	handled3 := cs.ServeHTTP(rec3, req3)
	assert.True(t, handled3)
	assert.Equal(t, http.StatusNotFound, rec3.Code)

	// Unrelated paths must be reported as not handled at all.
	req4 := httptest.NewRequest(http.MethodGet, "/some/other/path", nil)
	rec4 := httptest.NewRecorder()
	handled4 := cs.ServeHTTP(rec4, req4)
	assert.False(t, handled4)
}
