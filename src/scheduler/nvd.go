package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/requestid"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
)

const (
	nvdPageSize       = 2000
	nvdMaxPageBytes   = 64 << 20
	nvdUpdateWindow   = 120 * 24 * time.Hour
	nvdDefaultAPIURL  = "https://services.nvd.nist.gov/rest/json/cves/2.0"
	nvdUnkeyedSpacing = 6 * time.Second
	nvdKeyedSpacing   = 600 * time.Millisecond
)

var errNVDRetryNextRun = errors.New("NVD request was rate limited; it will be retried on the next scheduled run")

type nvdVulnerabilityPage struct {
	TotalResults    int `json:"totalResults"`
	Vulnerabilities []struct {
		CVE json.RawMessage `json:"cve"`
	} `json:"vulnerabilities"`
}

type nvdRequestLimiter struct {
	minimumSpacing time.Duration
	lastRequest    time.Time
}

func updateNVD(ctx context.Context, cfg *config.ServerConfig, client *http.Client, now time.Time) error {
	return updateNVDWithMinimumSpacing(ctx, cfg, client, now, 0)
}

func updateNVDWithMinimumSpacing(ctx context.Context, cfg *config.ServerConfig, client *http.Client, now time.Time, minimumSpacing time.Duration) error {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if client == nil {
		client = http.DefaultClient
	}
	apiURL, apiKey, securityDir := nvdConfiguration(cfg)
	parsedURL, err := url.Parse(apiURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return fmt.Errorf("invalid NVD source URL; an HTTPS endpoint is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	directory := filepath.Join(securityDir, "cve")
	nvdPath := filepath.Join(directory, "nvd.json")
	lastUpdatedPath := filepath.Join(directory, ".last_updated")
	records, hasRecords, err := readNVDRecords(nvdPath)
	if err != nil {
		return err
	}
	var lastUpdated time.Time
	if hasRecords {
		stamp, stampErr := os.ReadFile(lastUpdatedPath)
		if stampErr == nil {
			lastUpdated, err = time.Parse(time.RFC3339, strings.TrimSpace(string(stamp)))
			if err != nil {
				return fmt.Errorf("parse NVD last-updated marker: %w", err)
			}
		} else if !errors.Is(stampErr, os.ErrNotExist) {
			return fmt.Errorf("read NVD last-updated marker: %w", stampErr)
		}
	}
	if !hasRecords || lastUpdated.IsZero() {
		records = make(map[string]json.RawMessage)
	}

	if minimumSpacing == 0 {
		minimumSpacing = nvdUnkeyedSpacing
		if apiKey != "" {
			minimumSpacing = nvdKeyedSpacing
		}
	}
	limiter := &nvdRequestLimiter{minimumSpacing: minimumSpacing}
	if lastUpdated.IsZero() {
		if err := fetchNVDWindow(ctx, client, apiURL, apiKey, limiter, nil, now, records); err != nil {
			return err
		}
	} else {
		for start := lastUpdated.UTC(); start.Before(now); {
			end := start.Add(nvdUpdateWindow)
			if end.After(now) {
				end = now
			}
			if err := fetchNVDWindow(ctx, client, apiURL, apiKey, limiter, &start, end, records); err != nil {
				return err
			}
			start = end
		}
	}
	if len(records) == 0 {
		return fmt.Errorf("NVD response contained no CVE records")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create CVE directory: %w", err)
	}
	encoded, err := marshalNVDRecords(records)
	if err != nil {
		return fmt.Errorf("encode NVD CVE records: %w", err)
	}
	if err := writeSecurityFileAtomically(nvdPath, encoded, 0o644); err != nil {
		return fmt.Errorf("install NVD CVE records: %w", err)
	}
	marker := []byte(now.Format(time.RFC3339) + "\n")
	if err := writeSecurityFileAtomically(lastUpdatedPath, marker, 0o644); err != nil {
		return fmt.Errorf("write NVD update marker: %w", err)
	}
	return nil
}

func nvdConfiguration(cfg *config.ServerConfig) (string, string, string) {
	securityDir := filepath.Join(paths.DataDir(), "security")
	apiURL := nvdDefaultAPIURL
	apiKey := ""
	data := cfg.Data
	if configured, ok := data["security_dir"].(string); ok && strings.TrimSpace(configured) != "" {
		securityDir = filepath.Clean(strings.TrimSpace(configured))
	}
	if cve, ok := data["cve"].(map[string]interface{}); ok {
		if configured, ok := cve["source"].(string); ok && strings.TrimSpace(configured) != "" {
			apiURL = strings.TrimSpace(configured)
		}
		if configured, ok := cve["api_key"].(string); ok {
			apiKey = configured
		}
	}
	return apiURL, apiKey, securityDir
}

func fetchNVDWindow(ctx context.Context, client *http.Client, endpoint, apiKey string, limiter *nvdRequestLimiter, start *time.Time, end time.Time, records map[string]json.RawMessage) error {
	startIndex := 0
	for {
		if err := limiter.wait(ctx); err != nil {
			return err
		}
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return fmt.Errorf("parse NVD source URL: %w", err)
		}
		query := parsed.Query()
		query.Set("resultsPerPage", fmt.Sprint(nvdPageSize))
		query.Set("startIndex", fmt.Sprint(startIndex))
		if start != nil {
			query.Set("lastModStartDate", start.UTC().Format(time.RFC3339))
			query.Set("lastModEndDate", end.UTC().Format(time.RFC3339))
		}
		parsed.RawQuery = query.Encode()
		requestCtx, cancel := context.WithTimeout(ctx, time.Minute)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			cancel()
			return fmt.Errorf("build NVD request: %w", err)
		}
		request.Header.Set("User-Agent", "api/scheduler")
		request.Header.Set("Accept", "application/json")
		if apiKey != "" {
			request.Header.Set("apiKey", apiKey)
		}
		response, err := requestid.DoOutboundRequestWithID(client, request)
		if err != nil {
			cancel()
			return fmt.Errorf("request NVD API: %w", err)
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			_ = response.Body.Close()
			cancel()
			return fmt.Errorf("%w (HTTP %d)", errNVDRetryNextRun, response.StatusCode)
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			cancel()
			return fmt.Errorf("NVD API returned HTTP %d", response.StatusCode)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, nvdMaxPageBytes+1))
		closeErr := response.Body.Close()
		cancel()
		if readErr != nil {
			return fmt.Errorf("read NVD page: %w", readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close NVD response: %w", closeErr)
		}
		if len(body) > nvdMaxPageBytes {
			return fmt.Errorf("NVD page exceeded the maximum response size")
		}
		var page nvdVulnerabilityPage
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("decode NVD page: %w", err)
		}
		for _, item := range page.Vulnerabilities {
			var identifier struct {
				ID string `json:"id"`
			}
			if len(item.CVE) == 0 || json.Unmarshal(item.CVE, &identifier) != nil || identifier.ID == "" {
				return fmt.Errorf("NVD page contains a CVE record without an id")
			}
			records[identifier.ID] = append(json.RawMessage(nil), item.CVE...)
		}
		startIndex += len(page.Vulnerabilities)
		if startIndex >= page.TotalResults {
			return nil
		}
		if len(page.Vulnerabilities) == 0 {
			return fmt.Errorf("NVD pagination made no progress at startIndex %d", startIndex)
		}
	}
}

