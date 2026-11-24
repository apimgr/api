// Package update implements the AI.md PART 22 self-update system: querying
// GitHub Releases for the cumulative stable, beta and devel channels,
// defer_days-gated eligibility for the scheduled update_check task,
// mandatory SHA256 checksum verification, and platform-specific binary
// replacement (see update_unix.go / update_windows.go).
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apimgr/api/src/config"
)

// releasesRepo is the GitHub repository queried for release metadata.
const releasesRepo = "apimgr/api"

// apiBaseURL is the GitHub API base URL. It is a variable (rather than a
// constant baked into each request) solely so tests can point it at an
// httptest server instead of the real network.
var apiBaseURL = "https://api.github.com"

var restartPending atomic.Bool
var startupFailureMu sync.Mutex
var startupFailureVersion string
var startupFailureReason string

type appliedUpdate struct {
	PreviousVersion string `json:"previous_version"`
	NewVersion      string `json:"new_version"`
}

// TakeStartupFailure consumes the staged version whose startup application
// failed, allowing the initialized database to record the required audit row.
func TakeStartupFailure() (string, string) {
	startupFailureMu.Lock()
	defer startupFailureMu.Unlock()
	version, reason := startupFailureVersion, startupFailureReason
	startupFailureVersion = ""
	startupFailureReason = ""
	return version, reason
}

// TakeStartupSuccess consumes the update marker left before a successful
// privileged replacement so startup can emit the completion audit/event.
func TakeStartupSuccess(dataDir string) (string, string, error) {
	path := filepath.Join(dataDir, "update-applied.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	var applied appliedUpdate
	if err := json.Unmarshal(data, &applied); err != nil {
		return "", "", fmt.Errorf("read update completion marker: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", "", fmt.Errorf("remove update completion marker: %w", err)
	}
	return applied.PreviousVersion, applied.NewVersion, nil
}

// SanitizeUpdateFailure converts an internal update error into a short
// operator-facing class without exposing filesystem paths, URLs, or secrets.
func SanitizeUpdateFailure(err error) string {
	if err == nil {
		return "unknown update failure"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "checksum") || strings.Contains(message, "sha256"):
		return "release checksum verification failed"
	case strings.Contains(message, "permission") || strings.Contains(message, "access is denied"):
		return "insufficient permission to apply the update"
	case strings.Contains(message, "rename") || strings.Contains(message, "replace"):
		return "binary replacement failed"
	case strings.Contains(message, "download") || strings.Contains(message, "fetch") || strings.Contains(message, "github"):
		return "release download or verification failed"
	default:
		return "staged update could not be applied"
	}
}

func discardStaged(updateDir, version string, cause error) (bool, error) {
	if err := os.RemoveAll(updateDir); err != nil {
		cause = errors.Join(cause, fmt.Errorf("remove failed staged update: %w", err))
	}
	if version != "" {
		startupFailureMu.Lock()
		startupFailureVersion = version
		startupFailureReason = cause.Error()
		startupFailureMu.Unlock()
	}
	return true, cause
}

// RestartPending reports whether the scheduler staged an update and asked
// the server to shut down with EX_TEMPFAIL so its supervisor restarts it.
func RestartPending() bool { return restartPending.Load() }

// RequestRestart marks the process for the staged-update exit path.
func RequestRestart() { restartPending.Store(true) }

// Release-channel names accepted by SetBranch and the --update branch
// command (AI.md PART 22 "Update Branches").
const (
	BranchStable = "stable"
	BranchBeta   = "beta"
	BranchDevel  = "devel"
	BranchDaily  = BranchDevel
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
	_, ok := NormalizeBranch(branch)
	return ok
}

// NormalizeBranch maps documented rolling-channel aliases to their canonical
// stored name, devel.
func NormalizeBranch(branch string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(branch)) {
	case BranchStable:
		return BranchStable, true
	case BranchBeta:
		return BranchBeta, true
	case BranchDevel, "daily", "rolling", "dev", "development":
		return BranchDevel, true
	default:
		return "", false
	}
}

