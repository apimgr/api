package securitydata

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/health"
)

const blocklistMaxAge = 72 * time.Hour

type Blocklist struct {
	ID       string
	Type     string
	IPs      []netip.Prefix
	Domains  []domainPattern
	LoadedAt time.Time
}

type domainPattern struct {
	name     string
	wildcard bool
}

type BlocklistManager struct {
	mu       sync.RWMutex
	loadedAt map[string]time.Time
	lists    map[string]Blocklist
}

type CacheBlocklistSnapshot struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	IPs      []string             `json:"ips"`
	Domains  []CacheDomainPattern `json:"domains"`
	LoadedAt time.Time            `json:"loaded_at"`
}

type CacheDomainPattern struct {
	Name     string `json:"name"`
	Wildcard bool   `json:"wildcard"`
}

var DefaultBlocklists = &BlocklistManager{loadedAt: map[string]time.Time{}, lists: map[string]Blocklist{}}

func RegisterHealthChecks(cfg *config.ServerConfig) {
	health.UnregisterPrefix("blocklist_")
	health.Unregister("cve_nvd")
	health.Unregister("trivy_db")
	if cfg == nil {
		return
	}
	if cfg.Server.Security.Blocklists.Enabled {
		ids := []string{"firehol_level1"}
		for _, source := range cfg.Server.Security.Blocklists.ExtraSources {
			if source.Enabled == nil || *source.Enabled {
				ids = append(ids, source.ID)
			}
		}
		for _, id := range ids {
			sourceID := id
			health.Register("blocklist_"+sourceID, false, func() string { return DefaultBlocklists.Status(sourceID, time.Now().UTC()) })
		}
	}
	securityDir := configString(cfg.Data, "security_dir")
	if securityDir == "" {
		securityDir = filepath.Join("data", "security")
	}
	cvePath := filepath.Join(securityDir, "cve")
	health.Register("cve_nvd", false, func() string {
		return dataDirectoryStatus(cvePath, time.Now().UTC(), func() bool { return validNVDFile(filepath.Join(cvePath, "nvd.json")) })
	})
	trivyValues, _ := cfg.Data["trivy"].(map[string]interface{})
	trivyEnabled, _ := trivyValues["enabled"].(bool)
	if trivyEnabled {
		trivyPath := filepath.Join(securityDir, "trivy")
		health.Register("trivy_db", false, func() string {
			return dataDirectoryStatus(trivyPath, time.Now().UTC(), func() bool {
				info, err := os.Stat(filepath.Join(trivyPath, "trivy.db"))
				if err != nil || info.Size() == 0 {
					return false
				}
				data, err := os.ReadFile(filepath.Join(trivyPath, "metadata.json"))
				if err != nil {
					return false
				}
				var metadata struct {
					Version    int       `json:"Version"`
					NextUpdate time.Time `json:"NextUpdate"`
				}
				return json.Unmarshal(data, &metadata) == nil && metadata.Version > 0 && !metadata.NextUpdate.IsZero()
			})
		})
	}
}

func dataDirectoryStatus(directory string, now time.Time, valid func() bool) string {
	if !valid() {
		return "error"
	}
	stamp, err := os.ReadFile(filepath.Join(directory, ".last_updated"))
	if err != nil {
		return "error"
	}
	updated, err := time.Parse(time.RFC3339, strings.TrimSpace(string(stamp)))
	if err != nil {
		return "error"
	}
	if now.Sub(updated) > blocklistMaxAge {
		return "degraded"
	}
	return "ok"
}

func validNVDFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var records []json.RawMessage
	if json.Unmarshal(data, &records) != nil || len(records) == 0 {
		return false
	}
	return true
}

func ParseBlocklist(id, kind string, reader io.Reader, trusted []netip.Prefix) ([]string, Blocklist, int, error) {
	return parseBlocklist(id, kind, bufio.NewScanner(reader), trusted)
}

func TrustedNetworks(cfg *config.ServerConfig) []netip.Prefix { return trustedNetworks(cfg) }

