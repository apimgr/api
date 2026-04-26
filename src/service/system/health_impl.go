package system

// EndpointInfo represents a single API endpoint
type EndpointInfo struct {
	Path        string `json:"path"`
	Method      string `json:"method"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// EndpointList returns all API endpoints (1,418 total per SPEC.md)
// TODO: This should be generated/registered from actual routes
func EndpointList() []EndpointInfo {
	// Placeholder - will be populated from actual registered routes
	return []EndpointInfo{
		{Path: "/health", Method: "GET", Category: "system", Description: "Health check"},
		{Path: "/health/live", Method: "GET", Category: "system", Description: "Liveness probe"},
		{Path: "/health/ready", Method: "GET", Category: "system", Description: "Readiness probe"},
		{Path: "/system/info", Method: "GET", Category: "system", Description: "System information"},
		{Path: "/system/version", Method: "GET", Category: "system", Description: "Version details"},
		{Path: "/system/endpoints", Method: "GET", Category: "system", Description: "List all endpoints"},
		{Path: "/system/endpoints/count", Method: "GET", Category: "system", Description: "Count endpoints"},
	}
}

// EndpointCount returns total number of endpoints
func EndpointCount() map[string]int {
	endpoints := EndpointList()
	return map[string]int{
		"total": len(endpoints),
	}
}
