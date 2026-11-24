package health

import (
	"regexp"
	"strings"
	"sync"
)

type Check struct {
	Key      string
	Status   string
	Critical bool
}

type entry struct {
	key          string
	critical     bool
	run          func() string
	status       string
	updateFailed bool
}

var (
	mu       sync.RWMutex
	ordered  []entry
	indexes  = map[string]int{}
	validKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

func Register(key string, critical bool, check func() string) {
	if !validKey.MatchString(key) || check == nil {
		return
	}
	mu.Lock()
	if index, ok := indexes[key]; ok {
		ordered[index].critical = critical
		ordered[index].run = check
		mu.Unlock()
		return
	}
	indexes[key] = len(ordered)
	ordered = append(ordered, entry{key: key, critical: critical, run: check, status: "error"})
	mu.Unlock()
	status := normalize(check())
	mu.Lock()
	if current, ok := indexes[key]; ok {
		ordered[current].status = status
	}
	mu.Unlock()
}

func Refresh() {
	mu.RLock()
	snapshot := append([]entry(nil), ordered...)
	mu.RUnlock()
	results := make([]string, len(snapshot))
	for i, item := range snapshot {
		results[i] = normalize(item.run())
		if item.updateFailed && results[i] == "ok" {
			results[i] = "degraded"
		}
	}
	mu.Lock()
	for i, item := range snapshot {
		if current, ok := indexes[item.key]; ok && ordered[current].run != nil {
			ordered[current].status = results[i]
		}
	}
	mu.Unlock()
}

func MarkUpdateFailure(key string, failed bool) {
	mu.Lock()
	defer mu.Unlock()
	if index, ok := indexes[key]; ok {
		ordered[index].updateFailed = failed
		if failed && ordered[index].status == "ok" {
			ordered[index].status = "degraded"
		}
	}
}

func Snapshot() []Check {
	mu.RLock()
	defer mu.RUnlock()
	checks := make([]Check, 0, len(ordered))
	for _, item := range ordered {
		checks = append(checks, Check{Key: item.key, Status: item.status, Critical: item.critical})
	}
	return checks
}

func Unregister(key string) {
	mu.Lock()
	defer mu.Unlock()
	index, ok := indexes[key]
	if !ok {
		return
	}
	ordered = append(ordered[:index], ordered[index+1:]...)
	indexes = make(map[string]int, len(ordered))
	for i, item := range ordered {
		indexes[item.key] = i
	}
}

func UnregisterPrefix(prefix string) {
	mu.Lock()
	defer mu.Unlock()
	kept := ordered[:0]
	for _, item := range ordered {
		if !strings.HasPrefix(item.key, prefix) {
			kept = append(kept, item)
		}
	}
	ordered = kept
	indexes = make(map[string]int, len(ordered))
	for i, item := range ordered {
		indexes[item.key] = i
	}
}

func normalize(value string) string {
	switch value {
	case "ok", "degraded", "error":
		return value
	default:
		return "error"
	}
}

func resetForTesting() { mu.Lock(); ordered = nil; indexes = map[string]int{}; mu.Unlock() }
