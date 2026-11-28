// Command api-cli is the client for the api server: a CLI, TUI, and
// scripting-friendly interface to every server endpoint, per AI.md PART 32.
package main

import (
	"os"
	"strconv"
	"time"

	"github.com/apimgr/api/src/client/cmd"
	"github.com/apimgr/api/src/client/tui"
)

// Version, CommitID, BuildEpoch, and OfficialSite are injected at build time;
// BuildDate is derived from BuildEpoch at process start.
var (
	Version      = "devel"
	CommitID     = "N/A"
	BuildEpoch   = "0"
	BuildDate    = "N/A"
	OfficialSite = ""
)

func init() {
	if epoch, err := strconv.ParseInt(BuildEpoch, 10, 64); err == nil && epoch > 0 {
		BuildDate = time.Unix(epoch, 0).UTC().Format("2006-01-02T15:04:05Z")
	}
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