func parseBlocklist(id, kind string, scanner *bufio.Scanner, trusted []netip.Prefix) ([]string, Blocklist, int, error) {
	parsed := Blocklist{ID: id, Type: kind}
	canonical := make([]string, 0)
	skipped := 0
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if i := strings.IndexAny(line, " \t;"); i >= 0 {
			line = line[:i]
		}
		if line == "" {
			continue
		}
		switch kind {
		case "ip":
			prefix, err := netip.ParsePrefix(line)
			if err != nil {
				addr, addrErr := netip.ParseAddr(line)
				if addrErr != nil {
					skipped++
					continue
				}
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
			prefix = prefix.Masked()
			if id == "firehol_level1" && !prefix.Addr().Is4() {
				skipped++
				continue
			}
			if unsafePrefix(prefix) || overlapsAny(prefix, trusted) {
				skipped++
				continue
			}
			parsed.IPs = append(parsed.IPs, prefix)
			canonical = append(canonical, prefix.String())
		case "domain":
			wildcard := strings.HasPrefix(line, "*.")
			domain, ok := config.NormalizeBlocklistDomain(line)
			if !ok || wildcard && config.IsPublicSuffix(domain) {
				skipped++
				continue
			}
			parsed.Domains = append(parsed.Domains, domainPattern{name: domain, wildcard: wildcard})
			if wildcard {
				canonical = append(canonical, "*."+domain)
			} else {
				canonical = append(canonical, domain)
			}
		default:
			return nil, Blocklist{}, skipped, fmt.Errorf("unsupported blocklist type")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, Blocklist{}, skipped, err
	}
	if len(canonical) == 0 {
		return nil, Blocklist{}, skipped, fmt.Errorf("blocklist has no valid entries")
	}
	return canonical, parsed, skipped, nil
}

func unsafePrefix(prefix netip.Prefix) bool {
	addr := prefix.Addr()
	if addr.Is4() {
		unsafe := []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16"}
		for _, raw := range unsafe {
			if overlaps(prefix, netip.MustParsePrefix(raw)) {
				return true
			}
		}
	} else {
		unsafe := []string{"::/128", "::1/128", "::ffff:0:0/96", "fc00::/7", "fe80::/10"}
		for _, raw := range unsafe {
			if overlaps(prefix, netip.MustParsePrefix(raw)) {
				return true
			}
		}
	}
	return false
}

func overlapsAny(prefix netip.Prefix, others []netip.Prefix) bool {
	for _, other := range others {
		if overlaps(prefix, other) {
			return true
		}
	}
	return false
}

func overlaps(a, b netip.Prefix) bool {
	return a.Addr().Is4() == b.Addr().Is4() && (a.Contains(b.Addr()) || b.Contains(a.Addr()))
}

func (m *BlocklistManager) Reload(cfg *config.ServerConfig, now time.Time) {
	if cfg == nil || !cfg.Server.Security.Blocklists.Enabled {
		m.mu.Lock()
		m.lists = map[string]Blocklist{}
		m.loadedAt = map[string]time.Time{}
		m.mu.Unlock()
		return
	}
	securityDir := configString(cfg.Data, "security_dir")
	if securityDir == "" {
		securityDir = filepath.Join("data", "security")
	}
	root := filepath.Join(securityDir, "blocklists")
	stampBytes, stampErr := os.ReadFile(filepath.Join(root, ".last_updated"))
	_, stampParseErr := time.Parse(time.RFC3339, strings.TrimSpace(string(stampBytes)))
	sources := []config.BlocklistSource{{ID: "firehol_level1", Type: "ip", Enabled: boolPointer(true)}}
	for _, source := range cfg.Server.Security.Blocklists.ExtraSources {
		if source.Enabled == nil || *source.Enabled {
			sources = append(sources, source)
		}
	}
	loaded := make(map[string]Blocklist, len(sources))
	loadedAt := make(map[string]time.Time, len(sources))
	trusted := trustedNetworks(cfg)
	for _, source := range sources {
		path := filepath.Join(root, source.ID+".txt")
		if stampErr != nil || stampParseErr != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		_, parsed, _, err := parseBlocklist(source.ID, source.Type, bufio.NewScanner(strings.NewReader(string(data))), trusted)
		if err != nil {
			continue
		}
		stat, err := os.Stat(path)
		if err != nil {
			continue
		}
		stamp := stat.ModTime().UTC()
		parsed.LoadedAt = stamp
		loaded[source.ID] = parsed
		loadedAt[source.ID] = stamp
	}
	m.mu.Lock()
	m.lists, m.loadedAt = loaded, loadedAt
	m.mu.Unlock()
}

func (m *BlocklistManager) MatchesIP(addr netip.Addr) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, list := range m.lists {
		for _, prefix := range list.IPs {
			if prefix.Contains(addr) {
				return id, true
			}
		}
	}
	return "", false
}

// CacheSnapshot returns a detached representation of the active blocklists;
// it contains runtime-derived state and never reads operator configuration.
func (m *BlocklistManager) CacheSnapshot() []CacheBlocklistSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.lists))
	for id := range m.lists {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	snapshot := make([]CacheBlocklistSnapshot, 0, len(ids))
	for _, id := range ids {
		list := m.lists[id]
		entry := CacheBlocklistSnapshot{
			ID:       list.ID,
			Type:     list.Type,
			IPs:      make([]string, 0, len(list.IPs)),
			Domains:  make([]CacheDomainPattern, 0, len(list.Domains)),
			LoadedAt: list.LoadedAt,
		}
		for _, prefix := range list.IPs {
			entry.IPs = append(entry.IPs, prefix.String())
		}
		for _, domain := range list.Domains {
			entry.Domains = append(entry.Domains, CacheDomainPattern{Name: domain.name, Wildcard: domain.wildcard})
		}
		snapshot = append(snapshot, entry)
	}
	return snapshot
}

func (m *BlocklistManager) MatchesDomain(host string) (string, bool) {
	domain, ok := config.NormalizeBlocklistDomain(host)
	if !ok {
		return "", false
	}
	if addr, err := netip.ParseAddr(domain); err == nil && addr.IsValid() {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, list := range m.lists {
		for _, pattern := range list.Domains {
			if !pattern.wildcard && domain == pattern.name || pattern.wildcard && strings.HasSuffix(domain, "."+pattern.name) {
				return id, true
			}
		}
	}
	return "", false
}

func (m *BlocklistManager) Status(id string, now time.Time) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stamp, ok := m.loadedAt[id]
	if !ok {
		return "error"
	}
	if now.Sub(stamp) > blocklistMaxAge {
		return "degraded"
	}
	return "ok"
}

func configString(data map[string]interface{}, key string) string {
	value, _ := data[key].(string)
	return strings.TrimSpace(value)
}

func boolPointer(value bool) *bool { return &value }

func trustedNetworks(cfg *config.ServerConfig) []netip.Prefix {
	raw := append([]string{}, cfg.Server.TrustedProxies.Additional...)
	for _, entry := range cfg.Server.Security.Allowlist {
		raw = append(raw, entry.CIDR)
	}
	result := make([]netip.Prefix, 0, len(raw))
	for _, value := range raw {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			result = append(result, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(value); err == nil {
			result = append(result, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return result
}
