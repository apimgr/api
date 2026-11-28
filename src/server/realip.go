package server

import (
	"context"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/pires/go-proxyproto"
)

// originalPeerContextKey stores the original TCP peer address (before any
// X-Forwarded-For/X-Real-IP rewrite) so trust-gated consumers can always
// evaluate the real connecting peer, never a header-supplied value
const originalPeerContextKey contextKey = "originalPeer"

// alwaysTrustedProxyCIDRs are trusted without any configuration -
// loopback, RFC 1918 IPv4 private ranges, RFC 4193 IPv6 unique-local, and
// link-local ranges
var alwaysTrustedProxyCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"fc00::/7",
	"169.254.0.0/16",
	"fe80::/10",
}

// trustedProxyResolver resolves and caches the trusted_proxies.additional
// allow-list, re-resolving DNS names every 5 minutes
type trustedProxyResolver struct {
	mu          sync.RWMutex
	cidrs       []netip.Prefix
	ips         map[netip.Addr]bool
	lastResolve time.Time
	entries     []string
}

var proxyResolver = &trustedProxyResolver{}

// TorConnContext tags Tor backend connections with the circuit identity
// exported in Tor's PROXY-protocol source address.
func TorConnContext(previous func(context.Context, net.Conn) context.Context) func(context.Context, net.Conn) context.Context {
	return func(ctx context.Context, conn net.Conn) context.Context {
		if previous != nil {
			ctx = previous(ctx, conn)
		}
		proxyConn, ok := conn.(*proxyproto.Conn)
		if !ok {
			return ctx
		}
		header := proxyConn.ProxyHeader()
		if header == nil {
			return ctx
		}
		source, ok := header.SourceAddr.(*net.TCPAddr)
		if !ok {
			return ctx
		}
		ip := source.IP.To16()
		if ip == nil || ip[0] != 0xfc {
			return ctx
		}
		return context.WithValue(ctx, torCircuitContextKey, "tor:"+hex.EncodeToString(ip[8:16]))
	}
}

const torCircuitContextKey contextKey = "torCircuitID"

// IsTorRequest reports whether an HTTP connection arrived through the Tor backend.
func IsTorRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	circuitID, ok := r.Context().Value(torCircuitContextKey).(string)
	return ok && strings.HasPrefix(circuitID, "tor:")
}

