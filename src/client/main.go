// Command api-cli is the client for the api server: a CLI, TUI, and
// scripting-friendly interface to every server endpoint, per AI.md PART 32.
package main

import (
	"os"

	"github.com/apimgr/api/src/client/cmd"
	"github.com/apimgr/api/src/client/tui"
)

// version, commit, and buildDate are injected at build time via
// -ldflags "-X main.version=... -X main.commit=... -X main.buildDate=...".
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func init() {
	cmd.TUILauncher = tui.Run
}

func main() {
	build := cmd.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	}
	os.Exit(cmd.Execute(os.Args, build))
}
