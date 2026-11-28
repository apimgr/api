// Package update implements the cli's server-advertised, checksum-verified self-update flow.
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
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	clientapi "github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/paths"
	"github.com/apimgr/api/src/common/requestid"
	"github.com/apimgr/api/src/common/sanitize"
	serverupdate "github.com/apimgr/api/src/update"
)

type Discovery struct {
	OK   bool `json:"ok"`
	Data struct {
		Version string `json:"version"`
		API     struct {
			CurrentVersion string `json:"current_version"`
		} `json:"api"`
		CLIVersions   map[string]Release `json:"cli_versions"`
		CLIMinVersion string             `json:"cli_min_version"`
	} `json:"data"`
}

type Release struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type Decision struct {
	Discovery       Discovery
	Release         Release
	Platform        string
	Available       bool
	Forced          bool
	VersionMismatch bool
}

func PlatformKey() string { return runtime.GOOS + "-" + runtime.GOARCH }

func Discover(ctx context.Context, baseURL, token string, httpClient *http.Client) (Discovery, error) {
	var result Discovery
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api/autodiscover")
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return result, errors.New("invalid server URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return result, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", clientapi.UserAgent)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := requestid.DoOutboundRequestWithID(httpClient, request)
	if err != nil {
		return result, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("autodiscover returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result); err != nil {
		return result, errors.New("invalid autodiscover response")
	}
	if !result.OK {
		return result, errors.New("autodiscover response was not successful")
	}
	return result, nil
}

func Check(ctx context.Context, baseURL, token, currentVersion string, httpClient *http.Client) (Decision, error) {
	discovery, err := Discover(ctx, baseURL, token, httpClient)
	if err != nil {
		return Decision{}, err
	}
	decision := Decision{
		Discovery:       discovery,
		Platform:        PlatformKey(),
		VersionMismatch: hasMajorVersionMismatch(currentVersion, discovery.Data.Version),
	}
	decision.Release, decision.Available = discovery.Data.CLIVersions[decision.Platform]
	decision.Forced = decision.Discovery.Data.CLIMinVersion != "" && serverupdate.IsNewerVersion(decision.Discovery.Data.CLIMinVersion, currentVersion)
	if decision.Available && !serverupdate.IsNewerVersion(decision.Release.Version, currentVersion) {
		decision.Available = false
	}
	return decision, nil
}

func Install(ctx context.Context, baseURL, token, currentVersion, apiVersion string, release Release, httpClient *http.Client, logFile string) error {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("CLI updates require an HTTPS server URL")
	}
	endpoint, err := cliBinaryEndpoint(baseURL, apiVersion, paths.ProjectName+"-cli-"+PlatformKey()+platformExtension())
	if err != nil {
		return err
	}
	platform := PlatformKey()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("invalid CLI download URL")
	}
	request.Header.Set("User-Agent", clientapi.UserAgent)
	request.Header.Set("Accept", "application/octet-stream")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	response, err := requestid.DoOutboundRequestWithID(httpClient, request)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.Request == nil || response.Request.URL == nil || response.Request.URL.Scheme != "https" {
		return errors.New("CLI update download did not remain on HTTPS")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("CLI download returned HTTP %d", response.StatusCode)
	}

	stagingDir, err := secureTempDir()
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()
	tmpPath := filepath.Join(stagingDir, "cli.update.tmp")
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), response.Body)
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, release.SHA256) {
		if auditErr := audit(logFile, "cli.update_checksum_invalid", map[string]string{"old_version": currentVersion, "new_version": release.Version, "os_arch": platform}); auditErr != nil {
			return fmt.Errorf("CLI update checksum verification failed; audit event could not be written: %w", auditErr)
		}
		return errors.New("CLI update checksum verification failed")
	}
	if err := serverupdate.VerifyChecksum(tmpPath, release.SHA256); err != nil {
		return errors.New("CLI update checksum verification failed")
	}
	currentPath, err := os.Executable()
	if err != nil {
		return errors.New("cannot locate installed CLI binary")
	}
	if err := writeCompletionMarker(paths.DataDir(), currentVersion, release.Version, platform); err != nil {
		return fmt.Errorf("cannot record CLI update completion: %w", err)
	}
	if err := serverupdate.ReplaceBinary(currentPath, tmpPath); err != nil {
		_ = os.Remove(filepath.Join(paths.DataDir(), "cli-update.json"))
		return fmt.Errorf("you do not have permission to update %s; ask your admin or move the binary to a writable path", currentPath)
	}
	return serverupdate.Restart()
}

var apiVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var semverMajorPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)([-+][0-9A-Za-z.-]+)?$`)

func hasMajorVersionMismatch(clientVersion, serverVersion string) bool {
	client := semverMajorPattern.FindStringSubmatch(clientVersion)
	server := semverMajorPattern.FindStringSubmatch(serverVersion)
	return len(client) == 5 && len(server) == 5 && client[1] != server[1]
}

func cliBinaryEndpoint(baseURL, apiVersion, filename string) (string, error) {
	if !apiVersionPattern.MatchString(apiVersion) {
		return "", errors.New("autodiscover returned an invalid API version")
	}
	return strings.TrimRight(baseURL, "/") + "/api/" + apiVersion + "/cli/binaries/" + url.PathEscape(filename), nil
}

func platformExtension() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

type completionMarker struct {
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	OSArch     string `json:"os_arch"`
}

func writeCompletionMarker(dir, oldVersion, newVersion, platform string) error {
	marker := completionMarker{OldVersion: oldVersion, NewVersion: newVersion, OSArch: platform}
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "cli-update.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func RecordCompletion(logFile string) error {
	path := filepath.Join(paths.DataDir(), "cli-update.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var marker completionMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return errors.New("invalid CLI update completion marker")
	}
	if err := audit(logFile, "cli.update_completed", map[string]string{"old_version": marker.OldVersion, "new_version": marker.NewVersion, "os_arch": marker.OSArch}); err != nil {
		return err
	}
	return os.Remove(path)
}

func LogForced(logFile, currentVersion, minimum string) error {
	return audit(logFile, "cli.update_forced", map[string]string{"current_version": currentVersion, "minimum_version": minimum, "os_arch": PlatformKey()})
}

func secureTempDir() (string, error) {
	base := filepath.Join(os.TempDir(), paths.OrgName)
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(base)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("unsafe CLI update temporary directory")
	}
	dir, err := os.MkdirTemp(base, paths.ProjectName+"-XXXXXX")
	if err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0700); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

func audit(logFile, event string, fields map[string]string) error {
	entry := map[string]interface{}{"timestamp": time.Now().UTC().Format(time.RFC3339), "event": sanitize.SanitizeLogText(event)}
	fieldValues := make(map[string]interface{}, len(fields))
	for key, value := range fields {
		fieldValues[key] = value
	}
	for key, value := range sanitize.SanitizeLogFields(fieldValues) {
		entry[key] = value
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if logFile == "" {
		logFile = paths.LogFile()
	}
	if err := os.MkdirAll(filepath.Dir(logFile), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stderr, string(data))
	return err
}
