package ssl

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/apimgr/api/src/email"
)

// Config holds SSL/TLS configuration
type Config struct {
	Enabled                bool
	CertPath               string
	LetsEncrypt            LetsEncryptConfig
	OnDNSCertificateIssued func(time.Time)
}

// LetsEncryptConfig holds Let's Encrypt settings
type LetsEncryptConfig struct {
	Enabled        bool
	Email          string
	Challenge      string // http-01, tls-alpn-01, dns-01
	DNSProvider    string
	DNSCredentials map[string]string
}

// Manager handles SSL/TLS certificates
type Manager struct {
	config      Config
	certManager *autocert.Manager
	domains     []string
	managesACME bool
	mu          sync.RWMutex
	dnsMu       sync.Mutex
}

// NewManager creates a new SSL manager
func NewManager(cfg Config) *Manager {
	return &Manager{
		config: cfg,
	}
}

// GetTLSConfig returns TLS configuration for the server
func (m *Manager) GetTLSConfig(domains []string) (*tls.Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.domains = append(m.domains[:0], domains...)
	m.managesACME = false

	if !m.config.Enabled {
		return nil, nil
	}
	if m.config.LetsEncrypt.Enabled && ParseChallenge(m.config.LetsEncrypt.Challenge) == "dns-01" {
		if cert, key := m.findSystemCerts(domains); cert != "" && key != "" {
			if err := ValidateDNSProvider(m.config.LetsEncrypt.DNSProvider, m.config.LetsEncrypt.DNSCredentials); err != nil {
				log.Printf("SSL: [ERROR] DNS provider validation failed: %v", err)
				return nil, err
			}
			tlsCert, err := loadValidatedCertificate(cert, key, domains)
			if err != nil {
				return nil, err
			}
			return &tls.Config{Certificates: []tls.Certificate{tlsCert}, MinVersion: tls.VersionTLS12}, nil
		}
		m.managesACME = true
		return m.getLetsEncryptTLSConfig(domains)
	}

	// Check for existing certificates first (e.g., from /etc/letsencrypt/live)
	if cert, key := m.findExistingCerts(domains); cert != "" && key != "" {
		log.Printf("Using existing certificate: %s", cert)
		tlsCert, err := loadValidatedCertificate(cert, key, domains)
		if err != nil {
			return nil, err
		}
		return &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		}, nil
	}

	// Use Let's Encrypt if enabled
	if m.config.LetsEncrypt.Enabled {
		m.managesACME = true
		return m.getLetsEncryptTLSConfig(domains)
	}

	// Check for manual certificates
	if cert, key := m.findManualCerts(domains); cert != "" && key != "" {
		log.Printf("Using manual certificate: %s", cert)
		tlsCert, err := loadValidatedCertificate(cert, key, domains)
		if err != nil {
			return nil, err
		}
		return &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		}, nil
	}

	// Last resort: generate a self-signed certificate so the server can
	// still start over HTTPS, per AI.md PART 15 ("Let's Encrypt or local").
	return m.getSelfSignedTLSConfig(domains)
}

func loadValidatedCertificate(certPath, keyPath string, domains []string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to load certificate: %w", err)
	}
	if len(cert.Certificate) == 0 {
		return tls.Certificate{}, fmt.Errorf("certificate %s contains no leaf certificate", certPath)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to parse certificate %s: %w", certPath, err)
	}
	if err := validateCertificate(leaf, domains, time.Now()); err != nil {
		return tls.Certificate{}, fmt.Errorf("certificate %s is invalid: %w", certPath, err)
	}
	cert.Leaf = leaf
	return cert, nil
}

func validateCertificate(cert *x509.Certificate, domains []string, now time.Time) error {
	if cert == nil {
		return fmt.Errorf("certificate is empty")
	}
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("certificate is not valid yet")
	}
	if !now.Before(cert.NotAfter) {
		return fmt.Errorf("certificate has expired")
	}
	if len(domains) == 0 {
		return fmt.Errorf("no configured domain to validate")
	}

	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if cert.VerifyHostname(domain) == nil || certificateCommonNameMatches(cert, domain) {
			return nil
		}
	}
	return fmt.Errorf("certificate common name or subject alternative name does not match configured domain")
}

