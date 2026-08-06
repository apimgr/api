package network

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CallerInfo must split host:port when RemoteAddr has a port, fall back
// to the raw value when it doesn't, and only surface the whitelisted
// caller-identifying headers that are actually present.
func TestCallerInfo(t *testing.T) {
	s := New()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	req.Header.Set("X-Unrelated-Header", "should-not-appear")

	info := s.CallerInfo(req)
	assert.Equal(t, "203.0.113.5", info.IP)
	assert.Equal(t, "54321", info.Port)
	assert.Equal(t, "test-agent", info.Headers["User-Agent"])
	assert.Equal(t, "198.51.100.1", info.Headers["X-Forwarded-For"])
	assert.NotContains(t, info.Headers, "X-Unrelated-Header")

	// RemoteAddr without a port: IP falls back to the raw value, no port.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "not-a-host-port"
	info2 := s.CallerInfo(req2)
	assert.Equal(t, "not-a-host-port", info2.IP)
	assert.Equal(t, "", info2.Port)
	assert.Empty(t, info2.Headers)
}

// ParseUserAgent delegates to the shared parse service; verify the
// delegation actually returns populated data rather than a zero value.
func TestParseUserAgent(t *testing.T) {
	s := New()

	ua := s.ParseUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/91.0")
	assert.Equal(t, "Chrome", ua.Browser)
	assert.Equal(t, "Windows", ua.OS)
	assert.Equal(t, "Desktop", ua.Device)
}

// MACVendor covers a known OUI, an unknown-but-valid OUI, and a
// syntactically invalid MAC address.
func TestMACVendor(t *testing.T) {
	s := New()

	vendor, err := s.MACVendor("00:0C:29:12:34:56")
	require.NoError(t, err)
	assert.Equal(t, "VMware, Inc.", vendor)

	// Lowercase input and colon vs hyphen normalization.
	vendor, err = s.MACVendor("b8-27-eb-aa-bb-cc")
	require.NoError(t, err)
	assert.Equal(t, "Raspberry Pi Foundation", vendor)

	vendor, err = s.MACVendor("FF:FF:FF:FF:FF:FF")
	require.NoError(t, err)
	assert.Equal(t, "Unknown", vendor)

	_, err = s.MACVendor("not-a-mac")
	assert.ErrorIs(t, err, ErrInvalidMAC)

	_, err = s.MACVendor("")
	assert.ErrorIs(t, err, ErrInvalidMAC)
}

// SubnetCalculate covers a typical IPv4 /24, the /31 and /32 edge cases
// called out explicitly in the source (no usable host range), an IPv6
// block, and an invalid CIDR.
func TestSubnetCalculate(t *testing.T) {
	s := New()

	info, err := s.SubnetCalculate("192.168.1.0/24")
	require.NoError(t, err)
	assert.Equal(t, 4, info.Version)
	assert.Equal(t, "192.168.1.0", info.NetworkAddress)
	assert.Equal(t, "192.168.1.255", info.BroadcastAddress)
	assert.Equal(t, "255.255.255.0", info.SubnetMask)
	assert.Equal(t, "192.168.1.1", info.FirstHost)
	assert.Equal(t, "192.168.1.254", info.LastHost)
	assert.Equal(t, "254", info.UsableHosts)
	assert.Equal(t, "256", info.TotalAddresses)

	info, err = s.SubnetCalculate("10.0.0.0/31")
	require.NoError(t, err)
	assert.Equal(t, "0", info.UsableHosts)
	assert.Equal(t, "10.0.0.0", info.FirstHost)
	assert.Equal(t, "10.0.0.1", info.LastHost)

	info, err = s.SubnetCalculate("10.0.0.5/32")
	require.NoError(t, err)
	assert.Equal(t, "0", info.UsableHosts)
	assert.Equal(t, "10.0.0.5", info.FirstHost)
	assert.Equal(t, "10.0.0.5", info.LastHost)

	info, err = s.SubnetCalculate("2001:db8::/64")
	require.NoError(t, err)
	assert.Equal(t, 6, info.Version)
	assert.Equal(t, "2001:db8::", info.NetworkAddress)
	assert.Empty(t, info.BroadcastAddress)

	_, err = s.SubnetCalculate("not-a-cidr")
	assert.ErrorIs(t, err, ErrInvalidCIDR)
}

