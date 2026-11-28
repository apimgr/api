package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/stretchr/testify/require"
)

func TestAbuseDetectorCountsByClassAndExpiresSlidingWindow(t *testing.T) {
	detector := &abuseDetector{windowSeconds: 60, tracked: make(map[string]*abuseWindow)}
	start := time.Unix(1000, 0)
	class, global := detector.record("192.0.2.1", "read", start)
	require.Equal(t, 1, class)
	require.Equal(t, 1, global)
	class, global = detector.record("192.0.2.1", "write", start.Add(30*time.Second))
	require.Equal(t, 1, class)
	require.Equal(t, 2, global)
	class, global = detector.record("192.0.2.1", "read", start.Add(61*time.Second))
	require.Equal(t, 1, class)
	require.Equal(t, 2, global)
}

func TestAbuseThresholdDoesNotOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	require.Equal(t, maxInt, abuseThreshold(maxInt, 10))
	require.Equal(t, 20, abuseThreshold(10, 2))
}

func TestAbuseDetectorMemoryBoundUnderHighIPCardinality(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Security.AbuseDetection.RequestFlood.Window = time.Hour
	detector := newAbuseDetector(cfg)
	require.LessOrEqual(t, detector.maxTracked*int(detector.windowSeconds)*24, maxAbuseBucketBytes)
	for index := 0; index < detector.maxTracked+100; index++ {
		detector.record("192.0.2."+intToString(index), "read", time.Unix(int64(index+1), 0))
	}
	require.LessOrEqual(t, len(detector.tracked), detector.maxTracked)
	for _, state := range detector.tracked {
		require.Len(t, state.buckets, int(detector.windowSeconds))
	}
}

func TestAbuseDetectionCountsRateLimitedResponsesAndPersistsBlock(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.RateLimit.Read = config.RateLimitClassConfig{Requests: 1, Window: 60}
	cfg.Server.Security.AbuseDetection.RequestFlood.Multiplier = 2
	cfg.Server.Security.AbuseDetection.RequestFlood.Window = time.Minute
	cfg.Server.Security.AbuseDetection.RequestFlood.BlockDuration = time.Hour
	original := config.Get()
	config.Set(cfg)
	t.Cleanup(func() { config.Set(original) })
	require.NoError(t, database.InitDatabase(config.DatabaseConfig{Driver: "sqlite"}, t.TempDir()))
	t.Cleanup(func() { _ = database.Close() })

	hits := 0
	handler := abuseDetectionMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	for index := 0; index < 3; index++ {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/example", nil)
		request.RemoteAddr = "192.0.2.45:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if index < 2 {
			require.Equal(t, http.StatusTooManyRequests, response.Code)
		} else {
			require.Equal(t, http.StatusForbidden, response.Code)
		}
	}
	require.Equal(t, 2, hits)
	block, err := database.GetActiveIPBlock(context.Background(), "192.0.2.45", time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, block)
	require.Equal(t, "request_flood", block.Reason)
}

func TestAbuseDetectionSkipsAllowlistedRequestsAndHonorsPermanentBlocks(t *testing.T) {
	cfg := config.DefaultConfig()
	previous := config.Get()
	config.Set(cfg)
	t.Cleanup(func() { config.Set(previous) })
	cfg.Server.Security.AbuseDetection.RequestFlood.Multiplier = 1
	cfg.Server.Security.BlockedIPs = []config.BlockedIPEntry{{CIDR: "198.51.100.0/24"}}
	cfg.Server.Security.Allowlist = []config.AllowlistEntry{{CIDR: "198.51.100.9/32"}}
	called := 0
	handler := allowlistMiddleware(cfg)(abuseDetectionMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	})))
	for ip, want := range map[string]int{"198.51.100.9:1234": http.StatusNoContent, "198.51.100.10:1234": http.StatusForbidden} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = ip
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, want, response.Code)
	}
	require.Equal(t, 1, called)
}