func certificateCommonNameMatches(cert *x509.Certificate, domain string) bool {
	commonName := strings.TrimSpace(cert.Subject.CommonName)
	if commonName == "" {
		return false
	}
	if ip := net.ParseIP(domain); ip != nil {
		commonNameIP := net.ParseIP(commonName)
		return commonNameIP != nil && commonNameIP.Equal(ip)
	}
	// VerifyHostname implements the same wildcard and case rules for a CN
	// fallback while keeping SAN matching on the original certificate.
	commonNameCert := *cert
	commonNameCert.DNSNames = []string{commonName}
	commonNameCert.IPAddresses = nil
	return commonNameCert.VerifyHostname(domain) == nil
}

// getLetsEncryptTLSConfig configures the selected ACME challenge solver.
func (m *Manager) getLetsEncryptTLSConfig(domains []string) (*tls.Config, error) {
	if ParseChallenge(m.config.LetsEncrypt.Challenge) == "dns-01" {
		if err := ValidateDNSProvider(m.config.LetsEncrypt.DNSProvider, m.config.LetsEncrypt.DNSCredentials); err != nil {
			log.Printf("SSL: [ERROR] DNS provider validation failed: %v", err)
			return nil, err
		}
		return m.getDNS01TLSConfig(domains)
	}

	cacheDir := filepath.Join(m.config.CertPath, "autocert")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cert cache dir: %w", err)
	}

	m.certManager = &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domains...),
		Cache: renewalCache{
			Cache:    autocert.DirCache(cacheDir),
			CertPath: m.config.CertPath,
		},
		Email:       m.config.LetsEncrypt.Email,
		RenewBefore: 7 * 24 * time.Hour,
	}

	tlsConfig := m.certManager.TLSConfig()
	acmeGetCertificate := tlsConfig.GetCertificate

	// Preserve the existing HTTP-01/TLS-ALPN behavior when autocert cannot
	// complete issuance.
	tlsConfig.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		cert, err := acmeGetCertificate(hello)
		if err == nil {
			return cert, nil
		}

		log.Printf("SSL: ACME issuance failed for %s, falling back to self-signed: %v", hello.ServerName, err)
		domain := hello.ServerName
		if domain == "" && len(domains) > 0 {
			domain = domains[0]
		}
		certPath, keyPath, sErr := GenerateSelfSigned(m.config.CertPath, domain)
		if sErr != nil {
			return nil, fmt.Errorf("ACME issuance failed and self-signed fallback failed: %w", sErr)
		}
		fallback, sErr := tls.LoadX509KeyPair(certPath, keyPath)
		if sErr != nil {
			return nil, fmt.Errorf("ACME issuance failed and self-signed fallback failed to load: %w", sErr)
		}
		return &fallback, nil
	}

	return tlsConfig, nil
}

// renewalCache wraps autocert's certificate cache so that a completed
// issuance or renewal raises the AI.md PART 17 ssl_renewed operator event.
// autocert exposes no post-renewal callback of its own; writing the new
// certificate bundle to the cache is the exact moment the certificate becomes
// live, which makes Put the only reliable hook.
type renewalCache struct {
	autocert.Cache
	CertPath string
}

func (c renewalCache) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := c.Cache.Get(ctx, key)
	if err == nil {
		if !strings.Contains(key, "+") && safeCertificateDomain.MatchString(key) {
			if storeErr := c.storeCertificate(key, data); storeErr != nil {
				log.Printf("SSL: failed to sync app-managed certificate files for %s: %v", key, storeErr)
			}
		}
		return data, nil
	}
	if strings.Contains(key, "+") || !safeCertificateDomain.MatchString(key) {
		return data, err
	}
	domain := dnsCertificateDirectoryName(key)
	cert, certErr := os.ReadFile(filepath.Join(c.CertPath, "letsencrypt", domain, "fullchain.pem"))
	privateKey, keyErr := os.ReadFile(filepath.Join(c.CertPath, "letsencrypt", domain, "privkey.pem"))
	if certErr != nil || keyErr != nil {
		return nil, err
	}
	return append(cert, privateKey...), nil
}

