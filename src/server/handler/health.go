package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"
)

var (
	// startTime is when the server started
	startTime = time.Now()
	// Version information (set via ldflags)
	Version   = "dev"
	CommitID  = "unknown"
	BuildDate = "unknown"
	// dbPinger for health checks (set from main)
	dbPinger interface{ PingContext(context.Context) error }
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string            `json:"status"`
	Version   string            `json:"version"`
	Mode      string            `json:"mode"`
	Uptime    string            `json:"uptime"`
	Timestamp string            `json:"timestamp"`
	Node      NodeInfo          `json:"node"`
	Cluster   ClusterInfo       `json:"cluster"`
	Checks    map[string]string `json:"checks"`
}

// NodeInfo contains node information
type NodeInfo struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
}

// ClusterInfo contains cluster information
type ClusterInfo struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status,omitempty"`
	Nodes   int    `json:"nodes,omitempty"`
	Role    string `json:"role,omitempty"`
}

// VersionResponse represents the version endpoint response
type VersionResponse struct {
	Version    string `json:"version"`
	CommitID   string `json:"commit_id"`
	BuildDate  string `json:"build_date"`
	GoVersion  string `json:"go_version"`
	Platform   string `json:"platform"`
	Arch       string `json:"arch"`
}

// HandleHealthCheck handles /api/v1/healthz endpoint
func HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()

	// Calculate uptime
	uptime := getUptime()

	// Get application mode from environment or default to production
	mode := "production"
	if os.Getenv("API_MODE") == "development" {
		mode = "development"
	}

	// Perform health checks
	checks := performHealthChecks()

	// Determine overall status
	status := "healthy"
	for _, checkStatus := range checks {
		if checkStatus != "ok" {
			status = "unhealthy"
			break
		}
	}

	response := HealthResponse{
		Status:    status,
		Version:   Version,
		Mode:      mode,
		Uptime:    uptime,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Node: NodeInfo{
			ID:       "standalone",
			Hostname: hostname,
		},
		Cluster: ClusterInfo{
			Enabled: false,
		},
		Checks: checks,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleVersion handles /api/v1/version endpoint
func HandleVersion(w http.ResponseWriter, r *http.Request) {
	response := VersionResponse{
		Version:    Version,
		CommitID:   CommitID,
		BuildDate:  BuildDate,
		GoVersion:  runtime.Version(),
		Platform:   runtime.GOOS,
		Arch:       runtime.GOARCH,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// performHealthChecks runs all health checks
func performHealthChecks() map[string]string {
	checks := make(map[string]string)

	// Database check
	checks["database"] = checkDatabase()

	// Cache check
	checks["cache"] = checkCache()

	// Disk check
	checks["disk"] = checkDisk()

	return checks
}

// SetDatabase sets the database connection for health checks
func SetDatabase(db interface{ PingContext(context.Context) error }) {
	dbPinger = db
}

// checkDatabase checks database connectivity
func checkDatabase() string {
	if dbPinger == nil {
		return "unknown"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := dbPinger.PingContext(ctx); err != nil {
		return "error"
	}

	return "ok"
}

// checkCache checks cache connectivity
func checkCache() string {
	// In-memory cache is always available
	// Valkey/Redis check would go here if configured
	return "ok"
}

// checkDisk is implemented in health_unix.go and health_windows.go


// getUptime returns server uptime as human-readable string
func getUptime() string {
	duration := time.Since(startTime)

	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		return formatUptime(days, hours, minutes, "d", "h", "m")
	}
	if hours > 0 {
		return formatUptime(hours, minutes, 0, "h", "m", "")
	}
	return formatUptime(minutes, 0, 0, "m", "", "")
}

// formatUptime formats uptime components
func formatUptime(a, b, c int, aUnit, bUnit, cUnit string) string {
	if c > 0 && cUnit != "" {
		return formatDuration(a, aUnit, b, bUnit, c, cUnit)
	}
	if b > 0 && bUnit != "" {
		return formatDuration2(a, aUnit, b, bUnit)
	}
	return formatDuration1(a, aUnit)
}

// formatDuration formats 3-part duration
func formatDuration(a int, aUnit string, b int, bUnit string, c int, cUnit string) string {
	return fmt.Sprintf("%d%s %d%s %d%s", a, aUnit, b, bUnit, c, cUnit)
}

// formatDuration2 formats 2-part duration
func formatDuration2(a int, aUnit string, b int, bUnit string) string {
	return fmt.Sprintf("%d%s %d%s", a, aUnit, b, bUnit)
}

// formatDuration1 formats 1-part duration
func formatDuration1(a int, aUnit string) string {
	return fmt.Sprintf("%d%s", a, aUnit)
}

// HandleSystemInfo handles /api/v1/system/info endpoint
func HandleSystemInfo(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"name":      "api",
		"version":   Version,
		"commit_id": CommitID,
		"build_date": BuildDate,
		"go_version": runtime.Version(),
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
		"endpoints": 1418,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleLiveness handles /api/v1/system/liveness endpoint
func HandleLiveness(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status": "alive",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleReadiness handles /api/v1/system/readiness endpoint
func HandleReadiness(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status": "ready",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleEndpoints handles /api/v1/system/endpoints endpoint
func HandleEndpoints(w http.ResponseWriter, r *http.Request) {
	endpoints := map[string]interface{}{
		"total": 1418,
		"categories": []string{
			"system", "text", "crypto", "datetime", "network",
			"parse", "image", "math", "validate", "docker",
			"geo", "fun", "lorem", "dev", "generate",
			"language", "convert", "weather", "osint", "research", "test",
		},
		"message": "Full endpoint documentation available at /api/docs",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(endpoints)
}

// HandleStats handles /api/v1/system/stats endpoint
func HandleStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{
		"uptime":    time.Since(startTime).String(),
		"version":   Version,
		"endpoints": 1418,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stats)
}

// HandleHealth handles /api/v1/system/health endpoint (alias for HandleHealthCheck)
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	HandleHealthCheck(w, r)
}

