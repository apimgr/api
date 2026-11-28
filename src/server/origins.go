package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/i2p"
	appmode "github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/tor"
	"golang.org/x/net/publicsuffix"
)

// GetURLVars resolves the public origin for a request. Forwarded values are
// considered only when the original TCP peer is a configured trusted proxy.
func GetURLVars(r *http.Request) (proto, fqdn, port string) {
	if r == nil {
		return "http", GetFQDN(""), ""
	}
	cfg, _ := r.Context().Value(serverConfigKey).(*config.ServerConfig)
	peer := originalPeerAddr(r)
	trusted := cfg != nil && isTrustedPeer(cfg, peer)
	if manager := tor.Get(); manager != nil {
		if onion := manager.OnionAddress(); onion != "" && strings.EqualFold(r.Host, onion) {
			return "http", onion, ""
		}
	}
	if isI2PBackendRequest(r) {
		if manager := i2p.Get(); manager != nil {
			if address := manager.Address(); address != "" {
				return "http", address, ""
			}
		}
	}

	proto = "http"
	if r.TLS != nil {
		proto = "https"
	}
	if trusted {
		forwardedProto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto"))
		if strings.EqualFold(forwardedProto, "https") || strings.EqualFold(forwardedProto, "http") {
			proto = strings.ToLower(forwardedProto)
		} else if forwardedSSL := firstForwardedValue(r.Header.Get("X-Forwarded-Ssl")); strings.EqualFold(forwardedSSL, "on") {
			proto = "https"
		} else if strings.EqualFold(forwardedSSL, "off") {
			proto = "http"
		} else if scheme := firstForwardedValue(r.Header.Get("X-Url-Scheme")); strings.EqualFold(scheme, "https") || strings.EqualFold(scheme, "http") {
			proto = strings.ToLower(scheme)
		} else if strings.EqualFold(firstForwardedValue(r.Header.Get("Front-End-Https")), "on") {
			proto = "https"
		}
	}

	if trusted {
		for _, header := range []string{"X-Forwarded-Host", "X-Real-Host", "X-Original-Host"} {
			if candidate := firstForwardedValue(r.Header.Get(header)); isPublicURLHost(candidate) {
				fqdn, port = splitURLHost(candidate)
				break
			}
		}
	}
	domains := configuredDomains(paths.ProjectName)
	primaryDomain := ""
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain != "" {
			primaryDomain = domain
			break
		}
	}
	if fqdn == "" && primaryDomain != "" {
		fqdn, port = splitURLHost(primaryDomain)
	} else if fqdn == "" && isPublicURLHost(r.Host) {
		host, hostPort := splitURLHost(r.Host)
		if primaryDomain == "" || domainAllowsHost(domains, host) {
			fqdn, port = host, hostPort
		}
	}
	if fqdn == "" {
		fqdn = fallbackHostname()
	}
	if port == "" && isPublicURLHost(r.Host) {
		host, hostPort := splitURLHost(r.Host)
		if (primaryDomain == "" || domainAllowsHost(domains, host)) && hostPort != "" {
			port = hostPort
		}
	}
	if trusted {
		if forwardedPort := firstForwardedValue(r.Header.Get("X-Forwarded-Port")); validPort(forwardedPort) {
			port = forwardedPort
		} else if realPort := firstForwardedValue(r.Header.Get("X-Real-Port")); validPort(realPort) {
			port = realPort
		}
	}
	if port == "" && cfg != nil && validPort(cfg.Server.Port) {
		port = cfg.Server.Port
	}
	if port == "80" && proto == "http" || port == "443" && proto == "https" {
		port = ""
	}
	return proto, fqdn, port
}