// resolve refreshes the trusted-proxy allow-list from the configured
// entries, re-resolving DNS names at most once every 5 minutes
func (t *trustedProxyResolver) resolve(entries []string) {
	t.mu.Lock()
	sameEntries := len(entries) == len(t.entries)
	if sameEntries {
		for i, e := range entries {
			if e != t.entries[i] {
				sameEntries = false
				break
			}
		}
	}
	if sameEntries && time.Since(t.lastResolve) < 5*time.Minute {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	cidrs := make([]netip.Prefix, 0, len(entries))
	ips := make(map[netip.Addr]bool, len(entries))

	for _, entry := range entries {
		if strings.Contains(entry, "/") {
			if prefix, err := netip.ParsePrefix(entry); err == nil {
				bits := prefix.Bits()
				addr := prefix.Addr()
				if addr.Is4In6() {
					bits -= 96
					if bits < 0 {
						bits = 0
					}
				}
				prefix = netip.PrefixFrom(addr.Unmap(), bits)
				cidrs = append(cidrs, prefix.Masked())
			}
			continue
		}

		if ip, err := netip.ParseAddr(entry); err == nil {
			ips[ip.Unmap()] = true
			continue
		}

		// Treat as a DNS name - resolve to IPs
		resolved, err := net.LookupIP(entry)
		if err != nil {
			continue
		}
		for _, ip := range resolved {
			if addr, ok := netip.AddrFromSlice(ip); ok {
				ips[addr.Unmap()] = true
			}
		}
	}

	t.mu.Lock()
	t.cidrs = cidrs
	t.ips = ips
	t.entries = entries
	t.lastResolve = time.Now()
	t.mu.Unlock()
}

// trusts reports whether ip matches the resolved allow-list
func (t *trustedProxyResolver) trusts(ip netip.Addr) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ip = ip.Unmap()
	if t.ips[ip] {
		return true
	}
	for _, ipnet := range t.cidrs {
		if ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

// isTrustedPeer reports whether the given peer address (IP, with or
// without a port) is trusted to set X-Forwarded-*/X-Real-IP and related
// proxy headers - the always-trusted private ranges plus the configured
// server.trusted_proxies.additional allow-list
func isTrustedPeer(cfg *config.ServerConfig, addr string) bool {
	ip, ok := normalizeIP(addr)
	if !ok {
		return false
	}

	return isTrustedIP(cfg, ip)
}

// realIPMiddleware resolves the client IP from proxy headers, but only
// when the immediate TCP peer is a trusted proxy. The original peer
// address is preserved in the request context (under originalPeerContextKey)
// so trust-gated consumers elsewhere always evaluate the true connecting
// peer, never a header-supplied value. Untrusted peers fall through
// unmodified - RemoteAddr stays the direct connection.
func realIPMiddleware(cfg *config.ServerConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			originalPeer := r.RemoteAddr
			circuitID, torRequest := r.Context().Value(torCircuitContextKey).(string)
			if torRequest && circuitID != "" {
				originalPeer = circuitID
				r.RemoteAddr = circuitID
			}
			ctx := context.WithValue(r.Context(), originalPeerContextKey, originalPeer)
			ctx = context.WithValue(ctx, serverConfigKey, cfg)
			r = r.WithContext(ctx)
			if torRequest && circuitID != "" {
				next.ServeHTTP(w, r)
				return
			}

			if isTrustedPeer(cfg, originalPeer) {
				if clientIP := resolveForwardedClientIP(r, cfg, originalPeer); clientIP != "" {
					r.RemoteAddr = clientIP
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// resolveForwardedClientIP extracts the client IP from proxy headers,
// checked in priority order. Only called after the immediate peer has
// already been verified as trusted.
func resolveForwardedClientIP(r *http.Request, cfg *config.ServerConfig, peer string) string {
	order := []string{"X-Forwarded-For", "Forwarded", "CF-Connecting-IP", "True-Client-IP", "X-Real-IP", "X-Client-IP"}
	if cfg != nil {
		if configured := cfg.Server.TrustedProxies.ClientIPHeader; configured != "" && !strings.EqualFold(configured, "auto") {
			order = []string{http.CanonicalHeaderKey(configured)}
		}
	}
	peerIP, peerOK := normalizeIP(peer)
	if !peerOK {
		return ""
	}
	for _, header := range order {
		values := r.Header.Values(header)
		if len(values) == 0 {
			continue
		}
		if header == "X-Forwarded-For" || header == "Forwarded" {
			var hops []string
			for _, value := range values {
				if header == "Forwarded" {
					hops = append(hops, forwardedForValues(value)...)
				} else {
					hops = append(hops, strings.Split(value, ",")...)
				}
			}
			if len(hops) > 32 {
				hops = hops[len(hops)-32:]
			}
			return resolveProxyChain(hops, peerIP, cfg)
		}
		for _, value := range values {
			if addr, ok := normalizeIP(value); ok {
				return addr.String()
			}
		}
	}
	return ""
}

func resolveProxyChain(hops []string, peer netip.Addr, cfg *config.ServerConfig) string {
	nearest := peer
	var leftmost netip.Addr
	for i := len(hops) - 1; i >= 0; i-- {
		addr, ok := normalizeIP(hops[i])
		if !ok {
			return nearest.String()
		}
		leftmost = addr
		if !isTrustedIP(cfg, addr) {
			return addr.String()
		}
		nearest = addr
	}
	if leftmost.IsValid() {
		return leftmost.String()
	}
	return peer.String()
}

func isTrustedIP(cfg *config.ServerConfig, addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, cidr := range alwaysTrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err == nil && prefix.Contains(addr) {
			return true
		}
	}
	if cfg == nil {
		return false
	}
	if listenAddr, ok := normalizeIP(cfg.Server.Address); ok && listenAddr.Is4() {
		listenPrefix := netip.PrefixFrom(listenAddr, 24).Masked()
		if listenPrefix.Contains(addr) {
			return true
		}
	}
	proxyResolver.resolve(cfg.Server.TrustedProxies.Additional)
	return proxyResolver.trusts(addr)
}

func normalizeIP(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, false
	}
	host := value
	if strings.HasPrefix(host, "[") {
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = parsed
		} else if strings.HasSuffix(host, "]") {
			host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		} else {
			return netip.Addr{}, false
		}
	} else if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	} else if strings.Count(host, ":") == 1 {
		if i := strings.LastIndexByte(host, ':'); i > 0 && isDecimal(host[i+1:]) {
			host = host[:i]
		}
	}
	if i := strings.LastIndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func isDecimal(value string) bool {
	if value == "" {
		return false
	}
	_, err := strconv.Atoi(value)
	return err == nil
}

func forwardedForValues(value string) []string {
	var result []string
	for _, element := range splitOutsideQuotes(value, ',') {
		foundFor := false
		for _, parameter := range splitOutsideQuotes(element, ';') {
			name, raw, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if !found || !strings.EqualFold(strings.TrimSpace(name), "for") {
				continue
			}
			raw = strings.TrimSpace(raw)
			if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
				raw = strings.ReplaceAll(raw[1:len(raw)-1], `\"`, `"`)
			}
			result = append(result, raw)
			foundFor = true
			break
		}
		if !foundFor {
			result = append(result, "")
		}
	}
	return result
}

func splitOutsideQuotes(value string, separator byte) []string {
	var result []string
	start := 0
	quoted, escaped := false, false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if escaped {
			escaped = false
			continue
		}
		if quoted && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
			continue
		}
		if c == separator && !quoted {
			result = append(result, value[start:i])
			start = i + 1
		}
	}
	return append(result, value[start:])
}
