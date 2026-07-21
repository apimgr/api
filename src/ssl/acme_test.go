package ssl

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ShouldRenew must trigger at exactly 7 days remaining and below, and not
// above it.
func TestShouldRenew(t *testing.T) {
	assert.True(t, ShouldRenew(0))
	assert.True(t, ShouldRenew(7))
	assert.True(t, ShouldRenew(-1))
	assert.False(t, ShouldRenew(8))
	assert.False(t, ShouldRenew(365))
}

// AutoRenewInterval must be a 24-hour daily check, per spec.
func TestAutoRenewInterval(t *testing.T) {
	assert.Equal(t, 24*time.Hour, AutoRenewInterval())
}

// The manual ACME challenge functions are explicitly unimplemented and
// must return a descriptive error rather than silently succeeding.
func TestManualChallengeFunctionsReturnErrors(t *testing.T) {
	assert.Error(t, PerformHTTP01Challenge("example.com", "token", "keyauth"))
	assert.Error(t, PerformTLSALPN01Challenge("example.com", "keyauth"))
	assert.Error(t, PerformDNS01Challenge("example.com", "keyauth", "cloudflare", map[string]string{"api_key": "x"}))
}

// CheckCertificateExpiry must report the correct number of whole days
// remaining for a freshly generated (10-year) self-signed certificate,
// and must error on missing or malformed files.
func TestCheckCertificateExpiry(t *testing.T) {
	tmp := t.TempDir()
	certPath, _, err := GenerateSelfSigned(tmp, "expiry.test")
	require.NoError(t, err)

	days, err := CheckCertificateExpiry(certPath)
	require.NoError(t, err)
	// 10 years is roughly 3650 days; allow generous slack for clock skew.
	assert.Greater(t, days, 3000)

	_, err = CheckCertificateExpiry(filepath.Join(tmp, "missing.pem"))
	assert.Error(t, err)

	badPath := filepath.Join(tmp, "bad.pem")
	require.NoError(t, os.WriteFile(badPath, []byte("not a pem file"), 0644))
	_, err = CheckCertificateExpiry(badPath)
	assert.Error(t, err)
}

// GetCertificateInfo must report status "valid" for a freshly generated
// certificate and surface its DNS names, issuer, and validity window.
func TestGetCertificateInfoValid(t *testing.T) {
	tmp := t.TempDir()
	certPath, _, err := GenerateSelfSigned(tmp, "info.test")
	require.NoError(t, err)

	info, err := GetCertificateInfo(certPath)
	require.NoError(t, err)
	assert.Equal(t, "valid", info["status"])
	assert.Contains(t, info["domains"], "info.test")
	assert.Equal(t, "info.test", info["issuer"])

	daysRemaining, ok := info["days_remaining"].(int)
	require.True(t, ok)
	assert.Greater(t, daysRemaining, 0)
}

// GetCertificateInfo must propagate the underlying load error for a
// nonexistent certificate path.
func TestGetCertificateInfoMissingFile(t *testing.T) {
	tmp := t.TempDir()
	_, err := GetCertificateInfo(filepath.Join(tmp, "nope.pem"))
	assert.Error(t, err)
}

// RenewalTask must succeed with no renewal attempted when the certificate
// is nowhere near expiry.
func TestRenewalTaskNotDue(t *testing.T) {
	tmp := t.TempDir()
	certPath, _, err := GenerateSelfSigned(tmp, "renewal.test")
	require.NoError(t, err)

	assert.NoError(t, RenewalTask(certPath))
}

// RenewalTask must error out when the certificate file cannot be read at
// all (the expiry check fails first).
func TestRenewalTaskMissingCert(t *testing.T) {
	tmp := t.TempDir()
	assert.Error(t, RenewalTask(filepath.Join(tmp, "missing.pem")))
}