// BuildURL returns an absolute URL using the request's resolved public origin.
func BuildURL(r *http.Request, path string) string {
	proto, host, port := GetURLVars(r)
	if host == "" {
		return ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") && net.ParseIP(host) != nil {
		host = "[" + host + "]"
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if r != nil {
		if cfg, ok := r.Context().Value(serverConfigKey).(*config.ServerConfig); ok && cfg != nil && isTrustedPeer(cfg, originalPeerAddr(r)) {
			prefix := firstForwardedValue(r.Header.Get("X-Forwarded-Prefix"))
			if prefix == "" {
				prefix = firstForwardedValue(r.Header.Get("X-Forwarded-Path"))
			}
			if prefix == "" {
				prefix = firstForwardedValue(r.Header.Get("X-Script-Name"))
			}
			prefix = validURLPrefix(prefix)
			if prefix != "" {
				return applyURLPrefix(path, prefix, proto, host)
			}
		}
		if cfg, ok := r.Context().Value(serverConfigKey).(*config.ServerConfig); ok && cfg != nil {
			if prefix := validURLPrefix(cfg.Server.BaseURL); prefix != "" {
				return applyURLPrefix(path, prefix, proto, host)
			}
		}
	}
	return fmt.Sprintf("%s://%s%s", proto, host, path)
}

// GetFQDN resolves the static hostname used when no request context is
// available. DOMAIN is the explicit override; server.fqdn is informational
// configuration and is not an origin source.
var invalidDomainWarning sync.Once

func GetFQDN(projectName string) string {
	devMode := appmode.IsVerboseMode()
	domains := configuredDomains(projectName)
	hostname, _ := os.Hostname()
	return resolveFQDN(domains, hostname, os.Getenv("HOSTNAME"), globalIP(true), globalIP(false), devMode, projectName)
}

func configuredDomains(projectName string) []string {
	devMode := appmode.IsVerboseMode()
	domains := strings.Split(os.Getenv("DOMAIN"), ",")
	validDomains := make([]string, 0, len(domains))
	invalidDomain := false
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
		if domain == "" {
			continue
		}
		if isDomainNameValid(domain, devMode, projectName) {
			validDomains = append(validDomains, domain)
		} else {
			invalidDomain = true
		}
	}
	if invalidDomain {
		invalidDomainWarning.Do(func() {
			log.Print("Warning: invalid DOMAIN hostname entries were ignored")
		})
	}
	return validDomains
}

func resolveFQDN(domains []string, hostname, environmentHostname, publicIPv6, publicIPv4 string, devMode bool, projectName string) string {
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if isDomainNameValid(domain, devMode, projectName) && !isLoopbackHost(domain) {
			return domain
		}
	}
	for _, host := range []string{strings.TrimSpace(hostname), strings.TrimSpace(environmentHostname)} {
		if isDomainNameValid(host, devMode, projectName) && !isLoopbackHost(host) {
			return strings.ToLower(strings.TrimSuffix(host, "."))
		}
	}
	for _, host := range []string{publicIPv6, publicIPv4} {
		if validURLHost(host) && !isLoopbackHost(host) {
			return host
		}
	}
	return ""
}

func isDomainNameValid(host string, devMode bool, projectName string) bool {
	host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(host, ".")))
	if host == "" || strings.Contains(host, ":") || !validURLHost(host) || net.ParseIP(host) != nil {
		return false
	}
	if host == "localhost" {
		return devMode
	}
	if !strings.Contains(host, ".") {
		return false
	}
	if strings.HasSuffix(host, ".onion") || strings.HasSuffix(host, ".i2p") || strings.HasSuffix(host, ".exit") {
		return false
	}
	if projectName != "" && strings.HasSuffix(host, "."+strings.ToLower(strings.TrimSpace(projectName))) {
		return devMode
	}
	suffix, isICANN := publicsuffix.PublicSuffix(host)
	for _, devSuffix := range []string{"localhost", "test", "example", "invalid", "local", "lan", "internal", "home", "localdomain", "home.arpa", "intranet", "corp", "private"} {
		if suffix == devSuffix || host == devSuffix || strings.HasSuffix(host, "."+devSuffix) {
			return devMode
		}
	}
	if !isICANN && !devMode {
		return false
	}
	_, err := publicsuffix.EffectiveTLDPlusOne(host)
	return err == nil
}

func isPublicURLHost(value string) bool {
	if !validURLHost(value) {
		return false
	}
	host, _ := splitURLHost(value)
	return !isLoopbackHost(host)
}

