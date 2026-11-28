package server

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/i2p"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/tor"
)

type autodiscoverResponse struct {
	OK   bool             `json:"ok"`
	Data autodiscoverData `json:"data"`
}

type autodiscoverData struct {
	Name          string                  `json:"name"`
	Version       string                  `json:"version"`
	API           autodiscoverAPI         `json:"api"`
	URLs          []string                `json:"urls"`
	CLIVersions   autodiscoverCLIReleases `json:"cli_versions"`
	CLIMinVersion string                  `json:"cli_min_version,omitempty"`
	Onion         string                  `json:"onion,omitempty"`
	I2P           string                  `json:"i2p,omitempty"`
}

type autodiscoverAPI struct {
	CurrentVersion string `json:"current_version"`
}

type cliRelease struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type autodiscoverCLIReleases map[string]cliRelease

func (releases autodiscoverCLIReleases) TaggedResponseFields() sanitize.TaggedObject {
	platforms := make([]string, 0, len(releases))
	for platform := range releases {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	fields := make(sanitize.TaggedObject, 0, len(platforms))
	for _, platform := range platforms {
		fields = append(fields, sanitize.TaggedField{Name: platform, Value: releases[platform]})
	}
	return fields
}

func autodiscoverHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := autodiscoverData{
			Name:          paths.ProjectName,
			Version:       Version,
			API:           autodiscoverAPI{CurrentVersion: strings.TrimPrefix(APIBasePath(cfg), "/api/")},
			CLIVersions:   autodiscoverCLIReleases(discoverCLIVersions(cfg)),
			CLIMinVersion: strings.TrimSpace(cfg.Server.Update.CLIMinVersion),
			URLs:          autodiscoverOrigins(cfg, r),
		}
		if IsTorRequest(r) {
			// Overlay addresses are disclosed only in their dedicated fields;
			// never return a clearnet origin to a Tor visitor.
			data.URLs = []string{}
		}
		if cfg.Server.Tor.Expose {
			if manager := tor.Get(); manager != nil {
				data.Onion = manager.OnionAddress()
			}
		}
		if cfg.Server.I2P.Enabled && cfg.Server.I2P.Expose {
			if manager := i2p.Get(); manager != nil {
				snapshot := manager.Snapshot()
				if snapshot.Running {
					data.I2P = snapshot.Address
				}
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := writeTaggedJSON(w, autodiscoverResponse{OK: true, Data: data}); err != nil {
			log.Printf("autodiscover response write failed: %v", err)
		}
	}
}

// autodiscoverOrigins returns the request's public origin first, followed by
// configured DOMAIN origins and hosts learned from trusted reverse proxies.
func autodiscoverOrigins(cfg *config.ServerConfig, r *http.Request) []string {
	proto, host := "http", ""
	if r != nil {
		host = r.Host
		if r.TLS != nil {
			proto = "https"
		}
		if cfg != nil && isTrustedPeer(cfg, originalPeerAddr(r)) {
			if forwardedHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); validURLHost(forwardedHost) {
				host = forwardedHost
			}
			if forwardedProto := strings.ToLower(firstForwardedValue(r.Header.Get("X-Forwarded-Proto"))); forwardedProto == "https" || forwardedProto == "http" {
				proto = forwardedProto
			}
		}
	}
	if host == "" || !validURLHost(host) {
		resolvedProto, resolvedHost, resolvedPort := GetURLVars(r)
		proto, host = resolvedProto, resolvedHost
		if resolvedPort != "" {
			host = net.JoinHostPort(host, resolvedPort)
		}
	} else if strings.Contains(host, ":") && net.ParseIP(host) != nil {
		host = "[" + host + "]"
	}
	origins := []string{proto + "://" + host}
	origins = append(origins, domainEnvOrigins()...)
	origins = append(origins, learnedOrigins(cfg, r)...)
	seen := make(map[string]bool, len(origins))
	result := make([]string, 0, len(origins))
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			continue
		}
		origin = parsed.Scheme + "://" + parsed.Host
		if !seen[origin] {
			seen[origin] = true
			result = append(result, origin)
		}
	}
	return result
}

func discoverCLIVersions(cfg *config.ServerConfig) map[string]cliRelease {
	dir := strings.TrimSpace(cfg.CLI.BinaryDownload.Dir)
	if dir == "" {
		dir = filepath.Join(paths.DataDir(), "cli")
	}
	versions := make(map[string]cliRelease)
	versionBytes, err := os.ReadFile(filepath.Join(dir, "version.txt"))
	if err != nil {
		return versions
	}
	version := strings.TrimSpace(string(versionBytes))
	if version == "" {
		return versions
	}
	checksumBytes, err := os.ReadFile(filepath.Join(dir, "sha256.txt"))
	if err != nil {
		return versions
	}
	checksums := parseCLIShasum(string(checksumBytes))
	for _, platform := range []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64", "windows-arm64", "freebsd-amd64", "freebsd-arm64"} {
		filename := paths.ProjectName + "-cli-" + platform
		if strings.HasPrefix(platform, "windows-") {
			filename += ".exe"
		}
		path := filepath.Join(dir, filename)
		actual, err := hashFile(path)
		if err != nil {
			continue
		}
		want := strings.ToLower(checksums[filename])
		if len(want) != sha256.Size*2 || actual != want {
			log.Printf("WARN: CLI release checksum mismatch or missing checksum for %s; omitting from autodiscovery", filename)
			continue
		}
		versions[platform] = cliRelease{Version: version, SHA256: actual}
	}
	return versions
}

func parseCLIShasum(contents string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		hash := strings.TrimPrefix(strings.ToLower(fields[0]), "sha256:")
		filename := ""
		if len(fields) > 1 {
			filename = strings.TrimLeft(fields[len(fields)-1], "*./")
		}
		if len(hash) == sha256.Size*2 {
			if filename == "" {
				// A hash-only file is valid for a single artifact, but the
				// association is not knowable until exactly one binary exists.
				filename = ""
			}
			if filename != "" {
				result[filepath.Base(filename)] = hash
			}
		}
	}
	return result
}

func hashFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:]), nil
}
