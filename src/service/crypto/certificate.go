package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// GenerateCertificate creates a self-signed RSA X.509 certificate for
// commonName, valid for validDays days, and returns the PEM-encoded
// certificate and PKCS#1 private key. Intended for local development/testing
// use, not for issuing production-trusted certificates (no CA involved).
func GenerateCertificate(commonName string, validDays int) (string, string, error) {
	if commonName == "" {
		return "", "", fmt.Errorf("common name is required")
	}
	if validDays <= 0 {
		validDays = 365
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate RSA key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate serial number: %w", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.AddDate(0, 0, validDays)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		DNSNames:              []string{commonName},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	return string(certPEM), string(keyPEM), nil
}

// ParseCertificate parses a PEM-encoded X.509 certificate and returns its
// subject, issuer, validity window, serial number, DNS SANs, CA flag, and
// signature algorithm as a JSON-friendly map.
func ParseCertificate(certPEM string) (map[string]interface{}, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("input is not a PEM-encoded certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return map[string]interface{}{
		"subject":              cert.Subject.String(),
		"issuer":               cert.Issuer.String(),
		"serial_number":        cert.SerialNumber.String(),
		"not_before":           cert.NotBefore.UTC().Format(time.RFC3339),
		"not_after":            cert.NotAfter.UTC().Format(time.RFC3339),
		"dns_names":            cert.DNSNames,
		"is_ca":                cert.IsCA,
		"signature_algorithm":  cert.SignatureAlgorithm.String(),
		"public_key_algorithm": cert.PublicKeyAlgorithm.String(),
		"version":              cert.Version,
	}, nil
}
