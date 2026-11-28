// Package version provides shared build metadata for the server and CLI.
package version

import (
	"fmt"
	"time"
)

// BuildInfo contains the values embedded into a binary at build time.
type BuildInfo struct {
	Version    string
	CommitID   string
	BuildEpoch int64
}

// BuildDate returns the UTC RFC 3339 build timestamp, or "N/A" when the
// binary was built without a valid BuildEpoch value.
func (b BuildInfo) BuildDate() string {
	if b.BuildEpoch <= 0 {
		return "N/A"
	}
	return time.Unix(b.BuildEpoch, 0).UTC().Format(time.RFC3339)
}

// FormatOutput returns the plain-text version block consumed by scripts.
func FormatOutput(projectName, projectVersion, shortCommit, buildDate, goVersion, goOS, goArch string) string {
	return fmt.Sprintf("%s %s (%s)\nBuilt: %s\nGo: %s\nOS/Arch: %s/%s\n", projectName, projectVersion, shortCommit, buildDate, goVersion, goOS, goArch)
}
