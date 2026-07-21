package system

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHealth(t *testing.T) {
	// Force a small measurable uptime.
	origStart := StartTime
	StartTime = time.Now().Add(-2 * time.Second)
	defer func() { StartTime = origStart }()

	h := Health()
	assert.Equal(t, "healthy", h.Status)
	assert.Equal(t, Version, h.Version)
	assert.GreaterOrEqual(t, h.UptimeSeconds, int64(2))
	assert.NotEmpty(t, h.Uptime)
}

func TestLivenessProbe(t *testing.T) {
	probe := LivenessProbe()
	assert.Equal(t, map[string]string{"status": "alive"}, probe)
}

func TestReadinessProbe(t *testing.T) {
	probe := ReadinessProbe()
	assert.Equal(t, map[string]string{"status": "ready"}, probe)
}

func TestSystemInfo(t *testing.T) {
	origVersion, origCommit, origBuild := Version, CommitID, BuildDate
	Version = "1.2.3"
	CommitID = "abc1234"
	BuildDate = "2026-01-01"
	defer func() {
		Version, CommitID, BuildDate = origVersion, origCommit, origBuild
	}()

	info := SystemInfo()
	assert.Equal(t, "api", info.Name)
	assert.Equal(t, "1.2.3", info.Version)
	assert.Equal(t, "abc1234", info.CommitID)
	assert.Equal(t, "2026-01-01", info.BuildDate)
	assert.Equal(t, runtime.Version(), info.GoVersion)
	assert.Equal(t, runtime.GOOS, info.OS)
	assert.Equal(t, runtime.GOARCH, info.Arch)
	assert.Equal(t, 1418, info.Endpoints)
}

func TestVersionInfo(t *testing.T) {
	origVersion, origCommit, origBuild := Version, CommitID, BuildDate
	Version = "9.9.9"
	CommitID = "deadbee"
	BuildDate = "2099-12-31"
	defer func() {
		Version, CommitID, BuildDate = origVersion, origCommit, origBuild
	}()

	info := VersionInfo()
	assert.Equal(t, map[string]string{
		"version":    "9.9.9",
		"commit_id":  "deadbee",
		"build_date": "2099-12-31",
	}, info)
}
