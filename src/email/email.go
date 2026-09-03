package email

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config holds email configuration
type Config struct {
	Enabled   bool
	SMTPHost  string
	SMTPPort  int
	Username  string
	Password  string
	FromName  string
	FromEmail string
	// TLS selects the transport security mode for outbound mail, per
	// AI.md PART 17 "SMTP Config": auto, starttls, tls, none.
	TLS string
	// ReplyTo is server.notifications.email.reply_to. When non-empty it
	// is emitted as the Reply-To header on every message and supplies the
	// {notification_reply_to} template variable; empty means the header is
	// omitted entirely (AI.md PART 17 "Default Sender").
	ReplyTo string
}

// TLS mode values accepted by Config.TLS (AI.md PART 17 "SMTP Config").
const (
	TLSModeAuto     = "auto"
	TLSModeSTARTTLS = "starttls"
	TLSModeImplicit = "tls"
	TLSModeNone     = "none"
)

// implicitTLSPort is the conventional SMTPS port; in "auto" mode it
// selects implicit TLS instead of a plaintext dial + STARTTLS upgrade.
const implicitTLSPort = 465

// Message represents an email message
type Message struct {
	To      []string
	Subject string
	Body    string
	HTML    bool
}

// Client represents an email client
type Client struct {
	config Config
}

// NewClient creates a new email client
func NewClient(config Config) *Client {
	return &Client{
		config: config,
	}
}

// Config returns a copy of the client's configuration so callers outside
// this package can inspect the resolved SMTP settings (host/port for
// status output, ReplyTo for the {notification_reply_to} variable)
// without the package exposing its mutable internals.
func (c *Client) Config() Config {
	return c.config
}

// global holds the process-wide email Client, mirroring the
// src/tor.Set/Get singleton pattern so packages that cannot import the
// composition root (e.g. future scheduler notification tasks) can still
// send mail. Set once from main.go at startup, after SMTP auto-detect or
// connection-test; nil (never set) means email is unavailable.
var (
	globalMu sync.RWMutex
	global   *Client
)

// Set registers c as the process-wide email Client.
func Set(c *Client) {
	globalMu.Lock()
	defer globalMu.Unlock()
	global = c
}

// Get returns the process-wide email Client, or nil if none has been
// registered (email unavailable).
func Get() *Client {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return global
}

// Enabled reports whether the process-wide Client is registered and its
// config marks it enabled - the single check callers should use to decide
// whether to show or attempt any email-dependent feature, per AI.md
// PART 17 "Completely disable email features if no SMTP".
func Enabled() bool {
	c := Get()
	return c != nil && c.config.Enabled
}

// headerOrder fixes the emission order of the message headers so the
// rendered message is byte-stable across sends (Go map iteration is not).
var headerOrder = []string{"From", "To", "Reply-To", "Subject", "MIME-Version", "Content-Type"}

// buildMessage renders the RFC 5322 headers and body for msg.
func (c *Client) buildMessage(msg Message, from string) []byte {
	headers := map[string]string{
		"From":         fmt.Sprintf("%s <%s>", c.config.FromName, from),
		"To":           strings.Join(msg.To, ", "),
		"Subject":      msg.Subject,
		"MIME-Version": "1.0",
		"Content-Type": "text/plain; charset=utf-8",
	}
	if msg.HTML {
		headers["Content-Type"] = "text/html; charset=utf-8"
	}
	if c.config.ReplyTo != "" {
		headers["Reply-To"] = c.config.ReplyTo
	}

	var b strings.Builder
	for _, k := range headerOrder {
		v, ok := headers[k]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	b.WriteString("\r\n")
	b.WriteString(msg.Body)
	return []byte(b.String())
}

// isLoopbackHost reports whether host names the local machine. Local
// relays almost always present a self-signed certificate, so "auto" mode
// tolerates an unverifiable certificate there rather than refusing to
// deliver mail on a zero-config first run.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// dialSMTP opens an SMTP session honouring the configured TLS mode.
func (c *Client) dialSMTP() (*smtp.Client, error) {
	host := c.config.SMTPHost
	addr := net.JoinHostPort(host, strconv.Itoa(c.config.SMTPPort))
	mode := strings.ToLower(strings.TrimSpace(c.config.TLS))
	if mode == "" {
		mode = TLSModeAuto
	}

	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	loopback := isLoopbackHost(host)
	if mode == TLSModeAuto && loopback && c.config.SMTPPort == implicitTLSPort {
		// A loopback relay on the SMTPS port practically always presents a
		// self-signed certificate, and the connection never leaves this
		// machine, so there is no network path for an interceptor. Only
		// this one narrow combination relaxes verification; every remote
		// host, and every explicitly configured TLS mode, verifies.
		tlsConfig.InsecureSkipVerify = true
	}

	implicit := mode == TLSModeImplicit || (mode == TLSModeAuto && c.config.SMTPPort == implicitTLSPort)
	if implicit {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", addr, tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("TLS connection failed: %w", err)
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("SMTP handshake failed: %w", err)
		}
		return client, nil
	}

	client, err := c.dialPlain(addr, host)
	if err != nil {
		return nil, err
	}

	if mode == TLSModeNone {
		return client, nil
	}

	ok, _ := client.Extension("STARTTLS")
	if !ok {
		if mode == TLSModeSTARTTLS {
			client.Close()
			return nil, fmt.Errorf("server does not advertise STARTTLS")
		}
		return client, nil
	}
	if err := client.StartTLS(tlsConfig); err != nil {
		client.Close()
		if mode == TLSModeAuto && loopback {
			// A self-signed loopback relay fails verification; fall back to
			// the plaintext session, which cannot leave this machine.
			return c.dialPlain(addr, host)
		}
		return nil, fmt.Errorf("STARTTLS failed: %w", err)
	}
	return client, nil
}

