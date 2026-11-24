package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
)

// SupplementalSource is an explicitly referenced file with a stable archive
// path that differs from its on-disk path.
type SupplementalSource struct {
	Path        string
	ArchivePath string
}

// Sources returns the operator data required by PART 21. Optional trees are
// included only when present so a fresh installation still has a valid
// server.yml-only backup.
func Sources(configDir, dataDir string, includeSSL, includeData bool) []string {
	var sources []string
	if includeData {
		if info, err := os.Stat(dataDir); err == nil && info.IsDir() {
			sources = append(sources, dataDir)
		}
	} else {
		databasePath := filepath.Join(paths.GetDatabaseDir(dataDir), "server.db")
		if info, err := os.Stat(databasePath); err == nil && info.Mode().IsRegular() {
			sources = append(sources, databasePath)
		}
	}
	for _, path := range []string{
		filepath.Join(configDir, "server.yml"),
		filepath.Join(configDir, "yaml.d"),
		filepath.Join(configDir, "secrets.yml"),
		filepath.Join(configDir, "assets"),
		filepath.Join(configDir, "template"),
		filepath.Join(configDir, "security"),
	} {
		if info, err := os.Stat(path); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			sources = append(sources, path)
		}
	}
	if includeSSL {
		sslDir := filepath.Join(configDir, "ssl")
		if info, err := os.Stat(sslDir); err == nil && info.IsDir() {
			sources = append(sources, sslDir)
		}
	}
	return sources
}

// SourcesWithConfig adds only referenced connection TLS files and external
// manually supplied certificates to the normal backup source list.
func SourcesWithConfig(cfg *config.Config, configDir, dataDir string, includeSSL, includeData bool) ([]string, []SupplementalSource, error) {
	sources := Sources(configDir, dataDir, includeSSL, includeData)
	if cfg == nil {
		return nil, nil, fmt.Errorf("backup source configuration is required")
	}
	var supplemental []SupplementalSource
	if isRemoteDatabase(cfg.Server.Database.Driver) && cfg.Server.Database.Cache.Enabled {
		cachePath := cfg.Server.Database.Cache.Path
		if !filepath.IsAbs(cachePath) {
			cachePath = filepath.Join(dataDir, cachePath)
		}
		if info, statErr := os.Stat(cachePath); statErr == nil && info.Mode().IsRegular() {
			serverDBPath := filepath.Join(paths.GetDatabaseDir(dataDir), "server.db")
			filtered := sources[:0]
			for _, source := range sources {
				if filepath.Clean(source) != filepath.Clean(serverDBPath) {
					filtered = append(filtered, source)
				}
			}
			sources = filtered
			supplemental = append(supplemental, SupplementalSource{Path: cachePath, ArchivePath: "server.db"})
		}
	}
	sslRoot, err := filepath.Abs(filepath.Join(configDir, "ssl"))
	if err != nil {
		return nil, nil, err
	}
	if !includeSSL {
		for _, item := range []struct {
			value string
			name  string
		}{
			{cfg.Server.Cache.TLSCA, "tls_ca"},
			{cfg.Server.Cache.TLSCert, "tls_cert"},
			{cfg.Server.Cache.TLSKey, "tls_key"},
		} {
			if strings.TrimSpace(item.value) == "" {
				continue
			}
			path, relative, err := resolveSSLConnectionFile(sslRoot, item.value)
			if err != nil {
				return nil, nil, fmt.Errorf("server.cache.%s: %w", item.name, err)
			}
			supplemental = append(supplemental, SupplementalSource{Path: path, ArchivePath: "ssl/" + filepath.ToSlash(relative)})
		}
	}
	for _, item := range []struct {
		value string
		name  string
		file  string
	}{
		{cfg.Server.SSL.Cert, "server.ssl.cert", "cert.pem"},
		{cfg.Server.SSL.Key, "server.ssl.key", "key.pem"},
	} {
		if strings.TrimSpace(item.value) == "" {
			continue
		}
		path, err := filepath.Abs(item.value)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: resolve path: %w", item.name, err)
		}
		if relative, err := filepath.Rel(sslRoot, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			if err == nil {
				err = fmt.Errorf("path is not a regular file")
			}
			return nil, nil, fmt.Errorf("%s: inspect external TLS file: %w", item.name, err)
		}
		supplemental = append(supplemental, SupplementalSource{Path: path, ArchivePath: "ssl-external/" + item.file})
	}
	return sources, supplemental, nil
}

func isRemoteDatabase(driver string) bool {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "libsql", "turso":
		return true
	default:
		return false
	}
}

func resolveSSLConnectionFile(sslRoot, value string) (string, string, error) {
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(sslRoot, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(sslRoot)
	if err != nil {
		if os.IsNotExist(err) {
			resolvedRoot = sslRoot
		} else {
			return "", "", err
		}
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", "", fmt.Errorf("file must resolve inside the configured ssl directory")
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("path is not a regular file")
	}
	return resolvedPath, relative, nil
}

// RestoreRoots returns only the canonical configuration and database
// destinations a backup may restore. It intentionally does not grant the
// whole config or data directory as a writable root.
func RestoreRoots(configDir, dataDir string, includeSSL, includeData bool) []string {
	dbDir := paths.GetDatabaseDir(dataDir)
	roots := []string{
		filepath.Join(configDir, "server.yml"),
		filepath.Join(configDir, "yaml.d"),
		filepath.Join(configDir, "secrets.yml"),
		filepath.Join(configDir, "assets"),
		filepath.Join(configDir, "template"),
		filepath.Join(configDir, "security", "pgp.pub.asc"),
		filepath.Join(configDir, "security", "pgp.priv.asc.enc"),
		filepath.Join(configDir, "security", "pgp.priv.old.asc.enc"),
		filepath.Join(configDir, "security", "keyservers.state"),
		filepath.Join(configDir, "security"),
	}
	if includeData {
		roots = append(roots, dataDir)
	} else {
		roots = append(roots, filepath.Join(dbDir, "server.db"))
	}
	roots = append(roots, filepath.Join(configDir, "ssl"))
	return roots
}
