package osint

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// resolveTimeout bounds every system-resolver lookup performed before an
// OSINT function is allowed to reach out to a caller-supplied target
const resolveTimeout = 5 * time.Second

// isBlockedIP reports whether ip is loopback, link-local, private
// (RFC 1918/RFC 4193), unspecified, or multicast — none of these are
// legitimate OSINT targets and all are blocked to prevent SSRF /
// internal-network scanning
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

// validateTarget ensures a caller-supplied host is safe to resolve and
// connect to. Literal IP inputs are checked directly; hostnames are
// resolved through the system resolver (with a hard timeout) and every
// returned address is checked. This runs before any DNS/WHOIS/TLS call.
func validateTarget(ctx context.Context, host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("target host is required")
	}

	trimmed := strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")

	if ip := net.ParseIP(trimmed); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("target %q resolves to a non-routable address", host)
		}
		return nil
	}

	if strings.EqualFold(trimmed, "localhost") {
		return fmt.Errorf("target %q resolves to a non-routable address", host)
	}

	resolveCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	resolver := net.Resolver{}
	addrs, err := resolver.LookupIPAddr(resolveCtx, trimmed)
	if err != nil {
		return fmt.Errorf("failed to resolve %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("no addresses found for %q", host)
	}
	for _, a := range addrs {
		if isBlockedIP(a.IP) {
			return fmt.Errorf("target %q resolves to a non-routable address", host)
		}
	}
	return nil
}
