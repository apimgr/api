package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/apimgr/api/src/paths"
)

// feed is one downloadable security database file. Per AI.md PART 7 these
// databases are never embedded in the binary - they are downloaded to
// {data_dir}/security/ on first run and kept fresh by the scheduler.
type feed struct {
	// name is the human-readable feed name used in log lines.
	name string
	// url is the upstream source.
	url string
	// dest is the path relative to {data_dir}/security/ the file is saved to.
	dest string
	// token is an optional bearer token sent with the request, used by the
	// OCI-hosted Trivy database.
	token string
}

// trivyRegistry, trivyRepository and trivyTag locate the OCI-published Trivy
// vulnerability database refreshed by the cve_update task.
const (
	trivyRegistry   = "ghcr.io"
	trivyRepository = "aquasecurity/trivy-db"
	trivyTag        = "2"
)

// blocklistFeeds are the IP and domain blocklists refreshed by the
// blocklist_update task.
var blocklistFeeds = []feed{
	{
		name: "FireHOL level 1 IP blocklist",
		url:  "https://raw.githubusercontent.com/firehol/blocklist-ipsets/master/firehol_level1.netset",
		dest: "blocklists/firehol_level1.netset",
	},
	{
		name: "StevenBlack unified domain blocklist",
		url:  "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
		dest: "blocklists/domains.hosts",
	},
}

// cveFeeds are the NVD JSON 2.0 vulnerability feeds refreshed by the
// cve_update task. The "recent" feed carries newly published CVEs and
// "modified" carries recently changed ones; both are republished by NIST
// every two hours.
var cveFeeds = []feed{
	{
		name: "NVD recent CVE feed",
		url:  "https://nvd.nist.gov/feeds/json/cve/2.0/nvdcve-2.0-recent.json.gz",
		dest: "cve/nvdcve-2.0-recent.json.gz",
	},
	{
		name: "NVD modified CVE feed",
		url:  "https://nvd.nist.gov/feeds/json/cve/2.0/nvdcve-2.0-modified.json.gz",
		dest: "cve/nvdcve-2.0-modified.json.gz",
	},
}

// feedDownloadTimeout bounds a single feed download.
const feedDownloadTimeout = 10 * time.Minute

// blocklistUpdateTask refreshes the IP and domain blocklists.
func blocklistUpdateTask() error {
	log.Println("Scheduler: Updating IP/domain blocklists...")
	return downloadFeeds("blocklist", blocklistFeeds)
}

// cveUpdateTask refreshes the NVD CVE feeds and the Trivy vulnerability
// database. PART 18 gives Trivy no scheduled task of its own - cve_update
// owns it - so a Trivy failure is logged but only fails the task when the
// NVD refresh also failed.
func cveUpdateTask() error {
	log.Println("Scheduler: Updating CVE database...")

	cveErr := downloadFeeds("CVE", cveFeeds)

	if err := downloadTrivyDB(); err != nil {
		log.Printf("Scheduler: Trivy database update failed: %v", err)
		if cveErr != nil {
			return cveErr
		}
		return nil
	}

	log.Println("Scheduler: Trivy database updated")
	return cveErr
}

// downloadTrivyDB pulls the Trivy vulnerability database from its OCI
// registry into {data_dir}/security/trivy/. The database is published only
// as an OCI artifact, so the anonymous pull token, manifest and layer blob
// are fetched directly rather than through a plain file download.
func downloadTrivyDB() error {
	ctx, cancel := context.WithTimeout(context.Background(), feedDownloadTimeout)
	defer cancel()

	token, err := trivyPullToken(ctx)
	if err != nil {
		return err
	}

	digest, err := trivyLayerDigest(ctx, token)
	if err != nil {
		return err
	}

	target := filepath.Join(paths.DataDir(), "security", "trivy", "db.tar.gz")
	blobURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", trivyRegistry, trivyRepository, digest)

	return downloadFeed(feed{
		name:  "Trivy vulnerability database",
		url:   blobURL,
		token: token,
	}, target)
}

// trivyPullToken obtains an anonymous pull token for the Trivy DB repository.
func trivyPullToken(ctx context.Context) (string, error) {
	tokenURL := fmt.Sprintf("https://%s/token?scope=repository:%s:pull&service=%s", trivyRegistry, trivyRepository, trivyRegistry)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request pull token: %w", err)
	}
	defer resp.Body.Close()

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
func trivyLayerDigest(ctx context.Context, token string) (string, error) {
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", trivyRegistry, trivyRepository, trivyTag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("build manifest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch manifest: unexpected status %s", resp.Status)
	}

	var manifest struct {
		Layers []struct {
			Digest string `json:"digest"`
		} `json:"layers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return "", fmt.Errorf("decode manifest: %w", err)
	}
	if len(manifest.Layers) == 0 || manifest.Layers[0].Digest == "" {
		return "", fmt.Errorf("manifest contains no database layer")
	}

	return manifest.Layers[0].Digest, nil
}

// downloadFeeds fetches every feed into {data_dir}/security/. A single
// source failing is logged and tolerated (graceful degradation, AI.md PART
// 7); the task only fails when no source could be refreshed, so the retry
// and notification policy of PART 18 applies to a genuine outage rather than
// to one flaky mirror.
func downloadFeeds(kind string, feeds []feed) error {
	securityDir := filepath.Join(paths.DataDir(), "security")

	var (
		succeeded int
		lastErr   error
	)

	for _, f := range feeds {
		target := filepath.Join(securityDir, filepath.FromSlash(f.dest))
		if err := downloadFeed(f, target); err != nil {
			lastErr = err
			log.Printf("Scheduler: %s update failed for %s: %v", kind, f.name, err)
			continue
		}
		succeeded++
		log.Printf("Scheduler: %s updated (%s)", f.name, target)
	}

	if succeeded == 0 {
		return fmt.Errorf("%s update failed for all %d sources: %w", kind, len(feeds), lastErr)
	}

	log.Printf("Scheduler: %s update completed (%d/%d sources refreshed)", kind, succeeded, len(feeds))
	return nil
}

// downloadFeed fetches one feed to target, writing through a temporary file
// so a partial download never replaces a good database.
func downloadFeed(f feed, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), feedDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "api/scheduler")
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: unexpected status %s", resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	written, err := io.Copy(tmp, resp.Body)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if written == 0 {
		return fmt.Errorf("download produced an empty file")
	}

	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("install: %w", err)
	}

	return nil
}