// SetBranch validates branch and assigns it to cfg.Branch. server.yml
// remains the single source of truth for the selected update channel - the
// caller is responsible for persisting cfg via config.Save.
func SetBranch(cfg *config.UpdateConfig, branch string) error {
	canonical, ok := NormalizeBranch(branch)
	if !ok {
		return fmt.Errorf("invalid update channel %q: must be stable, beta, or devel", branch)
	}
	cfg.Branch = canonical
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
	canonical, ok := NormalizeBranch(branch)
	if !ok {
		return nil, fmt.Errorf("invalid update channel %q", branch)
	}
	branch = canonical
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
		if r.TagName == "devel" {
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
	canonical, ok := NormalizeBranch(branch)
	if !ok {
		return nil, fmt.Errorf("invalid update channel %q", branch)
	}
	branch = canonical
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
		if r.TagName == "devel" {
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

func Install(ctx context.Context, release *Release) (retErr error) {
	if release == nil {
		return fmt.Errorf("release is required")
	}
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
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			retErr = errors.Join(retErr, fmt.Errorf("remove temporary update: %w", err))
		}
	}()

	if err := downloadTo(ctx, tmpFile, downloadURL); err != nil {
		return errors.Join(err, tmpFile.Close())
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close downloaded update: %w", err)
	}

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

type stagedUpdate struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// Stage downloads and verifies a release into the data directory for the
// next privileged startup to apply. The running executable is never touched.
func Stage(ctx context.Context, release *Release, dataDir string) (retErr error) {
	if release == nil || release.TagName == "" {
		return fmt.Errorf("release version is required")
	}
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
	updateDir := filepath.Join(dataDir, "update")
	if err := os.MkdirAll(updateDir, 0700); err != nil {
		return fmt.Errorf("create update directory: %w", err)
	}
	if err := requireDirectory(updateDir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(updateDir, ".api.new-*")
	if err != nil {
		return fmt.Errorf("create staged update: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			retErr = errors.Join(retErr, fmt.Errorf("remove temporary staged binary: %w", err))
		}
	}()
	if err := downloadTo(ctx, tmp, downloadURL); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close staged update: %w", err)
	}
	hash, err := fetchExpectedChecksum(ctx, release, assetName)
	if err != nil {
		return fmt.Errorf("fetch update checksum: %w", err)
	}
	if err := verifyChecksum(tmpPath, hash); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0755); err != nil {
			return fmt.Errorf("set staged update permissions: %w", err)
		}
	}
	binaryPath := filepath.Join(updateDir, "api.new")
	if err := replaceStagedFile(tmpPath, binaryPath); err != nil {
		return err
	}
	metadata, err := json.Marshal(stagedUpdate{Version: release.TagName, SHA256: hash})
	if err != nil {
		return err
	}
	if err := writePrivateFile(filepath.Join(updateDir, "api.new.json"), metadata); err != nil {
		return errors.Join(err, os.Remove(binaryPath))
	}
	return nil
}

