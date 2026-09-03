package tor

import (
	"testing"
	"time"

	"github.com/cretz/bine/tor"
)

// TestServiceOnionAddress verifies the .onion suffix is appended to the
// stored service ID.
func TestServiceOnionAddress(t *testing.T) {
	tests := []struct {
		name    string
		onionID string
		want    string
	}{
		{"typical id", "abcdefghijklmnop", "abcdefghijklmnop.onion"},
		{"empty id", "", ".onion"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{onionID: tt.onionID}
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

	// A zero-value Dialer is safe to construct and assign - its
	// DialContext method is only assigned, never invoked, by this test.
	s.dialer = &tor.Dialer{}
	if !s.OutboundEnabled() {
		t.Error("expected OutboundEnabled() to be true once a dialer is set")
	}
}

// TestServiceGetHTTPClient verifies both the direct and Tor-routed client
// construction branches without needing a live Tor connection.
func TestServiceGetHTTPClient(t *testing.T) {
	tests := []struct {
		name        string
		dialer      *tor.Dialer
		useTor      bool
		wantTimeout time.Duration
	}{
		{"no dialer, tor requested", nil, true, 30 * time.Second},
		{"dialer present, tor not requested", &tor.Dialer{}, false, 30 * time.Second},
		{"dialer present, tor requested", &tor.Dialer{}, true, 60 * time.Second},
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