// Put stores the bundle first and only raises the event once the write
// succeeded, so a failed cache write never reports a renewal that did not
// actually take effect.
func (c renewalCache) Put(ctx context.Context, key string, data []byte) error {
	if err := c.Cache.Put(ctx, key, data); err != nil {
		return err
	}
	if !strings.Contains(key, "+") && safeCertificateDomain.MatchString(key) {
		if err := c.storeCertificate(key, data); err != nil {
			return err
		}
	}

	notifyCertificateRenewed(key, data)
	return nil
}

func (c renewalCache) storeCertificate(domain string, data []byte) error {
	var certificatePEM, privateKeyPEM []byte
	for rest := data; len(rest) > 0; {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = remaining
		encoded := pem.EncodeToMemory(block)
		if block.Type == "CERTIFICATE" {
			certificatePEM = append(certificatePEM, encoded...)
		} else if strings.Contains(block.Type, "PRIVATE KEY") {
			privateKeyPEM = append(privateKeyPEM, encoded...)
		}
	}
	if len(certificatePEM) == 0 || len(privateKeyPEM) == 0 {
		return errors.New("ACME certificate bundle is missing its certificate or private key")
	}
	directory := filepath.Join(c.CertPath, "letsencrypt", dnsCertificateDirectoryName(domain))
	if err := atomicPrivateWrite(filepath.Join(directory, "fullchain.pem"), certificatePEM); err != nil {
		return err
	}
	return atomicPrivateWrite(filepath.Join(directory, "privkey.pem"), privateKeyPEM)
}

// RenewCertificate runs the active autocert manager against its configured
// domains. The shared manager keeps HTTP-01 challenge state available to the
// live HTTP handler while the daily scheduler requests renewal.
func (m *Manager) RenewCertificate() error {
	m.mu.RLock()
	manager := m.certManager
	domains := append([]string(nil), m.domains...)
	m.mu.RUnlock()
	if ParseChallenge(m.config.LetsEncrypt.Challenge) == "dns-01" {
		if len(domains) == 0 {
			return errors.New("DNS-01 certificate domains are not configured")
		}
		return m.RenewDNSCertificate(domains)
	}
	if manager == nil {
		return errors.New("automatic ACME certificate manager is not configured")
	}
	for _, domain := range domains {
		if _, err := manager.GetCertificate(&tls.ClientHelloInfo{ServerName: domain}); err != nil {
			return fmt.Errorf("renew certificate for %s: %w", domain, err)
		}
	}
	return nil
}

// ManagedCertificatePath returns the app-managed certificate path for the
// first domain configured on this manager.
func (m *Manager) ManagedCertificatePath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.managesACME || m.config.CertPath == "" || len(m.domains) == 0 {
		return ""
	}
	domain := dnsCertificateDirectoryName(m.domains[0])
	return filepath.Join(m.config.CertPath, "letsencrypt", domain, "fullchain.pem")
}

// ManagedDomain returns the first domain configured on this manager.
func (m *Manager) ManagedDomain() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.domains) == 0 {
		return ""
	}
	return m.domains[0]
}

// notifyCertificateRenewed logs the ssl_renewed event and hands it to the
// operator notifier. autocert also caches its ACME account key and HTTP-01
// tokens; those keys carry a "+" separator and are skipped so only real
// certificate bundles are reported.
func notifyCertificateRenewed(key string, data []byte) {
	if strings.Contains(key, "+") {
		return
	}

	validUntil := "unknown"
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			break
		}
		validUntil = leaf.NotAfter.UTC().Format("2006-01-02 15:04:05 MST")
		break
	}

	log.Printf("SSL: [INFO] ssl_renewed fqdn=%s valid_until=%s", key, validUntil)
	email.OperatorSSLRenewed(key, validUntil)
}