// GetDisplayURL chooses an externally useful startup URL, replacing local or
// development-only names with a public interface address when one exists.
func GetDisplayURL(projectName string, port int, isHTTPS bool) string {
	host := GetFQDN(projectName)
	if host == "" {
		return ""
	}
	if isDevTLD(host, projectName) || strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		if ip := globalIP(true); ip != "" {
			host = ip
		} else if ip := globalIP(false); ip != "" {
			host = ip
		} else if isLoopbackHost(host) {
			return ""
		}
	}

	proto := "http"
	if isHTTPS {
		proto = "https"
	}
	urlHost := host
	if port > 0 && (port != 80 || proto != "http") && (port != 443 || proto != "https") {
		urlHost = net.JoinHostPort(host, strconv.Itoa(port))
	} else if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		urlHost = "[" + host + "]"
	}
	return proto + "://" + urlHost
}

func isDevTLD(host, projectName string) bool {
	lowerHost := strings.ToLower(strings.TrimSuffix(host, "."))
	lowerProject := strings.ToLower(strings.TrimSpace(projectName))
	if lowerProject != "" && (lowerHost == lowerProject || strings.HasSuffix(lowerHost, "."+lowerProject)) {
		return true
	}
	if lowerHost == "localhost" {
		return true
	}
	for _, tld := range []string{"local", "test", "example", "invalid", "localhost", "lan", "internal", "home", "localdomain", "home.arpa", "intranet", "corp", "private"} {
		if lowerHost == tld || strings.HasSuffix(lowerHost, "."+tld) {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func applyURLPrefix(path, prefix, proto, host string) string {
	path = prefix + "/" + strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	return fmt.Sprintf("%s://%s%s", proto, host, path)
}

func validURLPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" || strings.ContainsAny(prefix, "?#\\") {
		return ""
	}
	for _, segment := range strings.Split(prefix, "/") {
		if segment == ".." || segment == "." {
			return ""
		}
	}
	return "/" + strings.Trim(prefix, "/")
}

func validURLHost(value string) bool {
	host, port := splitURLHost(value)
	if host == "" || strings.ContainsAny(host, " \t\r\n/\\?#@") || port != "" && !validPort(port) {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if char == '-' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
				continue
			}
			return false
		}
	}
	return true
}

func splitURLHost(value string) (string, string) {
	if host, port, err := net.SplitHostPort(value); err == nil {
		return strings.Trim(host, "[]"), port
	}
	return strings.Trim(value, "[]"), ""
}

func validPort(value string) bool {
	if value == "" || len(value) > 5 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	port, err := strconv.Atoi(value)
	return err == nil && port > 0 && port <= 65535
}

func domainAllowsHost(domains []string, host string) bool {
	for _, domain := range domains {
		base, _ := splitURLHost(strings.TrimSpace(domain))
		if base == host || strings.HasSuffix(host, "."+base) {
			return true
		}
	}
	return false
}

func fallbackHostname() string {
	return GetFQDN("")
}

func globalIP(wantIPv6 bool) string {
	if addresses, err := net.InterfaceAddrs(); err == nil {
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || (ip.To4() == nil) != wantIPv6 || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

// originSet is a thread-safe, order-preserving, deduplicated collection of
// reverse-proxy-learned origins (step 3 of the CORS Allow-list Resolution
// Order). Ordering is kept stable so CORS/CSP output and tests are
// deterministic across requests.
type originSet struct {
	mu    sync.RWMutex
	order []string
	seen  map[string]bool
}

// learnedOriginStore accumulates X-Forwarded-Host values seen from trusted
// reverse-proxy peers over the life of the process.
var learnedOriginStore = &originSet{seen: map[string]bool{}}

// add records origin if it hasn't been seen before.
func (s *originSet) add(origin string) {
	if origin == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[origin] {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[origin] = true
	s.order = append(s.order, origin)
}

// snapshot returns a copy of the currently recorded origins, in the order
// they were first learned.
func (s *originSet) snapshot() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// resetLearnedOrigins clears the learned-origin store. Test-only helper so
// each test case starts from a clean slate.
func resetLearnedOrigins() {
	learnedOriginStore.mu.Lock()
	defer learnedOriginStore.mu.Unlock()
	learnedOriginStore.order = nil
	learnedOriginStore.seen = map[string]bool{}
}

// firstForwardedValue returns the first entry of a comma-separated proxy
// header value (X-Forwarded-Host/X-Forwarded-Proto may carry a chain when
// requests pass through multiple hops), trimmed of surrounding whitespace.
func firstForwardedValue(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.Split(header, ",")
	return strings.TrimSpace(parts[0])
}

// domainEnvOrigins parses the comma-separated DOMAIN environment variable
// into "https://" origins, per AI.md PART 16 CORS Allow-list Resolution
// Order step 2 ("every DOMAIN env hostname as an https:// origin").
func domainEnvOrigins() []string {
	raw := os.Getenv("DOMAIN")
	if raw == "" {
		return nil
	}
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(part), "."))
		if host == "" || !isDomainNameValid(host, appmode.IsVerboseMode(), paths.ProjectName) {
			continue
		}
		origins = append(origins, "https://"+host)
	}
	return origins
}

