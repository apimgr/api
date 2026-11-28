package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/apimgr/api/src/cache"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/geoip"
	"github.com/apimgr/api/src/securitydata"
	"github.com/stretchr/testify/require"
)

func TestWarmCacheStoresRuntimeDerivedMetadata(t *testing.T) {
	ctx := context.Background()
	store := cache.NewValueStore(config.CacheConfig{Type: "memory", Prefix: "test:"})
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	blocklists := []securitydata.CacheBlocklistSnapshot{{ID: "sample", Type: "ip", IPs: []string{"192.0.2.0/24"}}}
	geoInfo := geoip.DatabaseInfo{Loaded: true, Country: true}
	require.NoError(t, warmCache(ctx, store, blocklists, geoInfo))

	blocklistData, found, err := store.GetValue(ctx, "security:blocklist")
	require.NoError(t, err)
	require.True(t, found)
	var cachedBlocklists []securitydata.CacheBlocklistSnapshot
	require.NoError(t, json.Unmarshal(blocklistData, &cachedBlocklists))
	require.Equal(t, blocklists, cachedBlocklists)

	geoData, found, err := store.GetValue(ctx, "geoip:info")
	require.NoError(t, err)
	require.True(t, found)
	var cachedGeo geoip.DatabaseInfo
	require.NoError(t, json.Unmarshal(geoData, &cachedGeo))
	require.Equal(t, geoInfo, cachedGeo)
}
