package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/email"
)

const (
	maxAbuseTrackedIPs    = 20000
	maxAbuseAuxiliary     = 20000
	maxAbuseBucketBytes   = 32 << 20
	maxAbuseWindowSeconds = 24 * 60 * 60
)

type abuseBucket struct {
	second int64
	counts [4]uint32
}

type abuseWindow struct {
	lastSeen int64
	lastTick int64
	buckets  []abuseBucket
	totals   [4]uint64
}

type abuseDetector struct {
	mu            sync.Mutex
	windowSeconds int64
	blockDuration time.Duration
	multiplier    int
	tracked       map[string]*abuseWindow
	maxTracked    int
	active        map[string]activeAbuseBlock
	triggered     map[string]time.Time
}

type activeAbuseBlock struct {
	block     database.IPBlock
	persisted bool
}

func newAbuseDetector(cfg *config.ServerConfig) *abuseDetector {
	settings, warnings := cfg.Server.Security.AbuseDetection.Normalized()
	for _, warning := range warnings {
		slog.Warn(warning)
	}
	seconds := int64(settings.RequestFlood.Window / time.Second)
	if seconds < 1 {
		seconds = 60
	}
	if seconds > maxAbuseWindowSeconds {
		slog.Warn("server.security.abuse_detection.request_flood.window exceeds 24h; using 24h")
		seconds = maxAbuseWindowSeconds
	}
	maxTracked := maxAbuseBucketBytes / (int(seconds)*24 + 128)
	if maxTracked > maxAbuseTrackedIPs {
		maxTracked = maxAbuseTrackedIPs
	}
	if maxTracked < 1 {
		maxTracked = 1
	}
	return &abuseDetector{
		windowSeconds: seconds,
		blockDuration: settings.RequestFlood.BlockDuration,
		multiplier:    settings.RequestFlood.Multiplier,
		tracked:       make(map[string]*abuseWindow),
		maxTracked:    maxTracked,
		active:        make(map[string]activeAbuseBlock),
		triggered:     make(map[string]time.Time),
	}
}

func (detector *abuseDetector) record(ip, class string, now time.Time) (int, int) {
	detector.mu.Lock()
	defer detector.mu.Unlock()
	nowSeconds := now.Unix()
	state := detector.tracked[ip]
	if state == nil {
		if len(detector.tracked) >= detector.maxTracked {
			detector.evictOldestLocked()
		}
		state = &abuseWindow{buckets: make([]abuseBucket, int(detector.windowSeconds))}
		detector.tracked[ip] = state
	}
	windowSize := int64(len(state.buckets))
	if state.lastTick != 0 && nowSeconds < state.lastTick {
		nowSeconds = state.lastTick
	}
	state.lastSeen = nowSeconds
	if state.lastTick == 0 {
		state.lastTick = nowSeconds
	} else if elapsed := nowSeconds - state.lastTick; elapsed > 0 {
		if elapsed >= windowSize {
			state.totals = [4]uint64{}
			for index := range state.buckets {
				state.buckets[index] = abuseBucket{}
			}
		} else {
			for second := state.lastTick + 1; second <= nowSeconds; second++ {
				index := second % windowSize
				bucket := &state.buckets[index]
				if bucket.second != 0 {
					for countIndex, count := range bucket.counts {
						state.totals[countIndex] -= uint64(count)
					}
					*bucket = abuseBucket{}
				}
			}
		}
		state.lastTick = nowSeconds
	}
	classIndex := abuseClassIndex(class)
	index := nowSeconds % windowSize
	bucket := &state.buckets[index]
	if bucket.second != nowSeconds {
		if bucket.second != 0 {
			for countIndex, count := range bucket.counts {
				state.totals[countIndex] -= uint64(count)
			}
		}
		*bucket = abuseBucket{second: nowSeconds}
	}
	if bucket.counts[classIndex] < ^uint32(0) {
		bucket.counts[classIndex]++
		state.totals[classIndex]++
	}
	if bucket.counts[3] < ^uint32(0) {
		bucket.counts[3]++
		state.totals[3]++
	}
	return abuseCountInt(state.totals[classIndex]), abuseCountInt(state.totals[3])
}

