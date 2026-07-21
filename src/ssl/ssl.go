package ssl

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/acme/autocert"
)

// Config holds SSL/TLS configuration
type Config struct {
	Enabled     bool
	CertPath    string
	LetsEncrypt LetsEncryptConfig
}

// LetsEncryptConfig holds Let's Encrypt settings
type LetsEncryptConfig struct {
	Enabled         bool
	Email           string
	Challenge       string // http-01, tls-alpn-01, dns-01
	DNSProviderType string
	DNSProviderKey  string
	RFC2136Server   string
	RFC2136Name     string
	RFC2136Algo     string
}

// Manager handles SSL/TLS certificates
type Manager struct {
	config      Config
	certManager *autocert.Manager
	mu          sync.RWMutex
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

	if !m.config.Enabled {
		return nil, nil
	}

	// Check for existing certificates first (e.g., from /etc/letsencrypt/live)
	if cert, key := m.findExistingCerts(domains); cert != "" && key != "" {
		log.Printf("Using existing certificate: %s", cert)
		tlsCert, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("failed to load certificate: %w", err)
		}
		return &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		}, nil
	}

	// Use Let's Encrypt if enabled
	if m.config.LetsEncrypt.Enabled {
		return m.getLetsEncryptTLSConfig(domains)
	}

	// Check for manual certificates
	if cert, key := m.findManualCerts(domains); cert != "" && key != "" {
		log.Printf("Using manual certificate: %s", cert)
		tlsCert, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("failed to load certificate: %w", err)
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

// getLetsEncryptTLSConfig configures autocert for Let's Encrypt. autocert
// only supports the HTTP-01 and TLS-ALPN-01 challenge types - DNS-01 is not
// achievable through it (see PerformDNS01Challenge / AI.md PART 15 DNS-01
// Provider Configuration, which requires a lego-based client and per-provider
// credentials; tracked as a follow-up, not implemented here).
func (m *Manager) getLetsEncryptTLSConfig(domains []string) (*tls.Config, error) {
	if ParseChallenge(m.config.LetsEncrypt.Challenge) == "dns-01" {
		log.Printf("SSL: dns-01 challenge requested but not supported by the current ACME client; falling back to self-signed for %v", domains)
		return m.getSelfSignedTLSConfig(domains)
	}

	cacheDir := filepath.Join(m.config.CertPath, "autocert")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cert cache dir: %w", err)
	}

	m.certManager = &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domains...),
		Cache:      autocert.DirCache(cacheDir),
		Email:      m.config.LetsEncrypt.Email,
	}

	tlsConfig := m.certManager.TLSConfig()
	acmeGetCertificate := tlsConfig.GetCertificate

	// Wrap autocert's issuance so that a failed/unreachable ACME request
	// falls back to a self-signed certificate instead of failing the TLS
	// handshake outright, per AI.md PART 15's no-self-signed-fallback gap.
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

// findExistingCerts looks for system-managed certificates the app must never
// renew itself, per AI.md PART 15's 4-tier priority chain: certbot's
// /etc/letsencrypt/live (tiers 1-2), then the app-managed
// {config_dir}/ssl/letsencrypt/{fqdn}/ tier (tier 3, previously issued by
// this app via autocert and auto-renewed here rather than by certbot).
func (m *Manager) findExistingCerts(domains []string) (certPath, keyPath string) {
	for _, domain := range domains {
		cert := filepath.Join("/etc/letsencrypt/live", domain, "fullchain.pem")
		key := filepath.Join("/etc/letsencrypt/live", domain, "privkey.pem")
		if fileExists(cert) && fileExists(key) {
			return cert, key
		}
	}

	if m.config.CertPath != "" {
		for _, domain := range domains {
			cert := filepath.Join(m.config.CertPath, "letsencrypt", domain, "fullchain.pem")
			key := filepath.Join(m.config.CertPath, "letsencrypt", domain, "privkey.pem")
			if fileExists(cert) && fileExists(key) {
				return cert, key
			}
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
	w.Write([]byte(auth))
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
