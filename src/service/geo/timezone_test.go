package geo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers Timezone: invalid coordinate rejected without any network call, a
// successful Open-Meteo response, an empty timezone field treated as not
// found, and a non-200 upstream status.
func TestTimezone(t *testing.T) {
	s := New()

	t.Run("invalid coordinate", func(t *testing.T) {
		result, err := s.Timezone(999, 0)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid coordinate")
	})

	t.Run("successful result", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"timezone":"America/New_York","timezone_abbreviation":"EDT","utc_offset_seconds":-14400}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		result, err := s.Timezone(40.7128, -74.0060)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "America/New_York", result.Timezone)
		assert.Equal(t, "EDT", result.Abbreviation)
		assert.Equal(t, -14400, result.UTCOffsetSeconds)
	})

	t.Run("empty timezone in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		result, err := s.Timezone(0, 0)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no timezone found")
	})

	t.Run("upstream error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()
		withMockServer(t, server)

		result, err := s.Timezone(40.7128, -74.0060)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "status 502")
	})
}
