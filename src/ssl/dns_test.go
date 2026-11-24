package ssl

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDNS01ReusesAppManagedCertificateOnHandshake(t *testing.T) {
	root := t.TempDir()
	domain := "dns.example.test"
	certificatePath, privateKeyPath, err := GenerateSelfSigned(root, domain)
	require.NoError(t, err)
	certificatePEM, err := os.ReadFile(certificatePath)
	require.NoError(t, err)
	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	require.NoError(t, err)
	managedDirectory := filepath.Join(root, "letsencrypt", domain)
	require.NoError(t, os.MkdirAll(managedDirectory, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(managedDirectory, "fullchain.pem"), certificatePEM, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(managedDirectory, "privkey.pem"), privateKeyPEM, 0600))

	manager := NewManager(Config{
		CertPath: root,
		LetsEncrypt: LetsEncryptConfig{
			Enabled:   true,
			Challenge: "dns-01",
		},
	})
	tlsConfig, err := manager.getDNS01TLSConfig([]string{domain})
	require.NoError(t, err)
	certificate, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{ServerName: domain})
	require.NoError(t, err)
	require.NotNil(t, certificate.Leaf)
	assert.Equal(t, domain, certificate.Leaf.DNSNames[0])
}

func TestDNS01ReusesWildcardCertificateForConfiguredHost(t *testing.T) {
	root := t.TempDir()
	domain := "*.example.test"
	certificatePath, privateKeyPath, err := GenerateSelfSigned(root, domain)
	require.NoError(t, err)
	certificatePEM, err := os.ReadFile(certificatePath)
	require.NoError(t, err)
	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	require.NoError(t, err)
	managedDirectory := filepath.Join(root, "letsencrypt", "wildcard-example.test")
	require.NoError(t, os.MkdirAll(managedDirectory, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(managedDirectory, "fullchain.pem"), certificatePEM, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(managedDirectory, "privkey.pem"), privateKeyPEM, 0600))

	manager := NewManager(Config{
		CertPath: root,
		LetsEncrypt: LetsEncryptConfig{
			Enabled:   true,
			Challenge: "dns-01",
		},
	})
	tlsConfig, err := manager.getDNS01TLSConfig([]string{domain})
	require.NoError(t, err)
	certificate, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{ServerName: "api.example.test"})
	require.NoError(t, err)
	require.NotNil(t, certificate.Leaf)
	assert.Equal(t, domain, certificate.Leaf.DNSNames[0])
}

func TestDNSProviderValidationUsesAndRestoresScopedEnvironment(t *testing.T) {
	t.Setenv("CF_DNS_API_TOKEN", "preexisting-value")
	err := ValidateDNSProvider("cloudflare", map[string]string{"CF_DNS_API_TOKEN": "configured-value"})
	require.NoError(t, err)
	assert.Equal(t, "preexisting-value", os.Getenv("CF_DNS_API_TOKEN"))
}