// getSelfSignedTLSConfig generates (or reuses a previously generated)
// self-signed certificate for the first domain and returns a TLS config
// serving it. Used when Let's Encrypt is disabled and no existing/manual
// certificate was found, and for overlay addresses (.onion/.i2p) which
// Let's Encrypt cannot certify at all, per AI.md PART 15.
func (m *Manager) getSelfSignedTLSConfig(domains []string) (*tls.Config, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("no domain given to generate a self-signed certificate for")
	}

	certPath, keyPath, err := GenerateSelfSigned(m.config.CertPath, domains[0])
	if err != nil {
		return nil, fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	log.Printf("Using self-signed certificate: %s", certPath)
	tlsCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load self-signed certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// GetHTTPHandler returns HTTP handler for ACME challenges
func (m *Manager) GetHTTPHandler(fallback http.Handler) http.Handler {
	if m.certManager != nil {
		return m.certManager.HTTPHandler(fallback)
	}
	return fallback
}

// findExistingCerts returns system-managed certbot certificates. App-managed
// ACME certificates must pass through autocert so its renewal state and live
// HTTP-01/TLS-ALPN challenge handlers remain active.
func (m *Manager) findExistingCerts(domains []string) (certPath, keyPath string) {
	for _, domain := range domains {
		cert := filepath.Join("/etc/letsencrypt/live", dnsCertificateDirectoryName(domain), "fullchain.pem")
		key := filepath.Join("/etc/letsencrypt/live", dnsCertificateDirectoryName(domain), "privkey.pem")
		if fileExists(cert) && fileExists(key) {
			return cert, key
		}
	}

	if m.config.CertPath != "" && !m.config.LetsEncrypt.Enabled {
		for _, domain := range domains {
			cert := filepath.Join(m.config.CertPath, "letsencrypt", dnsCertificateDirectoryName(domain), "fullchain.pem")
			key := filepath.Join(m.config.CertPath, "letsencrypt", dnsCertificateDirectoryName(domain), "privkey.pem")
			if fileExists(cert) && fileExists(key) {
				return cert, key
			}
		}
	}

	return "", ""
}

func (m *Manager) findSystemCerts(domains []string) (certPath, keyPath string) {
	for _, domain := range domains {
		cert := filepath.Join("/etc/letsencrypt/live", dnsCertificateDirectoryName(domain), "fullchain.pem")
		key := filepath.Join("/etc/letsencrypt/live", dnsCertificateDirectoryName(domain), "privkey.pem")
		if fileExists(cert) && fileExists(key) {
			return cert, key
		}
	}
	return "", ""
}

// findManualCerts looks for manually placed certificates: the spec's tier-4
// {config_dir}/ssl/local/{fqdn}/{cert.pem,key.pem} layout (user-managed, no
// auto-renewal) plus legacy flat/fullchain naming under the configured cert
// path for backward compatibility.
func (m *Manager) findManualCerts(domains []string) (certPath, keyPath string) {
	if m.config.CertPath == "" {
		return "", ""
	}

	for _, domain := range domains {
		cert := filepath.Join(m.config.CertPath, "local", domain, "cert.pem")
		key := filepath.Join(m.config.CertPath, "local", domain, "key.pem")
		if fileExists(cert) && fileExists(key) {
			return cert, key
		}

		cert = filepath.Join(m.config.CertPath, domain+".crt")
		key = filepath.Join(m.config.CertPath, domain+".key")
		if fileExists(cert) && fileExists(key) {
			return cert, key
		}

		cert = filepath.Join(m.config.CertPath, domain, "fullchain.pem")
		key = filepath.Join(m.config.CertPath, domain, "privkey.pem")
		if fileExists(cert) && fileExists(key) {
			return cert, key
		}
	}

	return "", ""
}

// ChallengeServer handles ACME HTTP-01 challenges
type ChallengeServer struct {
	tokens map[string]string
	mu     sync.RWMutex
}

// NewChallengeServer creates a challenge server
func NewChallengeServer() *ChallengeServer {
	return &ChallengeServer{
		tokens: make(map[string]string),
	}
}

// SetToken sets a challenge token
func (cs *ChallengeServer) SetToken(token, auth string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.tokens[token] = auth
}

// ClearToken removes a challenge token
func (cs *ChallengeServer) ClearToken(token string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.tokens, token)
}

// ServeHTTP handles ACME challenge requests
func (cs *ChallengeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
		return false
	}

	token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
	cs.mu.RLock()
	auth, ok := cs.tokens[token]
	cs.mu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return true
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(auth))
	return true
}

// ParseChallenge parses challenge type from string
func ParseChallenge(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "http-01", "http01", "http":
		return "http-01"
	case "tls-alpn-01", "tlsalpn01", "tls-alpn", "tls":
		return "tls-alpn-01"
	case "dns-01", "dns01", "dns":
		return "dns-01"
	default:
		return "http-01"
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
