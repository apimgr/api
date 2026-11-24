package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/require"
)

func TestUpdateNVDFullPaginationAndIncrementalWindows(t *testing.T) {
	now := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	queries := make([]map[string]string, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		queries = append(queries, map[string]string{
			"startIndex": query.Get("startIndex"), "resultsPerPage": query.Get("resultsPerPage"),
			"lastModStartDate": query.Get("lastModStartDate"), "lastModEndDate": query.Get("lastModEndDate"),
		})
		if r.Header.Get("apiKey") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		startIndex, _ := strconv.Atoi(query.Get("startIndex"))
		if query.Get("lastModStartDate") == "" {
			if startIndex == 0 {
				_, _ = fmt.Fprint(w, `{"totalResults":3,"vulnerabilities":[{"cve":{"id":"CVE-1","description":"old"}},{"cve":{"id":"CVE-2"}}]}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"totalResults":3,"vulnerabilities":[{"cve":{"id":"CVE-3"}}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"totalResults":1,"vulnerabilities":[{"cve":{"id":"CVE-1","description":"new"}}]}`)
	}))
	defer server.Close()

	securityDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Data["security_dir"] = securityDir
	cfg.Data["cve"] = map[string]interface{}{"source": server.URL, "api_key": "test-key", "filter_by_cpe": false}
	ctx := context.Background()
	require.NoError(t, updateNVDWithMinimumSpacing(ctx, cfg, server.Client(), now, -1))

	nvdPath := filepath.Join(securityDir, "cve", "nvd.json")
	data, err := os.ReadFile(nvdPath)
	require.NoError(t, err)
	var initial []map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &initial))
	require.Len(t, initial, 3)
	require.Equal(t, "CVE-1", initial[0]["id"])
	require.Equal(t, "0", queries[0]["startIndex"])
	require.Equal(t, "2000", queries[0]["resultsPerPage"])
	require.Equal(t, "2", queries[1]["startIndex"])

	previousUpdate := now.Add(-121 * 24 * time.Hour)
	require.NoError(t, os.WriteFile(filepath.Join(securityDir, "cve", ".last_updated"), []byte(previousUpdate.Format(time.RFC3339)+"\n"), 0o644))
	require.NoError(t, updateNVDWithMinimumSpacing(ctx, cfg, server.Client(), now, -1))
	require.Len(t, queries, 4, "a 121-day gap must be walked in two windows after the initial two pages")
	firstStart, err := time.Parse(time.RFC3339, queries[2]["lastModStartDate"])
	require.NoError(t, err)
	firstEnd, err := time.Parse(time.RFC3339, queries[2]["lastModEndDate"])
	require.NoError(t, err)
	require.Equal(t, 120*24*time.Hour, firstEnd.Sub(firstStart))
	secondStart, err := time.Parse(time.RFC3339, queries[3]["lastModStartDate"])
	require.NoError(t, err)
	require.Equal(t, firstEnd, secondStart)

	data, err = os.ReadFile(nvdPath)
	require.NoError(t, err)
	var updated []map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &updated))
	require.Len(t, updated, 3)
	require.Equal(t, "new", updated[0]["description"])
	marker, err := os.ReadFile(filepath.Join(securityDir, "cve", ".last_updated"))
	require.NoError(t, err)
	require.Equal(t, now.Format(time.RFC3339)+"\n", string(marker))
}

func TestNVDRateLimitRetriesOnNextScheduledRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
	}))
	defer server.Close()
	limiter := &nvdRequestLimiter{}
	err := fetchNVDWindow(context.Background(), server.Client(), server.URL, "", limiter, nil, time.Now(), map[string]json.RawMessage{})
	require.ErrorIs(t, err, errNVDRetryNextRun)
	require.NotContains(t, err.Error(), "too many requests")
}

func TestReadNVDRecordsRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nvd.json")
	require.NoError(t, os.WriteFile(path, []byte(`[{"id":""}]`), 0o644))
	_, _, err := readNVDRecords(path)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "CVE-"))
}
