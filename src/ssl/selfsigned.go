package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// selfSignedValidity is long-lived (10 years) because self-signed certificates
// have no auto-renewal path - per AI.md PART 15, the "local" cert tier is
// user-managed with no auto-renewal, and this is also the fallback used for
// Tor (.onion) and I2P (.i2p), which Let's Encrypt cannot issue for at all.
const selfSignedValidity = 10 * 365 * 24 * time.Hour

// GenerateSelfSigned creates a self-signed certificate/key pair for fqdn and
// writes it to {sslDir}/local/{fqdn}/{cert.pem,key.pem} - sslDir is the SSL
// base directory ({config_dir}/ssl), matching the spec's tier-4 directory
// layout (AI.md PART 15). It is used as a last-resort fallback so the server
// can still start over HTTPS when Let's Encrypt is disabled, unreachable, or
// fails, and unconditionally for Tor/I2P overlay addresses which Let's
// Encrypt cannot certify.
func GenerateSelfSigned(sslDir, fqdn string) (certPath, keyPath string, err error) {
	dir := filepath.Join(sslDir, "local", fqdn)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", fmt.Errorf("failed to create self-signed cert dir: %w", err)
	}

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	if fileExists(certPath) && fileExists(keyPath) {
		return certPath, keyPath, nil
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: fqdn},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(selfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	if ip := net.ParseIP(fqdn); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{fqdn}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return "", "", fmt.Errorf("failed to create self-signed certificate: %w", err)
	}

	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return "", "", fmt.Errorf("failed to open %s for writing: %w", certPath, err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return "", "", fmt.Errorf("failed to write certificate: %w", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", "", fmt.Errorf("failed to open %s for writing: %w", keyPath, err)
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return "", "", fmt.Errorf("failed to write private key: %w", err)
	}

	return certPath, keyPath, nil
}
