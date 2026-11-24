package ssl

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/providers/dns"
	"github.com/go-acme/lego/v5/registration"
)

var dnsProviderEnvironmentMu sync.Mutex
var dnsProviderEnvironmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var safeCertificateDomain = regexp.MustCompile(`^(?:\*\.)?[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)

type acmeUser struct {
	email        string
	privateKey   crypto.Signer
	registration *acme.ExtendedAccount
}

func (u *acmeUser) GetEmail() string                       { return u.email }
func (u *acmeUser) GetRegistration() *acme.ExtendedAccount { return u.registration }
func (u *acmeUser) GetPrivateKey() crypto.Signer           { return u.privateKey }

// ValidateDNSProvider constructs lego's provider without touching DNS. The
// environment is scoped to the constructor call and restored before return.
func ValidateDNSProvider(provider string, credentials map[string]string) error {
	_, err := newDNSProvider(provider, credentials)
	return err
}

func newDNSProvider(provider string, credentials map[string]string) (dnsProvider, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return nil, errors.New("DNS provider is required")
	}
	for name := range credentials {
		if !dnsProviderEnvironmentName.MatchString(name) {
			return nil, fmt.Errorf("invalid DNS credential variable name %q", name)
		}
	}
	dnsProviderEnvironmentMu.Lock()
	defer dnsProviderEnvironmentMu.Unlock()
	previous := make(map[string]savedEnvironment, len(credentials))
	for name, value := range credentials {
		old, exists := os.LookupEnv(name)
		previous[name] = savedEnvironment{value: old, exists: exists}
		if err := os.Setenv(name, value); err != nil {
			restoreEnvironment(previous)
			return nil, fmt.Errorf("cannot configure DNS credential variable %s", name)
		}
	}
	defer restoreEnvironment(previous)
	providerObject, err := dns.NewDNSChallengeProviderByName(provider)
	if err != nil {
		return nil, redactProviderError(err, credentials)
	}
	return providerObject, nil
}

type dnsProvider interface {
	Present(ctx context.Context, domain, token, keyAuth string) error
	CleanUp(ctx context.Context, domain, token, keyAuth string) error
}

type savedEnvironment struct {
	value  string
	exists bool
}

func restoreEnvironment(values map[string]savedEnvironment) {
	for name, old := range values {
		if old.exists {
			_ = os.Setenv(name, old.value)
		} else {
			_ = os.Unsetenv(name)
		}
	}
}

func redactProviderError(err error, credentials map[string]string) error {
	message := err.Error()
	if strings.Contains(strings.ToLower(message), "nameserver is missing") || strings.Contains(strings.ToLower(message), "nameserver missing") {
		return errors.New("DNS provider configuration failed: DNSUPDATE_NAMESERVER")
	}
	message = redactCredentialValues(message, credentials)
	if message == "" {
		message = "invalid provider configuration"
	}
	return fmt.Errorf("DNS provider configuration failed: %s", message)
}

func redactCredentialValues(message string, credentials map[string]string) string {
	for _, value := range credentials {
		if value != "" {
			message = strings.ReplaceAll(message, value, "[redacted]")
		}
	}
	return message
}

func (m *Manager) getDNS01TLSConfig(domains []string) (*tls.Config, error) {
	if len(domains) == 0 {
		return nil, errors.New("no domain given for DNS-01 certificate request")
	}
	if m.config.CertPath == "" {
		return nil, errors.New("certificate path is required for DNS-01")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			requestDomains := append([]string(nil), domains...)
			validationDomains := append([]string(nil), domains...)
			if hello != nil && hello.ServerName != "" {
				configuredName := configuredDNSName(domains, hello.ServerName)
				if configuredName == "" {
					return nil, errors.New("DNS-01 certificate request host is not configured")
				}
				requestDomains = []string{configuredName}
				validationDomains = []string{hello.ServerName}
			} else if strings.HasPrefix(requestDomains[0], "*.") {
				validationDomains = []string{"wildcard-check" + requestDomains[0][1:]}
			}
			m.dnsMu.Lock()
			defer m.dnsMu.Unlock()
			domain := dnsCertificateDirectoryName(requestDomains[0])
			certPath := filepath.Join(m.config.CertPath, "letsencrypt", domain, "fullchain.pem")
			keyPath := filepath.Join(m.config.CertPath, "letsencrypt", domain, "privkey.pem")
			if fileExists(certPath) && fileExists(keyPath) {
				existing, err := loadValidatedCertificate(certPath, keyPath, validationDomains)
				if err == nil {
					return &existing, nil
				}
				if err != nil {
					log.Printf("SSL: app-managed DNS-01 certificate for %s is invalid; attempting renewal", requestDomains[0])
				}
			}
			return m.obtainDNSCertificateLocked(requestDomains)
		},
	}, nil
}

func configuredDNSName(domains []string, candidate string) string {
	for _, domain := range domains {
		if strings.EqualFold(strings.TrimSuffix(domain, "."), strings.TrimSuffix(candidate, ".")) {
			return domain
		}
		if strings.HasPrefix(domain, "*.") && strings.HasSuffix(strings.ToLower(candidate), strings.ToLower(domain[1:])) {
			return domain
		}
	}
	return ""
}

// RenewDNSCertificate performs a DNS-01 renewal for the supplied names. The
// scheduler calls this only when the currently served certificate is within
// the normal seven-day renewal window.
func (m *Manager) RenewDNSCertificate(domains []string) error {
	_, err := m.obtainDNSCertificate(domains)
	return err
}

func (m *Manager) obtainDNSCertificate(domains []string) (*tls.Certificate, error) {
	m.dnsMu.Lock()
	defer m.dnsMu.Unlock()
	return m.obtainDNSCertificateLocked(domains)
}

func (m *Manager) obtainDNSCertificateLocked(domains []string) (*tls.Certificate, error) {
	// This construction is also the required pre-request validation; its result
	// is handed directly to lego as the DNS-01 solver.
	provider, err := newDNSProvider(m.config.LetsEncrypt.DNSProvider, m.config.LetsEncrypt.DNSCredentials)
	if err != nil {
		log.Printf("SSL: [ERROR] DNS provider validation failed: %v", err)
		return nil, err
	}
	user, err := m.loadACMEUser()
	if err != nil {
		return nil, fmt.Errorf("DNS-01 account initialization failed")
	}
	clientConfig := lego.NewConfig(user)
	client, err := lego.NewClient(clientConfig)
	if err != nil {
		return nil, errors.New("DNS-01 ACME client initialization failed")
	}
	if err := client.Challenge.SetDNS01Provider(provider); err != nil {
		return nil, errors.New("DNS-01 solver initialization failed")
	}
	if user.registration == nil {
		account, registerErr := client.Registration.Register(context.Background(), registration.RegisterOptions{TermsOfServiceAgreed: true})
		if registerErr != nil {
			return nil, errors.New("DNS-01 ACME account registration failed")
		}
		user.registration = account
		if err := m.saveACMEUser(user); err != nil {
			return nil, errors.New("DNS-01 ACME account persistence failed")
		}
	}
	resource, err := client.Certificate.Obtain(context.Background(), certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
		KeyType: certcrypto.EC256,
	})
	if err != nil {
		// Keep provider response codes such as REFUSED and BADSIG visible while
		// removing every configured credential value from logs and errors.
		safeMessage := redactCredentialValues(err.Error(), m.config.LetsEncrypt.DNSCredentials)
		log.Printf("SSL: [ERROR] DNS-01 certificate issuance failed for %s: %s", strings.Join(domains, ","), safeMessage)
		return nil, fmt.Errorf("DNS-01 certificate issuance failed for %s: %s", strings.Join(domains, ","), safeMessage)
	}
	if err := m.storeDNSCertificate(domains[0], resource.Certificate, resource.PrivateKey); err != nil {
		return nil, errors.New("DNS-01 certificate storage failed")
	}
	notifyCertificateRenewed(domains[0], resource.Certificate)
	if m.config.OnDNSCertificateIssued != nil {
		m.config.OnDNSCertificateIssued(time.Now().UTC())
	}
	cert, err := tls.X509KeyPair(resource.Certificate, resource.PrivateKey)
	if err != nil {
		return nil, errors.New("DNS-01 certificate could not be loaded")
	}
	return &cert, nil
}

type persistedACMEUser struct {
	Email        string                `json:"email"`
	PrivateKey   string                `json:"private_key"`
	Registration *acme.ExtendedAccount `json:"registration,omitempty"`
}

func (m *Manager) loadACMEUser() (*acmeUser, error) {
	path := filepath.Join(m.config.CertPath, "lego", "account.json")
	if data, err := os.ReadFile(path); err == nil {
		var saved persistedACMEUser
		if err := json.Unmarshal(data, &saved); err != nil {
			return nil, err
		}
		block, _ := pem.Decode([]byte(saved.PrivateKey))
		if block == nil {
			return nil, errors.New("invalid ACME account private key")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		key, ok := parsed.(crypto.Signer)
		if !ok {
			return nil, errors.New("ACME account key is not a signer")
		}
		return &acmeUser{email: saved.Email, privateKey: key, registration: saved.Registration}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	user := &acmeUser{email: m.config.LetsEncrypt.Email, privateKey: key}
	if err := m.saveACMEUser(user); err != nil {
		return nil, err
	}
	return user, nil
}

func (m *Manager) saveACMEUser(user *acmeUser) error {
	keyBytes, err := x509.MarshalPKCS8PrivateKey(user.privateKey)
	if err != nil {
		return err
	}
	data, err := json.Marshal(persistedACMEUser{
		Email:        user.email,
		PrivateKey:   string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})),
		Registration: user.registration,
	})
	if err != nil {
		return err
	}
	dir := filepath.Join(m.config.CertPath, "lego")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return atomicPrivateWrite(filepath.Join(dir, "account.json"), data)
}

func (m *Manager) storeDNSCertificate(domain string, certPEM, keyPEM []byte) error {
	if !safeCertificateDomain.MatchString(domain) || strings.Contains(domain, "..") {
		return errors.New("invalid certificate domain")
	}
	dir := filepath.Join(m.config.CertPath, "letsencrypt", dnsCertificateDirectoryName(domain))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := atomicPrivateWrite(filepath.Join(dir, "fullchain.pem"), certPEM); err != nil {
		return err
	}
	return atomicPrivateWrite(filepath.Join(dir, "privkey.pem"), keyPEM)
}

func dnsCertificateDirectoryName(domain string) string {
	if strings.HasPrefix(domain, "*.") {
		return "wildcard-" + domain[2:]
	}
	return domain
}

func atomicPrivateWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".acme-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
