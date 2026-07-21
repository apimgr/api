package email

import (
	"net"
	"strconv"
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
	defer ln.Close()

	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			// Immediately close so any client read fails, forcing the
			// SMTP handshake to error out.
			conn.Close()
		}
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	err = TestConnection(host, port)
	require.Error(t, err)
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

func TestSendNotification_DisabledClientReturnsError(t *testing.T) {
	client := NewClient(Config{Enabled: false})

	err := SendNotification(client, []string{"a@example.com"}, "subject", "body")
	require.Error(t, err)
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
