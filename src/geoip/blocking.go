package geoip

import (
	"log"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/apimgr/api/src/config"
)

// Reason codes explaining a country-blocking decision. They are stable
// identifiers meant for structured logs and audit entries - a blocked
// request is always logged, and an allowed one records why the check did
// not apply.
const (
	// ReasonDisabled - server.geoip.enabled is false.
	ReasonDisabled = "geoip_disabled"
	// ReasonInvalidIP - the address could not be parsed, so the check is
	// skipped (fail-open per PART 19).
	ReasonInvalidIP = "invalid_ip"
	// ReasonPrivateIP - RFC 1918 / RFC 4193 / loopback / link-local
	// addresses are never looked up or country-blocked.
	ReasonPrivateIP = "private_ip"
	// ReasonAllowlisted - the address matched server.security.allowlist.
	ReasonAllowlisted = "ip_allowlisted"
	// ReasonNoRules - neither deny_countries nor allow_countries is set.
	ReasonNoRules = "no_country_rules"
	// ReasonNoCountryDatabase - the country database is disabled or missing,
	// so blocking is skipped with a warning.
	ReasonNoCountryDatabase = "country_database_unavailable"
	// ReasonNoCountryData - the database has no country for this address.
	ReasonNoCountryData = "country_unknown"
	// ReasonCountryAllowlisted - the country is in allow_countries.
	ReasonCountryAllowlisted = "country_in_allow_list"
	// ReasonCountryNotAllowlisted - allow_countries is set and the country
	// is not in it.
	ReasonCountryNotAllowlisted = "country_not_in_allow_list"
	// ReasonCountryDenied - the country is in deny_countries.
	ReasonCountryDenied = "country_in_deny_list"
	// ReasonCountryNotDenied - deny_countries is set and the country is not
	// in it.
	ReasonCountryNotDenied = "country_not_in_deny_list"
)

// CountryDecision is the advisory outcome of a country-blocking check.
// GeoIP is one risk signal among many (PART 19): a Blocked decision never
// substitutes for authentication, and the request must still traverse rate
// limiting, auth, and audit logging regardless of the outcome.
type CountryDecision struct {
	// Blocked is true only when a configured rule positively rejected the
	// resolved country. Every skip path leaves it false (fail-open).
	Blocked bool
	// Reason is one of the Reason* constants above.
	Reason string
	// CountryCode is the resolved ISO 3166-1 alpha-2 code, empty when no
	// lookup happened or the database had no answer.
	CountryCode string
}

// countryDBWarnOnce keeps the fail-open warning to one line per process
// instead of one per request when country blocking is configured but the
// country database is unavailable.
var countryDBWarnOnce sync.Once

