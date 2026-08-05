package network

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"link-local unicast", "169.254.1.1", true},
		{"link-local multicast", "224.0.0.1", true},
		{"private rfc1918 10", "10.0.0.1", true},
		{"private rfc1918 192", "192.168.1.1", true},
		{"private rfc1918 172", "172.16.0.1", true},
		{"unique-local v6", "fd00::1", true},
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},
		{"multicast v4", "239.1.1.1", true},
		{"cgnat", "100.64.0.1", true},
		{"cgnat upper bound", "100.127.255.255", true},
		{"benchmarking", "198.18.0.1", true},
		{"public v4", "8.8.8.8", false},
		{"public v6", "2001:4860:4860::8888", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("failed to parse test IP %q", c.ip)
			}
			if got := isBlockedIP(ip); got != c.want {
				t.Errorf("isBlockedIP(%q) = %v, want %v", c.ip, got, c.want)
			}
		})
	}
}

func TestIsBlockedIPNil(t *testing.T) {
	if !isBlockedIP(nil) {
		t.Error("isBlockedIP(nil) should be true")
	}
}

func TestValidateTargetBlocked(t *testing.T) {
	blocked := []string{
		"127.0.0.1",
		"127.0.0.1:8080",
		"localhost",
		"LOCALHOST",
		"10.0.0.5",
		"[::1]",
		"[::1]:443",
		"169.254.169.254",
	}
	for _, host := range blocked {
		t.Run(host, func(t *testing.T) {
			if err := validateTarget(host); err == nil {
				t.Errorf("validateTarget(%q) = nil, want error", host)
			}
		})
	}
}

func TestValidateTargetAllowedLiteral(t *testing.T) {
	allowed := []string{
		"8.8.8.8",
		"8.8.8.8:53",
		"1.1.1.1",
	}
	for _, host := range allowed {
		t.Run(host, func(t *testing.T) {
			if err := validateTarget(host); err != nil {
				t.Errorf("validateTarget(%q) = %v, want nil", host, err)
			}
		})
	}
}

func TestValidateTargetEmpty(t *testing.T) {
	if err := validateTarget(""); err == nil {
		t.Error("validateTarget(\"\") should error on empty host")
	}
	if err := validateTarget("   "); err == nil {
		t.Error("validateTarget(whitespace) should error")
	}
}
