package urlutil

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FetchRemoteImageConfig configures remote image fetching
type FetchRemoteImageConfig struct {
	// Max file size in bytes (default: 10MB)
	MaxSize int64
	// Request timeout (default: 30s)
	Timeout time.Duration
	// Allowed MIME types
	AllowedTypes []string
	// Allowed URL schemes (default: https only)
	AllowedSchemes []string
	// User-Agent sent with the request
	UserAgent string
}

// DefaultFetchRemoteImageConfig returns safe defaults
func DefaultFetchRemoteImageConfig() FetchRemoteImageConfig {
	return FetchRemoteImageConfig{
		// 10MB
		MaxSize:      10 * 1024 * 1024,
		Timeout:      30 * time.Second,
		AllowedTypes: []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/x-icon"},
		// NEVER allow http in production
		AllowedSchemes: []string{"https"},
		UserAgent:      "api/1.0",
	}
}

// ValidateRemoteURL validates a URL before fetching
func ValidateRemoteURL(rawURL string, cfg FetchRemoteImageConfig) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	schemeAllowed := false
	for _, s := range cfg.AllowedSchemes {
		if strings.EqualFold(u.Scheme, s) {
			schemeAllowed = true
			break
		}
	}
	if !schemeAllowed {
		return fmt.Errorf("scheme not allowed: %s (allowed: %v)", u.Scheme, cfg.AllowedSchemes)
	}

	// Block localhost/loopback by name before any DNS work so an SSRF attempt
	// against the server itself never reaches the resolver.
	hostname := strings.ToLower(u.Hostname())
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return fmt.Errorf("localhost URLs not allowed")
	}

	if strings.HasSuffix(hostname, ".local") || strings.HasSuffix(hostname, ".internal") {
		return fmt.Errorf("internal hostnames not allowed")
	}

	// SSRF prevention: every resolved address must be publicly routable
	if err := validateNotPrivateIP(hostname); err != nil {
		return err
	}

	return nil
}

// validateNotPrivateIP checks if hostname resolves to private IP
func validateNotPrivateIP(hostname string) error {
	if ip := net.ParseIP(hostname); ip != nil {
		if isDisallowedIP(ip) {
			return fmt.Errorf("private/local IP not allowed: %s", ip)
		}
		return nil
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}

	for _, ip := range ips {
		if isDisallowedIP(ip) {
			return fmt.Errorf("private/local IP not allowed: %s resolves to %s", hostname, ip)
		}
	}
	return nil
}

// isDisallowedIP reports whether an address is outside the publicly routable
// space and therefore an SSRF risk (RFC 1918, loopback, link-local, ULA,
// unspecified, and multicast).
func isDisallowedIP(ip net.IP) bool {
	return ip.IsPrivate() ||
		ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

// FetchRemoteImage safely fetches an image from a remote URL
func FetchRemoteImage(ctx context.Context, rawURL string, cfg FetchRemoteImageConfig) ([]byte, string, error) {
	if err := ValidateRemoteURL(rawURL, cfg); err != nil {
		return nil, "", fmt.Errorf("URL validation failed: %w", err)
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Every redirect hop is re-validated: an allowed origin must not be
			// able to bounce the fetch into private address space.
			if err := ValidateRemoteURL(req.URL.String(), cfg); err != nil {
				return fmt.Errorf("redirect blocked: %w", err)
			}
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = "api/1.0"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", strings.Join(cfg.AllowedTypes, ", "))

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	typeAllowed := false
	for _, t := range cfg.AllowedTypes {
		if strings.HasPrefix(contentType, t) {
			typeAllowed = true
			break
		}
	}
	if !typeAllowed {
		return nil, "", fmt.Errorf("content type not allowed: %s", contentType)
	}

	// Read one byte past the cap so an oversized body is detected rather than
	// silently truncated to exactly MaxSize.
	limitedReader := io.LimitReader(resp.Body, cfg.MaxSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}
	if int64(len(data)) > cfg.MaxSize {
		return nil, "", fmt.Errorf("file too large (max: %d bytes)", cfg.MaxSize)
	}

	return data, contentType, nil
}
