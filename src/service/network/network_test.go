package network

import (
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
