package system

import (
	"runtime"
	"time"
)

// System service provides health checks, version info, and system metadata
// Per AI.md PART 36 and SPEC.md section 3.21

var (
	// Set by main.go (embedded at build time via -ldflags)
	Version   = "dev"
	CommitID  = "unknown"
	BuildDate = "unknown"

	// Runtime info
	StartTime = time.Now()
)

// HealthResponse represents basic health check response
type HealthResponse struct {
	Status        string `json:"status"`         // "healthy"
	Version       string `json:"version"`        // e.g., "1.0.0"
	Uptime        string `json:"uptime"`         // e.g., "2h34m12s"
	UptimeSeconds int64  `json:"uptime_seconds"` // seconds since start
}

// SystemInfoResponse represents system information
type SystemInfoResponse struct {
	Name      string `json:"name"`       // "api" (CasTools)
	Version   string `json:"version"`    // e.g., "1.0.0"
	CommitID  string `json:"commit_id"`  // Git short hash
	BuildDate string `json:"build_date"` // Build timestamp
	GoVersion string `json:"go_version"` // Go runtime version
	OS        string `json:"os"`         // Operating system
	Arch      string `json:"arch"`       // Architecture
	Endpoints int    `json:"endpoints"`  // Total endpoint count
}

// Health returns basic health status
func Health() HealthResponse {
	uptime := time.Since(StartTime)
	return HealthResponse{
		Status:        "healthy",
		Version:       Version,
		Uptime:        uptime.String(),
		UptimeSeconds: int64(uptime.Seconds()),
	}
}

// LivenessProbe returns liveness status (for Kubernetes)
func LivenessProbe() map[string]string {
	return map[string]string{
		"status": "alive",
	}
}

// ReadinessProbe returns readiness status (for Kubernetes)
func ReadinessProbe() map[string]string {
	return map[string]string{
		"status": "ready",
	}
}

// SystemInfo returns detailed system information
func SystemInfo() SystemInfoResponse {
	return SystemInfoResponse{
		Name:      "api",
		Version:   Version,
		CommitID:  CommitID,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Endpoints: 1418, // Per SPEC.md - CasTools has 1,418 endpoints
	}
}

// VersionInfo returns version details
func VersionInfo() map[string]string {
	return map[string]string{
		"version":    Version,
		"commit_id":  CommitID,
		"build_date": BuildDate,
	}
}