func (limiter *nvdRequestLimiter) wait(ctx context.Context) error {
	if limiter.minimumSpacing <= 0 {
		limiter.lastRequest = time.Now()
		return nil
	}
	if !limiter.lastRequest.IsZero() {
		remaining := limiter.minimumSpacing - time.Since(limiter.lastRequest)
		if remaining > 0 {
			timer := time.NewTimer(remaining)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	limiter.lastRequest = time.Now()
	return nil
}

func readNVDRecords(path string) (map[string]json.RawMessage, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]json.RawMessage), false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read existing NVD records: %w", err)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, false, fmt.Errorf("decode existing NVD records: %w", err)
	}
	records := make(map[string]json.RawMessage, len(values))
	for _, value := range values {
		var identifier struct {
			ID string `json:"id"`
		}
		if len(value) == 0 || json.Unmarshal(value, &identifier) != nil || identifier.ID == "" {
			return nil, false, fmt.Errorf("existing NVD file contains an invalid CVE record")
		}
		records[identifier.ID] = value
	}
	return records, true, nil
}

func marshalNVDRecords(records map[string]json.RawMessage) ([]byte, error) {
	identifiers := make([]string, 0, len(records))
	for identifier := range records {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	values := make([]json.RawMessage, 0, len(identifiers))
	for _, identifier := range identifiers {
		values = append(values, records[identifier])
	}
	return json.MarshalIndent(values, "", "  ")
}

func writeSecurityFileAtomically(path string, contents []byte, permissions os.FileMode) (retErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if removeErr := os.Remove(temporaryPath); retErr == nil && removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			retErr = removeErr
		}
	}()
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(permissions); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceSecurityFileAtomically(temporaryPath, path)
}
