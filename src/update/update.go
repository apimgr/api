// Package update implements the AI.md PART 22 self-update system: querying
// GitHub Releases for the three cumulative release channels (stable, beta,
// daily), defer_days-gated eligibility for the scheduled update_check task,
// mandatory SHA256 checksum verification, and platform-specific binary
// replacement (see update_unix.go / update_windows.go).
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/apimgr/api/src/config"
)

// releasesRepo is the GitHub repository queried for release metadata.
const releasesRepo = "apimgr/api"

// apiBaseURL is the GitHub API base URL. It is a variable (rather than a
// constant baked into each request) solely so tests can point it at an
// httptest server instead of the real network.
var apiBaseURL = "https://api.github.com"

// Release-channel names accepted by SetBranch and the --update branch
// command (AI.md PART 22 "Update Branches").
const (
	BranchStable = "stable"
	BranchBeta   = "beta"
	BranchDaily  = "daily"
)

// Release mirrors the subset of the GitHub Releases API response used to
// select and download an update.
type Release struct {
	TagName     string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

// Asset is a single downloadable file attached to a GitHub release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// ValidBranch reports whether branch is one of the three supported update
// channels.
func ValidBranch(branch string) bool {
	switch branch {
	case BranchStable, BranchBeta, BranchDaily:
		return true
	}
	return false
}

// SetBranch validates branch and assigns it to cfg.Branch. server.yml
// remains the single source of truth for the selected update channel - the
// caller is responsible for persisting cfg via config.Save.
func SetBranch(cfg *config.UpdateConfig, branch string) error {
	if !ValidBranch(branch) {
		return fmt.Errorf("invalid update channel %q: must be one of stable, beta, daily", branch)
	}
	cfg.Branch = branch
	return nil
}

// CheckForUpdate checks GitHub releases for the newest update on branch,
// ignoring defer_days entirely. This is the manual-check path: AI.md PART 22
// "Defer Semantics" requires that manual "--update check"/"--update yes"
// always see and install the true latest release, ignoring defer_days.
//
// buildEpoch is the caller's own embedded build timestamp (Unix seconds),
// used to detect a newer nightly on the rolling "daily" tag. A nil Release
// with a nil error means the running version is already current.
func CheckForUpdate(ctx context.Context, currentVersion, branch string, buildEpoch int64) (*Release, error) {
	if branch == BranchStable {
		release, err := fetchLatestStableRelease(ctx, currentVersion)
		if err != nil {
			return nil, err
		}
		if release == nil || release.TagName == currentVersion {
			return nil, nil
		}
		return release, nil
	}

	releases, err := fetchReleaseList(ctx, currentVersion)
	if err != nil {
		return nil, err
	}

	// Releases are returned newest-first; channels are cumulative, so the
	// first match is the newest release across the channel and all
	// more-stable channels (beta users are never stuck behind a stable
	// release).
	for _, r := range releases {
		if !matchesBranch(r, branch) {
			continue
		}
		if r.TagName == "daily" {
			// Rolling tag: a newer nightly exists when the release was
			// published after this binary was built.
			if r.PublishedAt.Unix() > buildEpoch {
				return &r, nil
			}
			continue
		}
		if r.TagName != currentVersion {
			return &r, nil
		}
	}
	return nil, nil
}

// CheckEligible checks GitHub releases for the newest release on branch that
// is both newer than currentVersion and eligible under deferDays (AI.md PART
// 22 "Defer Semantics": a release is only eligible once
// now - published_at >= defer_days). This is the scheduled update_check task
// path only - manual checks must call CheckForUpdate instead to bypass the
// defer window, per an explicit operator action overriding it.
//
// Eligibility is evaluated per-release: the newest eligible release newer
// than currentVersion is returned, so a brand-new release never resets the
// clock for an older release that has already aged past the window.
func CheckEligible(ctx context.Context, currentVersion, branch string, buildEpoch int64, deferDays int) (*Release, error) {
	releases, err := fetchReleaseList(ctx, currentVersion)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	deferWindow := time.Duration(deferDays) * 24 * time.Hour

	for _, r := range releases {
		if !matchesBranch(r, branch) {
			continue
		}
		if r.TagName == "daily" {
			if r.PublishedAt.Unix() <= buildEpoch {
				continue
			}
		} else if r.TagName == currentVersion {
			continue
		}
		if deferDays > 0 && now.Sub(r.PublishedAt.UTC()) < deferWindow {
			continue
		}
		return &r, nil
	}
	return nil, nil
}

// Install downloads release's binary for the running platform, verifies its
// SHA256 checksum against the release's sha256.txt asset (mandatory - never
// skipped), and replaces the currently running binary. The caller is
// responsible for restarting the process/service afterward via Restart or
// RestartService.
func Install(ctx context.Context, release *Release) error {
	assetName := binaryAssetName()
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	tmpFile, err := os.CreateTemp("", "api-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	// Clean up on any early return; a successful replaceBinary moves the
	// file away so this becomes a harmless no-op.
	defer os.Remove(tmpPath)

	if err := downloadTo(ctx, tmpFile, downloadURL); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	expectedHash, err := fetchExpectedChecksum(ctx, release, assetName)
	if err != nil {
		return fmt.Errorf("failed to fetch checksum: %w", err)
	}
	if err := verifyChecksum(tmpPath, expectedHash); err != nil {
		return err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0755); err != nil {
			return fmt.Errorf("failed to set permissions: %w", err)
		}
	}

	currentPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	return replaceBinary(currentPath, tmpPath)
}

// downloadTo streams downloadURL's body into dst.
func downloadTo(ctx context.Context, dst io.Writer, downloadURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "api-update-client")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %d", resp.StatusCode)
	}
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	return nil
}

