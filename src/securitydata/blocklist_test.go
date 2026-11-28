package securitydata

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestParseBlocklistFiltersUnsafeIPAndInvalidLines(t *testing.T) {
	input := strings.NewReader("# comment\n203.0.113.4\n10.0.0.0/8\ninvalid\n198.51.100.0/24; note\n")
	entries, parsed, skipped, err := ParseBlocklist("test", "ip", input, []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 3 {
		t.Fatalf("skipped=%d, want 3", skipped)
	}
	if len(entries) != 1 || entries[0] != "198.51.100.0/24" {
		t.Fatalf("entries=%v", entries)
	}
	if len(parsed.IPs) != 1 {
		t.Fatalf("parsed IPs=%v", parsed.IPs)
	}
}

func TestParseBlocklistDomainPublicSuffixAndWildcardMatching(t *testing.T) {
	_, parsed, skipped, err := ParseBlocklist("domains", "domain", strings.NewReader("example.com\n*.example.net\n*.co.uk\nBÜCHER.example\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Fatalf("skipped=%d, want 1", skipped)
	}
	manager := &BlocklistManager{lists: map[string]Blocklist{"domains": parsed}, loadedAt: map[string]time.Time{}}
	for host, want := range map[string]bool{"example.com": true, "sub.example.net": true, "example.net": false, "other.co.uk": false, "xn--bcher-kva.example": true} {
		_, got := manager.MatchesDomain(host)
		if got != want {
			t.Errorf("MatchesDomain(%q)=%t want %t", host, got, want)
		}
	}
}

func TestBlocklistStatusFreshness(t *testing.T) {
	now := time.Now().UTC()
	manager := &BlocklistManager{loadedAt: map[string]time.Time{"fresh": now.Add(-time.Hour), "stale": now.Add(-73 * time.Hour)}}
	if got := manager.Status("fresh", now); got != "ok" {
		t.Fatalf("fresh status=%q", got)
	}
	if got := manager.Status("stale", now); got != "degraded" {
		t.Fatalf("stale status=%q", got)
	}
	if got := manager.Status("missing", now); got != "error" {
		t.Fatalf("missing status=%q", got)
	}
}

func TestBlocklistCacheSnapshotIsSortedAndDetached(t *testing.T) {
	manager := &BlocklistManager{lists: map[string]Blocklist{
		"z-list": {ID: "z-list", Type: "ip", IPs: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}},
		"a-list": {ID: "a-list", Type: "domain", Domains: []domainPattern{{name: "example.test", wildcard: true}}},
	}}
	snapshot := manager.CacheSnapshot()
	if len(snapshot) != 2 || snapshot[0].ID != "a-list" || snapshot[1].ID != "z-list" {
		t.Fatalf("snapshot order/content = %#v", snapshot)
	}
	snapshot[1].IPs[0] = "changed"
	if got := manager.CacheSnapshot()[1].IPs[0]; got != "192.0.2.0/24" {
		t.Fatalf("snapshot mutation changed manager data: %q", got)
	}
}
