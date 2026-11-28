package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/swagger"
	"github.com/stretchr/testify/require"
)

func TestAutodiscoverIncludesVerifiedCLIReleaseAndVersion(t *testing.T) {
	t.Setenv("DOMAIN", "primary.example.com,secondary.example.com")
	resetLearnedOrigins()
	t.Cleanup(resetLearnedOrigins)
	dir := t.TempDir()
	binary := []byte("release artifact")
	filename := "api-cli-linux-amd64"
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), binary, 0600))
	sum := sha256.Sum256(binary)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "version.txt"), []byte("2.3.4\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sha256.txt"), []byte(hex.EncodeToString(sum[:])+"  "+filename+"\n"), 0600))

	cfg := &config.ServerConfig{}
	cfg.CLI.BinaryDownload.Dir = dir
	cfg.Server.APIVersion = "v9"
	cfg.Server.Update.CLIMinVersion = "1.2.0"
	oldVersion := Version
	Version = "3.4.5"
	t.Cleanup(func() { Version = oldVersion })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/autodiscover", nil)
	autodiscoverHandler(cfg)(rec, req)
	require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	var body autodiscoverResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.OK)
	require.Equal(t, "api", body.Data.Name)
	require.Equal(t, "3.4.5", body.Data.Version)
	require.Equal(t, "v9", body.Data.API.CurrentVersion)
	require.Equal(t, "1.2.0", body.Data.CLIMinVersion)
	require.Equal(t, []string{"http://example.com", "https://primary.example.com", "https://secondary.example.com"}, body.Data.URLs)
	require.Equal(t, cliRelease{Version: "2.3.4", SHA256: hex.EncodeToString(sum[:])}, body.Data.CLIVersions["linux-amd64"])
}

func TestAutodiscoverOmitsChecksumMismatchAndUnexposedOverlays(t *testing.T) {
	dir := t.TempDir()
	filename := "api-cli-linux-amd64"
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte("modified"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "version.txt"), []byte("2.3.4\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sha256.txt"), []byte(hex.EncodeToString(make([]byte, sha256.Size))+"  "+filename+"\n"), 0600))

	cfg := &config.ServerConfig{}
	cfg.CLI.BinaryDownload.Dir = dir
	cfg.Server.I2P.Enabled = true
	cfg.Server.I2P.Expose = false
	cfg.Server.Tor.Expose = false
	rec := httptest.NewRecorder()
	autodiscoverHandler(cfg)(rec, httptest.NewRequest("GET", "/api/autodiscover", nil))
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data := body["data"].(map[string]any)
	require.NotContains(t, data["cli_versions"].(map[string]any), "linux-amd64")
	require.NotContains(t, data, "onion")
	require.NotContains(t, data, "i2p")
}

func TestAutodiscoverDoesNotReturnOriginsToTorRequests(t *testing.T) {
	t.Setenv("DOMAIN", "primary.example,secondary.example")
	cfg := &config.ServerConfig{}
	cfg.Server.Tor.Expose = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://service.onion/api/autodiscover", nil)
	req = req.WithContext(context.WithValue(req.Context(), torCircuitContextKey, "tor:0123456789abcdef"))
	autodiscoverHandler(cfg)(rec, req)

	var body autodiscoverResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.OK)
	require.Empty(t, body.Data.URLs)
}

func TestAutodiscoverContentNegotiation(t *testing.T) {
	cfg := &config.ServerConfig{}
	routes := []swagger.Route{apiRouteDefinition("GET", "/api/autodiscover")}
	handler := apiTextMiddleware(paths.ProjectName, &routes)(autodiscoverHandler(cfg))
	for _, test := range []struct {
		accept      string
		contentType string
	}{
		{accept: "application/json", contentType: "application/json; charset=utf-8"},
		{accept: "text/plain", contentType: "text/plain; charset=utf-8"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/autodiscover", nil)
		req.Header.Set("Accept", test.accept)
		handler.ServeHTTP(rec, req)
		require.Equal(t, 200, rec.Code)
		require.Equal(t, test.contentType, rec.Header().Get("Content-Type"))
	}
}
