// Command api-cli is the client for the api server: a CLI, TUI, and
// scripting-friendly interface to every server endpoint, per AI.md PART 32.
package main

import (
	"os"

	"github.com/apimgr/api/src/client/cmd"
	"github.com/apimgr/api/src/client/tui"
)

// Version, CommitID, and BuildDate are injected at build time via
// -ldflags "-X main.Version=... -X main.CommitID=... -X main.BuildDate=...",
// matching the Makefile/Dockerfile/release.yml ldflags targets.
var (
	Version      = "dev"
	CommitID     = "none"
	BuildDate    = "unknown"
	OfficialSite = ""
)

func init() {
	cmd.TUILauncher = tui.Run
}

func main() {
	build := cmd.BuildInfo{
		Version:   Version,
		Commit:    CommitID,
		BuildDate: BuildDate,
	}
	os.Exit(cmd.Execute(os.Args, build))
}
