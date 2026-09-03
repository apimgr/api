package geo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers ReverseGeocode: invalid coordinate rejected without any network
// call, a successful response with City falling back from Town, the
// Nominatim error field surfaced, and an empty display_name treated as no
// address found.
func TestReverseGeocode(t *testing.T) {
	s := New()

	t.Run("invalid coordinate", func(t *testing.T) {
		result, err := s.ReverseGeocode(999, 0)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid coordinate")
	})

	t.Run("successful result with town fallback", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"display_name":"1600 Pennsylvania Ave, Washington, DC","address":{"road":"Pennsylvania Ave","town":"Washington","county":"DC","state":"DC","postcode":"20500","country":"USA","country_code":"us"}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		result, err := s.ReverseGeocode(38.8977, -77.0365)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "1600 Pennsylvania Ave, Washington, DC", result.DisplayName)
		assert.Equal(t, "Washington", result.City)
		assert.Equal(t, "us", result.CountryCode)
	})

	t.Run("nominatim error surfaced", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"error":"Unable to geocode"}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		result, err := s.ReverseGeocode(0, 0)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "Unable to geocode")
	})

	t.Run("empty display name", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		result, err := s.ReverseGeocode(0, 0)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no address found")
	})
}
