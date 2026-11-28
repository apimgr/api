package scheduler

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/requestid"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/health"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/securitydata"
)

const (
	defaultTrivySource  = "ghcr.io/aquasecurity/trivy-db:2"
	trivyLayerMediaType = "application/vnd.aquasec.trivy.db.layer.v1.tar+gzip"
)

const fireHOLBlocklistURL = "https://raw.githubusercontent.com/firehol/blocklist-ipsets/master/firehol_level1.netset"

// feedDownloadTimeout bounds a single feed download.
const feedDownloadTimeout = 10 * time.Minute

// blocklistUpdateTask refreshes the IP and domain blocklists.
func blocklistUpdateTask() error {
	log.Println("Scheduler: Updating IP/domain blocklists...")
	cfg := config.Get()
	if cfg == nil || !cfg.Server.Security.Blocklists.Enabled {
		return nil
	}
	securityDir := filepath.Join(securityDataDirectory(cfg), "blocklists")
	sources := []config.BlocklistSource{{ID: "firehol_level1", Type: "ip", URL: fireHOLBlocklistURL}}
	for _, source := range cfg.Server.Security.Blocklists.ExtraSources {
		if source.Enabled == nil || *source.Enabled {
			sources = append(sources, source)
		}
	}
	succeeded := 0
	var lastErr error
	failedSources := make(map[string]bool)
	for _, source := range sources {
		if err := updateBlocklistSource(source, cfg, securityDir); err != nil {
			lastErr = err
			failedSources[source.ID] = true
			log.Printf("Scheduler: blocklist update failed for %s: %v", source.ID, err)
			continue
		}
		succeeded++
	}
	if succeeded > 0 {
		stamp := []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
		if err := writeSecurityFileAtomically(filepath.Join(securityDir, ".last_updated"), stamp, 0o644); err != nil {
			return err
		}
		securitydata.DefaultBlocklists.Reload(cfg, time.Now().UTC())
		securitydata.RegisterHealthChecks(cfg)
		for _, source := range sources {
			health.MarkUpdateFailure("blocklist_"+source.ID, failedSources[source.ID])
		}
		health.Refresh()
		if len(failedSources) != 0 {
			return fmt.Errorf("%d blocklist source(s) failed: %w", len(failedSources), lastErr)
		}
		return nil
	}
	securitydata.RegisterHealthChecks(cfg)
	for _, source := range sources {
		health.MarkUpdateFailure("blocklist_"+source.ID, true)
	}
	health.Refresh()
	return fmt.Errorf("blocklist update failed: %w", lastErr)
}

