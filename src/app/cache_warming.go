package app

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/apimgr/api/src/cache"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/geoip"
	"github.com/apimgr/api/src/securitydata"
)

const (
	blocklistCacheTTL = time.Hour
	geoInfoCacheTTL   = 7 * 24 * time.Hour
	cacheWarmInterval = time.Hour
)

func startCacheWarming(ctx context.Context, cfg config.CacheConfig) func() {
	store := cache.NewValueStore(cfg)
	warm := func(warmCtx context.Context) {
		if err := warmCache(warmCtx, store, securitydata.DefaultBlocklists.CacheSnapshot(), geoip.GetGeoIPDatabase().GetDatabaseInfo()); err != nil {
			log.Printf("cache warming failed: %v", err)
		}
	}
	warm(ctx)
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(cacheWarmInterval)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				warm(workerCtx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
		if err := store.Close(); err != nil {
			log.Printf("cache close failed: %v", err)
		}
	}
}

func warmCache(ctx context.Context, store cache.ValueStore, blocklists []securitydata.CacheBlocklistSnapshot, geoInfo geoip.DatabaseInfo) error {
	blocklistData, err := json.Marshal(blocklists)
	if err != nil {
		return err
	}
	if err := store.SetValue(ctx, "security:blocklist", blocklistData, blocklistCacheTTL); err != nil {
		return err
	}
	geoData, err := json.Marshal(geoInfo)
	if err != nil {
		return err
	}
	return store.SetValue(ctx, "geoip:info", geoData, geoInfoCacheTTL)
}