// dialPlain opens an unencrypted SMTP session to addr.
func (c *Client) dialPlain(addr, host string) (*smtp.Client, error) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMTP handshake failed: %w", err)
	}
	return client, nil
}

// Send sends an email message
func (c *Client) Send(msg Message) error {
	if !c.config.Enabled {
		return fmt.Errorf("email is not enabled")
	}
	if len(msg.To) == 0 {
		return fmt.Errorf("email has no recipients")
	}

	from := c.config.FromEmail
	if from == "" {
		from = "no-reply@localhost"
	}
	payload := c.buildMessage(msg, from)

	client, err := c.dialSMTP()
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	defer client.Close()

	if c.config.Username != "" && c.config.Password != "" {
		auth := smtp.PlainAuth("", c.config.Username, c.config.Password, c.config.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("failed to send email: SMTP auth failed: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	for _, rcpt := range msg.To {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Printf("Email: Sent to %v via %s", msg.To, c.config.SMTPHost)
	return nil
}

// dialTimeout bounds every SMTP dial - both the startup connection test
// and the auto-detection probe sweep, which walks up to 7 hosts x 3 ports
// and must not stall startup on unreachable candidates.
var dialTimeout = 5 * time.Second

// TestConnection tests the SMTP connection with an EHLO handshake, per
// AI.md PART 17 "Connection Test (when host is set)". Port 465 is spoken
// over implicit TLS; every other port is probed in plaintext.
func TestConnection(host string, port int) error {
	client, err := probeSMTP(host, port)
	if err != nil {
		return err
	}
	defer client.Quit()

	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("EHLO failed: %w", err)
	}

	log.Printf("Email: SMTP connection test successful (%s:%d)", host, port)
	return nil
}

// probeSMTP opens a bare SMTP session for handshake testing. Certificate
// verification is deliberately skipped here because a probe only needs to
// establish that something speaking SMTP is listening; no mail and no
// credentials cross this connection, and local relays are routinely
// self-signed. Actual delivery in Client.Send verifies certificates.
func probeSMTP(host string, port int) (*smtp.Client, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	if port == implicitTLSPort {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", addr,
			&tls.Config{ServerName: host, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err != nil {
			return nil, fmt.Errorf("connection failed: %w", err)
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("SMTP handshake failed: %w", err)
		}
		return client, nil
	}

	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMTP handshake failed: %w", err)
	}
	return client, nil
}

// AutoDetectSMTP probes for a usable local SMTP relay using the full
// priority table from AI.md PART 17 "SMTP Auto-Detection", deriving the
// FQDN candidate from the DOMAIN env var or the system hostname. Callers
// that already resolved the server FQDN should use AutoDetectSMTPFor.
func AutoDetectSMTP() (host string, port int, found bool) {
	return AutoDetectSMTPFor(detectFQDN())
}

// AutoDetectSMTPFor probes the AI.md PART 17 host priority list -
// 127.0.0.1, the Docker bridge gateway, the default gateway, the given
// FQDN, the global IPv4 address, then mail.{fqdn} and smtp.{fqdn} - on
// ports 25, 465 and 587, returning the first host/port pair that
// completes an SMTP handshake. Finding nothing is not an error: email
// features are simply left disabled.
func AutoDetectSMTPFor(fqdn string) (host string, port int, found bool) {
	// Ports to check (in priority order, per AI.md PART 17)
	ports := []int{25, 465, 587}

	log.Println("Email: Auto-detecting SMTP server...")

	for _, h := range detectionHosts(fqdn) {
		for _, p := range ports {
			if err := TestConnection(h, p); err == nil {
				log.Printf("Email: Auto-detected SMTP at %s:%d", h, p)
				return h, p, true
			}
		}
	}

	log.Println("Email: No SMTP server detected (email features will be disabled)")
	return "", 0, false
}

// detectionHosts builds the ordered, de-duplicated candidate host list
// from AI.md PART 17's auto-detection priority table. Candidates that
// cannot be determined on this machine (no default gateway, no global
// IPv4, no FQDN) are simply skipped.
func detectionHosts(fqdn string) []string {
	candidates := []string{
		"127.0.0.1",
		"172.17.0.1",
		defaultGatewayIP(),
		fqdn,
		globalIPv4(),
	}
	if fqdn != "" {
		candidates = append(candidates, "mail."+fqdn, "smtp."+fqdn)
	}

	seen := make(map[string]bool, len(candidates))
	hosts := make([]string, 0, len(candidates))
	for _, h := range candidates {
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		hosts = append(hosts, h)
	}
	return hosts
}

// detectFQDN resolves the FQDN candidate for auto-detection priorities
// 4, 6 and 7 from the DOMAIN env var, falling back to the system
// hostname. A single-label hostname is rejected: it cannot host the
// mail./smtp. subdomain candidates and duplicates the loopback probe.
func detectFQDN() string {
	if v := strings.TrimSpace(os.Getenv("DOMAIN")); v != "" {
		// DOMAIN accepts a comma-separated list; the first entry is the
		// primary domain (PART 15 "TLS/Let's Encrypt").
		if idx := strings.Index(v, ","); idx != -1 {
			v = strings.TrimSpace(v[:idx])
		}
		if strings.Contains(v, ".") {
			return v
		}
	}
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	if !strings.Contains(name, ".") {
		return ""
	}
	return name
}

// globalIPv4 returns the first globally routable IPv4 address bound to a
// local interface, or "" when the host has none (the common case behind
// NAT), per auto-detection priority 5.
func globalIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil {
			continue
		}
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		return ip.String()
	}
	return ""
}
