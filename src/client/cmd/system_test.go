package cmd

import (
	"testing"

	"github.com/apimgr/api/src/client/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSystemCommands_HappyPath table-drives every generated system
// subcommand (health, liveness, readiness, info, version, endpoints,
// stats), verifying each hits its own dedicated GET endpoint.
func TestSystemCommands_HappyPath(t *testing.T) {
	actions := []string{"health", "liveness", "readiness", "info", "version", "endpoints", "stats"}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			srv, rec := newRecordingServer(t, 200, `{"status":"ok"}`)
			client := api.New(srv.URL, "")

			_, err := runCommand(t, client, "system", action, nil)
			require.NoError(t, err)
			assert.Equal(t, "GET", rec.Method)
			assert.Equal(t, "/api/v1/system/"+action, rec.Path)
		})
	}
}

// TestSystemCommands_ArgsAreIgnored verifies system commands take no
// positional arguments and don't error even if extras are supplied, since
// the underlying Run function never reads args.
func TestSystemCommands_ArgsAreIgnored(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{"status":"ok"}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "system", "health", []string{"unused", "extra"})
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/system/health", rec.Path)
}

// TestSystemCommands_ServerErrorPropagates verifies a 503 from the server
// surfaces as an error to the caller.
func TestSystemCommands_ServerErrorPropagates(t *testing.T) {
	srv, _ := newRecordingServer(t, 503, `{"error":"unavailable"}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "system", "readiness", nil)
	require.Error(t, err)

	apiErr, ok := err.(*api.Error)
	require.True(t, ok)
	assert.Equal(t, 503, apiErr.StatusCode)
}

// TestSystemCommands_UnregisteredActionNotFound verifies an action that
// was never registered isn't discoverable through findCommand.
func TestSystemCommands_UnregisteredActionNotFound(t *testing.T) {
	_, ok := findCommand("system", "does-not-exist")
	assert.False(t, ok)
}
