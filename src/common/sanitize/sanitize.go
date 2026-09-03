// Package sanitize implements the credential-redaction half of the Output
// Sanitization Pipeline (AI.md PART 11) as a shared, mode-independent
// helper. Per AI.md PART 6 "Debug Mode", keys, tokens, passwords, and
// secrets are ALWAYS redacted — no mode, not even `debug`, is allowed to
// emit them, so this package deliberately has no mode awareness at all.
package sanitize

import (
	"regexp"
	"strings"
)

// RedactedPlaceholder replaces every credential value this package strips.
const RedactedPlaceholder = "REDACTED"

// credentialKeys are the field/parameter names whose values are always
// redacted, matching the sensitive-parameter list in AI.md PART 11's
// Output Sanitization Pipeline.
var credentialKeys = []string{
	"access_token",
	"api_key",
	"apikey",
	"auth",
	"authorization",
	"client_secret",
	"code",
	"credential",
	"encryption_key",
	"key",
	"passphrase",
	"password",
	"private_key",
	"refresh_token",
	"secret",
	"session",
	"token",
}

// credentialAssignment matches `name=value`, `name: value`, and
// `name="value"` style assignments for any credential key, capturing the
// key plus its separator so only the value is replaced. The separator
// group also absorbs an HTTP auth scheme (`Bearer `/`Basic `) so a header
// line keeps its scheme and loses only the credential itself.
var credentialAssignment = regexp.MustCompile(
	`(?i)\b(` + strings.Join(credentialKeys, "|") + `)(\s*[:=]\s*(?:bearer\s+|basic\s+)?)("[^"]*"|'[^']*'|[^\s,;&)"']+)`)

// bearerToken matches an HTTP bearer/basic credential in free-form text.
var bearerToken = regexp.MustCompile(`(?i)\b(bearer|basic)\s+([A-Za-z0-9\-._~+/=]+)`)

// urlCredentials matches the `user:password@` userinfo section of a URL so
// database and cache connection strings never leak their password.
var urlCredentials = regexp.MustCompile(`(?i)([a-z][a-z0-9+.\-]*://)([^/\s:@]+):([^/\s@]+)@`)

// IsCredentialKey reports whether a field or parameter name identifies a
// credential whose value must never be emitted.
func IsCredentialKey(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, key := range credentialKeys {
		if normalized == key || strings.HasSuffix(normalized, "_"+key) {
			return true
		}
	}
	return false
}

// RedactCredentials replaces credential values found in free-form text
// (error strings, log lines, debug dumps) with RedactedPlaceholder. It is
// applied in every mode, including debug, and never removes non-credential
// detail so diagnostics stay useful.
func RedactCredentials(s string) string {
	if s == "" {
		return s
	}

	redacted := urlCredentials.ReplaceAllString(s, "${1}${2}:"+RedactedPlaceholder+"@")
	redacted = credentialAssignment.ReplaceAllString(redacted, "${1}${2}"+RedactedPlaceholder)
	redacted = bearerToken.ReplaceAllString(redacted, "${1} "+RedactedPlaceholder)

	return redacted
}
