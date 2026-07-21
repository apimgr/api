package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenericHandler exercises the happy path plus empty-string boundary
// inputs for service/endpoint, and confirms the response envelope contract
// (status, content-type, JSON body shape, trailing newline).
func TestGenericHandler(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		endpoint string
		wantData string
	}{
		{"basic pair", "weather", "forecast", "weather/forecast operational"},
		{"empty service", "", "ping", "/ping operational"},
		{"empty endpoint", "network", "", "network/ operational"},
		{"both empty", "", "", "/ operational"},
		{"special characters", "svc-1", "sub/path", "svc-1/sub/path operational"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerFunc := GenericHandler(tt.service, tt.endpoint)
			require.NotNil(t, handlerFunc)

			req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
			rec := httptest.NewRecorder()

			handlerFunc(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			body := rec.Body.String()
			// The handler writes a JSON document followed by an extra
			// trailing newline via fmt.Fprintln.
			require.True(t, strings.Count(body, "\n") >= 2)

			var resp Response
			decoder := json.NewDecoder(strings.NewReader(body))
			require.NoError(t, decoder.Decode(&resp))

			assert.True(t, resp.Success)
			assert.Equal(t, "v1", resp.Version)
			assert.Empty(t, resp.Error)
			assert.NotEmpty(t, resp.Timestamp)
			assert.Equal(t, tt.wantData, resp.Data)
		})
	}
}

// TestGenericHandler_Idempotent confirms repeated invocations of the same
// handler instance produce consistent, independent responses (no shared
// mutable state leaking between calls).
func TestGenericHandler_Idempotent(t *testing.T) {
	handlerFunc := GenericHandler("osint", "lookup")

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/osint/lookup", nil)
		rec := httptest.NewRecorder()
		handlerFunc(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp Response
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(rec.Body.String())), &resp))
		assert.Equal(t, "osint/lookup operational", resp.Data)
	}
}

// TestResponse_JSONTags confirms the omitempty contract on Data/Error: a
// zero-value Response should not emit those keys at all.
func TestResponse_JSONTags(t *testing.T) {
	resp := Response{Success: false, Timestamp: "2026-01-01T00:00:00Z", Version: "v1"}

	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &m))

	_, hasData := m["data"]
	_, hasError := m["error"]
	assert.False(t, hasData, "data should be omitted when nil")
	assert.False(t, hasError, "error should be omitted when empty")
	assert.Equal(t, false, m["success"])
}
