package sanitize

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCredentialKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"token", "token", true},
		{"password uppercase", "PASSWORD", true},
		{"suffixed", "owner_token", true},
		{"suffixed secret", "cookie_signing_secret", true},
		{"padded", "  api_key  ", true},
		{"plain field", "username", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCredentialKey(tt.input))
		})
	}
}

func TestRedactCredentials(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantMissing string
		wantPresent string
	}{
		{"equals form", "connect failed: password=hunter2", "hunter2", "password=" + RedactedPlaceholder},
		{"colon form", "token: tok_abc123", "tok_abc123", "token: " + RedactedPlaceholder},
		{"quoted value", `secret="s3cr3t value"`, "s3cr3t", "secret=" + RedactedPlaceholder},
		{"bearer header", "Authorization: Bearer abc.def.ghi", "abc.def.ghi", "Bearer " + RedactedPlaceholder},
		{"database url", "dial libsql://admin:sup3rs3cret@db.example.com/app", "sup3rs3cret", "admin:" + RedactedPlaceholder + "@"},
		{"api key query", "GET /items?api_key=zzz123&page=2", "zzz123", "page=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactCredentials(tt.input)
			assert.NotContains(t, got, tt.wantMissing)
			assert.Contains(t, got, tt.wantPresent)
		})
	}
}

// TestRedactCredentialsKeepsDiagnostics confirms redaction only removes
// credential values, so debug-mode diagnostics stay useful.
func TestRedactCredentialsKeepsDiagnostics(t *testing.T) {
	assert.Equal(t, "", RedactCredentials(""))

	input := "sqlite: no such table: api_tokens (attempt 3 of 5)"
	assert.Equal(t, input, RedactCredentials(input))
}
