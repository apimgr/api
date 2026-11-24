package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/go-chi/chi/v5"
)

func cliBinaryDownloadHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.CLI.BinaryDownload.RequireAuth {
			token := bearerToken(r)
			credential, err := validateConfiguredOwnerToken(r.Context(), cfg, token)
			if err != nil {
				if token != "" && strings.HasPrefix(r.UserAgent(), paths.ProjectName+"-cli/") {
					if auditErr := database.WriteAuditEventWithSource(r.Context(), "cli.token_revoked_detected", "", getClientIP(r), "cli", map[string]any{"token_prefix": ownerTokenPrefix(token)}); auditErr != nil {
						log.Printf("CLI download: could not record invalid-token audit event: %v", auditErr)
					}
				}
				w.Header().Set("WWW-Authenticate", "Bearer")
				writeTokenAPIError(w, r, "TOKEN_REVOKED", "errors.unauthorized", http.StatusUnauthorized)
				return
			}
			_ = credential
		}

		artifact := chi.URLParam(r, "artifact")
		platform, filename, ok := cliArtifact(artifact)
		if !ok {
			http.NotFound(w, r)
			return
		}
		versions := discoverCLIVersions(cfg)
		release, ok := versions[platform]
		if !ok {
			http.NotFound(w, r)
			return
		}
		dir := strings.TrimSpace(cfg.CLI.BinaryDownload.Dir)
		if dir == "" {
			dir = filepath.Join(paths.DataDir(), "cli")
		}
		path := filepath.Join(dir, filename)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = file.Close() }()
		if actual, err := hashOpenFile(file); err != nil || actual != release.SHA256 {
			http.NotFound(w, r)
			return
		}
		if err := database.WriteAuditEventWithSource(r.Context(), "cli.update_started", "", getClientIP(r), "cli", map[string]any{"platform": platform, "served_version": release.Version}); err != nil {
			log.Printf("CLI download: could not record update audit event: %v", err)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, filename, info.ModTime(), file)
	}
}

func cliArtifact(name string) (platform, filename string, ok bool) {
	for _, candidate := range []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64", "windows-arm64", "freebsd-amd64", "freebsd-arm64"} {
		filename = paths.ProjectName + "-cli-" + candidate
		if strings.HasPrefix(candidate, "windows-") {
			filename += ".exe"
		}
		if name == filename {
			return candidate, filename, true
		}
	}
	return "", "", false
}

func hashOpenFile(file *os.File) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
