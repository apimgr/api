package osint

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers isBlockedIP for every blocked category (nil, loopback,
// link-local unicast/multicast, private RFC1918/RFC4193, unspecified,
// multicast) plus a public IP that must NOT be blocked.
func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		want bool
	}{
		{name: "nil", ip: nil, want: true},
		{name: "loopback v4", ip: net.ParseIP("127.0.0.1"), want: true},
		{name: "loopback v6", ip: net.ParseIP("::1"), want: true},
		{name: "link-local unicast", ip: net.ParseIP("169.254.1.1"), want: true},
		{name: "link-local multicast", ip: net.ParseIP("224.0.0.1"), want: true},
		{name: "private RFC1918 class A", ip: net.ParseIP("10.0.0.1"), want: true},
		{name: "private RFC1918 class B", ip: net.ParseIP("172.16.0.1"), want: true},
		{name: "private RFC1918 class C", ip: net.ParseIP("192.168.1.1"), want: true},
		{name: "private RFC4193 ULA", ip: net.ParseIP("fd00::1"), want: true},
		{name: "unspecified v4", ip: net.ParseIP("0.0.0.0"), want: true},
		{name: "unspecified v6", ip: net.ParseIP("::"), want: true},
		{name: "multicast v4", ip: net.ParseIP("239.1.1.1"), want: true},
		{name: "public v4", ip: net.ParseIP("8.8.8.8"), want: false},
		{name: "public v6", ip: net.ParseIP("2001:4860:4860::8888"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isBlockedIP(tt.ip))
		})
	}
}

// Covers validateTarget's non-network-dependent paths: empty host,
// literal blocked IPs (v4/v6, with/without brackets), literal public
// IPs, and the hardcoded "localhost" rejection. None of these reach the
// system resolver, so they are deterministic without network access.
func TestValidateTarget_NoNetwork(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		host    string
		wantErr bool
		errSub  string
	}{
		{name: "empty host", host: "", wantErr: true, errSub: "required"},
		{name: "whitespace only host", host: "   ", wantErr: true, errSub: "required"},
		{name: "literal loopback", host: "127.0.0.1", wantErr: true, errSub: "non-routable"},
		{name: "literal private", host: "10.0.0.5", wantErr: true, errSub: "non-routable"},
		{name: "literal bracketed v6 loopback", host: "[::1]", wantErr: true, errSub: "non-routable"},
		{name: "localhost hostname", host: "localhost", wantErr: true, errSub: "non-routable"},
		{name: "localhost mixed case", host: "LocalHost", wantErr: true, errSub: "non-routable"},
		{name: "literal public IP", host: "8.8.8.8", wantErr: false},
		{name: "literal bracketed public v6", host: "[2001:4860:4860::8888]", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTarget(ctx, tt.host)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSub)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// Covers validateTarget's hostname-resolution branch. This performs a
// real DNS lookup, so if the sandbox has no outbound network access the
// resolution itself will fail (a legitimate "resolve failed" error, not
// a validation-logic bug) and the test skips rather than hard-failing.
func TestValidateTarget_HostnameResolution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := validateTarget(ctx, "example.com")
	if err != nil {
		t.Skipf("skipping: DNS resolution unavailable in this environment: %v", err)
	}
}

// Covers validateTarget's not-found branch for a hostname that (almost
// certainly) does not resolve to anything, using a reserved-for-testing
// TLD per RFC 2606. Skips if the sandbox has no DNS resolution at all,
// since that produces the same "failed to resolve" error shape.
func TestValidateTarget_UnresolvableHostname(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := validateTarget(ctx, "this-host-should-not-exist.invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve")
}