// A byte-boundary case for the increment/decrement helpers exercised
// indirectly through SubnetCalculate: crossing a .255 -> next octet
// carry, and a .0 -> borrow from the previous octet.
func TestSubnetCalculateByteCarry(t *testing.T) {
	s := New()

	info, err := s.SubnetCalculate("10.0.0.0/23")
	require := require.New(t)
	require.NoError(err)
	assert.Equal(t, "10.0.0.1", info.FirstHost)
	assert.Equal(t, "10.0.1.254", info.LastHost)
	assert.Equal(t, "10.0.1.255", info.BroadcastAddress)
}

// GenerateULA must always produce a /48 prefix under fd00::/8 (the RFC
// 4193 Local bit set), and repeated calls should not collide.
func TestGenerateULA(t *testing.T) {
	s := New()

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		ula, err := s.GenerateULA()
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(ula, "fd"))
		assert.True(t, strings.HasSuffix(ula, "/48"))
		assert.False(t, seen[ula], "GenerateULA produced duplicate: %s", ula)
		seen[ula] = true
	}
}

// RandomPort must always stay within the unprivileged dynamic/private
// range across many draws.
func TestRandomPort(t *testing.T) {
	s := New()

	for i := 0; i < 200; i++ {
		port, err := s.RandomPort()
		require.NoError(t, err)
		assert.GreaterOrEqual(t, port, unprivilegedPortMin)
		assert.LessOrEqual(t, port, unprivilegedPortMax)
	}
}

// incrementIPv4/decrementIPv4 are exercised indirectly through
// SubnetCalculate above, but the borrow branch of decrementIPv4 (when a
// trailing byte is already zero and must wrap to 0xFF) can never be
// reached through SubnetCalculate because a broadcast address's least
// significant byte is always a host bit (all 1s) for any prefix shorter
// than /31. Call it directly to exercise that branch.
func TestDecrementIPv4Borrow(t *testing.T) {
	ip := net.IP{10, 0, 1, 0}
	decrementIPv4(ip)
	assert.Equal(t, net.IP{10, 0, 0, 255}, ip)
}

// ParseURL covers a fully-populated URL (user info, port, query,
// fragment), a minimal scheme+host URL, and both required-component
// error paths (missing scheme, missing host).
func TestParseURL(t *testing.T) {
	s := New()

	t.Run("fully populated", func(t *testing.T) {
		info, err := s.ParseURL("https://alice@example.com:8443/path/to?foo=bar&foo=baz#section")
		require.NoError(t, err)
		assert.Equal(t, "https", info.Scheme)
		assert.Equal(t, "alice", info.User)
		assert.Equal(t, "example.com:8443", info.Host)
		assert.Equal(t, "example.com", info.Hostname)
		assert.Equal(t, "8443", info.Port)
		assert.Equal(t, "/path/to", info.Path)
		assert.Equal(t, "foo=bar&foo=baz", info.RawQuery)
		assert.Equal(t, []string{"bar", "baz"}, info.Query["foo"])
		assert.Equal(t, "section", info.Fragment)
	})

	t.Run("minimal", func(t *testing.T) {
		info, err := s.ParseURL("http://example.com")
		require.NoError(t, err)
		assert.Equal(t, "http", info.Scheme)
		assert.Empty(t, info.User)
		assert.Equal(t, "example.com", info.Host)
		assert.Empty(t, info.Port)
	})

	t.Run("missing scheme", func(t *testing.T) {
		info, err := s.ParseURL("example.com/path")
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "scheme and host")
	})

	t.Run("missing host", func(t *testing.T) {
		info, err := s.ParseURL("file:///etc/passwd")
		require.Error(t, err)
		assert.Nil(t, info)
	})

	t.Run("unparseable", func(t *testing.T) {
		info, err := s.ParseURL("http://[::1")
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "invalid URL")
	})
}

