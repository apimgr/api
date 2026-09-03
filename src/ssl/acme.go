package ssl

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/apimgr/api/src/email"
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

// expiryWarnDays is the point from which AI.md PART 17 wants an expiry
// warning in the log (30 and 14 days are log-only).
const expiryWarnDays = 30

// expiryEmailDays are the urgent milestones AI.md PART 17 emails the operator
// about; an already-expired certificate is always urgent.
var expiryEmailDays = []int{7, 3, 1}

// shouldWarnExpiry reports whether an expiry WARN line is due.
func shouldWarnExpiry(daysUntilExpiry int) bool {
	return daysUntilExpiry <= expiryWarnDays
}

// shouldEmailExpiry reports whether the ssl_expiring email is due. Only the
// milestone days are emailed, so a certificate sitting inside the urgent
// window does not mail the operator on every daily run.
func shouldEmailExpiry(daysUntilExpiry int) bool {
	if daysUntilExpiry <= 0 {
		return true
	}
	for _, day := range expiryEmailDays {
		if daysUntilExpiry == day {
			return true
		}
	}
	return false
}

// expiresInText renders the remaining lifetime for operator-facing output.
func expiresInText(daysUntilExpiry int) string {
	switch {
	case daysUntilExpiry <= 0:
		return "expired"
	case daysUntilExpiry == 1:
		return "1 day"
	default:
		return fmt.Sprintf("%d days", daysUntilExpiry)
	}
}

// certificateSubject returns the name an operator recognizes the certificate
// by: its first SAN, falling back to the subject common name.
func certificateSubject(leaf *x509.Certificate) string {
	if leaf == nil {
		return ""
	}
	if len(leaf.DNSNames) > 0 {
		return leaf.DNSNames[0]
	}
	return leaf.Subject.CommonName
}

// reportExpiry logs the structured WARN line and raises the ssl_expiring
// operator notification, per AI.md PART 17: 30/14 days are log-only early
// warnings, 7/3/1 days (and an expired certificate) are urgent and emailed.
// Email dispatch is best effort and never changes the renewal outcome.
func reportExpiry(certPath string, daysUntilExpiry int) {
	if !shouldWarnExpiry(daysUntilExpiry) {
		return
	}

	leaf, err := loadLeafCertificate(certPath)
	if err != nil {
		log.Printf("SSL: [WARN] ssl_expiring expires_in=%s error=%v", expiresInText(daysUntilExpiry), err)
		return
	}

	fqdn := certificateSubject(leaf)
	expiryDate := leaf.NotAfter.UTC().Format(time.RFC3339)
	log.Printf("SSL: [WARN] ssl_expiring fqdn=%s expires_in=%s expiry_date=%s",
		fqdn, expiresInText(daysUntilExpiry), expiryDate)

	if shouldEmailExpiry(daysUntilExpiry) {
		email.OperatorSSLExpiring(fqdn, expiresInText(daysUntilExpiry), expiryDate)
	}
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
	reportExpiry(certPath, daysUntilExpiry)

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
