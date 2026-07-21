package ssl

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"time"
)

// CheckCertificateExpiry loads the certificate at certPath and returns the
// number of whole days remaining until its NotAfter date.
func CheckCertificateExpiry(certPath string) (int, error) {
	leaf, err := loadLeafCertificate(certPath)
	if err != nil {
		return 0, err
	}

	remaining := time.Until(leaf.NotAfter)
	days := int(remaining.Hours() / 24)
	if remaining > 0 && days == 0 {
		// Less than a day left, but not yet expired - round up so callers
		// don't treat it as "0 days" being falsy/ignorable.
		days = 1
	}

	return days, nil
}

// ShouldRenew determines if a certificate should be renewed.
// Renews if within 7 days of expiry, per AI.md PART 15.
func ShouldRenew(daysUntilExpiry int) bool {
	return daysUntilExpiry <= 7
}

// PerformHTTP01Challenge completes an HTTP-01 challenge. Not implemented on
// this manual ACME path - the working HTTP-01 flow is autocert's built-in
// handler (see Manager.GetHTTPHandler in ssl.go).
func PerformHTTP01Challenge(domain, token, keyAuth string) error {
	return fmt.Errorf("SSL: HTTP-01 challenge for %s: manual ACME challenge handling is not implemented, use ssl.Manager (autocert) instead", domain)
}

// PerformTLSALPN01Challenge completes a TLS-ALPN-01 challenge. Not
// implemented on this manual ACME path - the working TLS-ALPN-01 flow is
// handled internally by autocert (see Manager.getLetsEncryptTLSConfig in
// ssl.go).
func PerformTLSALPN01Challenge(domain, keyAuth string) error {
	return fmt.Errorf("SSL: TLS-ALPN-01 challenge for %s: manual ACME challenge handling is not implemented, use ssl.Manager (autocert) instead", domain)
}

// PerformDNS01Challenge completes a DNS-01 challenge. Not implemented:
// autocert (the ACME client actually wired up in ssl.go) only supports
// HTTP-01 and TLS-ALPN-01. Full DNS-01 support requires a lego-based client
// with a per-provider plugin model (AI.md PART 15, DNS-01 Provider
// Configuration) - tracked as a follow-up needing a product decision on
// provider scope, not implemented here. Returning an explicit error rather
// than a silent no-op so callers cannot mistake this for a completed
// challenge.
func PerformDNS01Challenge(domain, keyAuth, provider string, credentials map[string]string) error {
	return fmt.Errorf("SSL: DNS-01 challenge for %s via %s: DNS-01 is not supported by the current ACME client (autocert); requires a lego-based client, not yet implemented", domain, provider)
}

// RenewalTask is the scheduler task for certificate renewal
func RenewalTask(certPath string) error {
	log.Println("SSL: Running certificate renewal check...")

	// Check certificate expiry
	daysUntilExpiry, err := CheckCertificateExpiry(certPath)
	if err != nil {
		log.Printf("SSL: Failed to check certificate: %v", err)
		return err
	}

	log.Printf("SSL: Certificate expires in %d days", daysUntilExpiry)

	// Renew if needed (within 7 days, per AI.md PART 15)
	if ShouldRenew(daysUntilExpiry) {
		log.Println("SSL: Certificate renewal needed (within 7 days)")
		return fmt.Errorf("certificate renewal not yet implemented for manual ACME path; use ssl.Manager (autocert) which renews automatically")
	}

	log.Println("SSL: Certificate is valid, no renewal needed")
	return nil
}

// GetCertificateInfo returns information about a certificate: subject
// domains, issuer, validity window, and days remaining until expiry.
func GetCertificateInfo(certPath string) (map[string]interface{}, error) {
	leaf, err := loadLeafCertificate(certPath)
	if err != nil {
		return nil, err
	}

	daysRemaining, err := CheckCertificateExpiry(certPath)
	if err != nil {
		return nil, err
	}

	status := "valid"
	if time.Now().After(leaf.NotAfter) {
		status = "expired"
	} else if ShouldRenew(daysRemaining) {
		status = "renewal_due"
	}

	return map[string]interface{}{
		"status":         status,
		"domains":        leaf.DNSNames,
		"issuer":         leaf.Issuer.CommonName,
		"not_before":     leaf.NotBefore,
		"not_after":      leaf.NotAfter,
		"days_remaining": daysRemaining,
	}, nil
}

// loadLeafCertificate loads and parses the leaf (first) certificate stored
// at certPath (a PEM-encoded certificate file, as written by GenerateSelfSigned
// or Manager's certificate loading paths).
func loadLeafCertificate(certPath string) (*x509.Certificate, error) {
	pemData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM certificate block found in %s", certPath)
	}

	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return leaf, nil
}

// AutoRenewInterval returns the check interval for auto-renewal
// Per spec: Check daily at 03:00
func AutoRenewInterval() time.Duration {
	return 24 * time.Hour
}
