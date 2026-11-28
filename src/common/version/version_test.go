package version

import "testing"

func TestBuildDate(t *testing.T) {
	if got := (BuildInfo{BuildEpoch: 0}).BuildDate(); got != "N/A" {
		t.Fatalf("unset build date = %q, want N/A", got)
	}
	if got := (BuildInfo{BuildEpoch: 1765112713}).BuildDate(); got != "2025-12-07T13:05:13Z" {
		t.Fatalf("build date = %q", got)
	}
}

func TestFormatOutputIsScriptFriendlyPlainText(t *testing.T) {
	want := "api 1.2.3 (abc1234)\nBuilt: 2026-01-02T03:04:05Z\nGo: go1.25.0\nOS/Arch: linux/amd64\n"
	if got := FormatOutput("api", "1.2.3", "abc1234", "2026-01-02T03:04:05Z", "go1.25.0", "linux", "amd64"); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}
