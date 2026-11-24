package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestCLIBinaryDownloadServesOnlyChecksumVerifiedArtifact(t *testing.T) {
	dir := t.TempDir()
	artifact := []byte("cli release")
	filename := "api-cli-linux-amd64"
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), artifact, 0600))
	sum := sha256.Sum256(artifact)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "version.txt"), []byte("1.2.3\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sha256.txt"), []byte(hex.EncodeToString(sum[:])+"  "+filename+"\n"), 0600))
	cfg := &config.Config{}
	cfg.CLI.BinaryDownload.Dir = dir
	router := chi.NewRouter()
	router.Get("/cli/binaries/{artifact}", cliBinaryDownloadHandler(cfg))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/cli/binaries/"+filename, nil))
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	require.Equal(t, string(artifact), rec.Body.String())
}

func TestCLIBinaryDownloadRequiresBearerWhenConfigured(t *testing.T) {
	cfg := &config.Config{}
	cfg.CLI.BinaryDownload.RequireAuth = true
	router := chi.NewRouter()
	router.Get("/cli/binaries/{artifact}", cliBinaryDownloadHandler(cfg))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/cli/binaries/api-cli-linux-amd64", nil))
	require.Equal(t, 401, rec.Code)
	require.Contains(t, rec.Body.String(), "TOKEN_REVOKED")
}
