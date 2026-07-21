package cmd

import (
	"testing"

	"github.com/apimgr/api/src/client/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNetworkCommands_HappyPath table-drives every network subcommand's
// success path, including the optional user-agent override.
func TestNetworkCommands_HappyPath(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantPath  string
		wantQuery map[string]string
	}{
		{"ip", nil, "/api/v1/network/ip", nil},
		{"user-agent", nil, "/api/v1/network/user-agent", map[string]string{}},
		{"user-agent", []string{"Mozilla/5.0"}, "/api/v1/network/user-agent", map[string]string{"ua": "Mozilla/5.0"}},
		{"mac", []string{"AA:BB:CC:DD:EE:FF"}, "/api/v1/network/mac/AA:BB:CC:DD:EE:FF", nil},
		{"subnet", []string{"10.0.0.0/24"}, "/api/v1/network/subnet", map[string]string{"cidr": "10.0.0.0/24"}},
		{"ula", nil, "/api/v1/network/ula", nil},
		{"port", nil, "/api/v1/network/port", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name+"_"+tc.wantPath, func(t *testing.T) {
			srv, rec := newRecordingServer(t, 200, `{"result":"ok"}`)
			client := api.New(srv.URL, "")

			_, err := runCommand(t, client, "network", tc.name, tc.args)
			require.NoError(t, err)
			assert.Equal(t, "GET", rec.Method)
			assert.Equal(t, tc.wantPath, rec.Path)
			for k, v := range tc.wantQuery {
				assert.Equal(t, v, rec.Query.Get(k))
			}
		})
	}
}

// TestNetworkCommands_MissingRequiredArgs verifies mac and subnet require
// their positional argument.
func TestNetworkCommands_MissingRequiredArgs(t *testing.T) {
	for _, name := range []string{"mac", "subnet"} {
		t.Run(name, func(t *testing.T) {
			srv, rec := newRecordingServer(t, 200, `{}`)
			client := api.New(srv.URL, "")

			_, err := runCommand(t, client, "network", name, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing required argument")
			assert.Empty(t, rec.Method)
		})
	}
}

// TestNetworkCommands_UnauthorizedMapsToAPIError verifies a 401 surfaces as
// a typed *api.Error with ExitAuth-mappable status.
func TestNetworkCommands_UnauthorizedMapsToAPIError(t *testing.T) {
	srv, _ := newRecordingServer(t, 401, `{"error":"unauthorized"}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "network", "ip", nil)
	require.Error(t, err)

	apiErr, ok := err.(*api.Error)
	require.True(t, ok)
	assert.Equal(t, 401, apiErr.StatusCode)
}