// binaryAssetName returns the expected release asset name for this
// platform, matching the Makefile binary-naming convention
// "{project_name}-{os}-{arch}[.exe]".
func binaryAssetName() string {
	name := "api-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// fetchExpectedChecksum downloads the release's sha256.txt asset and
// returns the SHA256 hash recorded for assetName.
func fetchExpectedChecksum(ctx context.Context, release *Release, assetName string) (string, error) {
	var checksumsURL string
	for _, asset := range release.Assets {
		if asset.Name == "sha256.txt" {
			checksumsURL = asset.BrowserDownloadURL
			break
		}
	}
	if checksumsURL == "" {
		return "", fmt.Errorf("release has no sha256.txt asset")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "api-update-client")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum download failed: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	// Each line is "{sha256}  {filename}".
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s", assetName)
}

// verifyChecksum verifies the SHA256 checksum of the file at filePath
// against expectedHash. Checksum verification is mandatory and is never
// skipped for a self-update install.
func verifyChecksum(filePath, expectedHash string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actualHash := hex.EncodeToString(h.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}

// matchesBranch implements the cumulative-channel rule: each channel also
// accepts every release from all more-stable channels.
func matchesBranch(r Release, branch string) bool {
	// Stable releases match every channel.
	if !r.Prerelease {
		return true
	}
	isBeta := strings.HasSuffix(r.TagName, "-beta")
	// The daily channel is a single rolling release: tag "daily", rebuilt
	// nightly.
	isDaily := r.TagName == "daily"
	switch branch {
	case BranchBeta:
		return isBeta
	case BranchDaily:
		return isBeta || isDaily
	default:
		return false
	}
}

// fetchLatestStableRelease queries the GitHub Releases "latest" endpoint,
// used only for the stable channel's manual-check fast path. currentVersion
// is unused beyond documenting intent; callers compare TagName themselves.
func fetchLatestStableRelease(ctx context.Context, currentVersion string) (*Release, error) {
	url := apiBaseURL + "/repos/" + releasesRepo + "/releases/latest"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "api-update-client/"+currentVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// No releases published at all - already current.
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

// fetchReleaseList queries the GitHub Releases list endpoint (newest
// first), used by every non-stable-fast-path lookup: beta/daily manual
// checks and every CheckEligible call, since defer_days eligibility
// requires scanning more than just the single newest release.
func fetchReleaseList(ctx context.Context, currentVersion string) ([]Release, error) {
	url := apiBaseURL + "/repos/" + releasesRepo + "/releases"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "api-update-client/"+currentVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}
