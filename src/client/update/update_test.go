package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckUsesAutodiscoverReleaseForCurrentPlatform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autodiscover" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "api-cli/dev" {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer owner-token" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"version":"3.0.0","api":{"current_version":"v9"},"cli_versions":{"%s":{"version":"2.0.0","sha256":"abc"}},"cli_min_version":"1.5.0"}}`, PlatformKey())
	}))
	defer server.Close()

	decision, err := Check(context.Background(), server.URL, "owner-token", "1.0.0", server.Client())
	require.NoError(t, err)
	require.True(t, decision.Available)
	require.True(t, decision.Forced)
	require.Equal(t, "2.0.0", decision.Release.Version)
	require.Equal(t, "abc", decision.Release.SHA256)
	require.Equal(t, "v9", decision.Discovery.Data.API.CurrentVersion)
	require.True(t, decision.VersionMismatch)
}

func TestHasMajorVersionMismatch(t *testing.T) {
	tests := []struct {
		name, client, server string
		want                 bool
	}{
		{name: "same major stable and prerelease", client: "v1.2.3", server: "1.9.0-rc.1"},
		{name: "different major", client: "1.9.0", server: "2.0.0", want: true},
		{name: "invalid client version", client: "dev", server: "2.0.0"},
		{name: "invalid server version", client: "1.0.0", server: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, hasMajorVersionMismatch(test.client, test.server))
		})
	}
}

func TestCLIBinaryEndpointUsesDiscoveredAPIVersion(t *testing.T) {
	endpoint, err := cliBinaryEndpoint("https://api.example.test/", "v9", "api-cli-linux-amd64")
	require.NoError(t, err)
	require.Equal(t, "https://api.example.test/api/v9/cli/binaries/api-cli-linux-amd64", endpoint)
}

func TestCLIBinaryEndpointRejectsInvalidAPIVersion(t *testing.T) {
	_, err := cliBinaryEndpoint("https://api.example.test", "../v1", "api-cli-linux-amd64")
	require.Error(t, err)
}

func TestCheckDoesNotAdvertiseDowngradeOrSameVersion(t *testing.T) {
	for _, current := range []string{"2.0.0", "3.0.0"} {
		t.Run(current, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"cli_versions":{"%s":{"version":"2.0.0","sha256":"abc"}}}}`, PlatformKey())
			}))
			defer server.Close()
			decision, err := Check(context.Background(), server.URL, "", current, server.Client())
			require.NoError(t, err)
			require.False(t, decision.Available)
		})
	}
}

func TestAuditSanitizesSensitiveFieldsAndTerminalText(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli.log")
	err := audit(logPath, "update\x1b[31m🔥", map[string]string{
		"access_token": "owner-secret",
		"message":      "private\x1b[0m🔥\npassword=hunter2",
	})
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	line := strings.TrimSuffix(string(data), "\n")
	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &entry))
	require.Equal(t, "REDACTED", entry["access_token"])
	require.NotContains(t, line, "owner-secret")
	require.NotContains(t, line, "hunter2")
	require.NotContains(t, line, "\x1b")
	require.NotContains(t, line, "🔥")
	for _, r := range line {
		require.True(t, r >= 0x20 && r <= 0x7e, "CLI audit log contains non-ASCII/control character %U", r)
	}
}
