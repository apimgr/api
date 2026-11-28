package banner

import (
	"bytes"
	"strings"
	"testing"

	"github.com/apimgr/api/src/common/display"
)

func TestPrintStartupUsesPlainTextForDumbTerminal(t *testing.T) {
	var out bytes.Buffer
	if err := PrintStartup(BannerConfig{
		AppName: "api", Version: "1.2.3", AppMode: "production",
		URLs: []string{"http://localhost:8080"}, Writer: &out, Lang: "en",
		Emojis: true, Terminal: display.DisplayEnv{IsTerminal: true, TerminalType: "dumb", Cols: 100},
	}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "🚀") || strings.Contains(got, "\x1b[") {
		t.Fatalf("dumb terminal banner contains display control or emoji: %q", got)
	}
	if !strings.Contains(got, "api v1.2.3") || !strings.Contains(got, "http://localhost:8080") {
		t.Fatalf("plain banner missing required content: %q", got)
	}
}

func TestPrintStartupSelectsWidthVariants(t *testing.T) {
	for _, test := range []struct {
		width int
		want  string
		avoid string
	}{
		{80, "╭", ""},
		{60, "🚀", "╭"},
		{40, "api 1.2.3", "🚀"},
		{39, "api localhost:8080", "🚀"},
	} {
		var out bytes.Buffer
		if err := PrintStartup(BannerConfig{
			AppName: "api", Version: "1.2.3", AppMode: "production",
			URLs: []string{"http://localhost:8080"}, Writer: &out, Lang: "en", Emojis: true,
			Terminal: display.DisplayEnv{IsTerminal: true, Cols: test.width},
		}); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		if !strings.Contains(got, test.want) || (test.avoid != "" && strings.Contains(got, test.avoid)) {
			t.Fatalf("width %d output %q does not match variant", test.width, got)
		}
	}
}