// Ping's SSRF guard and input-validation paths are fully exercisable
// without live network access: empty host and any blocked (loopback/
// private) target must error before any dial is attempted.
func TestPing_ValidationErrors(t *testing.T) {
	s := New()

	t.Run("empty host", func(t *testing.T) {
		result, err := s.Ping("", 4)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "host is required")
	})

	t.Run("blocked loopback target", func(t *testing.T) {
		result, err := s.Ping("127.0.0.1", 1)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("blocked private target with port", func(t *testing.T) {
		result, err := s.Ping("10.0.0.5:9999", 1)
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

// Ping's dial loop itself requires reaching a real routable host, which
// is not guaranteed to be reachable from inside a sandboxed CI/test
// container with no outbound network access. Reaching a real TCP peer
// on the loopback interface is not a substitute here because Ping
// deliberately rejects loopback targets as part of its SSRF guard, so
// there is no way to exercise the successful-dial branch without a live
// external network.
func TestPing_LiveDial(t *testing.T) {
	t.Skip("reason: Ping rejects loopback targets (SSRF guard), so the success path requires a live, routable external host that is not guaranteed inside a sandboxed test container")
}

// SSLInfo shares Ping's SSRF guard; cover the same input-validation
// paths without any live TLS handshake.
func TestSSLInfo_ValidationErrors(t *testing.T) {
	s := New()

	t.Run("empty host", func(t *testing.T) {
		result, err := s.SSLInfo("")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "host is required")
	})

	t.Run("blocked loopback target", func(t *testing.T) {
		result, err := s.SSLInfo("127.0.0.1")
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

// SSLInfo's TLS handshake requires reaching a real routable host with a
// valid certificate, which is not guaranteed to be reachable from a
// sandboxed test container with no outbound network access, and (like
// Ping) cannot be substituted with a local loopback listener since
// SSLInfo rejects loopback targets as part of its SSRF guard.
func TestSSLInfo_LiveHandshake(t *testing.T) {
	t.Skip("reason: SSLInfo rejects loopback targets (SSRF guard), so the success path requires a live, routable external host with a valid cert that is not guaranteed inside a sandboxed test container")
}

// whoisQuery is unexported and hardcodes port 43 via
// net.JoinHostPort(server, "43") internally. The test container runs as
// root, so a local stand-in WHOIS server can bind 127.0.0.1:43 directly
// to exercise the full dial/write/read success path without live
// internet access.
func TestWhoisQuery(t *testing.T) {
	t.Run("successful query", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:43")
		if err != nil {
			t.Skipf("reason: could not bind 127.0.0.1:43 in this environment (%v) — port 43 may already be in use or unavailable", err)
		}
		defer ln.Close()

		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			buf := make([]byte, 256)
			n, _ := conn.Read(buf)
			if strings.TrimSpace(string(buf[:n])) == "example.com" {
				conn.Write([]byte("domain: EXAMPLE.COM\n"))
			}
		}()

		result, err := whoisQuery("127.0.0.1", "example.com")
		require.NoError(t, err)
		assert.Contains(t, result, "domain: EXAMPLE.COM")
	})

	t.Run("connection refused", func(t *testing.T) {
		// Bind and immediately close to obtain a host nothing is
		// listening on for the whoisQuery-hardcoded port 43.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := ln.Addr().String()
		ln.Close()

		host, _, err := net.SplitHostPort(addr)
		require.NoError(t, err)

		_, err = whoisQuery(host+"x", "example.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to reach whois server")
	})
}

// Whois validates its input before making any network call, which is
// coverable without live internet access; the full success/referral
// flow requires reaching the real whois.iana.org server and is not
// exercised here for the same reason as Ping/SSLInfo's live paths.
func TestWhois_ValidationErrors(t *testing.T) {
	s := New()

	t.Run("empty domain", func(t *testing.T) {
		result, err := s.Whois("")
		require.Error(t, err)
		assert.Empty(t, result)
		assert.Contains(t, err.Error(), "domain is required")
	})

	t.Run("whitespace only domain", func(t *testing.T) {
		result, err := s.Whois("   ")
		require.Error(t, err)
		assert.Empty(t, result)
	})
}

// Whois's full success/referral-following flow requires reaching the
// real whois.iana.org server and cannot be redirected to a local
// stand-in without modifying the actual (non-test) source, which is not
// permitted here.
func TestWhois_LiveQuery(t *testing.T) {
	t.Skip("reason: Whois queries the real whois.iana.org server and cannot be redirected to a local stand-in, so it requires live internet access not guaranteed inside a sandboxed test container")
}
