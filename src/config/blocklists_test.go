package config

import "testing"

func TestBlocklistsValidateSources(t *testing.T) {
	tests := []struct {
		name    string
		sources []BlocklistSource
		wantErr bool
	}{
		{"https source", []BlocklistSource{{ID: "custom_feed", Type: "domain", URL: "https://example.org/list.txt"}}, false},
		{"reserved ID", []BlocklistSource{{ID: "firehol_level1", Type: "ip", URL: "https://example.org/list.txt"}}, true},
		{"invalid ID", []BlocklistSource{{ID: "Bad-ID", Type: "ip", URL: "https://example.org/list.txt"}}, true},
		{"invalid type", []BlocklistSource{{ID: "custom", Type: "url", URL: "https://example.org/list.txt"}}, true},
		{"non HTTPS", []BlocklistSource{{ID: "custom", Type: "ip", URL: "http://example.org/list.txt"}}, true},
		{"duplicate ID", []BlocklistSource{{ID: "custom", Type: "ip", URL: "https://example.org/1"}, {ID: "custom", Type: "ip", URL: "https://example.org/2"}}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := (BlocklistsConfig{Enabled: true, ExtraSources: test.sources}).Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error=%v wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestBlocklistsDefaultEnabledWithoutSources(t *testing.T) {
	blocklists := defaultConfig().Server.Security.Blocklists
	if !blocklists.Enabled || len(blocklists.ExtraSources) != 0 {
		t.Fatalf("unexpected blocklist defaults: %#v", blocklists)
	}
}
