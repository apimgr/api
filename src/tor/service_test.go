package tor

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

type testContextDialer struct{}

func (testContextDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

// TestServiceOnionAddress verifies the Tor-owned hostname is returned as read.
func TestServiceOnionAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{"typical address", "abcdefghijklmnop.onion", "abcdefghijklmnop.onion"},
		{"empty address", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{onionAddress: tt.address}
			if got := s.OnionAddress(); got != tt.want {
				t.Errorf("OnionAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestServiceOutboundEnabled verifies OutboundEnabled reflects whether a
// dialer was configured.
func TestServiceOutboundEnabled(t *testing.T) {
	s := &Service{}
	if s.OutboundEnabled() {
		t.Error("expected OutboundEnabled() to be false with no dialer")
	}

	s.dialer = testContextDialer{}
	if !s.OutboundEnabled() {
		t.Error("expected OutboundEnabled() to be true once a dialer is set")
	}
}

// TestServiceGetHTTPClient verifies both the direct and Tor-routed client
// construction branches without needing a live Tor connection.
func TestServiceGetHTTPClient(t *testing.T) {
	tests := []struct {
		name        string
		dialer      proxy.ContextDialer
		useTor      bool
		wantTimeout time.Duration
	}{
		{"no dialer, tor requested", nil, true, 30 * time.Second},
		{"dialer present, tor not requested", testContextDialer{}, false, 30 * time.Second},
		{"dialer present, tor requested", testContextDialer{}, true, 60 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{dialer: tt.dialer}
			client := s.GetHTTPClient(tt.useTor)
			if client == nil {
				t.Fatal("expected a non-nil client")
			}
			if client.Timeout != tt.wantTimeout {
				t.Errorf("Timeout = %v, want %v", client.Timeout, tt.wantTimeout)
			}
			if tt.useTor && tt.dialer != nil && client.Transport == nil {
				t.Error("expected a custom Transport when routed through Tor")
			}
		})
	}
}

// TestServicePingNoControl verifies Ping errors cleanly (never panics) when
// there is no live Tor control connection - the only branch reachable
// without a real Tor process in this sandbox.
func TestServicePingNoControl(t *testing.T) {
	s := &Service{}
	if err := s.Ping(); err == nil {
		t.Error("expected an error when tor control connection is unavailable")
	}
}

// TestServiceCloseNoProcess verifies Close is a no-op when no Tor process
// was ever started - the only branch reachable without a real Tor process
// in this sandbox.
func TestServiceCloseNoProcess(t *testing.T) {
	s := &Service{}
	if err := s.Close(); err != nil {
		t.Errorf("Close() with no process = %v, want nil", err)
	}
}
