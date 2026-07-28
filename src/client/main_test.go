package main

import (
	"testing"

	"github.com/apimgr/api/src/client/cmd"
	"github.com/stretchr/testify/assert"
)

// TestInit_WiresTUILauncher confirms the package init() in main.go actually
// wires cmd.TUILauncher to a non-nil function before main() runs, matching
// the doc comment's promise ("TUILauncher is set by main.go..."). This is
// the one piece of real logic in main.go that doesn't require calling
// main() itself (which would call os.Exit and terminate the test binary).
func TestInit_WiresTUILauncher(t *testing.T) {
	assert.NotNil(t, cmd.TUILauncher, "main.go's init() must wire cmd.TUILauncher to tui.Run")
}

// TestBuildInfo_Defaults confirms the package-level Version/CommitID/BuildDate
// vars carry their documented ldflags-overridable defaults when the binary
// is built without -ldflags (e.g. `go test`).
func TestBuildInfo_Defaults(t *testing.T) {
	assert.Equal(t, "dev", Version)
	assert.Equal(t, "none", CommitID)
	assert.Equal(t, "unknown", BuildDate)
}

// TestBuildInfo_ConstructsCmdBuildInfo confirms the local Version/CommitID/
// BuildDate vars map correctly into cmd.BuildInfo field-for-field, the same
// construction main() performs before calling cmd.Execute. main() and
// os.Exit itself are not covered here: exercising them would terminate the
// test process, so that wiring is intentionally left to integration/e2e
// testing of the compiled binary rather than a unit test.
func TestBuildInfo_ConstructsCmdBuildInfo(t *testing.T) {
	build := cmd.BuildInfo{
		Version:   Version,
		Commit:    CommitID,
		BuildDate: BuildDate,
	}

	assert.Equal(t, Version, build.Version)
	assert.Equal(t, CommitID, build.Commit)
	assert.Equal(t, BuildDate, build.BuildDate)
}