// ApplyStaged verifies and atomically installs a staged update. Call only
// before privilege drop; false means no staged update is waiting.
func ApplyStaged(dataDir, currentVersion string) (didStage bool, retErr error) {
	updateDir := filepath.Join(dataDir, "update")
	metadataPath := filepath.Join(updateDir, "api.new.json")
	if _, err := os.Lstat(updateDir); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return true, err
	}
	if err := requireDirectory(updateDir); err != nil {
		return true, err
	}
	if err := requireRegularFile(metadataPath); err != nil && !os.IsNotExist(err) {
		return true, err
	}
	data, err := os.ReadFile(metadataPath)
	if os.IsNotExist(err) {
		if cleanupErr := os.RemoveAll(updateDir); cleanupErr != nil {
			return true, cleanupErr
		}
		return false, nil
	}
	if err != nil {
		return true, err
	}
	var staged stagedUpdate
	if err := json.Unmarshal(data, &staged); err != nil {
		return discardStaged(updateDir, "", fmt.Errorf("invalid staged update metadata: %w", err))
	}
	binaryPath := filepath.Join(updateDir, "api.new")
	if err := requireRegularFile(binaryPath); err != nil {
		return discardStaged(updateDir, staged.Version, err)
	}
	if staged.Version == "" || staged.SHA256 == "" {
		return discardStaged(updateDir, "", fmt.Errorf("staged update metadata is incomplete"))
	}
	// The staging directory is writable by the dropped service account. Fetch
	// the release checksum again so a modified binary and sidecar cannot be
	// promoted together by a compromised service account.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	releases, err := fetchReleaseList(ctx, currentVersion)
	if err != nil {
		return true, fmt.Errorf("fetch staged release for verification: %w", err)
	}
	var release *Release
	for i := range releases {
		if releases[i].TagName == staged.Version {
			release = &releases[i]
			break
		}
	}
	if release == nil {
		return discardStaged(updateDir, staged.Version, fmt.Errorf("staged version %q is no longer present in the release feed", staged.Version))
	}
	expectedHash, err := fetchExpectedChecksum(ctx, release, binaryAssetName())
	if err != nil || !strings.EqualFold(expectedHash, staged.SHA256) {
		if err != nil {
			return true, fmt.Errorf("fetch authoritative staged checksum: %w", err)
		}
		return discardStaged(updateDir, staged.Version, fmt.Errorf("staged checksum does not match the release checksum"))
	}
	if err := verifyChecksum(binaryPath, staged.SHA256); err != nil {
		return discardStaged(updateDir, staged.Version, fmt.Errorf("staged update verification failed: %w", err))
	}
	if !isNewerRelease(staged.Version, currentVersion) {
		return discardStaged(updateDir, staged.Version, fmt.Errorf("staged version %q is not newer than %q", staged.Version, currentVersion))
	}
	exe, err := os.Executable()
	if err != nil {
		return true, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return true, err
	}
	// Copy from the service-user-writable staging tree into a root-owned
	// temporary file beside the executable. Re-hash the copied bytes before
	// replacement, so a path swap between verification and rename cannot
	// redirect the privileged replacement to a symlink or different file.
	stagedFile, err := os.Open(binaryPath)
	if err != nil {
		return discardStaged(updateDir, staged.Version, err)
	}
	installFile, err := os.CreateTemp(filepath.Dir(exe), ".api-update-*")
	if err != nil {
		return true, errors.Join(err, stagedFile.Close())
	}
	installPath := installFile.Name()
	defer func() {
		if err := os.Remove(installPath); err != nil && !os.IsNotExist(err) {
			retErr = errors.Join(retErr, fmt.Errorf("remove temporary installed binary: %w", err))
		}
	}()
	_, copyErr := io.Copy(installFile, stagedFile)
	closeStageErr := stagedFile.Close()
	closeInstallErr := installFile.Close()
	if err := errors.Join(copyErr, closeStageErr, closeInstallErr); err != nil {
		return discardStaged(updateDir, staged.Version, err)
	}
	if err := verifyChecksum(installPath, staged.SHA256); err != nil {
		return discardStaged(updateDir, staged.Version, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(installPath, 0755); err != nil {
			return discardStaged(updateDir, staged.Version, err)
		}
	}
	markerPath := filepath.Join(dataDir, "update-applied.json")
	marker, err := json.Marshal(appliedUpdate{PreviousVersion: currentVersion, NewVersion: staged.Version})
	if err != nil {
		return discardStaged(updateDir, staged.Version, err)
	}
	if err := writeAtomicPrivateFile(markerPath, marker); err != nil {
		return discardStaged(updateDir, staged.Version, err)
	}
	if err := replaceBinary(exe, installPath); err != nil {
		if removeErr := os.Remove(markerPath); removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, removeErr)
		}
		return discardStaged(updateDir, staged.Version, err)
	}
	if err := os.RemoveAll(updateDir); err != nil {
		return true, fmt.Errorf("remove staged update directory: %w", err)
	}
	return true, Restart()
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("update path is not a real directory")
	}
	return nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("staged update file is not a regular file")
	}
	return nil
}