func abuseCountInt(count uint64) int {
	maxInt := uint64(^uint(0) >> 1)
	if count > maxInt {
		return int(maxInt)
	}
	return int(count)
}

func abuseClassIndex(class string) int {
	switch class {
	case "read":
		return 0
	case "write":
		return 1
	default:
		return 2
	}
}

func (detector *abuseDetector) evictOldestLocked() {
	oldestIP := ""
	oldestTime := int64(^uint64(0) >> 1)
	for ip, state := range detector.tracked {
		if state.lastSeen < oldestTime {
			oldestIP, oldestTime = ip, state.lastSeen
		}
	}
	if oldestIP != "" {
		delete(detector.tracked, oldestIP)
	}
}

func (detector *abuseDetector) markTriggered(ip string, now time.Time) bool {
	detector.mu.Lock()
	defer detector.mu.Unlock()
	if until := detector.triggered[ip]; until.After(now) {
		return false
	}
	if len(detector.triggered) >= maxAbuseAuxiliary {
		evictOldestExpiry(detector.triggered)
	}
	detector.triggered[ip] = now.Add(detector.blockDuration)
	for key, until := range detector.triggered {
		if !until.After(now) {
			delete(detector.triggered, key)
		}
	}
	return true
}

func (detector *abuseDetector) rememberBlock(block database.IPBlock, persisted bool) {
	detector.mu.Lock()
	if len(detector.active) >= maxAbuseAuxiliary {
		evictOldestActiveBlock(detector.active)
	}
	detector.active[block.IP] = activeAbuseBlock{block: block, persisted: persisted}
	detector.mu.Unlock()
}

func evictOldestExpiry(entries map[string]time.Time) {
	oldestIP := ""
	var oldest time.Time
	for ip, until := range entries {
		if oldestIP == "" || until.Before(oldest) {
			oldestIP, oldest = ip, until
		}
	}
	if oldestIP != "" {
		delete(entries, oldestIP)
	}
}

func evictOldestActiveBlock(entries map[string]activeAbuseBlock) {
	oldestIP := ""
	var oldest time.Time
	for ip, entry := range entries {
		if oldestIP == "" || entry.block.ExpiresAt.Before(oldest) {
			oldestIP, oldest = ip, entry.block.ExpiresAt
		}
	}
	if oldestIP != "" {
		delete(entries, oldestIP)
	}
}

func (detector *abuseDetector) activeBlock(ip string, now time.Time) (activeAbuseBlock, bool) {
	detector.mu.Lock()
	entry, ok := detector.active[ip]
	if !ok || !entry.block.ExpiresAt.After(now) {
		delete(detector.active, ip)
		ok = false
	}
	detector.mu.Unlock()
	return entry, ok
}

func (detector *abuseDetector) markBlockPersisted(ip string) {
	detector.mu.Lock()
	if entry, ok := detector.active[ip]; ok {
		entry.persisted = true
		detector.active[ip] = entry
	}
	detector.mu.Unlock()
}

