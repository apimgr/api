package ssl

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GenerateSelfSigned must write cert.pem/key.pem under
// {sslDir}/local/{fqdn}/ containing a valid, currently-active certificate
// for the requested DNS name.
func TestGenerateSelfSignedDNSName(t *testing.T) {
	tmp := t.TempDir()
	fqdn := "example.test"

	certPath, keyPath, err := GenerateSelfSigned(tmp, fqdn)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmp, "local", fqdn, "cert.pem"), certPath)
	assert.Equal(t, filepath.Join(tmp, "local", fqdn, "key.pem"), keyPath)

	certPEM, err := os.ReadFile(certPath)
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	assert.Equal(t, fqdn, cert.Subject.CommonName)
	assert.Contains(t, cert.DNSNames, fqdn)
	assert.Empty(t, cert.IPAddresses)
	assert.True(t, cert.NotBefore.Before(cert.NotAfter))

	// Key file must exist and be non-empty.
	keyPEM, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	assert.NotEmpty(t, keyPEM)
}

// When fqdn is an IP address, the certificate must be issued for that IP
// (IPAddresses), not as a DNS name.
func TestGenerateSelfSignedIPAddress(t *testing.T) {
	tmp := t.TempDir()
	fqdn := "127.0.0.1"

	certPath, _, err := GenerateSelfSigned(tmp, fqdn)
	require.NoError(t, err)

	certPEM, err := os.ReadFile(certPath)
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	assert.Empty(t, cert.DNSNames)
	require.Len(t, cert.IPAddresses, 1)
	assert.True(t, cert.IPAddresses[0].Equal(net.ParseIP(fqdn)))
}

// A second call for the same sslDir/fqdn must reuse the existing
// cert/key pair rather than regenerating it (idempotent, per the
// fileExists short-circuit).
func TestGenerateSelfSignedReusesExisting(t *testing.T) {
	tmp := t.TempDir()
	fqdn := "reuse.test"

	certPath1, keyPath1, err := GenerateSelfSigned(tmp, fqdn)
	require.NoError(t, err)
	firstCert, err := os.ReadFile(certPath1)
	require.NoError(t, err)

	certPath2, keyPath2, err := GenerateSelfSigned(tmp, fqdn)
	require.NoError(t, err)
	secondCert, err := os.ReadFile(certPath2)
	require.NoError(t, err)

	assert.Equal(t, certPath1, certPath2)
	assert.Equal(t, keyPath1, keyPath2)
	assert.Equal(t, firstCert, secondCert)
}