func updateBlocklistSource(source config.BlocklistSource, cfg *config.ServerConfig, directory string) error {
	ctx, cancel := context.WithTimeout(context.Background(), feedDownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "api/scheduler")
	resp, err := requestid.DoOutboundRequestWithID(http.DefaultClient, req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Request == nil || resp.Request.URL == nil || resp.Request.URL.Scheme != "https" {
		return fmt.Errorf("source redirected away from HTTPS")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("source returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20+1))
	if err != nil {
		return err
	}
	if len(body) > 64<<20 {
		return fmt.Errorf("source exceeds size limit")
	}
	canonical, _, skipped, err := securitydata.ParseBlocklist(source.ID, source.Type, strings.NewReader(string(body)), securitydata.TrustedNetworks(cfg))
	if err != nil {
		return err
	}
	if skipped > 0 {
		log.Printf("Scheduler: skipped %d invalid or unsafe entries from blocklist %s", skipped, source.ID)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(directory, source.ID+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	for _, line := range canonical {
		if _, err := io.WriteString(tmp, line+"\n"); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceSecurityFileAtomically(tmpName, filepath.Join(directory, source.ID+".txt"))
}

// cveUpdateTask refreshes NVD CVE records and the optional Trivy vulnerability
// database. PART 18 gives Trivy no scheduled task of its own - cve_update
// owns it - so either enabled database failing lets the scheduler retry the
// combined update while each last-good database remains in place.
func cveUpdateTask() error {
	log.Println("Scheduler: Updating CVE database...")

	if err := updateNVD(context.Background(), config.Get(), http.DefaultClient, time.Now().UTC()); err != nil {
		log.Printf("Scheduler: NVD update failed: %v", err)
		securitydata.RegisterHealthChecks(config.Get())
		health.MarkUpdateFailure("cve_nvd", true)
		health.Refresh()
		return err
	}
	log.Println("Scheduler: NVD CVE records updated")
	cfg := config.Get()
	if cfg == nil || !dataConfigBool(cfg.Data, "trivy", "enabled") {
		securitydata.RegisterHealthChecks(cfg)
		health.MarkUpdateFailure("cve_nvd", false)
		health.Refresh()
		return nil
	}

	if err := downloadTrivyDB(); err != nil {
		log.Printf("Scheduler: Trivy database update failed: %v", err)
		securitydata.RegisterHealthChecks(config.Get())
		health.MarkUpdateFailure("cve_nvd", false)
		health.MarkUpdateFailure("trivy_db", true)
		health.Refresh()
		return err
	}

	securitydata.RegisterHealthChecks(config.Get())
	health.MarkUpdateFailure("cve_nvd", false)
	health.MarkUpdateFailure("trivy_db", false)
	health.Refresh()
	log.Println("Scheduler: Trivy database updated")
	return nil
}

// downloadTrivyDB pulls the Trivy vulnerability database from its OCI
// registry into {data_dir}/security/trivy/. The database is published only
// as an OCI artifact, so the anonymous pull token, manifest and layer blob
// are fetched directly rather than through a plain file download.
func downloadTrivyDB() error {
	cfg := config.Get()
	if cfg == nil || !dataConfigBool(cfg.Data, "trivy", "enabled") {
		return nil
	}
	source := dataConfigString(cfg.Data, "trivy", "source")
	if source == "" {
		source = defaultTrivySource
	}
	targetDir := filepath.Join(securityDataDirectory(cfg), "trivy")
	if trivyDatabaseFresh(targetDir, time.Now().UTC()) {
		return nil
	}
	registry, repository, tag, err := parseOCIImageReference(source)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), feedDownloadTimeout)
	defer cancel()

	token, err := trivyPullToken(ctx, registry, repository)
	if err != nil {
		return err
	}

	digest, err := trivyLayerDigest(ctx, token, registry, repository, tag)
	if err != nil {
		return err
	}
	blobURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repository, digest)
	return installTrivyDatabase(ctx, http.DefaultClient, blobURL, token, targetDir)
}

func trivyDatabaseFresh(directory string, now time.Time) bool {
	dbInfo, err := os.Stat(filepath.Join(directory, "trivy.db"))
	if err != nil || dbInfo.Size() == 0 {
		return false
	}
	data, err := os.ReadFile(filepath.Join(directory, "metadata.json"))
	if err != nil {
		return false
	}
	var metadata trivyMetadata
	return json.Unmarshal(data, &metadata) == nil && metadata.Version > 0 && metadata.NextUpdate.After(now)
}

// trivyPullToken obtains an anonymous pull token for the Trivy DB repository.
func trivyPullToken(ctx context.Context, registry, repository string) (string, error) {
	tokenURL := fmt.Sprintf("https://%s/token?scope=repository:%s:pull&service=%s", registry, repository, registry)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}

	resp, err := requestid.DoOutboundRequestWithID(http.DefaultClient, req)
	if err != nil {
		return "", fmt.Errorf("request pull token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Request == nil || resp.Request.URL == nil || resp.Request.URL.Scheme != "https" {
		return "", fmt.Errorf("pull token endpoint redirected away from HTTPS")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request pull token: unexpected status %s", resp.Status)
	}

	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode pull token: %w", err)
	}
	if payload.Token == "" {
		return "", fmt.Errorf("registry returned an empty pull token")
	}

	return payload.Token, nil
}

// trivyLayerDigest resolves the digest of the database layer in the Trivy DB
// manifest.
func trivyLayerDigest(ctx context.Context, token, registry, repository, tag string) (string, error) {
	return trivyLayerDigestWithClient(ctx, http.DefaultClient, token, registry, repository, tag)
}

func trivyLayerDigestWithClient(ctx context.Context, client *http.Client, token, registry, repository, tag string) (string, error) {
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("build manifest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := requestid.DoOutboundRequestWithID(client, req)
	if err != nil {
		return "", fmt.Errorf("fetch manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Request == nil || resp.Request.URL == nil || resp.Request.URL.Scheme != "https" {
		return "", fmt.Errorf("manifest endpoint redirected away from HTTPS")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch manifest: unexpected status %s", resp.Status)
	}

	var manifest struct {
		Layers []struct {
			Digest    string `json:"digest"`
			MediaType string `json:"mediaType"`
		} `json:"layers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return "", fmt.Errorf("decode manifest: %w", err)
	}
	for _, layer := range manifest.Layers {
		if layer.MediaType == trivyLayerMediaType && layer.Digest != "" {
			return layer.Digest, nil
		}
	}
	return "", fmt.Errorf("manifest contains no Trivy database layer")
}

type trivyMetadata struct {
	Version    int       `json:"Version"`
	NextUpdate time.Time `json:"NextUpdate"`
	UpdatedAt  time.Time `json:"UpdatedAt"`
}

func installTrivyDatabase(ctx context.Context, client *http.Client, blobURL, token, targetDir string) error {
	if trivyDatabaseFresh(targetDir, time.Now().UTC()) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, blobURL, nil)
	if err != nil {
		return fmt.Errorf("build Trivy blob request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := requestid.DoOutboundRequestWithID(client, req)
	if err != nil {
		return fmt.Errorf("fetch Trivy database layer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Request == nil || resp.Request.URL == nil || resp.Request.URL.Scheme != "https" {
		return fmt.Errorf("trivy layer endpoint redirected away from HTTPS")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch Trivy database layer: unexpected status %s", resp.Status)
	}
	parent := filepath.Dir(targetDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create Trivy directory: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".trivy-stage-*")
	if err != nil {
		return fmt.Errorf("create Trivy staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := os.Chmod(stage, 0o755); err != nil {
		return fmt.Errorf("set Trivy staging permissions: %w", err)
	}
	gz, err := gzip.NewReader(io.LimitReader(resp.Body, 1<<30))
	if err != nil {
		return fmt.Errorf("open Trivy layer: %w", err)
	}
	tr := tar.NewReader(gz)
	foundDB, foundMetadata := false, false
	for {
		header, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("read Trivy archive: %w", nextErr)
		}
		name := filepath.Base(filepath.Clean(header.Name))
		if name != "trivy.db" && name != "metadata.json" {
			continue
		}
		maxSize := int64(512 << 20)
		if name == "metadata.json" {
			maxSize = 1 << 20
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxSize {
			return fmt.Errorf("invalid Trivy archive entry")
		}
		out, createErr := os.OpenFile(filepath.Join(stage, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if createErr != nil {
			return fmt.Errorf("create Trivy data file: %w", createErr)
		}
		written, copyErr := io.CopyN(out, tr, header.Size)
		closeErr := out.Close()
		if copyErr != nil {
			return fmt.Errorf("extract Trivy data file: %w", copyErr)
		}
		if closeErr != nil || written != header.Size {
			return fmt.Errorf("incomplete Trivy data file")
		}
		if name == "trivy.db" {
			foundDB = true
		} else {
			foundMetadata = true
		}
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close Trivy archive: %w", err)
	}
	if !foundDB || !foundMetadata {
		return fmt.Errorf("trivy archive lacks trivy.db or metadata.json")
	}
	metadataBytes, err := os.ReadFile(filepath.Join(stage, "metadata.json"))
	if err != nil {
		return err
	}
	var metadata trivyMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil || metadata.NextUpdate.IsZero() || metadata.Version <= 0 {
		return fmt.Errorf("trivy metadata is invalid")
	}
	stamp := []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
	if err := os.WriteFile(filepath.Join(stage, ".last_updated"), stamp, 0o644); err != nil {
		return err
	}
	return replaceTrivyDirectory(stage, targetDir)
}

func parseOCIImageReference(reference string) (registry, repository, tag string, err error) {
	registry, remainder, found := strings.Cut(reference, "/")
	if !found || registry == "" || remainder == "" {
		return "", "", "", fmt.Errorf("invalid Trivy OCI source")
	}
	separator := strings.LastIndex(remainder, ":")
	if separator < 0 || separator == len(remainder)-1 {
		return "", "", "", fmt.Errorf("trivy OCI source must include a tag")
	}
	repository, tag = remainder[:separator], remainder[separator+1:]
	if repository == "" || strings.ContainsAny(registry, "@?#") || strings.ContainsAny(repository, "@?#") || strings.ContainsAny(tag, "/?#") {
		return "", "", "", fmt.Errorf("invalid Trivy OCI source")
	}
	return registry, repository, tag, nil
}

func dataConfigString(data map[string]interface{}, section, key string) string {
	values, ok := data[section].(map[string]interface{})
	if !ok {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func dataConfigBool(data map[string]interface{}, section, key string) bool {
	values, ok := data[section].(map[string]interface{})
	if !ok {
		return false
	}
	value, _ := values[key].(bool)
	return value
}

func securityDataDirectory(cfg *config.ServerConfig) string {
	if cfg != nil {
		if configured, ok := cfg.Data["security_dir"].(string); ok && strings.TrimSpace(configured) != "" {
			return strings.TrimSpace(configured)
		}
	}
	return filepath.Join(paths.DataDir(), "security")
}
