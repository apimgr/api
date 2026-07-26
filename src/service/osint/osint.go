package osint

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/apimgr/api/src/geoip"
)

// Service provides OSINT (Open Source Intelligence) utilities
type Service struct{}

// New creates a new OSINT service
func New() *Service {
	return &Service{}
}

// Domain information
type DomainInfo struct {
	Domain      string   `json:"domain"`
	Registrar   string   `json:"registrar"`
	Created     string   `json:"created"`
	Expires     string   `json:"expires"`
	NameServers []string `json:"nameservers"`
}

// IP information
type IPInfo struct {
	IP        string  `json:"ip"`
	Country   string  `json:"country"`
	Region    string  `json:"region"`
	City      string  `json:"city"`
	ISP       string  `json:"isp"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

const whoisDialTimeout = 8 * time.Second

// WHOISLookup performs a free, keyless WHOIS lookup over TCP/43. It first
// queries the IANA root WHOIS server for the registrar-designated referral
// server, then queries that server for the actual record. The target is
// validated (loopback/link-local/private blocked) before any connection.
func (s *Service) WHOISLookup(domain string) (*DomainInfo, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil, fmt.Errorf("domain is required")
	}

	ctx := context.Background()
	if err := validateTarget(ctx, domain); err != nil {
		return nil, err
	}

	raw, err := queryWHOIS(ctx, "whois.iana.org", domain)
	if err != nil {
		return nil, err
	}

	if referral := parseWHOISReferral(raw); referral != "" {
		if err := validateTarget(ctx, referral); err == nil {
			if refRaw, err := queryWHOIS(ctx, referral, domain); err == nil {
				raw = refRaw
			}
		}
	}

	info := parseWHOISResponse(raw)
	info.Domain = domain
	return info, nil
}

// queryWHOIS sends a single WHOIS query to server:43 and returns the raw
// text response
func queryWHOIS(ctx context.Context, server, query string) (string, error) {
	dialer := net.Dialer{Timeout: whoisDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(server, "43"))
	if err != nil {
		return "", fmt.Errorf("failed to connect to WHOIS server %s: %w", server, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(whoisDialTimeout)); err != nil {
		return "", fmt.Errorf("failed to set WHOIS deadline: %w", err)
	}

	if _, err := conn.Write([]byte(query + "\r\n")); err != nil {
		return "", fmt.Errorf("failed to send WHOIS query: %w", err)
	}

	var sb strings.Builder
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// parseWHOISReferral extracts a "refer:" or "whois:" field pointing to the
// authoritative WHOIS server for the query
func parseWHOISReferral(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "refer:") || strings.HasPrefix(lower, "whois:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// parseWHOISResponse extracts the common WHOIS fields from a raw response.
// WHOIS output format varies by registry, so field names are matched
// case-insensitively against the most common label variants.
func parseWHOISResponse(raw string) *DomainInfo {
	info := &DomainInfo{}
	var nameServers []string

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%") || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		if value == "" {
			continue
		}

		switch key {
		case "registrar", "sponsoring registrar":
			info.Registrar = value
		case "creation date", "created", "created on", "domain registration date", "registered on":
			if info.Created == "" {
				info.Created = value
			}
		case "registry expiry date", "expiration date", "expiry date",
			"registrar registration expiration date", "paid-till":
			if info.Expires == "" {
				info.Expires = value
			}
		case "name server", "nserver", "nameserver", "nameservers":
			nameServers = append(nameServers, value)
		}
	}

	info.NameServers = nameServers
	return info
}

// DNSLookup performs a DNS lookup for the given record type via the system
// resolver. Only DNS records are returned — no connection is made to the
// resolved addresses, so the SSRF surface is limited to a literal-IP input
// check.
func (s *Service) DNSLookup(domain, recordType string) ([]string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, fmt.Errorf("domain is required")
	}
	if ip := net.ParseIP(domain); ip != nil && isBlockedIP(ip) {
		return nil, fmt.Errorf("target %q resolves to a non-routable address", domain)
	}

	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()
	resolver := net.Resolver{}

	switch strings.ToUpper(strings.TrimSpace(recordType)) {
	case "A":
		ips, err := resolver.LookupIP(ctx, "ip4", domain)
		if err != nil {
			return nil, fmt.Errorf("a-record lookup failed: %w", err)
		}
		results := make([]string, 0, len(ips))
		for _, ip := range ips {
			results = append(results, ip.String())
		}
		return results, nil

	case "AAAA":
		ips, err := resolver.LookupIP(ctx, "ip6", domain)
		if err != nil {
			return nil, fmt.Errorf("AAAA lookup failed: %w", err)
		}
		results := make([]string, 0, len(ips))
		for _, ip := range ips {
			results = append(results, ip.String())
		}
		return results, nil

	case "MX":
		records, err := resolver.LookupMX(ctx, domain)
		if err != nil {
			return nil, fmt.Errorf("MX lookup failed: %w", err)
		}
		results := make([]string, 0, len(records))
		for _, r := range records {
			results = append(results, fmt.Sprintf("%d %s", r.Pref, r.Host))
		}
		return results, nil

	case "TXT":
		records, err := resolver.LookupTXT(ctx, domain)
		if err != nil {
			return nil, fmt.Errorf("TXT lookup failed: %w", err)
		}
		return records, nil

	case "NS":
		records, err := resolver.LookupNS(ctx, domain)
		if err != nil {
			return nil, fmt.Errorf("NS lookup failed: %w", err)
		}
		results := make([]string, 0, len(records))
		for _, r := range records {
			results = append(results, r.Host)
		}
		return results, nil

	case "CNAME":
		cname, err := resolver.LookupCNAME(ctx, domain)
		if err != nil {
			return nil, fmt.Errorf("CNAME lookup failed: %w", err)
		}
		return []string{cname}, nil

	default:
		return nil, fmt.Errorf("unsupported record type: %s", recordType)
	}
}

// IPLookup resolves geolocation for a public IP address using the locally
// cached MaxMind GeoLite2-derived database (no per-request outbound call).
// Private/loopback/link-local addresses are rejected.
func (s *Service) IPLookup(ipStr string) (*IPInfo, error) {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ipStr)
	}
	if isBlockedIP(ip) {
		return nil, fmt.Errorf("lookup of private/loopback/link-local addresses is not permitted")
	}

	entry, err := geoip.Get().Lookup(ip.String())
	if err != nil {
		return nil, fmt.Errorf("IP lookup failed: %w", err)
	}

	return &IPInfo{
		IP:        entry.IP,
		Country:   entry.Country,
		Region:    entry.Region,
		City:      entry.City,
		ISP:       entry.ASNOrg,
		Latitude:  entry.Latitude,
		Longitude: entry.Longitude,
	}, nil
}

const sslDialTimeout = 8 * time.Second

// SSLInfo connects to host:443 (or host:port) and reads the peer's TLS
// certificate. No data beyond the TLS handshake is sent. The target is
// validated (loopback/link-local/private blocked) before any connection.
func (s *Service) SSLInfo(domain string) (map[string]interface{}, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, fmt.Errorf("domain is required")
	}

	host, port, err := net.SplitHostPort(domain)
	if err != nil {
		host = domain
		port = "443"
	}

	ctx, cancel := context.WithTimeout(context.Background(), sslDialTimeout)
	defer cancel()
	if err := validateTarget(ctx, host); err != nil {
		return nil, err
	}

	dialer := net.Dialer{Timeout: sslDialTimeout}
	conn, err := tls.DialWithDialer(&dialer, "tcp", net.JoinHostPort(host, port), &tls.Config{ServerName: host})
	if err != nil {
		return nil, fmt.Errorf("TLS handshake with %s failed: %w", domain, err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificate presented by %s", domain)
	}
	cert := certs[0]

	return map[string]interface{}{
		"subject":             cert.Subject.String(),
		"issuer":              cert.Issuer.String(),
		"not_before":          cert.NotBefore,
		"not_after":           cert.NotAfter,
		"dns_names":           cert.DNSNames,
		"serial_number":       cert.SerialNumber.String(),
		"version":             cert.Version,
		"signature_algorithm": cert.SignatureAlgorithm.String(),
		"is_expired":          time.Now().After(cert.NotAfter),
	}, nil
}

// commonSubdomainLabels is a small, fixed wordlist of frequently-used
// subdomain labels used for subdomain enumeration via the system DNS
// resolver — the same "System DNS resolver" trust boundary already used by
// DNSLookup. This is not a brute-force scan: it is a bounded, fixed set of
// well-known labels resolved one at a time.
var commonSubdomainLabels = []string{
	"www", "mail", "webmail", "smtp", "pop", "imap", "ftp",
	"ns1", "ns2", "dns", "api", "dev", "staging", "test",
	"admin", "portal", "blog", "shop", "store", "vpn", "cdn",
	"m", "mobile", "secure", "app", "cpanel", "autodiscover",
}

// Subdomain describes a discovered subdomain and the IPv4 addresses it
// resolves to
type Subdomain struct {
	Name string   `json:"name"`
	IPs  []string `json:"ips"`
}

// SubdomainEnum discovers subdomains of domain by resolving a small fixed
// wordlist of common subdomain labels through the system DNS resolver. Only
// labels that successfully resolve are returned. Same trust boundary and
// SSRF posture as DNSLookup: no connection is made to any resolved address,
// only the DNS answer is reported.
func (s *Service) SubdomainEnum(domain string) ([]Subdomain, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil, fmt.Errorf("domain is required")
	}
	if ip := net.ParseIP(domain); ip != nil {
		return nil, fmt.Errorf("subdomain enumeration requires a domain name, not an IP address")
	}

	resolver := net.Resolver{}
	var found []Subdomain
	for _, label := range commonSubdomainLabels {
		host := label + "." + domain
		ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
		ips, err := resolver.LookupIP(ctx, "ip4", host)
		cancel()
		if err != nil || len(ips) == 0 {
			continue
		}
		addrs := make([]string, 0, len(ips))
		for _, ip := range ips {
			addrs = append(addrs, ip.String())
		}
		found = append(found, Subdomain{Name: host, IPs: addrs})
	}
	if found == nil {
		found = []Subdomain{}
	}
	return found, nil
}

const techStackDialTimeout = 8 * time.Second

// generatorMetaRe extracts the content of a <meta name="generator"> tag
var generatorMetaRe = regexp.MustCompile(`(?i)<meta\s+name=["']generator["']\s+content=["']([^"']+)["']`)

// TechStackInfo summarizes technology signals observed in a single HTTP
// response
type TechStackInfo struct {
	URL        string   `json:"url"`
	StatusCode int      `json:"status_code"`
	Server     string   `json:"server,omitempty"`
	PoweredBy  string   `json:"x_powered_by,omitempty"`
	Generator  string   `json:"generator,omitempty"`
	Cookies    []string `json:"cookie_names,omitempty"`
	Detected   []string `json:"detected"`
}

// TechStack performs a single direct HTTP GET to a user-supplied URL and
// inspects the response headers, cookies, and HTML <meta name="generator">
// tag for common technology signatures. Analogous in shape to SSLInfo: one
// direct, user-directed connection, no data sent beyond the HTTP request
// line and standard headers, redirects are not followed. The target is
// validated (loopback/link-local/private blocked) before any connection.
func (s *Service) TechStack(rawURL string) (*TechStackInfo, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("url scheme must be http or https")
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("url must include a host")
	}

	ctx, cancel := context.WithTimeout(context.Background(), techStackDialTimeout)
	defer cancel()
	if err := validateTarget(ctx, parsed.Hostname()); err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: techStackDialTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", parsed.String(), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	info := &TechStackInfo{
		URL:        parsed.String(),
		StatusCode: resp.StatusCode,
		Server:     resp.Header.Get("Server"),
		PoweredBy:  resp.Header.Get("X-Powered-By"),
	}
	for _, c := range resp.Cookies() {
		info.Cookies = append(info.Cookies, c.Name)
	}
	if m := generatorMetaRe.FindSubmatch(body); m != nil {
		info.Generator = string(m[1])
	}

	info.Detected = detectTechnologies(info, body)
	return info, nil
}

// detectTechnologies applies a small set of header/cookie/body heuristics
// to guess commonly-used technologies from the single response already
// fetched by TechStack — no additional outbound calls are made.
func detectTechnologies(info *TechStackInfo, body []byte) []string {
	var detected []string
	add := func(name string) {
		for _, d := range detected {
			if d == name {
				return
			}
		}
		detected = append(detected, name)
	}

	server := strings.ToLower(info.Server)
	switch {
	case strings.Contains(server, "nginx"):
		add("Nginx")
	case strings.Contains(server, "apache"):
		add("Apache")
	case strings.Contains(server, "cloudflare"):
		add("Cloudflare")
	case strings.Contains(server, "iis"):
		add("Microsoft IIS")
	}

	if info.PoweredBy != "" {
		add(info.PoweredBy)
	}
	if info.Generator != "" {
		add(info.Generator)
	}

	html := string(body)
	signatures := map[string]string{
		"wp-content":       "WordPress",
		"wp-includes":      "WordPress",
		"/sites/default/":  "Drupal",
		"Joomla!":          "Joomla",
		"__NEXT_DATA__":    "Next.js",
		"ng-version":       "Angular",
		"data-vue-":        "Vue.js",
		"cdn.shopify.com":  "Shopify",
		"cdn.jsdelivr.net": "jsDelivr CDN",
	}
	for needle, name := range signatures {
		if strings.Contains(html, needle) {
			add(name)
		}
	}

	for _, c := range info.Cookies {
		switch {
		case strings.HasPrefix(c, "PHPSESSID"):
			add("PHP")
		case strings.HasPrefix(c, "JSESSIONID"):
			add("Java")
		case strings.HasPrefix(c, "ASP.NET_SessionId"):
			add("ASP.NET")
		}
	}

	if detected == nil {
		detected = []string{}
	}
	return detected
}
