package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"cli_versions":{"%s":{"version":"2.0.0","sha256":"abc"}},"cli_min_version":"1.5.0"}}`, PlatformKey())
	}))
	defer server.Close()

	decision, err := Check(context.Background(), server.URL, "owner-token", "1.0.0", server.Client())
	require.NoError(t, err)
	require.True(t, decision.Available)
	require.True(t, decision.Forced)
	require.Equal(t, "2.0.0", decision.Release.Version)
	require.Equal(t, "abc", decision.Release.SHA256)
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
