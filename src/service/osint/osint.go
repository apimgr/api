package osint

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
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
