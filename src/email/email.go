package email

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/netip"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	xhtml "golang.org/x/net/html"
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
	To       []string
	Subject  string
	Body     string
	HTML     bool
	TextBody string
	HTMLBody string
	// ReplyTo overrides the client's default only for this message. Callers
	// must supply a validated mailbox address, never raw user input.
	ReplyTo string
	// OmitReplyTo suppresses both the per-message override and the client's
	// default for notifications that must not route replies to submitters.
	OmitReplyTo bool
	Attachments []Attachment
}

// Attachment is a binary MIME attachment. Filenames are treated as display
// metadata and never used as filesystem paths by the mailer.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Client represents an email client
type Client struct {
	config      Config
	dialTimeout time.Duration
}

// NewClient creates a new email client
func NewClient(config Config) *Client {
	return newClient(config, true)
}

func newClient(config Config, logTLSPolicy bool) *Client {
	mode := strings.ToLower(strings.TrimSpace(config.TLS))
	if mode == "" {
		mode = TLSModeAuto
	}
	if logTLSPolicy && mode != TLSModeNone && isPrivateSMTPIP(config.SMTPHost) {
		log.Printf("SMTP host %s is a private address: TLS certificate verification skipped", config.SMTPHost)
	}
	return &Client{
		config:      config,
		dialTimeout: dialTimeout,
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
var headerOrder = []string{"From", "To", "Reply-To", "Subject", "Date", "Message-ID", "MIME-Version", "Auto-Submitted", "Content-Type"}

// buildMessage renders the RFC 5322 headers and body for msg.
func (c *Client) buildMessage(msg Message, from, recipient string) ([]byte, error) {
	parsedFrom, err := mail.ParseAddress(from)
	if err != nil || parsedFrom.Address != from {
		return nil, fmt.Errorf("email has an invalid sender address")
	}
	if strings.ContainsAny(c.config.FromName, "\r\n") {
		return nil, fmt.Errorf("email has an invalid sender name")
	}
	parsedRecipient, err := mail.ParseAddress(recipient)
	if err != nil || parsedRecipient.Address != recipient || strings.ContainsAny(recipient, "\r\n") {
		return nil, fmt.Errorf("email has an invalid recipient address")
	}
	if strings.TrimSpace(msg.Subject) == "" || strings.ContainsAny(msg.Subject, "\r\n") {
		return nil, fmt.Errorf("email has an invalid subject")
	}
	if strings.ContainsAny(msg.ReplyTo, "\r\n") {
		return nil, fmt.Errorf("email has an invalid Reply-To address")
	}
	messageID, err := createMessageID(parsedFrom.Address)
	if err != nil {
		return nil, fmt.Errorf("create email Message-ID: %w", err)
	}
	textBody, htmlBody := messageBodies(msg)
	if strings.TrimSpace(textBody) == "" || strings.TrimSpace(htmlBody) == "" {
		return nil, fmt.Errorf("email body must include non-empty text and HTML alternatives")
	}
	subject := msg.Subject
	if !isASCII(subject) {
		subject = mime.QEncoding.Encode("UTF-8", subject)
	}
	headers := map[string]string{
		"From":           (&mail.Address{Name: c.config.FromName, Address: from}).String(),
		"To":             recipient,
		"Subject":        subject,
		"Date":           time.Now().Format(time.RFC1123Z),
		"Message-ID":     messageID,
		"MIME-Version":   "1.0",
		"Auto-Submitted": "auto-generated",
	}
	replyTo := msg.ReplyTo
	if replyTo == "" && !msg.OmitReplyTo {
		replyTo = c.config.ReplyTo
	}
	if replyTo != "" {
		parsedReply, parseErr := mail.ParseAddress(replyTo)
		if parseErr != nil || parsedReply.Address != replyTo || strings.ContainsAny(replyTo, "\r\n") {
			return nil, fmt.Errorf("email has an invalid Reply-To address")
		}
		headers["Reply-To"] = replyTo
	}

	var body bytes.Buffer
	var topWriter, alternativeWriter *multipart.Writer
	if len(msg.Attachments) == 0 {
		alternativeWriter = multipart.NewWriter(&body)
		headers["Content-Type"] = "multipart/alternative; boundary=" + alternativeWriter.Boundary()
		if err := writeAlternatives(alternativeWriter, textBody, htmlBody); err != nil {
			return nil, err
		}
		if err := alternativeWriter.Close(); err != nil {
			return nil, err
		}
	} else {
		var alternative bytes.Buffer
		alternativeWriter = multipart.NewWriter(&alternative)
		if err := writeAlternatives(alternativeWriter, textBody, htmlBody); err != nil {
			return nil, err
		}
		if err := alternativeWriter.Close(); err != nil {
			return nil, err
		}
		topWriter = multipart.NewWriter(&body)
		headers["Content-Type"] = "multipart/mixed; boundary=" + topWriter.Boundary()
		partHeader := make(textproto.MIMEHeader)
		partHeader.Set("Content-Type", "multipart/alternative; boundary="+alternativeWriter.Boundary())
		part, err := topWriter.CreatePart(partHeader)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(alternative.Bytes()); err != nil {
			return nil, err
		}
		for _, attachment := range msg.Attachments {
			if strings.ContainsAny(attachment.Filename+attachment.ContentType, "\r\n") {
				return nil, fmt.Errorf("email attachment has invalid MIME metadata")
			}
			filename := filepath.Base(strings.ReplaceAll(attachment.Filename, "\\", "/"))
			if filename == "" || filename == "." {
				filename = "attachment.bin"
			}
			contentType := strings.TrimSpace(attachment.ContentType)
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			partHeader := make(textproto.MIMEHeader)
			partHeader.Set("Content-Type", mime.FormatMediaType(contentType, map[string]string{"name": filename}))
			partHeader.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
			partHeader.Set("Content-Transfer-Encoding", "base64")
			part, err := topWriter.CreatePart(partHeader)
			if err != nil {
				return nil, err
			}
			encoded := base64.StdEncoding.EncodeToString(attachment.Data)
			for len(encoded) > 76 {
				if _, err := part.Write([]byte(encoded[:76] + "\r\n")); err != nil {
					return nil, err
				}
				encoded = encoded[76:]
			}
			if _, err := part.Write([]byte(encoded + "\r\n")); err != nil {
				return nil, err
			}
		}
		if err := topWriter.Close(); err != nil {
			return nil, err
		}
	}

	var out bytes.Buffer
	for _, key := range headerOrder {
		if value, ok := headers[key]; ok {
			folded := foldHeader(key, value)
			for _, line := range strings.Split(key+": "+folded, "\r\n") {
				if len(line) > 998 {
					return nil, fmt.Errorf("email header %s exceeds 998 octets", key)
				}
			}
			fmt.Fprintf(&out, "%s: %s\r\n", key, folded)
		}
	}
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

func writeAlternatives(writer *multipart.Writer, textBody, htmlBody string) error {
	for _, alternative := range []struct{ contentType, body string }{
		{"text/plain; charset=UTF-8", textBody},
		{"text/html; charset=UTF-8", htmlBody},
	} {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", alternative.contentType)
		header.Set("Content-Transfer-Encoding", "quoted-printable")
		part, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		encoder := quotedprintable.NewWriter(part)
		if _, err := encoder.Write([]byte(alternative.body)); err != nil {
			return err
		}
		if err := encoder.Close(); err != nil {
			return err
		}
	}
	return nil
}

func messageBodies(msg Message) (plain, rich string) {
	plain = msg.TextBody
	rich = msg.HTMLBody
	if msg.HTML && rich == "" {
		rich = msg.Body
	}
	if !msg.HTML && plain == "" {
		plain = msg.Body
	}
	if plain == "" && rich != "" {
		plain = plainTextFromHTML(rich)
	}
	if rich == "" && plain != "" {
		rich = "<!doctype html><html><body><pre>" + html.EscapeString(plain) + "</pre></body></html>"
	}
	return plain, rich
}

func plainTextFromHTML(source string) string {
	doc, err := xhtml.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	var out strings.Builder
	var visit func(*xhtml.Node)
	visit = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && (node.Data == "script" || node.Data == "style") {
			return
		}
		if node.Type == xhtml.TextNode {
			out.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if node.Type == xhtml.ElementNode && (node.Data == "br" || node.Data == "p" || node.Data == "div" || node.Data == "li") {
			out.WriteByte('\n')
		}
	}
	visit(doc)
	return strings.TrimSpace(out.String())
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}

func createMessageID(from string) (string, error) {
	_, domain, ok := strings.Cut(from, "@")
	if !ok || domain == "" {
		return "", fmt.Errorf("sender address has no domain")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "<" + hex.EncodeToString(random[:]) + "@" + domain + ">", nil
}

func foldHeader(name, value string) string {
	limit := 78 - len(name) - 2
	if len(value) <= limit || strings.Contains(value, "\r\n") {
		return value
	}
	words := strings.Fields(value)
	var out strings.Builder
	line := 0
	for index, word := range words {
		sep := " "
		if index == 0 {
			sep = ""
		}
		if line+len(sep)+len(word) > limit {
			out.WriteString("\r\n ")
			line = 1
			sep = ""
		}
		out.WriteString(sep)
		out.WriteString(word)
		line += len(sep) + len(word)
	}
	return out.String()
}

// dialSMTP opens an SMTP session honouring the configured TLS mode.
func (c *Client) dialSMTP() (*smtp.Client, error) {
	client, _, err := c.dialSMTPWithConn()
	return client, err
}

func (c *Client) dialSMTPWithConn() (*smtp.Client, net.Conn, error) {
	host := c.config.SMTPHost
	addr := net.JoinHostPort(host, strconv.Itoa(c.config.SMTPPort))
	mode := strings.ToLower(strings.TrimSpace(c.config.TLS))
	if mode == "" {
		mode = TLSModeAuto
	}

	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if mode != TLSModeNone && isPrivateSMTPIP(host) {
		tlsConfig.InsecureSkipVerify = true
	}

	implicit := mode == TLSModeImplicit || (mode == TLSModeAuto && c.config.SMTPPort == implicitTLSPort)
	if implicit {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: c.dialTimeout}, "tcp", addr, tlsConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("TLS connection failed: %w", err)
		}
		_ = conn.SetDeadline(time.Now().Add(c.dialTimeout))
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("SMTP handshake failed: %w", err)
		}
		_ = conn.SetDeadline(time.Time{})
		return client, conn, nil
	}

	conn, err := net.DialTimeout("tcp", addr, c.dialTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("connection failed: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(c.dialTimeout))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("SMTP handshake failed: %w", err)
	}

	if mode == TLSModeNone {
		_ = conn.SetDeadline(time.Time{})
		return client, conn, nil
	}

	ok, _ := client.Extension("STARTTLS")
	if !ok {
		if mode == TLSModeSTARTTLS {
			_ = client.Close()
			return nil, nil, fmt.Errorf("server does not advertise STARTTLS")
		}
		_ = conn.SetDeadline(time.Time{})
		return client, conn, nil
	}
	if err := client.StartTLS(tlsConfig); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("STARTTLS failed: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	return client, conn, nil
}

// Send sends an email message
func (c *Client) Send(msg Message) error {
	if !c.config.Enabled {
		return fmt.Errorf("email is not enabled")
	}
	if len(msg.To) == 0 {
		return fmt.Errorf("email has no recipients")
	}
	replyTo := msg.ReplyTo
	if replyTo == "" && !msg.OmitReplyTo {
		replyTo = c.config.ReplyTo
	}
	if replyTo != "" {
		parsed, err := mail.ParseAddress(replyTo)
		if err != nil || parsed.Address != replyTo || strings.ContainsAny(replyTo, "\r\n") {
			return fmt.Errorf("email has an invalid Reply-To address")
		}
	}

	from := c.config.FromEmail
	if from == "" {
		from = "no-reply@localhost"
	}
	for _, recipient := range msg.To {
		if err := c.sendOne(msg, from, recipient); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) sendOne(msg Message, from, recipient string) error {
	payload, err := c.buildMessage(msg, from, recipient)
	if err != nil {
		return err
	}
	client, err := c.dialSMTP()
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	defer func() { _ = client.Close() }()

	if c.config.Username != "" && c.config.Password != "" {
		if _, ok := client.TLSConnectionState(); !ok {
			return fmt.Errorf("SMTP authentication refused without TLS")
		}
		auth := smtp.PlainAuth("", c.config.Username, c.config.Password, c.config.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("failed to send email: SMTP auth failed: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
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

	log.Printf("Email: Sent notification via %s", c.config.SMTPHost)
	return nil
}

// dialTimeout bounds normal SMTP dials and configured-host startup checks.
var dialTimeout = 5 * time.Second

// autoDetectDialTimeout keeps the ordered first-run probe sweep bounded so
// unreachable relays do not delay HTTP startup beyond its integration-test
// readiness window.
const autoDetectDialTimeout = time.Second

// TestConnection tests an SMTP endpoint using tls:auto, including the
// port-specific STARTTLS behavior used during SMTP autodetection.
func TestConnection(host string, port int) error {
	return testConnectionWithConfig(Config{SMTPHost: host, SMTPPort: port, TLS: TLSModeAuto}, false)
}

// TestConnectionWithConfig checks the SMTP session using the same TLS mode
// and certificate policy as actual delivery.
func TestConnectionWithConfig(config Config) error {
	return testConnectionWithConfig(config, false)
}

func testConnectionWithConfig(config Config, logTLSPolicy bool) error {
	return testConnectionWithTimeout(config, logTLSPolicy, dialTimeout)
}

func testConnectionWithTimeout(config Config, logTLSPolicy bool, timeout time.Duration) error {
	smtpClient := newClient(config, logTLSPolicy)
	smtpClient.dialTimeout = timeout
	client, conn, err := smtpClient.dialSMTPWithConn()
	if err != nil {
		return err
	}
	defer func() { _ = client.Quit() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("EHLO failed: %w", err)
	}
	if config.Username != "" && config.Password != "" {
		if _, ok := client.TLSConnectionState(); !ok {
			return fmt.Errorf("SMTP authentication requires TLS")
		}
	}

	log.Printf("Email: SMTP connection test successful (%s:%d)", config.SMTPHost, config.SMTPPort)
	return nil
}

func isPrivateSMTPIP(host string) bool {
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	sharedIPv4 := netip.MustParsePrefix("100.64.0.0/10")
	return addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || sharedIPv4.Contains(addr)
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
			if err := testConnectionWithTimeout(Config{SMTPHost: h, SMTPPort: p, TLS: TLSModeAuto}, false, autoDetectDialTimeout); err == nil {
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