func TestDNSProviderValidationDoesNotAcceptUnknownProvider(t *testing.T) {
	err := ValidateDNSProvider("not-a-lego-provider", map[string]string{"EXAMPLE_TOKEN": "secret-value"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-value")
}

func TestRFC2136ProviderAcceptsBothCredentialNamespacesAndDefaultPort(t *testing.T) {
	require.NoError(t, ValidateDNSProvider("rfc2136", map[string]string{"DNSUPDATE_NAMESERVER": "127.0.0.1"}))
	require.NoError(t, ValidateDNSProvider("dnsupdate", map[string]string{"RFC2136_NAMESERVER": "127.0.0.1"}))
}

func TestDNSProviderValidationReportsMissingRFC2136VariableName(t *testing.T) {
	t.Setenv("DNSUPDATE_NAMESERVER", "")
	t.Setenv("RFC2136_NAMESERVER", "")
	err := ValidateDNSProvider("rfc2136", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DNSUPDATE_NAMESERVER")
}

func TestDNSChallengeConfigurationFailsWithoutSelfSignedFallback(t *testing.T) {
	manager := NewManager(Config{
		Enabled:  true,
		CertPath: t.TempDir(),
		LetsEncrypt: LetsEncryptConfig{
			Enabled:     true,
			Challenge:   "dns-01",
			DNSProvider: "not-a-lego-provider",
		},
	})
	config, err := manager.GetTLSConfig([]string{"dns.example.test"})
	require.Error(t, err)
	assert.Nil(t, config)
	assert.NotContains(t, err.Error(), "secret")
	assert.NoFileExists(t, filepath.Join(manager.config.CertPath, "local", "dns.example.test", "cert.pem"))
}

func TestRFC2136SignedUpdateInsertsAndRemovesChallenge(t *testing.T) {
	const keyName = "acme.example.test."
	const secret = "IwBTJx9wrDp4Y1RyC3H0gA=="
	messages, updates, stop := startDNSUpdateServer(t, map[string]string{keyName: secret})
	defer stop()
	provider, err := newDNSProvider("rfc2136", map[string]string{
		"DNSUPDATE_NAMESERVER":     updates,
		"DNSUPDATE_ZONES":          "example.test.",
		"DNSUPDATE_TSIG_KEY":       keyName,
		"DNSUPDATE_TSIG_SECRET":    secret,
		"DNSUPDATE_TSIG_ALGORITHM": dns.HmacSHA256,
		"DNSUPDATE_DNS_TIMEOUT":    "2",
	})
	require.NoError(t, err)
	require.NoError(t, provider.Present(context.Background(), "www.example.test", "token", "key-authorization"))
	require.NoError(t, provider.CleanUp(context.Background(), "www.example.test", "token", "key-authorization"))
	first, second := <-messages, <-messages
	classes := updateTXTClasses(first)
	assert.Contains(t, classes, dns.ClassINET)
	assert.Contains(t, classes, dns.ClassANY)
	classes = updateTXTClasses(second)
	assert.Contains(t, classes, dns.ClassNONE)
	assert.True(t, first.IsTsig() != nil)
	assert.True(t, second.IsTsig() != nil)
}

func TestRFC2136WrongTSIGKeyFails(t *testing.T) {
	const keyName = "acme.example.test."
	_, updates, stop := startDNSUpdateServer(t, map[string]string{keyName: "IwBTJx9wrDp4Y1RyC3H0gA=="})
	defer stop()
	provider, err := newDNSProvider("rfc2136", map[string]string{
		"DNSUPDATE_NAMESERVER":     updates,
		"DNSUPDATE_ZONES":          "example.test.",
		"DNSUPDATE_TSIG_KEY":       keyName,
		"DNSUPDATE_TSIG_SECRET":    "AAAAAAAAAAAAAAAAAAAAAA==",
		"DNSUPDATE_TSIG_ALGORITHM": dns.HmacSHA256,
		"DNSUPDATE_DNS_TIMEOUT":    "2",
	})
	require.NoError(t, err)
	err = provider.Present(context.Background(), "www.example.test", "token", "key-authorization")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "AAAAAAAA")
}

func TestRFC2136UnsignedUpdateAndRFC2136Aliases(t *testing.T) {
	messages, updates, stop := startDNSUpdateServer(t, nil)
	defer stop()
	t.Setenv("DNSUPDATE_NAMESERVER", "")
	t.Setenv("DNSUPDATE_ZONES", "")
	provider, err := newDNSProvider("dnsupdate", map[string]string{
		"RFC2136_NAMESERVER": updates,
		"RFC2136_ZONES":      "example.test.",
	})
	require.NoError(t, err)
	require.NoError(t, provider.Present(context.Background(), "www.example.test", "token", "key-authorization"))
	update := <-messages
	assert.Nil(t, update.IsTsig())
	require.NoError(t, provider.CleanUp(context.Background(), "www.example.test", "token", "key-authorization"))
	assert.Nil(t, (<-messages).IsTsig())
}

func TestRFC2136NameserverWithoutPortUsesDefaultPort(t *testing.T) {
	provider, err := newDNSProvider("rfc2136", map[string]string{
		"DNSUPDATE_NAMESERVER": "127.0.0.1",
	})
	require.NoError(t, err)
	assert.NotNil(t, provider)
}

func startDNSUpdateServer(t *testing.T, secrets map[string]string) (<-chan *dns.Msg, string, func()) {
	t.Helper()
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	updates := make(chan *dns.Msg, 8)
	handler := dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
		if request.Opcode == dns.OpcodeUpdate {
			updates <- request
		}
		reply := new(dns.Msg)
		reply.SetReply(request)
		if request.IsTsig() != nil && w.TsigStatus() != nil {
			reply.SetRcode(request, dns.RcodeBadSig)
		}
		_ = w.WriteMsg(reply)
	})
	server := &dns.Server{
		PacketConn: packet,
		Handler:    handler,
		TsigSecret: secrets,
		MsgAcceptFunc: func(header dns.Header) dns.MsgAcceptAction {
			if int(header.Bits>>11)&0xF == dns.OpcodeUpdate {
				return dns.MsgAccept
			}
			return dns.DefaultMsgAcceptFunc(header)
		},
	}
	go func() { _ = server.ActivateAndServe() }()
	return updates, packet.LocalAddr().String(), func() { _ = server.Shutdown() }
}

func updateTXTClasses(message *dns.Msg) []int {
	var classes []int
	for _, record := range message.Ns {
		if strings.EqualFold(record.Header().Name, "_acme-challenge.www.example.test.") && record.Header().Rrtype == dns.TypeTXT {
			classes = append(classes, int(record.Header().Class))
		}
	}
	return classes
}
