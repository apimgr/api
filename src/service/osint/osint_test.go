package osint

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers New: returns a non-nil, usable Service.
func TestNew(t *testing.T) {
	s := New()
	require.NotNil(t, s)
}

// Covers parseWHOISReferral: "refer:" field, "whois:" field, case
// insensitivity, no referral present, and a malformed line with no
// colon separator.
func TestParseWHOISReferral(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "refer field",
			raw:  "domain: EXAMPLE.COM\nrefer:   whois.verisign-grs.com\n",
			want: "whois.verisign-grs.com",
		},
		{
			name: "whois field",
			raw:  "whois:  whois.nic.io\n",
			want: "whois.nic.io",
		},
		{
			name: "case insensitive prefix",
			raw:  "REFER: whois.example.net\n",
			want: "whois.example.net",
		},
		{
			name: "no referral",
			raw:  "domain: EXAMPLE.COM\nstatus: active\n",
			want: "",
		},
		{
			name: "empty input",
			raw:  "",
			want: "",
		},
		{
			name: "malformed line no colon",
			raw:  "refer whois.example.com\n",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseWHOISReferral(tt.raw))
		})
	}
}

// Covers parseWHOISResponse: registrar/creation/expiry/nameserver field
// extraction across label-name variants, comment-line skipping (%, #),
// blank-line skipping, malformed lines, empty values, and the
// first-value-wins behavior for duplicate creation/expiry fields.
func TestParseWHOISResponse(t *testing.T) {
	t.Run("full record", func(t *testing.T) {
		raw := strings.Join([]string{
			"% This is a comment",
			"# Another comment style",
			"",
			"Domain Name: EXAMPLE.COM",
			"Registrar: Example Registrar, Inc.",
			"Creation Date: 1995-08-14T04:00:00Z",
			"Registry Expiry Date: 2025-08-13T04:00:00Z",
			"Name Server: NS1.EXAMPLE.COM",
			"Name Server: NS2.EXAMPLE.COM",
			"malformed line without colon",
			"Empty Field:",
		}, "\n")

		info := parseWHOISResponse(raw)
		assert.Equal(t, "Example Registrar, Inc.", info.Registrar)
		assert.Equal(t, "1995-08-14T04:00:00Z", info.Created)
		assert.Equal(t, "2025-08-13T04:00:00Z", info.Expires)
		assert.Equal(t, []string{"NS1.EXAMPLE.COM", "NS2.EXAMPLE.COM"}, info.NameServers)
	})

	t.Run("sponsoring registrar variant", func(t *testing.T) {
		info := parseWHOISResponse("Sponsoring Registrar: Sponsor Corp\n")
		assert.Equal(t, "Sponsor Corp", info.Registrar)
	})

	t.Run("alternate created/expiry labels", func(t *testing.T) {
		raw := "created: 2020-01-01\nexpiration date: 2030-01-01\n"
		info := parseWHOISResponse(raw)
		assert.Equal(t, "2020-01-01", info.Created)
		assert.Equal(t, "2030-01-01", info.Expires)
	})

	t.Run("paid-till maps to expires", func(t *testing.T) {
		info := parseWHOISResponse("paid-till: 2030-05-01\n")
		assert.Equal(t, "2030-05-01", info.Expires)
	})

	t.Run("first value wins for duplicate fields", func(t *testing.T) {
		raw := "Creation Date: first\nCreated On: second\n"
		info := parseWHOISResponse(raw)
		assert.Equal(t, "first", info.Created)
	})

	t.Run("nserver and nameserver aliases", func(t *testing.T) {
		raw := "nserver: a.example.com\nnameserver: b.example.com\nnameservers: c.example.com\n"
		info := parseWHOISResponse(raw)
		assert.Equal(t, []string{"a.example.com", "b.example.com", "c.example.com"}, info.NameServers)
	})

	t.Run("empty input", func(t *testing.T) {
		info := parseWHOISResponse("")
		assert.Empty(t, info.Registrar)
		assert.Empty(t, info.NameServers)
	})
}

// Covers WHOISLookup's validation error paths, which are deterministic
// without network access (empty domain, whitespace domain, and a
// literal blocked IP passed as the domain).
func TestWHOISLookup_ValidationErrors(t *testing.T) {
	s := New()

	tests := []struct {
		name   string
		domain string
		errSub string
	}{
		{name: "empty domain", domain: "", errSub: "domain is required"},
		{name: "whitespace domain", domain: "   ", errSub: "domain is required"},
		{name: "blocked literal IP", domain: "127.0.0.1", errSub: "non-routable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := s.WHOISLookup(tt.domain)
			require.Error(t, err)
			assert.Nil(t, info)
			assert.Contains(t, err.Error(), tt.errSub)
		})
	}
}