func isNewerRelease(candidate, current string) bool {
	clean := func(s string) string { return strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V") }
	if candidate == "devel" {
		return current != "devel"
	}
	if current == "devel" {
		return false
	}
	a, b := strings.Split(clean(candidate), "."), strings.Split(clean(current), ".")
	for i := 0; i < len(a) || i < len(b); i++ {
		var av, bv int
		if i < len(a) {
			var err error
			av, err = strconv.Atoi(strings.SplitN(a[i], "-", 2)[0])
			if err != nil {
				return false
			}
		}
		if i < len(b) {
			var err error
			bv, err = strconv.Atoi(strings.SplitN(b[i], "-", 2)[0])
			if err != nil {
				return false
			}
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}

func replaceStagedFile(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove previous staged update: %w", err)
	}
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("publish staged update: %w", err)
	}
	return nil
}

func writePrivateFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

func writeAtomicPrivateFile(path string, content []byte) (retErr error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".update-marker-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			retErr = errors.Join(retErr, fmt.Errorf("remove temporary update marker: %w", err))
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if _, err := tmp.Write(content); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceStagedFile(tmpPath, path)
}

// downloadTo streams downloadURL's body into dst.
func downloadTo(ctx context.Context, dst io.Writer, downloadURL string) (retErr error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "api-update-client")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, resp.Body.Close()) }()

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
func fetchExpectedChecksum(ctx context.Context, release *Release, assetName string) (checksum string, retErr error) {
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
	defer func() { retErr = errors.Join(retErr, resp.Body.Close()) }()
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
func verifyChecksum(filePath, expectedHash string) (retErr error) {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()

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
	// The devel channel is a single rolling release: tag "devel", rebuilt
	// nightly.
	isDaily := r.TagName == "devel"
	switch branch {
	case BranchBeta:
		return isBeta
	case BranchDevel:
		return isBeta || isDaily
	default:
		return false
	}
}

// fetchLatestStableRelease queries the GitHub Releases "latest" endpoint,
// used only for the stable channel's manual-check fast path. currentVersion
// is unused beyond documenting intent; callers compare TagName themselves.
func fetchLatestStableRelease(ctx context.Context, currentVersion string) (releaseResult *Release, retErr error) {
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
	defer func() { retErr = errors.Join(retErr, resp.Body.Close()) }()

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
func fetchReleaseList(ctx context.Context, currentVersion string) (releaseResults []Release, retErr error) {
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
	defer func() { retErr = errors.Join(retErr, resp.Body.Close()) }()

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

// IsNewerVersion compares release versions using the same stable/beta/devel
// ordering as the server updater.
func IsNewerVersion(candidate, current string) bool {
	return isNewerRelease(candidate, current)
}

// VerifyChecksum exposes the mandatory SHA-256 verifier to companion binaries.
func VerifyChecksum(filePath, expectedHash string) error {
	return verifyChecksum(filePath, expectedHash)
}

// ReplaceBinary installs a verified staged binary using a same-filesystem
// temporary file so atomic replacement works when the download lives in the
// system temporary directory.
func ReplaceBinary(currentPath, stagedPath string) (retErr error) {
	info, err := os.Stat(currentPath)
	if err != nil {
		return fmt.Errorf("cannot stat installed binary: %w", err)
	}
	dir := filepath.Dir(currentPath)
	staged, err := os.Open(stagedPath)
	if err != nil {
		return fmt.Errorf("cannot open staged binary: %w", err)
	}
	defer func() {
		if err := staged.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close staged binary: %w", err))
		}
	}()
	tmp, err := os.CreateTemp(dir, ".api-update-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot stage binary beside install path: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			retErr = errors.Join(retErr, fmt.Errorf("remove temporary update binary: %w", err))
		}
	}()
	if err := tmp.Chmod(info.Mode()); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, staged); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceBinary(currentPath, tmpPath)
}