// originalPeerAddr returns the pre-rewrite TCP peer address recorded by
// realIPMiddleware, falling back to r.RemoteAddr when unset (e.g. in tests
// that don't run the full middleware chain).
func originalPeerAddr(r *http.Request) string {
	if v, ok := r.Context().Value(originalPeerContextKey).(string); ok && v != "" {
		return v
	}
	return r.RemoteAddr
}

// learnedOrigins implements steps 2 and 3 of the CORS Allow-list Resolution
// Order (AI.md PART 16): every DOMAIN env hostname, plus any host learned
// from a trusted reverse-proxy's X-Forwarded-Host header. This is also the
// exact `{learned_origins}` set that AI.md PART 11's CSP auto-detection
// injects into connect-src/frame-ancestors/form-action — it deliberately
// excludes the operator's explicit server.cors.allowed_origins list (step 1)
// and the "*" fallback (step 4), since those aren't "learned", they're
// configured or defaulted.
func learnedOrigins(cfg *config.ServerConfig, r *http.Request) []string {
	result := domainEnvOrigins()

	if r != nil {
		peer := originalPeerAddr(r)
		if isTrustedPeer(cfg, peer) {
			if fwHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); fwHost != "" {
				scheme := "https"
				if proto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); proto != "" {
					scheme = proto
				}
				learnedOriginStore.add(scheme + "://" + fwHost)
			}
		}
	}

	result = append(result, learnedOriginStore.snapshot()...)

	seen := map[string]bool{}
	deduped := make([]string, 0, len(result))
	for _, o := range result {
		if o == "" || seen[o] {
			continue
		}
		seen[o] = true
		deduped = append(deduped, o)
	}
	return deduped
}

// resolveCORSOrigins implements the full CORS Allow-list Resolution Order
// from AI.md PART 16:
//  1. explicit server.cors.allowed_origins ("" as the sole entry disables
//     CORS entirely and stops resolution)
//  2. every DOMAIN env hostname as an https:// origin
//  3. reverse-proxy-learned hosts via X-Forwarded-Host, trusted peers only
//  4. default "*" if nothing else resolved
//
// A "*" anywhere in the explicit config list short-circuits to pure
// wildcard mode, since mixing a literal "*" with specific origins in the
// same Access-Control-Allow-Origin response is not meaningful and would
// make "credentials never sent with *" ambiguous to enforce.
func resolveCORSOrigins(cfg *config.ServerConfig, r *http.Request) (origins []string, disabled bool) {
	explicit := cfg.Web.CORS.AllowedOrigins

	if len(explicit) == 1 && explicit[0] == "" {
		return nil, true
	}

	for _, o := range explicit {
		if o == "*" {
			return []string{"*"}, false
		}
	}

	seen := map[string]bool{}
	result := make([]string, 0, len(explicit))
	add := func(o string) {
		if o == "" || seen[o] {
			return
		}
		seen[o] = true
		result = append(result, o)
	}

	for _, o := range explicit {
		add(o)
	}
	for _, o := range learnedOrigins(cfg, r) {
		add(o)
	}

	if len(result) == 0 {
		return []string{"*"}, false
	}
	return result, false
}