// Covers WHOISLookup's happy path end to end (IANA referral resolution
// plus registrar parsing). This requires real outbound TCP/43
// connectivity, which is frequently unavailable in sandboxed CI
// containers, so a connection failure is treated as an environment
// limitation and skipped rather than failed.
func TestWHOISLookup_RealNetwork(t *testing.T) {
	s := New()
	info, err := s.WHOISLookup("example.com")
	if err != nil {
		t.Skipf("skipping: WHOIS network access unavailable in this environment: %v", err)
	}
	assert.Equal(t, "example.com", info.Domain)
}

// Covers queryWHOIS's own connection-failure path directly: dialing a
// server name that cannot resolve/connect must return a wrapped error
// rather than panicking or hanging past the deadline.
func TestQueryWHOIS_ConnectionFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := queryWHOIS(ctx, "this-host-should-not-exist.invalid", "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")
}

// Covers DNSLookup's validation and default-branch error paths, which
// don't require network access: empty domain, a blocked literal IP
// target, and an unsupported record type.
func TestDNSLookup_ValidationErrors(t *testing.T) {
	s := New()

	t.Run("empty domain", func(t *testing.T) {
		results, err := s.DNSLookup("", "A")
		require.Error(t, err)
		assert.Nil(t, results)
		assert.Contains(t, err.Error(), "domain is required")
	})

	t.Run("blocked literal IP", func(t *testing.T) {
		results, err := s.DNSLookup("127.0.0.1", "A")
		require.Error(t, err)
		assert.Nil(t, results)
		assert.Contains(t, err.Error(), "non-routable")
	})

	t.Run("unsupported record type", func(t *testing.T) {
		results, err := s.DNSLookup("example.com", "BOGUS")
		require.Error(t, err)
		assert.Nil(t, results)
		assert.Contains(t, err.Error(), "unsupported record type")
	})
}

// Covers DNSLookup's real-resolver paths across every supported record
// type. Requires outbound DNS; skips per-subtest on resolution failure
// rather than failing the whole suite in a network-restricted sandbox.
func TestDNSLookup_RealNetwork(t *testing.T) {
	s := New()

	for _, recordType := range []string{"A", "AAAA", "MX", "TXT", "NS", "CNAME"} {
		t.Run(recordType, func(t *testing.T) {
			results, err := s.DNSLookup("example.com", recordType)
			if err != nil {
				t.Skipf("skipping: DNS lookup unavailable in this environment: %v", err)
			}
			assert.NotEmpty(t, results)
		})
	}
}

// Covers IPLookup's validation error paths (invalid IP string, blocked
// private/loopback IP) which never touch the GeoIP database, plus the
// success path against a public IP with no MMDB databases loaded (the
// default state of the process-wide singleton in this test binary) —
// Lookup degrades gracefully to an IP-only result rather than erroring.
func TestIPLookup(t *testing.T) {
	s := New()

	t.Run("invalid IP", func(t *testing.T) {
		info, err := s.IPLookup("not-an-ip")
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "invalid IP address")
	})

	t.Run("blocked loopback", func(t *testing.T) {
		info, err := s.IPLookup("127.0.0.1")
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "not permitted")
	})

	t.Run("blocked private", func(t *testing.T) {
		info, err := s.IPLookup("192.168.1.1")
		require.Error(t, err)
		assert.Nil(t, info)
	})

	t.Run("public IP with no mmdb loaded degrades gracefully", func(t *testing.T) {
		info, err := s.IPLookup("8.8.8.8")
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, "8.8.8.8", info.IP)
	})
}

// Covers SSLInfo's validation error path (empty domain), which is
// deterministic without network access.
func TestSSLInfo_ValidationErrors(t *testing.T) {
	s := New()

	results, err := s.SSLInfo("")
	require.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "domain is required")
}

// Covers SSLInfo's blocked-target path (host:port form with a private
// target), which is rejected by validateTarget before any TLS dial.
func TestSSLInfo_BlockedTarget(t *testing.T) {
	s := New()

	results, err := s.SSLInfo("127.0.0.1:8443")
	require.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "non-routable")
}

// Covers SSLInfo's real-network happy path: connects to a well-known
// public host and reads back its certificate. Skips on any network
// error since outbound HTTPS/443 may be unavailable in the sandbox.
func TestSSLInfo_RealNetwork(t *testing.T) {
	s := New()

	info, err := s.SSLInfo("example.com")
	if err != nil {
		t.Skipf("skipping: TLS network access unavailable in this environment: %v", err)
	}
	assert.Contains(t, info, "subject")
	assert.Contains(t, info, "not_after")
}

// Sanity check that isBlockedIP's public-IP branch and net.ParseIP
// agree on a small independent set of addresses, guarding against a
// future edit accidentally flipping the boolean.
func TestIsBlockedIP_PublicSanity(t *testing.T) {
	ips := []string{"1.1.1.1", "8.8.4.4", "93.184.216.34"}
	for _, s := range ips {
		ip := net.ParseIP(s)
		require.NotNil(t, ip)
		assert.False(t, isBlockedIP(ip), "expected %s to be public", s)
	}
}
