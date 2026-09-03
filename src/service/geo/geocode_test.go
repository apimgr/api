package geo

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectingTransport rewrites every outgoing request's scheme/host to
// point at a local httptest.Server while preserving path and query, so the
// package's hardcoded provider endpoint constants can be exercised against
// a fully-controlled fake server without any code changes to the package
// under test.
type redirectingTransport struct {
	targetURL *url.URL
	base      http.RoundTripper
}

func (rt *redirectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.targetURL.Scheme
	req.URL.Host = rt.targetURL.Host
	req.Host = rt.targetURL.Host
	return rt.base.RoundTrip(req)
}

// withMockServer swaps the package-level httpClient for one that redirects
// all traffic to the given httptest.Server for the duration of the test,
// restoring the original client on cleanup.
func withMockServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	original := httpClient
	target, err := url.Parse(server.URL)
	require.NoError(t, err)

	httpClient = &http.Client{
		Timeout:   original.Timeout,
		Transport: &redirectingTransport{targetURL: target, base: http.DefaultTransport},
	}

	t.Cleanup(func() {
		httpClient = original
	})
}

// Covers Geocode: empty query rejected without any network call, a
// successful search response, an empty-results response, and a non-200
// upstream status.
func TestGeocode(t *testing.T) {
	s := New()

	t.Run("empty query", func(t *testing.T) {
		results, err := s.Geocode("")
		require.Error(t, err)
		assert.Nil(t, results)
		assert.Contains(t, err.Error(), "query is required")
	})

	t.Run("successful results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"display_name":"New York, NY, USA","lat":"40.7128","lon":"-74.0060","type":"city","class":"place"}]`))
		}))
		defer server.Close()
		withMockServer(t, server)

		results, err := s.Geocode("New York")
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "New York, NY, USA", results[0].DisplayName)
		assert.InDelta(t, 40.7128, results[0].Latitude, 0.0001)
		assert.InDelta(t, -74.0060, results[0].Longitude, 0.0001)
	})

	t.Run("no results found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()
		withMockServer(t, server)

		results, err := s.Geocode("Nowhereville")
		require.Error(t, err)
		assert.Nil(t, results)
		assert.Contains(t, err.Error(), "no results found")
	})

	t.Run("upstream error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()
		withMockServer(t, server)

		results, err := s.Geocode("New York")
		require.Error(t, err)
		assert.Nil(t, results)
		assert.Contains(t, err.Error(), "status 502")
	})
}