func abuseDetectionMiddleware(cfg *config.ServerConfig) func(http.Handler) http.Handler {
	detector := newAbuseDetector(cfg)
	if database.GetServerDB() != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := database.PurgeExpiredIPBlocks(ctx, time.Now().UTC()); err != nil {
			slog.Warn("abuse detection: could not purge expired IP blocks", "error", err)
		}
		cancel()
	}
	rateLimits, _ := cfg.Server.RateLimit.Normalized()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isAllowlistedRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			address, err := netip.ParseAddr(strings.TrimSpace(getClientIP(r)))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			address = address.Unmap()
			ip := address.String()
			class, classLimit := abuseRequestClass(r, rateLimits)
			classCount, globalCount := detector.record(ip, class, time.Now())
			now := time.Now()
			if active, ok := detector.activeBlock(ip, now); ok {
				if !active.persisted && persistAbuseBlock(active.block) == nil {
					detector.markBlockPersisted(ip)
				}
				writeLocalizedAPIError(w, r, http.StatusForbidden, "FORBIDDEN")
				return
			}
			if block, lookupErr := database.GetActiveIPBlock(r.Context(), ip, now); lookupErr == nil && block != nil {
				detector.rememberBlock(*block, true)
				writeLocalizedAPIError(w, r, http.StatusForbidden, "FORBIDDEN")
				return
			}
			if isConfiguredPermanentBlock(address, cfg) {
				writeLocalizedAPIError(w, r, http.StatusForbidden, "FORBIDDEN")
				return
			}
			classThreshold := abuseThreshold(classLimit, detector.multiplier)
			globalThreshold := abuseThreshold(rateLimits.GlobalBurst, detector.multiplier)
			classFlood := classCount > classThreshold
			globalFlood := globalCount > globalThreshold
			if !classFlood && !globalFlood {
				next.ServeHTTP(w, r)
				return
			}
			if detector.markTriggered(ip, now) {
				duration := detector.blockDuration
				expires := now.Add(duration)
				details := map[string]any{
					"reason": "request_flood", "class": class, "class_count": classCount,
					"class_threshold": classThreshold, "global_count": globalCount,
					"global_threshold": globalThreshold, "duration_seconds": int64(duration / time.Second),
				}
				if cfg.Server.Security.AbuseDetection.AutoBlockIP {
					block := database.IPBlock{IP: ip, Reason: "request_flood", BlockedAt: now.UTC(), ExpiresAt: expires.UTC(), OffenseCount: classCount, AutoBlocked: true}
					saveErr := persistAbuseBlock(block)
					persisted := saveErr == nil
					if saveErr != nil {
						slog.Warn("abuse detection: could not persist temporary IP block", "error", saveErr)
					}
					detector.rememberBlock(block, persisted)
					if auditErr := database.WriteAuditEventWithSource(r.Context(), "security.ip_blocked", "", ip, auditSourceForRequest(r), map[string]any{
						"reason": "request_flood", "duration_seconds": int64(duration / time.Second), "auto_blocked": true,
					}); auditErr != nil {
						slog.Warn("abuse detection: could not record temporary IP block", "error", auditErr)
					}
				}
				if auditErr := database.WriteAuditEventWithSource(r.Context(), "abuse.request_flood", "", ip, auditSourceForRequest(r), details); auditErr != nil {
					slog.Warn("abuse detection: could not record request flood", "error", auditErr)
				}
				if cfg.Server.Security.AbuseDetection.AutoAlert {
					email.OperatorSecurityAlert("Request flood detected", ip, "class="+class+" class_count="+intToString(classCount)+" global_count="+intToString(globalCount))
				}
			}
			if cfg.Server.Security.AbuseDetection.AutoBlockIP {
				writeLocalizedAPIError(w, r, http.StatusForbidden, "FORBIDDEN")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func persistAbuseBlock(block database.IPBlock) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return database.SaveTemporaryIPBlock(ctx, block)
}

func abuseThreshold(limit, multiplier int) int {
	maxInt := int(^uint(0) >> 1)
	if limit <= 0 || multiplier <= 0 {
		return maxInt
	}
	if limit > maxInt/multiplier {
		return maxInt
	}
	return limit * multiplier
}

func abuseRequestClass(r *http.Request, limits config.RateLimitConfig) (string, int) {
	switch {
	case isHealthCheckPath(r.URL.Path):
		return "health", limits.Health.Requests
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		return "read", limits.Read.Requests
	default:
		return "write", limits.Write.Requests
	}
}

func isConfiguredPermanentBlock(address netip.Addr, cfg *config.ServerConfig) bool {
	current := config.Get()
	if current != nil {
		cfg = current
	}
	for _, entry := range cfg.Server.Security.BlockedIPs {
		if prefix, err := netip.ParsePrefix(entry.CIDR); err == nil && prefix.Contains(address) {
			return true
		}
		if blocked, err := netip.ParseAddr(entry.CIDR); err == nil && blocked.Unmap() == address {
			return true
		}
	}
	return false
}

func auditSourceForRequest(r *http.Request) string {
	if isMCPDispatch(r) || strings.Contains(r.URL.Path, "/server/mcp") || r.URL.Path == "/mcp" || r.URL.Path == "/api/mcp" {
		return "mcp"
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return "api"
	}
	return "web"
}