// IsValidCountryCode reports whether s is a well-formed ISO 3166-1 alpha-2
// code: exactly two ASCII letters, case-insensitive.
func IsValidCountryCode(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// NormalizeCountryCodes trims, uppercases, and de-duplicates a configured
// country list, dropping anything that is not a valid ISO 3166-1 alpha-2
// code. An invalid entry is warned about and ignored rather than failing
// startup.
func NormalizeCountryCodes(codes []string) []string {
	seen := make(map[string]struct{}, len(codes))
	normalized := make([]string, 0, len(codes))

	for _, raw := range codes {
		code := strings.ToUpper(strings.TrimSpace(raw))
		if code == "" {
			continue
		}
		if !IsValidCountryCode(code) {
			log.Printf("GeoIP: ignoring invalid country code %q, expected ISO 3166-1 alpha-2", raw)
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		normalized = append(normalized, code)
	}

	return normalized
}

// IsPrivateIP reports whether ip is an address that is never geolocated or
// country-blocked: RFC 1918 private space, RFC 4193 unique-local, loopback,
// link-local, and the unspecified address.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// IsAllowlisted reports whether ip matches any server.security.allowlist
// entry. A bare address with no prefix length is treated as a single host.
// Unparseable entries are ignored so one bad line cannot disable the whole
// allowlist.
func IsAllowlisted(ip net.IP, allowlist []config.AllowlistEntry) bool {
	if ip == nil {
		return false
	}

	for _, entry := range allowlist {
		value := strings.TrimSpace(entry.CIDR)
		if value == "" {
			continue
		}

		if strings.Contains(value, "/") {
			_, network, err := net.ParseCIDR(value)
			if err != nil {
				log.Printf("GeoIP: ignoring invalid allowlist CIDR %q: %v", entry.CIDR, err)
				continue
			}
			if network.Contains(ip) {
				return true
			}
			continue
		}

		parsed := net.ParseIP(value)
		if parsed == nil {
			log.Printf("GeoIP: ignoring invalid allowlist address %q", entry.CIDR)
			continue
		}
		if parsed.Equal(ip) {
			return true
		}
	}

	return false
}

// ResolvePreset returns the country codes stored under a named
// server.geoip.presets entry. Presets are pure config-reuse conveniences:
// nothing in this package ever applies one automatically, and enforcement is
// always driven by deny_countries/allow_countries as configured at request
// time.
func ResolvePreset(geo config.GeoIPConfig, name string) ([]string, bool) {
	if geo.Presets == nil {
		return nil, false
	}

	codes, ok := geo.Presets[strings.TrimSpace(name)]
	if !ok {
		return nil, false
	}

	return NormalizeCountryCodes(codes), true
}

// PresetNames returns the configured server.geoip.presets names in sorted
// order. Presets are a config-reuse convenience only, so this is a read-only
// listing for operator-facing surfaces - selecting one never changes what is
// enforced.
func PresetNames(geo config.GeoIPConfig) []string {
	names := make([]string, 0, len(geo.Presets))
	for name := range geo.Presets {
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidatePresets reports every server.geoip.presets entry that holds an
// invalid ISO 3166-1 alpha-2 code or no usable code at all. Presets are never
// auto-applied, so a bad entry is surfaced as a warning rather than a startup
// failure; the returned names let the caller log exactly which preset needs
// operator attention.
func ValidatePresets(geo config.GeoIPConfig) []string {
	var invalid []string

	for _, name := range PresetNames(geo) {
		// A key carrying surrounding whitespace can never be looked up,
		// since ResolvePreset trims the requested name.
		if name != strings.TrimSpace(name) {
			invalid = append(invalid, name)
			continue
		}

		codes, ok := ResolvePreset(geo, name)
		if !ok || len(codes) == 0 || hasMalformedCode(geo.Presets[name]) {
			invalid = append(invalid, name)
		}
	}

	return invalid
}

// hasMalformedCode reports whether any non-blank entry in codes is not a
// well-formed ISO 3166-1 alpha-2 code. Blank entries are ignored so trailing
// YAML whitespace is not reported as an operator error.
func hasMalformedCode(codes []string) bool {
	for _, raw := range codes {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if !IsValidCountryCode(raw) {
			return true
		}
	}
	return false
}

// CountryCodeOf resolves the ISO 3166-1 alpha-2 country for an IP using the
// country database, falling back to the city database's country_code field.
// It returns an empty string whenever no database can answer, never an error,
// so callers can treat "unknown" and "unavailable" identically.
func (g *GeoIPDB) CountryCodeOf(ip net.IP) string {
	if ip == nil {
		return ""
	}

	entry, err := g.Lookup(ip.String())
	if err != nil || entry == nil {
		return ""
	}

	return strings.ToUpper(strings.TrimSpace(entry.CountryCode))
}

// CheckCountry evaluates the configured country rules for one client
// address. The address passed in must be the immediate connection origin the
// server actually sees - for Tor traffic that is the exit node, and the exit
// node's country is what gets evaluated, never an inferred user origin.
//
// The check is advisory and fails open at every uncertainty: disabled GeoIP,
// an unparseable address, a private/internal address, an allowlisted
// address, an unconfigured rule set, a missing country database, and an
// unknown country all return Blocked=false with the reason recorded. A
// blocked decision still leaves rate limiting, authentication, and audit
// logging to the rest of the pipeline.
func (g *GeoIPDB) CheckCountry(ipStr string, geo config.GeoIPConfig, allowlist []config.AllowlistEntry) CountryDecision {
	if !geo.Enabled {
		return CountryDecision{Reason: ReasonDisabled}
	}

	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return CountryDecision{Reason: ReasonInvalidIP}
	}

	if IsPrivateIP(ip) {
		return CountryDecision{Reason: ReasonPrivateIP}
	}

	if IsAllowlisted(ip, allowlist) {
		return CountryDecision{Reason: ReasonAllowlisted}
	}

	allowCountries := NormalizeCountryCodes(geo.AllowCountries)
	denyCountries := NormalizeCountryCodes(geo.DenyCountries)

	if len(allowCountries) == 0 && len(denyCountries) == 0 {
		return CountryDecision{Reason: ReasonNoRules}
	}

	if !geo.Databases.Country || !g.HasCountryDB() {
		countryDBWarnOnce.Do(func() {
			log.Println("GeoIP: country blocking is configured but the country database is unavailable, blocking is skipped")
		})
		return CountryDecision{Reason: ReasonNoCountryDatabase}
	}

	code := g.CountryCodeOf(ip)
	if code == "" {
		return CountryDecision{Reason: ReasonNoCountryData}
	}

	return evaluateCountryRules(code, allowCountries, denyCountries)
}

// evaluateCountryRules applies the configured allow/deny lists to an already
// resolved country code. allow_countries wins whenever both lists are set:
// allowlist mode is strictly the narrower of the two. Both lists must already
// be normalized by NormalizeCountryCodes.
func evaluateCountryRules(code string, allowCountries, denyCountries []string) CountryDecision {
	if len(allowCountries) == 0 && len(denyCountries) == 0 {
		return CountryDecision{Reason: ReasonNoRules, CountryCode: code}
	}

	if len(allowCountries) > 0 {
		if containsCountry(allowCountries, code) {
			return CountryDecision{Reason: ReasonCountryAllowlisted, CountryCode: code}
		}
		return CountryDecision{Blocked: true, Reason: ReasonCountryNotAllowlisted, CountryCode: code}
	}

	if containsCountry(denyCountries, code) {
		return CountryDecision{Blocked: true, Reason: ReasonCountryDenied, CountryCode: code}
	}

	return CountryDecision{Reason: ReasonCountryNotDenied, CountryCode: code}
}
