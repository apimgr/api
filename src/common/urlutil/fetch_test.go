package urlutil

import (
	"net"
	"testing"
)

func TestIsDisallowedIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "nil", want: true},
		{name: "private", ip: "10.0.0.1", want: true},
		{name: "loopback", ip: "127.0.0.1", want: true},
		{name: "carrier-grade NAT", ip: "100.64.0.1", want: true},
		{name: "benchmarking", ip: "198.18.0.1", want: true},
		{name: "documentation range 1", ip: "192.0.2.1", want: true},
		{name: "documentation range 2", ip: "198.51.100.1", want: true},
		{name: "documentation range 3", ip: "203.0.113.1", want: true},
		{name: "public", ip: "8.8.8.8", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var ip net.IP
			if test.ip != "" {
				ip = net.ParseIP(test.ip)
				if ip == nil {
					t.Fatalf("invalid test IP %q", test.ip)
				}
			}
			if got := isDisallowedIP(ip); got != test.want {
				t.Errorf("isDisallowedIP(%v) = %v, want %v", ip, got, test.want)
			}
		})
	}
}
