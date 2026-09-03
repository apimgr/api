package cmd

import (
	"testing"

	"github.com/apimgr/api/src/client/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCryptoCommands_HappyPath table-drives every crypto subcommand's
// success path, asserting the exact request method/path/query the command
// builds against the real (recording) server.
func TestCryptoCommands_HappyPath(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantPath  string
		wantQuery map[string]string
	}{
		{"bcrypt without cost", []string{"hunter2"}, "/api/v1/crypto/bcrypt/hunter2", nil},
		{"bcrypt with cost", []string{"hunter2", "12"}, "/api/v1/crypto/bcrypt/12/hunter2", nil},
		{"bcrypt-verify", []string{"hunter2", "$2a$hash"}, "/api/v1/crypto/bcrypt/verify/hunter2/$2a$hash", nil},
		{"password without length", nil, "/api/v1/crypto/password", nil},
		{"password with length", []string{"32"}, "/api/v1/crypto/password/32", nil},
		{"password-strength", []string{"hunter2"}, "/api/v1/crypto/password/strength/hunter2", nil},
		{"pin without length", nil, "/api/v1/crypto/pin", nil},
		{"pin with length", []string{"6"}, "/api/v1/crypto/pin/6", nil},
		{"totp-secret without issuer", nil, "/api/v1/crypto/totp/secret", map[string]string{}},
		{"totp-secret with issuer", []string{"acme"}, "/api/v1/crypto/totp/secret", map[string]string{"issuer": "acme"}},
		{"totp-code", []string{"SECRET"}, "/api/v1/crypto/totp/code/SECRET", nil},
		{"totp-verify", []string{"SECRET", "123456"}, "/api/v1/crypto/totp/verify/SECRET/123456", nil},
		{"random-bytes", []string{"16"}, "/api/v1/crypto/random/bytes/16", nil},
		{"random-hex", []string{"16"}, "/api/v1/crypto/random/hex/16", nil},
	}

	names := map[string]string{
		"bcrypt without cost": "bcrypt", "bcrypt with cost": "bcrypt",
		"bcrypt-verify": "bcrypt-verify", "password without length": "password",
		"password with length": "password", "password-strength": "password-strength",
		"pin without length": "pin", "pin with length": "pin",
		"totp-secret without issuer": "totp-secret", "totp-secret with issuer": "totp-secret",
		"totp-code": "totp-code", "totp-verify": "totp-verify",
		"random-bytes": "random-bytes", "random-hex": "random-hex",
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := newRecordingServer(t, 200, `{"result":"ok"}`)
			client := api.New(srv.URL, "")

			_, err := runCommand(t, client, "crypto", names[tc.name], tc.args)
			require.NoError(t, err)

			assert.Equal(t, "GET", rec.Method)
			assert.Equal(t, tc.wantPath, rec.Path)
			if tc.wantQuery != nil {
				for k, v := range tc.wantQuery {
					assert.Equal(t, v, rec.Query.Get(k))
				}
			}
		})
	}
}

// TestCryptoCommands_MissingRequiredArgs verifies commands with required
// positional arguments fail fast with a descriptive error and never touch
// the server.
func TestCryptoCommands_MissingRequiredArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"bcrypt", nil},
		{"bcrypt-verify", []string{"only-password"}},
		{"password-strength", nil},
		{"totp-code", nil},
		{"totp-verify", []string{"only-secret"}},
		{"random-bytes", nil},
		{"random-hex", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := newRecordingServer(t, 200, `{}`)
			client := api.New(srv.URL, "")

			_, err := runCommand(t, client, "crypto", tc.name, tc.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing required argument")
			assert.Empty(t, rec.Method, "server must not be called when a required arg is missing")
		})
	}
}

// TestCryptoCommands_ServerErrorPropagates verifies a non-2xx server
// response surfaces as an *api.Error from the command, not a swallowed nil.
func TestCryptoCommands_ServerErrorPropagates(t *testing.T) {
	srv, _ := newRecordingServer(t, 500, `{"error":"boom"}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "crypto", "pin", nil)
	require.Error(t, err)

	apiErr, ok := err.(*api.Error)
	require.True(t, ok, "expected *api.Error, got %T", err)
	assert.Equal(t, 500, apiErr.StatusCode)
}

// TestCryptoCommands_ConnectionErrorPropagates verifies a command whose
// client points at an unreachable server returns an error rather than
// panicking.
func TestCryptoCommands_ConnectionErrorPropagates(t *testing.T) {
	client := api.New("http://127.0.0.1:1", "")
	_, err := runCommand(t, client, "crypto", "pin", nil)
	require.Error(t, err)
}
