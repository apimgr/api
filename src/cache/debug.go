package cache

import (
	"log/slog"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
)

func debugCacheLog(name string, hit bool, evictions, size int) {
	if !mode.IsDebugEnabled() {
		return
	}
	cfg := config.Get()
	if cfg == nil || !cfg.Server.Debug.LogCache {
		return
	}
	slog.Debug("cache lookup", "cache", name, "hit", hit, "evictions", evictions, "size", size)
}
