package email

import (
	"mime"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		SMTPHost:  "smtp.example.com",
		SMTPPort:  587,
		FromName:  "Test",
		FromEmail: "test@example.com",
	}

	client := NewClient(cfg)
	require.NotNil(t, client)
	assert.Equal(t, cfg, client.config)
}

func TestSend_DisabledReturnsError(t *testing.T) {
	client := NewClient(Config{Enabled: false})

	err := client.Send(Message{To: []string{"a@example.com"}, Subject: "hi", Body: "body"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestSend_EnabledButUnreachableHostFails(t *testing.T) {
	client := NewClient(Config{
		Enabled:  true,
		SMTPHost: "127.0.0.1",
		// Port 1 is a privileged, essentially-never-listening port, so the
		// dial fails fast without needing a live SMTP server.
		SMTPPort:  1,
		FromEmail: "test@example.com",
	})

	err := client.Send(Message{To: []string{"a@example.com"}, Subject: "hi", Body: "body"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send email")
}

func TestTestConnection_UnreachableHostFails(t *testing.T) {
	err := TestConnection("127.0.0.1", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
}

func TestTestConnection_NonSMTPPortFailsHandshake(t *testing.T) {
	// Start a bare TCP listener that never speaks SMTP, so the dial
	// succeeds but the SMTP handshake fails - exercising the
	// "SMTP handshake failed" path without any live SMTP server.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			// Immediately close so any client read fails, forcing the
			// SMTP handshake to error out.
			_ = conn.Close()
		}
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	err = TestConnection(host, port)
	require.Error(t, err)
}

func TestTestConnection_SilentSMTPGreetingIsBounded(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()

	_, portText, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	started := time.Now()
	err = testConnectionWithTimeout(Config{SMTPHost: "127.0.0.1", SMTPPort: port, TLS: TLSModeNone}, false, 100*time.Millisecond)
	require.Error(t, err)
	assert.Less(t, time.Since(started), time.Second)
	select {
	case conn := <-accepted:
		_ = conn.Close()
	default:
	}
}

func TestAutoDetectSMTP_NoServerFound(t *testing.T) {
	// In the test/CI sandbox there is no SMTP server listening on any of
	// the checked host/port combinations, so this should reliably report
	// not-found rather than hang or error.
	host, port, found := AutoDetectSMTP()
	if found {
		t.Skipf("unexpectedly found a live SMTP server at %s:%d in this environment", host, port)
	}
	assert.False(t, found)
	assert.Equal(t, "", host)
	assert.Equal(t, 0, port)
}

func TestSend_DisabledClientNeverAttempts(t *testing.T) {
	client := NewClient(Config{Enabled: false})

	err := client.Send(Message{To: []string{"a@example.com"}, Subject: "subject", Body: "body"})
	require.Error(t, err)
}

func TestBuildMessage_HeaderOrderAndReplyTo(t *testing.T) {
	client := NewClient(Config{
		Enabled:   true,
		FromName:  "api",
		FromEmail: "no-reply@example.com",
		ReplyTo:   "ops@example.com",
	})

	raw := mustBuildMessage(t, client, Message{
		To:      []string{"a@example.com"},
		Subject: "Hello",
		Body:    "Body line",
	})

	assert.Less(t, strings.Index(raw, "From:"), strings.Index(raw, "To:"))
	assert.Less(t, strings.Index(raw, "To:"), strings.Index(raw, "Reply-To:"))
	assert.Less(t, strings.Index(raw, "Reply-To:"), strings.Index(raw, "Subject:"))
	assert.Contains(t, raw, "Reply-To: ops@example.com")
	assert.Contains(t, raw, "Content-Type: multipart/alternative")
	assert.Contains(t, raw, "Body line")
}

func TestBuildMessage_OmitsReplyToWhenUnset(t *testing.T) {
	client := NewClient(Config{Enabled: true, FromEmail: "no-reply@example.com"})
	raw := mustBuildMessage(t, client, Message{
		To:      []string{"a@example.com"},
		Subject: "Hello",
		Body:    "Body",
	})

	assert.NotContains(t, raw, "Reply-To:")
}

func TestBuildMessage_HTMLContentType(t *testing.T) {
	client := NewClient(Config{Enabled: true, FromEmail: "no-reply@example.com"})
	raw := mustBuildMessage(t, client, Message{
		To:      []string{"a@example.com"},
		Subject: "Hello",
		Body:    "<p>Body</p>",
		HTML:    true,
	})

	assert.Contains(t, raw, "Content-Type: multipart/alternative")
	assert.Contains(t, raw, "Content-Type: text/plain; charset=UTF-8")
	assert.Contains(t, raw, "Content-Type: text/html; charset=UTF-8")
}

func TestBuildMessage_EncryptedAttachmentIsBase64MIMEPart(t *testing.T) {
	client := NewClient(Config{Enabled: true, FromEmail: "no-reply@example.com"})
	raw := mustBuildMessage(t, client, Message{
		To:      []string{"security@example.com"},
		Subject: "Encrypted report",
		Body:    "Ciphertext attached.",
		Attachments: []Attachment{{
			Filename: "sec_123.enc", ContentType: "application/octet-stream", Data: []byte("secret bytes"),
		}},
	})

	assert.Contains(t, raw, "Content-Type: multipart/mixed;")
	assert.Contains(t, raw, "boundary=")
	assert.Contains(t, raw, "Ciphertext attached.")
	assert.Contains(t, raw, "filename=sec_123.enc")
	assert.Contains(t, raw, "c2VjcmV0IGJ5dGVz")
	assert.NotContains(t, raw, "secret bytes")
}

func mustBuildMessage(t *testing.T, client *Client, msg Message) string {
	t.Helper()
	if client.config.FromEmail == "" {
		client.config.FromEmail = "no-reply@example.com"
	}
	raw, err := client.buildMessage(msg, client.config.FromEmail, msg.To[0])
	require.NoError(t, err)
	return string(raw)
}

func TestBuildMessage_RFC5322HeadersAndEncodedNames(t *testing.T) {
	client := NewClient(Config{FromName: "Système API", FromEmail: "no-reply@example.com"})
	raw := mustBuildMessage(t, client, Message{
		To: []string{"one@example.com"}, Subject: "Mise à jour réussie", Body: "Message body",
	})
	message, err := mail.ReadMessage(strings.NewReader(raw))
	require.NoError(t, err)
	date, err := mail.ParseDate(message.Header.Get("Date"))
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), date, time.Minute)
	assert.Regexp(t, `^<[0-9a-f]{32}@example\.com>$`, message.Header.Get("Message-ID"))
	assert.Equal(t, "auto-generated", message.Header.Get("Auto-Submitted"))
	assert.Equal(t, "1.0", message.Header.Get("MIME-Version"))
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, "Mise à jour réussie", subject)
	from, err := mail.ParseAddress(message.Header.Get("From"))
	require.NoError(t, err)
	assert.Equal(t, "Système API", from.Name)
	assert.Equal(t, "one@example.com", message.Header.Get("To"))
	assert.NotContains(t, message.Header, "Bcc")
}

func TestBuildMessage_RejectsHeaderInjectionAndEmptySubject(t *testing.T) {
	base := Message{To: []string{"one@example.com"}, Subject: "Subject", Body: "Body"}
	for name, mutate := range map[string]func(*Message, *Client){
		"recipient":     func(msg *Message, _ *Client) { msg.To[0] = "victim@example.com\r\nBcc: x@example.com" },
		"subject":       func(msg *Message, _ *Client) { msg.Subject = "safe\r\nBcc: x@example.com" },
		"sender name":   func(_ *Message, client *Client) { client.config.FromName = "safe\r\nBcc: x@example.com" },
		"empty subject": func(msg *Message, _ *Client) { msg.Subject = "  " },
	} {
		t.Run(name, func(t *testing.T) {
			msg := base
			msg.To = append([]string(nil), base.To...)
			client := NewClient(Config{FromName: "API", FromEmail: "no-reply@example.com"})
			mutate(&msg, client)
			_, err := client.buildMessage(msg, client.config.FromEmail, msg.To[0])
			require.Error(t, err)
		})
	}
}

func TestPrivateSMTPAddressClassification(t *testing.T) {
	for _, host := range []string{"10.0.0.1", "172.31.255.1", "192.168.1.2", "100.64.0.1", "127.0.0.1", "169.254.2.3", "::1", "fc00::1", "fe80::1"} {
		assert.True(t, isPrivateSMTPIP(host), host)
	}
	for _, host := range []string{"8.8.8.8", "2001:4860:4860::8888", "mail.example.com"} {
		assert.False(t, isPrivateSMTPIP(host), host)
	}
}

func TestMessage_Fields(t *testing.T) {
	msg := Message{
		To:      []string{"a@example.com", "b@example.com"},
		Subject: "Test Subject",
		Body:    "Test Body",
		HTML:    true,
	}

	assert.Len(t, msg.To, 2)
	assert.Equal(t, "Test Subject", msg.Subject)
	assert.Equal(t, "Test Body", msg.Body)
	assert.True(t, msg.HTML)
}

func TestTestConnection_TimesOutQuickly(t *testing.T) {
	start := time.Now()
	// A non-routable address (TEST-NET-1 documentation range) forces the
	// dial to hang until the 5-second DialTimeout expires rather than
	// getting an immediate connection-refused.
	_ = TestConnection("192.0.2.1", 25)
	elapsed := time.Since(start)

	// Must not exceed the 5s DialTimeout in email.go by more than a small
	// margin.
	assert.LessOrEqual(t, elapsed, 7*time.Second)
}
