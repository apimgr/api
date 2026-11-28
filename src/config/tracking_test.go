package config

import "testing"

func TestTrackingConfigEnabledFollowsPlatformFormats(t *testing.T) {
	tests := []struct {
		name string
		cfg  TrackingConfig
		want bool
	}{
		{"disabled", TrackingConfig{}, false},
		{"google legacy", TrackingConfig{Type: "google", ID: "UA-1-2"}, true},
		{"google ga4", TrackingConfig{Type: "google", ID: "G-A"}, true},
		{"matomo needs url", TrackingConfig{Type: "matomo", ID: "1"}, false},
		{"matomo valid", TrackingConfig{Type: "matomo", ID: "1", URL: "https://analytics.example"}, true},
		{"owa id is nonempty", TrackingConfig{Type: "owa", ID: "site id", URL: "https://analytics.example"}, true},
		{"fathom custom id", TrackingConfig{Type: "fathom", ID: "site-id"}, true},
		{"cloudflare token", TrackingConfig{Type: "cloudflare", ID: "beacon-token"}, true},
		{"cloudflare empty", TrackingConfig{Type: "cloudflare"}, false},
		{"simple auto detected", TrackingConfig{Type: "simple"}, true},
		{"invalid url", TrackingConfig{Type: "fathom", ID: "site-id", URL: "javascript:alert(1)"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Enabled(); got != tt.want {
				t.Fatalf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
