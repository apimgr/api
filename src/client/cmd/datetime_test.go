package cmd

import (
	"testing"

	"github.com/apimgr/api/src/client/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatetimeCommands_HappyPath table-drives every datetime subcommand's
// success path against the real registered Run function.
func TestDatetimeCommands_HappyPath(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
	}{
		{"now", nil, "/api/v1/datetime/now"},
		{"now", []string{"UTC"}, "/api/v1/datetime/now/UTC"},
		{"timestamp", nil, "/api/v1/datetime/timestamp"},
		{"convert", []string{"1700000000"}, "/api/v1/datetime/convert/1700000000"},
		{"convert", []string{"1700000000", "UTC"}, "/api/v1/datetime/convert/1700000000/UTC"},
		{"to-unix", []string{"2024-01-01"}, "/api/v1/datetime/to-unix/2024-01-01"},
		{"add", []string{"1700000000", "1h"}, "/api/v1/datetime/add/1700000000/1h"},
		{"diff", []string{"1700000000", "1700003600"}, "/api/v1/datetime/diff/1700000000/1700003600"},
		{"timezones", nil, "/api/v1/datetime/timezones"},
		{"timezone", []string{"UTC"}, "/api/v1/datetime/timezone/UTC"},
		{"timezone-convert", []string{"1700000000", "UTC", "EST"}, "/api/v1/datetime/timezone/convert/1700000000/UTC/EST"},
	}

	for _, tc := range tests {
		t.Run(tc.wantPath, func(t *testing.T) {
			srv, rec := newRecordingServer(t, 200, `{"result":"ok"}`)
			client := api.New(srv.URL, "")

			_, err := runCommand(t, client, "datetime", tc.name, tc.args)
			require.NoError(t, err)
			assert.Equal(t, "GET", rec.Method)
			assert.Equal(t, tc.wantPath, rec.Path)
		})
	}
}

// TestDatetimeCommands_MissingRequiredArgs verifies every required-arg
// command errors out before contacting the server.
func TestDatetimeCommands_MissingRequiredArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"convert", nil},
		{"to-unix", nil},
		{"add", []string{"1700000000"}},
		{"diff", []string{"1700000000"}},
		{"timezone", nil},
		{"timezone-convert", []string{"1700000000", "UTC"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := newRecordingServer(t, 200, `{}`)
			client := api.New(srv.URL, "")

			_, err := runCommand(t, client, "datetime", tc.name, tc.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing required argument")
			assert.Empty(t, rec.Method)
		})
	}
}

// TestDatetimeCommands_NotFoundMapsToAPIError verifies a 404 from the
// server surfaces as a typed *api.Error the caller can branch on.
func TestDatetimeCommands_NotFoundMapsToAPIError(t *testing.T) {
	srv, _ := newRecordingServer(t, 404, `{"error":"unknown timezone"}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "datetime", "timezone", []string{"Nowhere/Fake"})
	require.Error(t, err)

	apiErr, ok := err.(*api.Error)
	require.True(t, ok)
	assert.Equal(t, 404, apiErr.StatusCode)
}
