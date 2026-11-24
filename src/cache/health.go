package cache

import "sync"

var cacheHealth = struct {
	sync.RWMutex
	components map[string]string
}{components: make(map[string]string)}

// SetHealthStatus updates one cache component's availability. Status must be
// "ok", "degraded", or "error"; an empty or invalid status is treated as
// "error". An "ok" status removes that component from the degraded set.
func SetHealthStatus(component, status string) {
	if component == "" {
		return
	}
	if status != "ok" && status != "degraded" && status != "error" {
		status = "error"
	}
	cacheHealth.Lock()
	defer cacheHealth.Unlock()
	if status == "ok" {
		delete(cacheHealth.components, component)
		return
	}
	cacheHealth.components[component] = status
}

// HealthStatus returns the worst currently registered cache component status.
func HealthStatus() string {
	cacheHealth.RLock()
	defer cacheHealth.RUnlock()
	status := "ok"
	for _, componentStatus := range cacheHealth.components {
		if componentStatus == "error" {
			return "error"
		}
		if componentStatus == "degraded" {
			status = "degraded"
		}
	}
	return status
}
