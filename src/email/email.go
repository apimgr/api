package email

import (
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Config holds email configuration
type Config struct {
	Enabled  bool
	SMTPHost string
	SMTPPort int
	Username string
	Password string
	FromName string
	FromEmail string
	TLS      string // auto, starttls, tls, none
}

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

// Send sends an email message
func (c *Client) Send(msg Message) error {
	if !c.config.Enabled {
		return fmt.Errorf("email is not enabled")
	}

	// Build email
	from := c.config.FromEmail
	if from == "" {
		from = "noreply@localhost"
	}

	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("%s <%s>", c.config.FromName, from)
	headers["To"] = strings.Join(msg.To, ", ")
	headers["Subject"] = msg.Subject
	headers["MIME-Version"] = "1.0"

	if msg.HTML {
		headers["Content-Type"] = "text/html; charset=utf-8"
	} else {
		headers["Content-Type"] = "text/plain; charset=utf-8"
	}

	// Build message
	var emailMsg string
	for k, v := range headers {
		emailMsg += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	emailMsg += "\r\n" + msg.Body

	// Send via SMTP
	addr := fmt.Sprintf("%s:%d", c.config.SMTPHost, c.config.SMTPPort)

	// Simple auth (if credentials provided)
	var auth smtp.Auth
	if c.config.Username != "" && c.config.Password != "" {
		auth = smtp.PlainAuth("", c.config.Username, c.config.Password, c.config.SMTPHost)
	}

	err := smtp.SendMail(addr, auth, from, msg.To, []byte(emailMsg))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Printf("Email: Sent to %v via %s", msg.To, c.config.SMTPHost)
	return nil
}

// TestConnection tests the SMTP connection
func TestConnection(host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)

	// Attempt to connect
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer conn.Close()

	// Try SMTP handshake
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("SMTP handshake failed: %w", err)
	}
	defer client.Quit()

	// Try EHLO
	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("EHLO failed: %w", err)
	}

	log.Printf("Email: SMTP connection test successful (%s:%d)", host, port)
	return nil
}

// AutoDetectSMTP attempts to auto-detect a local SMTP server
// Tests common SMTP ports on localhost and Docker gateway
func AutoDetectSMTP() (host string, port int, found bool) {
	// Hosts to check (in order)
	hosts := []string{
		"localhost",
		"127.0.0.1",
		"172.17.0.1", // Docker host
	}

	// Ports to check (in order)
	ports := []int{25, 587, 465}

	log.Println("Email: Auto-detecting SMTP server...")

	for _, h := range hosts {
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

// SendNotification sends a notification email
// This is a convenience function for system notifications
func SendNotification(client *Client, to []string, subject, body string) error {
	msg := Message{
		To:      to,
		Subject: subject,
		Body:    body,
		HTML:    false,
	}

	return client.Send(msg)
}

// SendWelcomeEmail sends a welcome email to a new user
func SendWelcomeEmail(client *Client, to, username string) error {
	msg := Message{
		To:      []string{to},
		Subject: "Welcome to API Toolkit",
		Body: fmt.Sprintf(`Hello %s,

Welcome to API Toolkit! Your account has been created successfully.

You can now access the admin panel at:
http://localhost:64580/admin

Best regards,
API Toolkit Team
`, username),
		HTML: false,
	}

	return client.Send(msg)
}

// SendPasswordResetEmail sends a password reset email
func SendPasswordResetEmail(client *Client, to, token string) error {
	resetURL := fmt.Sprintf("http://localhost:64580/auth/reset?token=%s", token)

	msg := Message{
		To:      []string{to},
		Subject: "Password Reset Request",
		Body: fmt.Sprintf(`A password reset was requested for your account.

Click the link below to reset your password:
%s

This link will expire in 1 hour.

If you did not request this reset, please ignore this email.

Best regards,
API Toolkit Team
`, resetURL),
		HTML: false,
	}

	return client.Send(msg)
}
