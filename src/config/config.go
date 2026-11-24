package config

import (
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/paths"
	"gopkg.in/yaml.v3"
)

// Config represents the complete server configuration
type Config struct {
	Server           ServerConfig           `yaml:"server"`
	Web              WebConfig              `yaml:"web"`
	Output           OutputConfig           `yaml:"output"`
	Data             map[string]interface{} `yaml:"data,omitempty"`
	CLI              CLIConfig              `yaml:"cli"`
	GeneratedSecrets []string               `yaml:"-"`
}

// OutputConfig holds CLI color/emoji output settings per AI.md PART 8
// "NO_COLOR Support". Color and Emoji are pointers so an absent key is
// distinguishable from an explicit false, matching the CLI flag > config
// file > NO_COLOR env var > auto-detect priority order.
type OutputConfig struct {
	Color *bool `yaml:"color,omitempty"`
	Emoji *bool `yaml:"emoji,omitempty"`
}

// CLIConfig controls server-side distribution of operator-provisioned CLI
// release artifacts.
type CLIConfig struct {
	BinaryDownload CLIBinaryDownloadConfig `yaml:"binary_download"`
}

// CLIBinaryDownloadConfig selects the release-artifact directory and whether
// downloads require operator authentication.
type CLIBinaryDownloadConfig struct {
	Dir         string `yaml:"dir"`
	RequireAuth bool   `yaml:"require_auth"`
}

// ServerConfig holds server-related settings
type ServerConfig struct {
	Port       string             `yaml:"port"`
	FQDN       string             `yaml:"fqdn"`
	Address    string             `yaml:"address"`
	Mode       string             `yaml:"mode"`
	APIVersion string             `yaml:"api_version"`
	BaseURL    string             `yaml:"baseurl"`
	Limits     ServerLimitsConfig `yaml:"limits"`
	// Token is the global operator token (AI.md PART 8 two-tier auth). It
	// lives only in secrets.yml, is never written to the api_tokens table,
	// and is validated by constant-time comparison against its SHA-256 hash
	// cached in memory at startup.
	Token          string                 `yaml:"token"`
	Branding       BrandingConfig         `yaml:"branding"`
	SSL            SSLConfig              `yaml:"ssl"`
	Scheduler      SchedulerConfig        `yaml:"scheduler"`
	TrustedProxies TrustedProxiesConfig   `yaml:"trusted_proxies"`
	RateLimit      RateLimitConfig        `yaml:"rate_limit"`
	Database       DatabaseConfig         `yaml:"database"`
	Cache          CacheConfig            `yaml:"cache"`
	Debug          DebugConfig            `yaml:"debug"`
	Healthz        HealthzConfig          `yaml:"healthz"`
	Logs           LogsConfig             `yaml:"logs"`
	Tokens         ServerTokenConfig      `yaml:"tokens"`
	Update         UpdateConfig           `yaml:"update"`
	Tor            TorConfig              `yaml:"tor"`
	I2P            I2PConfig              `yaml:"i2p"`
	Metrics        MetricsConfig          `yaml:"metrics"`
	Backup         BackupConfig           `yaml:"backup"`
	Restore        RestoreConfig          `yaml:"restore"`
	Compliance     ComplianceConfig       `yaml:"compliance"`
	Notifications  NotificationsConfig    `yaml:"notifications"`
	Security       ServerSecurityConfig   `yaml:"security"`
	GeoIP          GeoIPConfig            `yaml:"geoip"`
	Contact        ContactConfig          `yaml:"contact"`
	Privacy        PrivacyConfig          `yaml:"privacy"`
	Tracking       TrackingConfig         `yaml:"tracking"`
	CSRF           CSRFConfig             `yaml:"csrf"`
	I18N           I18NConfig             `yaml:"i18n"`
	Pages          map[string]interface{} `yaml:"pages,omitempty"`
}

// ServerLimitsConfig bounds HTTP request bodies and connection timeouts.
type ServerLimitsConfig struct {
	MaxBodySize  string `yaml:"max_body_size"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
	IdleTimeout  string `yaml:"idle_timeout"`
}

// DefaultServerLimitsConfig returns the request and connection limits from
// AI.md PART 12.
func DefaultServerLimitsConfig() ServerLimitsConfig {
	return ServerLimitsConfig{MaxBodySize: "10MB", ReadTimeout: "30s", WriteTimeout: "30s", IdleTimeout: "120s"}
}

// Normalized replaces missing or malformed server limits with their defaults.
func (c ServerLimitsConfig) Normalized() (ServerLimitsConfig, []string) {
	defaults := DefaultServerLimitsConfig()
	var warnings []string
	for _, item := range []struct {
		name     string
		value    *string
		fallback string
	}{
		{"server.limits.max_body_size", &c.MaxBodySize, defaults.MaxBodySize},
		{"server.limits.read_timeout", &c.ReadTimeout, defaults.ReadTimeout},
		{"server.limits.write_timeout", &c.WriteTimeout, defaults.WriteTimeout},
		{"server.limits.idle_timeout", &c.IdleTimeout, defaults.IdleTimeout},
	} {
		valid := false
		if item.name == "server.limits.max_body_size" {
			_, err := ParseByteSize(*item.value)
			valid = err == nil
		} else {
			duration, err := time.ParseDuration(*item.value)
			valid = err == nil && duration > 0
		}
		if !valid {
			warnings = append(warnings, item.name+" is invalid; using "+item.fallback)
			*item.value = item.fallback
		}
	}
	return c, warnings
}

// MaxBodySizeBytes returns the validated request body limit in bytes.
func (c ServerLimitsConfig) MaxBodySizeBytes() int64 {
	normalized, _ := c.Normalized()
	size, err := ParseByteSize(normalized.MaxBodySize)
	if err != nil {
		return 10 << 20
	}
	return size
}

// DebugConfig controls explicitly enabled diagnostics per AI.md PART 6.
type DebugConfig struct {
	Pprof                bool   `yaml:"pprof"`
	LogQueries           bool   `yaml:"log_queries"`
	LogCache             bool   `yaml:"log_cache"`
	LogBodies            bool   `yaml:"log_bodies"`
	MaxBodyLogSize       string `yaml:"max_body_log_size"`
	BlockProfileRate     int    `yaml:"block_profile_rate"`
	MutexProfileFraction int    `yaml:"mutex_profile_fraction"`
	RuntimeEndpoints     bool   `yaml:"runtime_endpoints"`
}

// Normalized replaces invalid profiling rates with the documented default.
func (c DebugConfig) Normalized() (DebugConfig, []string) {
	var warnings []string
	if c.BlockProfileRate < 0 {
		c.BlockProfileRate = 1
		warnings = append(warnings, "server.debug.block_profile_rate cannot be negative; using 1")
	}
	if c.MutexProfileFraction < 0 {
		c.MutexProfileFraction = 1
		warnings = append(warnings, "server.debug.mutex_profile_fraction cannot be negative; using 1")
	}
	return c, warnings
}

// I18NConfig selects the server's default and enabled interface languages.
// Every embedded locale is enabled by default; the six AI.md-required
// languages remain enabled even when an operator narrows the optional set.
type I18NConfig struct {
	DefaultLanguage    string   `yaml:"default_language"`
	AvailableLanguages []string `yaml:"available_languages"`
}

// ServerTokenConfig controls anonymous owner-token behavior per AI.md PART 8.
type ServerTokenConfig struct {
	MaxLinked     int    `yaml:"max_linked"`
	CookieMaxAge  string `yaml:"cookie_max_age"`
	IdlePurgeDays int    `yaml:"idle_purge_days"`
}

// Normalized replaces invalid owner-token settings with their documented
// defaults and returns warnings for the caller to log.
func (c ServerTokenConfig) Normalized() (ServerTokenConfig, []string) {
	var warnings []string
	if c.MaxLinked < 0 {
		warnings = append(warnings, "server.tokens.max_linked cannot be negative; using the default of 25")
		c.MaxLinked = 25
	}
	if c.IdlePurgeDays < 0 {
		warnings = append(warnings, "server.tokens.idle_purge_days cannot be negative; using the default of 0")
		c.IdlePurgeDays = 0
	}
	if _, err := parseTokenCookieMaxAge(c.CookieMaxAge); err != nil {
		warnings = append(warnings, "server.tokens.cookie_max_age is invalid; using the default of 1y")
		c.CookieMaxAge = "1y"
	}
	return c, warnings
}

func parseTokenCookieMaxAge(value string) (time.Duration, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	duration, err := time.ParseDuration(value)
	if err == nil && duration > 0 {
		return duration, nil
	}
	for index, character := range value {
		if character < '0' || character > '9' {
			amount, parseErr := strconv.Atoi(value[:index])
			if parseErr != nil || amount <= 0 {
				break
			}
			unit := value[index:]
			switch unit {
			case "d":
				return time.Duration(amount) * 24 * time.Hour, nil
			case "w":
				return time.Duration(amount) * 7 * 24 * time.Hour, nil
			case "y":
				return time.Duration(amount) * 365 * 24 * time.Hour, nil
			}
			break
		}
	}
	return 0, fmt.Errorf("cookie max-age must be positive")
}

// CookieMaxAgeDuration returns the normalized owner-cookie lifetime.
func (c ServerTokenConfig) CookieMaxAgeDuration() time.Duration {
	duration, err := parseTokenCookieMaxAge(c.CookieMaxAge)
	if err != nil {
		return 365 * 24 * time.Hour
	}
	return duration
}

// Normalized applies the registry defaults and keeps the required language
// set enabled. Invalid language codes are discarded with a warning.
func (c I18NConfig) Normalized() (I18NConfig, []string) {
	var warnings []string
	known := i18n.SupportedLanguageCodes()
	if len(c.AvailableLanguages) == 0 {
		c.AvailableLanguages = known
	}
	allowed := make(map[string]bool, len(known))
	for _, code := range known {
		allowed[code] = true
	}
	selected := make(map[string]bool, len(c.AvailableLanguages))
	for _, code := range c.AvailableLanguages {
		if !allowed[code] {
			warnings = append(warnings, fmt.Sprintf("server.i18n.available_languages contains unsupported language %q; ignoring it", code))
			continue
		}
		selected[code] = true
	}
	for _, code := range i18n.RequiredLanguageCodes() {
		if !selected[code] {
			selected[code] = true
			warnings = append(warnings, fmt.Sprintf("server.i18n.available_languages must include required language %q; enabling it", code))
		}
	}
	c.AvailableLanguages = c.AvailableLanguages[:0]
	for _, code := range known {
		if selected[code] {
			c.AvailableLanguages = append(c.AvailableLanguages, code)
		}
	}
	if c.DefaultLanguage == "" {
		c.DefaultLanguage = i18n.DefaultLanguage
	}
	if !selected[c.DefaultLanguage] {
		warnings = append(warnings, fmt.Sprintf("server.i18n.default_language %q is not enabled; using %q", c.DefaultLanguage, i18n.DefaultLanguage))
		c.DefaultLanguage = i18n.DefaultLanguage
	}
	return c, warnings
}

// BackupConfig holds backup encryption settings per AI.md PART 21
// "Backup Encryption". Encryption is optional unless Compliance.Enabled is
// true, in which case EncryptionPassword is mandatory and scheduled backups
// are blocked until it is set.
type BackupConfig struct {
	Encryption BackupEncryptionConfig `yaml:"encryption"`
	// Retention bounds how many archives are kept and how much space they
	// may occupy (PART 21 "Backup Retention").
	Retention BackupRetentionConfig `yaml:"retention"`
	// EncryptionPassword derives an AES-256-GCM key via Argon2id when
	// non-empty. Never logged, never returned by any API. The interactive
	// CLI/WebUI/API restore and manual-backup flows always prompt for this
	// rather than accepting it as a flag (PART 21 "no password CLI flag").
	EncryptionPassword string `yaml:"encryption_password"`
}

// BackupEncryptionConfig mirrors PART 21's "true if password was set" flag,
// shown to operators as backup encryption status.
type BackupEncryptionConfig struct {
	Enabled bool `yaml:"enabled"`
}

// BackupRetentionConfig bounds the backup archive set per AI.md PART 21
// "Backup Retention". Retention priority runs yearly > monthly > weekly >
// daily, and a single archive can satisfy several categories at once.
// Invalid values warn and fall back to the default rather than failing
// startup.
type BackupRetentionConfig struct {
	// MaxBackups is the number of daily full backups to keep (minimum 1).
	MaxBackups int `yaml:"max_backups"`
	// KeepWeekly retains that many Sunday backups; 0 disables.
	KeepWeekly int `yaml:"keep_weekly"`
	// KeepMonthly retains that many first-of-month backups; 0 disables.
	KeepMonthly int `yaml:"keep_monthly"`
	// KeepYearly retains that many January 1st backups; 0 disables.
	KeepYearly int `yaml:"keep_yearly"`
	// MaxTotalSize is a hard cap expressed as a percentage of the backup
	// volume ("10%") or an absolute size ("50G"). It overrides the count
	// limits by deleting oldest first. "0" or any falsey word disables it.
	MaxTotalSize string `yaml:"max_total_size"`
}

// Normalized returns the retention policy with invalid values replaced by
// their defaults, per PART 21's "warn and fall back" rule. The second
// return value lists the human-readable warnings for the caller to log.
func (r BackupRetentionConfig) Normalized() (BackupRetentionConfig, []string) {
	normalized := r
	var warnings []string

	if normalized.MaxBackups < 1 {
		warnings = append(warnings, "server.backup.retention.max_backups must be at least 1; using the default of 1")
		normalized.MaxBackups = 1
	}
	if normalized.KeepWeekly < 0 {
		warnings = append(warnings, "server.backup.retention.keep_weekly cannot be negative; disabling weekly retention")
		normalized.KeepWeekly = 0
	}
	if normalized.KeepMonthly < 0 {
		warnings = append(warnings, "server.backup.retention.keep_monthly cannot be negative; disabling monthly retention")
		normalized.KeepMonthly = 0
	}
	if normalized.KeepYearly < 0 {
		warnings = append(warnings, "server.backup.retention.keep_yearly cannot be negative; disabling yearly retention")
		normalized.KeepYearly = 0
	}
	if normalized.MaxBackups > 7 {
		warnings = append(warnings, "server.backup.retention.max_backups is unusually high; accepting it but disk usage will grow")
	}

	return normalized, warnings
}

// SizeCapDisabled reports whether max_total_size is switched off, honoring
// the shared falsey word list ("0", "false", "no", "none", "disable",
// "disabled", "off").
func (r BackupRetentionConfig) SizeCapDisabled() bool {
	value := strings.TrimSpace(r.MaxTotalSize)
	if value == "" || value == "0" {
		return true
	}
	return IsFalsy(value)
}

// ComplianceConfig gates mandatory backup encryption per AI.md PART 21
// "Compliance Mode Enforcement" (HIPAA, SOC2, etc.).
type ComplianceConfig struct {
	Enabled bool `yaml:"enabled"`
}

// MetricsConfig holds metrics settings, per AI.md PART 20. The endpoint
// paths are fixed (/server/metrics[/{service}] plus API and root aliases);
// they are not operator-configurable. Metrics are internal-only
// (firewall/proxy/NetworkPolicy restricted per PART 20 Access Control) AND
// each service carries its own mandatory bearer token.
type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
	// Root gates the /metrics[/{service}] root alias, which Prometheus
	// scrapers expect by default.
	Root MetricsRootConfig `yaml:"root"`
	// Auth carries the per-service bearer tokens.
	Auth MetricsAuthConfig `yaml:"auth"`
	// IncludeSystem enables host CPU/memory/disk gauges.
	IncludeSystem bool `yaml:"include_system"`
	// IncludeRuntime enables Go runtime gauges.
	IncludeRuntime bool `yaml:"include_runtime"`
	// Loki bounds the recent-log-entry stream served to Loki.
	Loki MetricsLokiConfig `yaml:"loki"`
	// DurationBuckets overrides the http_request_duration_seconds buckets.
	DurationBuckets []float64 `yaml:"duration_buckets"`
	// SizeBuckets overrides the http_request/response_size_bytes buckets.
	SizeBuckets []float64 `yaml:"size_buckets"`
}

// TorConfig holds Tor hidden-service settings, per AI.md PART 31. Hidden
// service support is always enabled when a Tor binary is found - there is
// no separate enable/disable flag.
type TorConfig struct {
	// Binary is the path to the tor executable (empty = auto-detect).
	Binary string `yaml:"binary"`
	// UseNetwork routes the server's own outbound requests through Tor.
	UseNetwork bool `yaml:"use_network"`
	// MaxCircuits is the maximum number of circuits Tor may keep open.
	MaxCircuits int `yaml:"max_circuits"`
	// StartupTimeout bounds the Tor control-port startup wait in seconds.
	StartupTimeout int `yaml:"startup_timeout"`
	// CircuitTimeout is the circuit build timeout, in seconds.
	CircuitTimeout int `yaml:"circuit_timeout"`
	// BootstrapTimeout is the max seconds to wait for Tor to bootstrap.
	BootstrapTimeout int `yaml:"bootstrap_timeout"`
	// SafeLogging scrubs sensitive info from Tor's own logs.
	SafeLogging bool `yaml:"safe_logging"`
	// MaxStreamsPerCircuit limits streams per circuit.
	MaxStreamsPerCircuit int `yaml:"max_streams_per_circuit"`
	// CloseCircuitOnStreamLimit closes a circuit once the stream limit is hit.
	CloseCircuitOnStreamLimit bool `yaml:"close_circuit_on_stream_limit"`
	// BandwidthRate is Tor's sustained bandwidth rate (e.g. "1 MB").
	BandwidthRate string `yaml:"bandwidth_rate"`
	// BandwidthBurst is Tor's burst bandwidth allowance (e.g. "2 MB").
	BandwidthBurst string `yaml:"bandwidth_burst"`
	// MaxMonthlyBandwidth caps monthly bandwidth, or "unlimited".
	MaxMonthlyBandwidth string `yaml:"max_monthly_bandwidth"`
	// NumIntroPoints is the number of hidden-service introduction points.
	NumIntroPoints int `yaml:"num_intro_points"`
	// VirtualPort is the .onion port clients connect to.
	VirtualPort int  `yaml:"virtual_port"`
	Expose      bool `yaml:"expose"`
}

func (c TorConfig) Normalized() (TorConfig, []string) {
	var warnings []string
	if c.StartupTimeout < 10 || c.StartupTimeout > 300 {
		c.StartupTimeout = 60
		warnings = append(warnings, "server.tor.startup_timeout outside 10-300; using 60")
	}
	return c, warnings
}

// I2PConfig controls the optional eepsite. Enabled defaults to the computed
// container value during config construction; an operator setting, env var,
// or CLI flag can override it.
type I2PConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Expose           bool   `yaml:"expose"`
	Binary           string `yaml:"binary"`
	SAMAddress       string `yaml:"sam_address"`
	VirtualPort      int    `yaml:"virtual_port"`
	InboundLength    int    `yaml:"inbound_length"`
	OutboundLength   int    `yaml:"outbound_length"`
	InboundQuantity  int    `yaml:"inbound_quantity"`
	OutboundQuantity int    `yaml:"outbound_quantity"`
	SignatureType    int    `yaml:"signature_type"`
	BootstrapTimeout int    `yaml:"bootstrap_timeout"`
}

// Normalized applies the PART 31.2 I2P bounds while retaining the operator's
// explicit enabled choice.
func (c I2PConfig) Normalized() (I2PConfig, []string) {
	var warnings []string
	if c.SAMAddress == "" {
		c.SAMAddress = "127.0.0.1:7656"
	}
	if c.VirtualPort < 1 || c.VirtualPort > 65535 {
		c.VirtualPort = 80
		warnings = append(warnings, "server.i2p.virtual_port outside 1-65535; using 80")
	}
	if c.InboundLength < 0 || c.InboundLength > 7 {
		c.InboundLength = 3
		warnings = append(warnings, "server.i2p.inbound_length outside 0-7; using 3")
	}
	if c.OutboundLength < 0 || c.OutboundLength > 7 {
		c.OutboundLength = 3
		warnings = append(warnings, "server.i2p.outbound_length outside 0-7; using 3")
	}
	if c.InboundQuantity < 1 || c.InboundQuantity > 16 {
		c.InboundQuantity = 5
		warnings = append(warnings, "server.i2p.inbound_quantity outside 1-16; using 5")
	}
	if c.OutboundQuantity < 1 || c.OutboundQuantity > 16 {
		c.OutboundQuantity = 5
		warnings = append(warnings, "server.i2p.outbound_quantity outside 1-16; using 5")
	}
	if c.SignatureType < 0 {
		c.SignatureType = 7
		warnings = append(warnings, "server.i2p.signature_type must be non-negative; using 7")
	}
	if c.BootstrapTimeout < 30 || c.BootstrapTimeout > 600 {
		c.BootstrapTimeout = 300
		warnings = append(warnings, "server.i2p.bootstrap_timeout outside 30-600; using 300")
	}
	return c, warnings
}

// UpdateConfig holds release-channel and auto-update settings
type UpdateConfig struct {
	// Branch selects the release channel: stable, beta, or devel.
	Branch string `yaml:"branch"`
	// AutoInstall auto-installs updates found by the update_check task.
	// Default OFF: the task only notifies; installing is always an
	// explicit operator decision
	AutoInstall bool `yaml:"auto_install"`
	// DeferDays is the defer window (0-365): a release is only eligible
	// once it is this many days old
	DeferDays     int    `yaml:"defer_days"`
	CLIMinVersion string `yaml:"cli_min_version"`
}

// Normalized applies the canonical PART 22 channel names. Historical aliases
// remain accepted on input and are stored as devel.
func (c UpdateConfig) Normalized() (UpdateConfig, []string) {
	value := strings.ToLower(strings.TrimSpace(c.Branch))
	switch value {
	case "", "stable":
		c.Branch = "stable"
	case "beta":
		c.Branch = "beta"
	case "devel", "daily", "rolling", "dev", "development":
		c.Branch = "devel"
		if value != "devel" {
			return c, []string{fmt.Sprintf("server.update.branch alias %q is normalized to devel", value)}
		}
	default:
		c.Branch = "stable"
		return c, []string{fmt.Sprintf("server.update.branch %q is invalid; using stable", value)}
	}
	return c, nil
}

// HealthzConfig holds health-check endpoint settings
type HealthzConfig struct {
	Root HealthzRootConfig `yaml:"root"`
}

// HealthzRootConfig controls whether health information is exposed at "/"
// in addition to "/server/healthz"
type HealthzRootConfig struct {
	Enabled bool `yaml:"enabled"`
}

// TrustedProxiesConfig holds the reverse-proxy trust allow-list
// Only peers in this list (plus always-trusted private ranges) may set
// X-Forwarded-*/X-Real-IP and related client-IP/FQDN/proto headers
type TrustedProxiesConfig struct {
	// Additional IPs/CIDRs/DNS names to trust beyond the always-trusted
	// private ranges (RFC 1918, RFC 4193, loopback, link-local)
	Additional []string `yaml:"additional"`
}

// BrandingConfig holds branding/SEO settings
type BrandingConfig struct {
	Title       string `yaml:"title"`
	Tagline     string `yaml:"tagline"`
	Description string `yaml:"description"`
	// Absolute file path or https:// URL; empty = use embedded default
	Favicon string `yaml:"favicon"`
	// Absolute file path or https:// URL; empty = use embedded default
	Logo string `yaml:"logo"`
	// Content is loaded from yaml.d/branding.yml and never written to server.yml.
	Content map[string]PageContentConfig `yaml:"content"`
}

// PageContentConfig stores operator-authored public-page content.
type PageContentConfig struct {
	Format string      `yaml:"format"`
	Body   interface{} `yaml:"body"`
}

// SSLConfig holds SSL/TLS settings
type SSLConfig struct {
	Enabled     bool              `yaml:"enabled"`
	Cert        string            `yaml:"cert"`
	Key         string            `yaml:"key"`
	CertPath    string            `yaml:"cert_path"`
	LetsEncrypt LetsEncryptConfig `yaml:"letsencrypt"`
}

// LetsEncryptConfig holds Let's Encrypt settings
type LetsEncryptConfig struct {
	Enabled        bool              `yaml:"enabled"`
	Email          string            `yaml:"email"`
	Challenge      string            `yaml:"challenge"`
	DNSProvider    string            `yaml:"dns_provider,omitempty"`
	DNSCredentials map[string]string `yaml:"dns_credentials,omitempty"`
}

// RateLimitConfig holds rate limiting settings
type RateLimitConfig struct {
	// GET/HEAD requests, per minute per IP
	Read RateLimitClassConfig `yaml:"read"`
	// POST/PUT/PATCH/DELETE requests, per minute per IP
	Write RateLimitClassConfig `yaml:"write"`
	// Health/status endpoints, per minute per IP
	Health RateLimitClassConfig `yaml:"health"`
	// Strict actions include token reveal, rotate and restore.
	Strict RateLimitClassConfig `yaml:"strict"`
	// AuthFailures locks out an IP after repeated invalid/revoked/expired tokens.
	AuthFailures RateLimitClassConfig `yaml:"auth_failures"`
	// Absolute ceiling across all endpoint types, per minute per IP
	GlobalBurst int `yaml:"global_burst"`
}

// Normalized applies the fixed PART 12 defaults to missing or invalid rate
// limit values. There is deliberately no enabled toggle: limiting is always on.
func (c RateLimitConfig) Normalized() (RateLimitConfig, []string) {
	warnings := make([]string, 0)
	defaults := []struct {
		name             string
		value            *RateLimitClassConfig
		requests, window int
	}{
		{"read", &c.Read, 120, 60},
		{"write", &c.Write, 10, 60},
		{"health", &c.Health, 120, 60},
		{"strict", &c.Strict, 10, 900},
		{"auth_failures", &c.AuthFailures, 5, 900},
	}
	for _, item := range defaults {
		if item.value.Requests <= 0 || item.value.Window <= 0 {
			warnings = append(warnings, fmt.Sprintf("server.rate_limit.%s must have positive requests and window; using defaults", item.name))
			item.value.Requests, item.value.Window = item.requests, item.window
		}
	}
	if c.GlobalBurst <= 0 {
		warnings = append(warnings, "server.rate_limit.global_burst must be positive; using 240")
		c.GlobalBurst = 240
	}
	return c, warnings
}

// RateLimitClassConfig holds the requests/window pair for one rate limit class
type RateLimitClassConfig struct {
	Requests int `yaml:"requests"`
	Window   int `yaml:"window"`
}

// DatabaseConfig holds database/storage settings. Driver accepts the
// friendly config aliases from AI.md PART 3 ("sqlite"/"sqlite2"/"sqlite3"
// all normalize to sqlite; "libsql"/"turso" both normalize to libsql).
// For the sqlite driver, URL is the on-disk database file path. For the
// libsql driver (remote-only, per AI.md), URL is the server URL
// (libsql://host?authToken=xxx or https://host) and Token is an optional
// separate auth token appended to the URL when the URL itself has none.
type DatabaseConfig struct {
	Driver string              `yaml:"driver"`
	URL    string              `yaml:"url"`
	Token  string              `yaml:"token"`
	Pool   DatabasePoolConfig  `yaml:"pool"`
	Cache  DatabaseCacheConfig `yaml:"cache"`
}

// DatabaseCacheConfig configures the optional local SQLite cache used with a
// remote libSQL database.
type DatabaseCacheConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Path         string `yaml:"path"`
	SyncInterval string `yaml:"sync_interval"`
	OfflineMode  bool   `yaml:"offline_mode"`
	MaxAge       string `yaml:"max_age"`
}

// Normalized supplies safe cache defaults and rejects non-positive durations.
func (c DatabaseCacheConfig) Normalized() (DatabaseCacheConfig, []string) {
	var warnings []string
	if strings.TrimSpace(c.Path) == "" {
		c.Path = filepath.Join(paths.DataDir(), "db", "cache.db")
	}
	if c.SyncInterval == "" {
		c.SyncInterval = "30s"
	} else if interval, err := time.ParseDuration(c.SyncInterval); err != nil || interval <= 0 {
		warnings = append(warnings, "server.database.cache.sync_interval is invalid; using 30s")
		c.SyncInterval = "30s"
	}
	if c.MaxAge == "" {
		c.MaxAge = "1h"
	} else if maxAge, err := time.ParseDuration(c.MaxAge); err != nil || maxAge <= 0 {
		warnings = append(warnings, "server.database.cache.max_age is invalid; using 1h")
		c.MaxAge = "1h"
	}
	return c, warnings
}

// DatabasePoolConfig sets connection pool bounds for the database backend.
type DatabasePoolConfig struct {
	MaxOpen     int    `yaml:"max_open"`
	MaxIdle     int    `yaml:"max_idle"`
	MaxLifetime string `yaml:"max_lifetime"`
	MaxIdleTime string `yaml:"max_idle_time"`
}

// NotificationsConfig holds server.notifications.* settings per AI.md
// PART 17 "Email & Notifications".
type NotificationsConfig struct {
	Email         EmailConfig         `yaml:"email"`
	DiskLow       DiskLowConfig       `yaml:"disk_low"`
	DatabaseError DatabaseErrorConfig `yaml:"database_error"`
	// WebUI carries the public-visitor toast/banner settings (PART 17).
	WebUI WebUINotificationsConfig `yaml:"webui"`
}

// EmailConfig holds server.notifications.email.* settings.
type EmailConfig struct {
	SMTP SMTPConfig `yaml:"smtp"`
	From FromConfig `yaml:"from"`
	// ReplyTo populates the {notification_reply_to} template variable and
	// the outbound Reply-To header. Empty means no Reply-To is set.
	ReplyTo string `yaml:"reply_to"`
	// Events carries the per-event email toggles (PART 17).
	Events EmailEventsConfig `yaml:"events"`
}

// SMTPConfig holds server.notifications.email.smtp.* settings. Host/Port
// start empty and are populated by email.AutoDetectSMTP() on the first
// successful startup probe, then persisted so restarts don't re-probe; an
// explicitly configured Host skips auto-detect and is connection-tested
// instead, every startup. All five fields are also overridable via the
// SMTP_* runtime env vars (config-rules.md "Env vars"), checked on every
// load - useful for containers.
type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	TLS      string `yaml:"tls"`
}

// FromConfig holds server.notifications.email.from.* settings. Name
// defaults to the app title, Email defaults to no-reply@{fqdn} - both
// resolved at startup when left empty.
type FromConfig struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

// CacheConfig holds server.cache.* settings per AI.md PART 12 "Cache
// Configuration". Cache is optional; Type "none" disables it, "memory"
// (default) is in-process, "valkey"/"redis" connect to an external cache
// used for sessions, rate-limit counters, and optional response caching.
// Timeout and TTL are Go duration strings (e.g. "5s", "1h"), parsed with
// time.ParseDuration by consumers; an invalid value falls back to the
// documented default rather than failing startup.
type CacheConfig struct {
	Type string `yaml:"type"`
	// URL, when set, takes precedence over Host/Port/Username/Password/DB.
	// Format: redis://[[username:]password@]host[:port][/database] or
	// valkey://... ; rediss://... for TLS.
	URL      string `yaml:"url"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	// TLS enables a TLS connection; TLSSkipVerify disables certificate
	// verification (never recommended, available for self-signed setups).
	TLS           bool   `yaml:"tls"`
	TLSSkipVerify bool   `yaml:"tls_skip_verify"`
	TLSCA         string `yaml:"tls_ca"`
	TLSCert       string `yaml:"tls_cert"`
	TLSKey        string `yaml:"tls_key"`
	PoolSize      int    `yaml:"pool_size"`
	MinIdle       int    `yaml:"min_idle"`
	// Timeout is the connection/command timeout as a duration string.
	Timeout string `yaml:"timeout"`
	// Prefix is prepended to every cache key to avoid collisions between
	// applications sharing the same Valkey/Redis instance.
	Prefix string `yaml:"prefix"`
	// TTL is the default entry lifetime as a duration string.
	TTL string `yaml:"ttl"`
	// Templates enables caching of parsed HTML templates. Caching is
	// config-driven, not mode-driven (AI.md PART 6): when the operator sets
	// this key the value is honored in every mode. Unset (nil) means "use
	// the mode default" — enabled in production, disabled in development
	// and debug so templates hot-reload.
	Templates *bool `yaml:"templates,omitempty"`
	// Static enables long-lived caching of static assets, with the same
	// config-driven semantics and mode defaults as Templates.
	Static *bool `yaml:"static,omitempty"`
}

// LogsConfig holds logging settings
type LogsConfig struct {
	Level    string            `yaml:"level"`
	Access   LogConfig         `yaml:"access"`
	Server   LogConfig         `yaml:"server"`
	Error    LogConfig         `yaml:"error"`
	App      LogConfig         `yaml:"app"`
	Auth     LogConfig         `yaml:"auth"`
	Audit    AuditLogConfig    `yaml:"audit"`
	Security SecurityLogConfig `yaml:"security"`
	Debug    DebugLogConfig    `yaml:"debug"`
}

// LogConfig holds settings for a log type
type LogConfig struct {
	Filename string `yaml:"filename"`
	Format   string `yaml:"format"`
	Custom   string `yaml:"custom"`
	Rotate   string `yaml:"rotate"`
	Keep     string `yaml:"keep"`
}

// AuditLogConfig holds audit log settings
type AuditLogConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Filename string `yaml:"filename"`
	Format   string `yaml:"format"`
	Rotate   string `yaml:"rotate"`
	Keep     string `yaml:"keep"`
	Compress bool   `yaml:"compress"`
}

// SecurityLogConfig holds security log settings
type SecurityLogConfig struct {
	Filename string `yaml:"filename"`
	Format   string `yaml:"format"`
	Custom   string `yaml:"custom"`
	Rotate   string `yaml:"rotate"`
	Keep     string `yaml:"keep"`
}

// DebugLogConfig holds debug log settings
type DebugLogConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Filename string `yaml:"filename"`
	Format   string `yaml:"format"`
	Custom   string `yaml:"custom"`
	Rotate   string `yaml:"rotate"`
	Keep     string `yaml:"keep"`
}

// WebConfig holds web-related settings
type WebConfig struct {
	LLMS              LLMSConfig              `yaml:"llms"`
	UI                UIConfig                `yaml:"ui"`
	Robots            RobotsConfig            `yaml:"robots"`
	Security          SecurityConfig          `yaml:"security"`
	CORS              CORSConfig              `yaml:"cors"`
	HSTS              HSTSConfig              `yaml:"hsts"`
	PermissionsPolicy PermissionsPolicyConfig `yaml:"-"`
	Reports           WebReportsConfig        `yaml:"reports"`
	CSP               CSPConfig               `yaml:"csp"`
	Headers           WebHeadersConfig        `yaml:"headers"`
}

// LLMSConfig controls the generated /.well-known/llms.txt document (AI.md
// PART 11/16). AI.md does not define the custom_sections item shape, so the
// parser retains arbitrary YAML values until that contract is specified.
type LLMSConfig struct {
	IncludeEndpoints bool  `yaml:"include_endpoints"`
	IncludeSchemas   bool  `yaml:"include_schemas"`
	CustomSections   []any `yaml:"custom_sections"`
}

// CORSConfig holds the CORS allowed-origin list (AI.md PART 16 → "CORS
// Allow-list Resolution Order"). AllowedOrigins is the explicit config
// source (step 1 of the resolution order); a single "" entry disables CORS
// entirely and stops resolution, and an empty/omitted list falls through to
// the DOMAIN-env / reverse-proxy-learned / "*"-default sources instead.
type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// UnmarshalYAML accepts either the current list form
// (`cors: {allowed_origins: [...]}`) or a legacy bare-string form
// (`cors: "*"` / `cors: "https://example.com"`) left over from before this
// field became a nested struct, auto-migrating the latter into a
// single-entry AllowedOrigins list. Decoding into (*plain)(c) — the same
// underlying struct, not a fresh zero value — preserves whatever
// defaultConfig() already populated for any key absent from the document.
func (c *CORSConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var legacy string
		if err := node.Decode(&legacy); err != nil {
			return err
		}
		c.AllowedOrigins = []string{legacy}
		return nil
	}

	type plain CORSConfig
	return node.Decode((*plain)(c))
}

// HSTSConfig holds Strict-Transport-Security header settings (AI.md PART 11
// "Security Headers"); only emitted when SSL is enabled.
type HSTSConfig struct {
	Enabled           bool `yaml:"enabled"`
	MaxAgeSeconds     int  `yaml:"max_age_seconds"`
	IncludeSubdomains bool `yaml:"include_subdomains"`
	Preload           bool `yaml:"preload"`
}

// PermissionsPolicyConfig holds the project-declared Permissions-Policy
// values. The field is internal and is never configurable in server.yml.
type PermissionsPolicyConfig struct {
	Accelerometer           string `yaml:"accelerometer"`
	Camera                  string `yaml:"camera"`
	DisplayCapture          string `yaml:"display-capture"`
	Geolocation             string `yaml:"geolocation"`
	Gyroscope               string `yaml:"gyroscope"`
	HID                     string `yaml:"hid"`
	IdleDetection           string `yaml:"idle-detection"`
	Magnetometer            string `yaml:"magnetometer"`
	Microphone              string `yaml:"microphone"`
	MIDI                    string `yaml:"midi"`
	ScreenWakeLock          string `yaml:"screen-wake-lock"`
	Serial                  string `yaml:"serial"`
	USB                     string `yaml:"usb"`
	XRSpatialTracking       string `yaml:"xr-spatial-tracking"`
	AttributionReporting    string `yaml:"attribution-reporting"`
	BrowsingTopics          string `yaml:"browsing-topics"`
	Autoplay                string `yaml:"autoplay"`
	EncryptedMedia          string `yaml:"encrypted-media"`
	Fullscreen              string `yaml:"fullscreen"`
	Payment                 string `yaml:"payment"`
	PictureInPicture        string `yaml:"picture-in-picture"`
	PublicKeyCredentialsGet string `yaml:"publickey-credentials-get"`
	StorageAccess           string `yaml:"storage-access"`
}

// Header returns the Permissions-Policy header value built from every
// non-empty feature, in the fixed spec order, so output is deterministic
// across restarts (a Go map would not be).
func (p PermissionsPolicyConfig) Header() string {
	pairs := []struct{ name, value string }{
		{"accelerometer", p.Accelerometer},
		{"camera", p.Camera},
		{"display-capture", p.DisplayCapture},
		{"geolocation", p.Geolocation},
		{"gyroscope", p.Gyroscope},
		{"hid", p.HID},
		{"idle-detection", p.IdleDetection},
		{"magnetometer", p.Magnetometer},
		{"microphone", p.Microphone},
		{"midi", p.MIDI},
		{"screen-wake-lock", p.ScreenWakeLock},
		{"serial", p.Serial},
		{"usb", p.USB},
		{"xr-spatial-tracking", p.XRSpatialTracking},
		{"attribution-reporting", p.AttributionReporting},
		{"browsing-topics", p.BrowsingTopics},
		{"autoplay", p.Autoplay},
		{"encrypted-media", p.EncryptedMedia},
		{"fullscreen", p.Fullscreen},
		{"payment", p.Payment},
		{"picture-in-picture", p.PictureInPicture},
		{"publickey-credentials-get", p.PublicKeyCredentialsGet},
		{"storage-access", p.StorageAccess},
	}
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		if pair.value == "" {
			continue
		}
		parts = append(parts, pair.name+"="+pair.value)
	}
	return strings.Join(parts, ", ")
}

// WebReportsConfig holds rate limits for the browser reporting endpoints
// (AI.md PART 11 "Reporting API"), separate from the general API rate limits.
type WebReportsConfig struct {
	RateLimitPerMinute  int `yaml:"rate_limit_per_minute"`
	RateLimitPerIPBurst int `yaml:"rate_limit_per_ip_burst"`
}

// CSPConfig holds Content-Security-Policy generation settings (AI.md PART 11
// "Content Security Policy"). Each `*Extra` field appends to the built-in
// default directive; each `*Override` field replaces it entirely.
type CSPConfig struct {
	Enabled            bool    `yaml:"enabled"`
	Mode               string  `yaml:"mode"`
	DefaultSrcOverride string  `yaml:"default_src_override"`
	ScriptSrcExtra     string  `yaml:"script_src_extra"`
	ScriptSrcOverride  string  `yaml:"script_src_override"`
	StyleSrcExtra      string  `yaml:"style_src_extra"`
	StyleSrcOverride   string  `yaml:"style_src_override"`
	ImgSrcExtra        string  `yaml:"img_src_extra"`
	ImgSrcOverride     string  `yaml:"img_src_override"`
	FontSrcExtra       string  `yaml:"font_src_extra"`
	FontSrcOverride    string  `yaml:"font_src_override"`
	ConnectSrcExtra    string  `yaml:"connect_src_extra"`
	ConnectSrcOverride string  `yaml:"connect_src_override"`
	FrameSrcExtra      string  `yaml:"frame_src_extra"`
	FrameSrcOverride   string  `yaml:"frame_src_override"`
	FormActionExtra    string  `yaml:"form_action_extra"`
	FormActionOverride string  `yaml:"form_action_override"`
	ReportsEnabled     bool    `yaml:"reports_enabled"`
	ReportsSampleRate  float64 `yaml:"reports_sample_rate"`
}

// WebHeadersConfig holds the remaining PART 11 response-header toggles not
// covered by HSTSConfig/CSPConfig/PermissionsPolicyConfig.
type WebHeadersConfig struct {
	COOP                    string              `yaml:"coop"`
	COEP                    string              `yaml:"coep"`
	CORP                    string              `yaml:"corp"`
	OriginAgentCluster      bool                `yaml:"origin_agent_cluster"`
	CrossDomainPolicies     string              `yaml:"cross_domain_policies"`
	DNSPrefetchControl      string              `yaml:"dns_prefetch_control"`
	HonorSecGPC             bool                `yaml:"honor_sec_gpc"`
	HonorDNT                bool                `yaml:"honor_dnt"`
	SecFetchValidation      bool                `yaml:"sec_fetch_validation"`
	ServerTimingInDebugOnly bool                `yaml:"server_timing_in_debug_only"`
	ClearSiteData           ClearSiteDataConfig `yaml:"clear_site_data"`
	NEL                     NELConfig           `yaml:"nel"`
	// ReferrerPolicy is config-driven (rather than the historical hardcoded
	// header value) so the IDEA.md → Header Tightening Auto-Map can tighten
	// it to "no-referrer" for COPPA/PCI-DSS/GLBA-flagged projects. Empty
	// string on an old server.yml (upgraded from before this field existed)
	// falls back to the same "strict-origin-when-cross-origin" default that
	// was previously hardcoded — see securityHeadersMiddleware.
	ReferrerPolicy string `yaml:"referrer_policy"`
}

// ClearSiteDataConfig controls when the Clear-Site-Data header is emitted
// (AI.md PART 11 "Security Headers" — token revocation / consent withdrawal).
type ClearSiteDataConfig struct {
	OnTokenRevocation   bool `yaml:"on_token_revocation"`
	OnConsentWithdrawal bool `yaml:"on_consent_withdrawal"`
	ExecutionContexts   bool `yaml:"execution_contexts"`
}

// NELConfig holds Network Error Logging header settings.
type NELConfig struct {
	Enabled           bool    `yaml:"enabled"`
	MaxAgeSeconds     int     `yaml:"max_age_seconds"`
	IncludeSubdomains bool    `yaml:"include_subdomains"`
	SampleRate        float64 `yaml:"sample_rate"`
}

// UIConfig holds UI settings
type UIConfig struct {
	Theme   string `yaml:"theme"`
	Logo    string `yaml:"logo"`
	Favicon string `yaml:"favicon"`
}

// RobotsConfig holds robots.txt settings
type RobotsConfig struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
}

// SecurityConfig holds security.txt settings
type SecurityConfig struct {
	ReportURL     string    `yaml:"report_url"`
	Contact       string    `yaml:"contact"`
	PublishPGPKey bool      `yaml:"publish_pgp_key"`
	Keyservers    []string  `yaml:"keyservers"`
	Expires       time.Time `yaml:"expires"`
	Policy        string    `yaml:"policy"`
}

// Global config with mutex for hot reload
var (
	currentConfig *Config
	configMu      sync.RWMutex
)

// generateRandomPort generates a random port in the 64xxx range
func generateRandomPort() string {
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand read failure: fall back to a fixed in-range port
		// rather than silently continuing with a zeroed/partial buffer.
		return "64000"
	}
	// Generate port between 64000-64999
	port := 64000 + (int(buf[0])<<8|int(buf[1]))%1000
	return strconv.Itoa(port)
}

// DefaultConfig returns a fully populated default configuration. Exported
// for tests and any other caller needing production defaults without going
// through Load (e.g. security-headers middleware tests).
func DefaultConfig() *Config {
	return defaultConfig()
}

// defaultConfig returns the default configuration
func defaultConfig() *Config {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}

	return &Config{
		Data: map[string]interface{}{
			"security_dir": filepath.Join(paths.DataDir(), "security"),
			"cve": map[string]interface{}{
				"source":        "https://services.nvd.nist.gov/rest/json/cves/2.0",
				"filter_by_cpe": false,
				"api_key":       "",
			},
			"trivy": map[string]interface{}{
				"enabled": false,
				"source":  "ghcr.io/aquasecurity/trivy-db:2",
			},
		},
		Server: ServerConfig{
			Port:       generateRandomPort(),
			FQDN:       hostname,
			Address:    "0.0.0.0",
			Mode:       "production",
			APIVersion: "v1",
			BaseURL:    "/",
			Limits:     DefaultServerLimitsConfig(),
			Branding: BrandingConfig{
				Title:       "CasTools",
				Tagline:     "Universal API Toolkit",
				Description: "Universal API toolkit for text, crypto, network, and system utilities",
			},
			SSL: SSLConfig{
				Enabled:  false,
				CertPath: "",
				LetsEncrypt: LetsEncryptConfig{
					Enabled:   false,
					Email:     "",
					Challenge: "http-01",
				},
			},
			Scheduler: defaultSchedulerConfig(),
			TrustedProxies: TrustedProxiesConfig{
				Additional: []string{},
			},
			RateLimit: RateLimitConfig{
				Read:         RateLimitClassConfig{Requests: 120, Window: 60},
				Write:        RateLimitClassConfig{Requests: 10, Window: 60},
				Health:       RateLimitClassConfig{Requests: 120, Window: 60},
				Strict:       RateLimitClassConfig{Requests: 10, Window: 900},
				AuthFailures: RateLimitClassConfig{Requests: 5, Window: 900},
				GlobalBurst:  240,
			},
			Database: DatabaseConfig{
				Driver: "sqlite",
				URL:    filepath.Join(paths.DataDir(), "db", "server.db"),
				Pool: DatabasePoolConfig{
					MaxOpen: 25, MaxIdle: 5, MaxLifetime: "5m", MaxIdleTime: "1m",
				},
				Cache: DatabaseCacheConfig{
					Enabled: false, Path: filepath.Join(paths.DataDir(), "db", "cache.db"),
					SyncInterval: "30s", OfflineMode: true, MaxAge: "1h",
				},
			},
			Cache: CacheConfig{
				Type:     "memory",
				Host:     "localhost",
				Port:     6379,
				PoolSize: 10,
				MinIdle:  2,
				Timeout:  "5s",
				Prefix:   "api:",
				TTL:      "1h",
			},
			Debug: DebugConfig{
				Pprof:                true,
				LogQueries:           true,
				LogCache:             true,
				LogBodies:            false,
				MaxBodyLogSize:       "10KB",
				BlockProfileRate:     1,
				MutexProfileFraction: 1,
				RuntimeEndpoints:     true,
			},
			Healthz: HealthzConfig{
				Root: HealthzRootConfig{
					Enabled: false,
				},
			},
			Update: UpdateConfig{
				Branch:      "stable",
				AutoInstall: false,
				DeferDays:   0,
			},
			Metrics: MetricsConfig{
				Enabled:        true,
				Root:           MetricsRootConfig{Enabled: true},
				Auth:           MetricsAuthConfig{AllowUnauthenticated: false},
				IncludeSystem:  true,
				IncludeRuntime: true,
				Loki:           MetricsLokiConfig{MaxEntries: 1000, MaxAge: "1h"},
				// Buckets match AI.md PART 20 "Configuration" exactly.
				DurationBuckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
				SizeBuckets:     []float64{100, 1000, 10000, 100000, 1000000, 10000000},
			},
			Backup: BackupConfig{
				Encryption: BackupEncryptionConfig{
					Enabled: false,
				},
				EncryptionPassword: "",
				Retention: BackupRetentionConfig{
					MaxBackups:   1,
					KeepWeekly:   0,
					KeepMonthly:  0,
					KeepYearly:   0,
					MaxTotalSize: "10%",
				},
			},
			Restore: DefaultRestoreConfig(),
			Compliance: ComplianceConfig{
				Enabled: false,
			},
			Security: defaultServerSecurityConfig(),
			GeoIP:    defaultGeoIPConfig(),
			Contact:  defaultContactConfig(hostname),
			Privacy:  defaultPrivacyConfig(),
			Tracking: TrackingConfig{},
			CSRF:     defaultCSRFConfig(),
			I18N: I18NConfig{
				DefaultLanguage:    i18n.DefaultLanguage,
				AvailableLanguages: i18n.SupportedLanguageCodes(),
			},
			Notifications: NotificationsConfig{
				Email: EmailConfig{
					SMTP: SMTPConfig{
						Port: 587,
						TLS:  "auto",
					},
					Events: DefaultEmailEventsConfig(),
				},
				WebUI:         DefaultWebUINotificationsConfig(),
				DiskLow:       DefaultDiskLowConfig(),
				DatabaseError: DefaultDatabaseErrorConfig(),
			},
			Tor: TorConfig{
				Binary:                    "",
				UseNetwork:                false,
				MaxCircuits:               32,
				StartupTimeout:            60,
				CircuitTimeout:            60,
				BootstrapTimeout:          180,
				SafeLogging:               true,
				MaxStreamsPerCircuit:      100,
				CloseCircuitOnStreamLimit: true,
				BandwidthRate:             "1 MB",
				BandwidthBurst:            "2 MB",
				MaxMonthlyBandwidth:       "100 GB",
				NumIntroPoints:            3,
				VirtualPort:               80,
			},
			I2P: I2PConfig{
				Enabled:          paths.IsRunningInContainer(),
				Expose:           false,
				SAMAddress:       "127.0.0.1:7656",
				VirtualPort:      80,
				InboundLength:    3,
				OutboundLength:   3,
				InboundQuantity:  5,
				OutboundQuantity: 5,
				SignatureType:    7,
				BootstrapTimeout: 300,
			},
			Logs: LogsConfig{
				Level: "warn",
				Access: LogConfig{
					Filename: "access.log",
					Format:   "apache",
					Rotate:   "monthly",
					Keep:     "none",
				},
				Server: LogConfig{
					Filename: "server.log",
					Format:   "text",
					Rotate:   "weekly,50MB",
					Keep:     "none",
				},
				Error: LogConfig{
					Filename: "error.log",
					Format:   "text",
					Rotate:   "weekly,50MB",
					Keep:     "none",
				},
				App: LogConfig{
					Filename: "app.log",
					Format:   "logfmt",
					Rotate:   "weekly,50MB",
					Keep:     "none",
				},
				Auth: LogConfig{
					Filename: "auth.log",
					Format:   "syslog",
					Rotate:   "weekly,50MB",
					Keep:     "none",
				},
				Audit: AuditLogConfig{
					Enabled:  true,
					Filename: "audit.log",
					Format:   "json",
					Rotate:   "daily",
					Keep:     "90",
					Compress: false,
				},
				Security: SecurityLogConfig{
					Filename: "security.log",
					Format:   "fail2ban",
					Rotate:   "weekly,50MB",
					Keep:     "none",
				},
				Debug: DebugLogConfig{
					Enabled:  false,
					Filename: "debug.log",
					Format:   "text",
					Rotate:   "weekly,50MB",
					Keep:     "none",
				},
			},
			Tokens: ServerTokenConfig{
				MaxLinked:     25,
				CookieMaxAge:  "1y",
				IdlePurgeDays: 0,
			},
		},
		Web: WebConfig{
			LLMS: LLMSConfig{IncludeEndpoints: true, IncludeSchemas: false, CustomSections: []any{}},
			UI: UIConfig{
				Theme:   "dark",
				Logo:    "",
				Favicon: "",
			},
			Robots: RobotsConfig{
				Allow: []string{"/", "/api"},
				Deny:  []string{"/admin"},
			},
			Security: SecurityConfig{
				ReportURL:     "https://github.com/apimgr/api/security/advisories/new",
				Contact:       "security@" + hostname,
				PublishPGPKey: true,
				Expires:       time.Now().AddDate(1, 0, 0),
			},
			CORS: CORSConfig{AllowedOrigins: []string{"*"}},
			HSTS: HSTSConfig{
				Enabled:           true,
				MaxAgeSeconds:     63072000,
				IncludeSubdomains: true,
				Preload:           true,
			},
			PermissionsPolicy: PermissionsPolicyConfig{
				Accelerometer:           "()",
				Camera:                  "()",
				DisplayCapture:          "()",
				Geolocation:             "()",
				Gyroscope:               "()",
				HID:                     "()",
				IdleDetection:           "()",
				Magnetometer:            "()",
				Microphone:              "()",
				MIDI:                    "()",
				ScreenWakeLock:          "()",
				Serial:                  "()",
				USB:                     "()",
				XRSpatialTracking:       "()",
				AttributionReporting:    "()",
				BrowsingTopics:          "()",
				Autoplay:                "(self)",
				EncryptedMedia:          "(self)",
				Fullscreen:              "(self)",
				Payment:                 "(self)",
				PictureInPicture:        "(self)",
				PublicKeyCredentialsGet: "(self)",
				StorageAccess:           "(self)",
			},
			Reports: WebReportsConfig{
				RateLimitPerMinute:  60,
				RateLimitPerIPBurst: 10,
			},
			CSP: CSPConfig{
				Enabled:           true,
				Mode:              "enforce",
				ReportsEnabled:    true,
				ReportsSampleRate: 1.0,
			},
			Headers: WebHeadersConfig{
				COOP:                    "unsafe-none",
				COEP:                    "unsafe-none",
				CORP:                    "cross-origin",
				OriginAgentCluster:      true,
				CrossDomainPolicies:     "none",
				DNSPrefetchControl:      "",
				HonorSecGPC:             true,
				HonorDNT:                false,
				SecFetchValidation:      true,
				ServerTimingInDebugOnly: true,
				ReferrerPolicy:          "strict-origin-when-cross-origin",
				ClearSiteData: ClearSiteDataConfig{
					OnTokenRevocation:   true,
					OnConsentWithdrawal: true,
					ExecutionContexts:   false,
				},
				NEL: NELConfig{
					Enabled:           true,
					MaxAgeSeconds:     2592000,
					IncludeSubdomains: true,
					SampleRate:        1.0,
				},
			},
		},
	}
}

// Load loads configuration from file or creates default
func Load() (*Config, error) {
	cfg := defaultConfig()
	_, serverFileErr := os.Stat(filepath.Join(paths.ConfigDir(), "server.yml"))
	firstRun := os.IsNotExist(serverFileErr)
	data, err := loadMergedConfig(cfg)
	if err != nil {
		return cfg, err
	}
	if err := unmarshalConfigResilient(data, cfg); err != nil {
		return cfg, fmt.Errorf("decode merged configuration: %w", err)
	}
	if err := cfg.Server.Security.Blocklists.Validate(); err != nil {
		return cfg, err
	}

	applyDatabaseEnvOverrides(cfg)
	applySMTPEnvOverrides(cfg)
	applyI2PEnvOverrides(cfg)
	var languageWarnings []string
	cfg.Server.I18N, languageWarnings = cfg.Server.I18N.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}
	cfg.Server.Tokens, languageWarnings = cfg.Server.Tokens.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}
	cfg.Server.Update, languageWarnings = cfg.Server.Update.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}
	cfg.Server.RateLimit, languageWarnings = cfg.Server.RateLimit.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}
	cfg.Server.Debug, languageWarnings = cfg.Server.Debug.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}
	cfg.Server.I2P, languageWarnings = cfg.Server.I2P.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}
	cfg.Server.Tor, languageWarnings = cfg.Server.Tor.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}
	cfg.Server.Restore, languageWarnings = cfg.Server.Restore.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}
	cfg.Server.Limits, languageWarnings = cfg.Server.Limits.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}
	cfg.Server.Database.Cache, languageWarnings = cfg.Server.Database.Cache.Normalized()
	for _, warning := range languageWarnings {
		log.Printf("Warning: %s", warning)
	}

	// First-run IDEA-driven security header defaults must be applied after the
	// merged config is loaded and before generated values are persisted.
	if firstRun {
		applyIdeaHeaderAutoMap(cfg)
	}

	// Secrets are generated only after all configuration sources have been
	// merged, then persisted to secrets.yml before the process serves traffic.
	if ensureFirstRunSecrets(cfg) {
		if err := Save(cfg); err != nil {
			return cfg, err
		}
	}

	// Store in global
	configMu.Lock()
	currentConfig = cfg
	configMu.Unlock()

	return cfg, nil
}

// applyI2PEnvOverrides resolves I2P_ENABLED over server.i2p.enabled.
func applyI2PEnvOverrides(cfg *Config) {
	if value, ok := os.LookupEnv("I2P_ENABLED"); ok {
		if enabled, err := ParseBool(value, cfg.Server.I2P.Enabled); err == nil {
			cfg.Server.I2P.Enabled = enabled
		} else {
			log.Printf("Warning: invalid I2P_ENABLED value; retaining configured I2P setting")
		}
	}
}

// applyDatabaseEnvOverrides applies DATABASE_DRIVER/DATABASE_URL/DATABASE_DIR
// runtime environment variables over the loaded config. These are checked
// on every load (not just first run), and take priority over the
// server.yml values when explicitly set.
func applyDatabaseEnvOverrides(cfg *Config) {
	if v := os.Getenv("DATABASE_DRIVER"); v != "" {
		cfg.Server.Database.Driver = v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.Server.Database.URL = v
	} else if v := os.Getenv("DATABASE_DIR"); v != "" {
		cfg.Server.Database.URL = filepath.Join(v, "server.db")
	}
}

// applySMTPEnvOverrides applies the SMTP_* runtime environment variables
// (AI.md PART 17 "Environment Variable Priority") over the loaded config.
// These are checked on every load (not just first run), and take priority
// over the server.yml values when explicitly set - useful for containers.
func applySMTPEnvOverrides(cfg *Config) {
	smtp := &cfg.Server.Notifications.Email.SMTP
	from := &cfg.Server.Notifications.Email.From

	if v := os.Getenv("SMTP_HOST"); v != "" {
		smtp.Host = v
	}
	if v := os.Getenv("SMTP_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			smtp.Port = port
		}
	}
	if v := os.Getenv("SMTP_USERNAME"); v != "" {
		smtp.Username = v
	}
	if v := os.Getenv("SMTP_PASSWORD"); v != "" {
		smtp.Password = v
	}
	if v := os.Getenv("SMTP_TLS"); v != "" {
		smtp.TLS = v
	}
	if v := os.Getenv("SMTP_FROM_NAME"); v != "" {
		from.Name = v
	}
	if v := os.Getenv("SMTP_FROM_EMAIL"); v != "" {
		from.Email = v
	}
}

// Save saves configuration to file
func Save(cfg *Config) error {
	return saveConfig(cfg)
}

// Get returns the current configuration (thread-safe)
func Get() *Config {
	configMu.RLock()
	cfg := currentConfig
	configMu.RUnlock()
	if cfg != nil {
		return cfg
	}
	cfg, _ = Load()
	return cfg
}

// Set updates the current configuration (thread-safe)
func Set(cfg *Config) {
	configMu.Lock()
	defer configMu.Unlock()
	currentConfig = cfg
}

// Reload reloads configuration from file
func Reload() error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	Set(cfg)
	return nil
}

// GetConfigPath returns the path to the config file
func GetConfigPath() string {
	return filepath.Join(paths.ConfigDir(), "server.yml")
}

// Legacy compatibility - expose WebUI and WebRobots from Web config
func (c *Config) GetWebUI() UIConfig {
	return c.Web.UI
}

func (c *Config) GetWebRobots() RobotsConfig {
	return c.Web.Robots
}

func (c *Config) GetWebSecurity() SecurityConfig {
	return c.Web.Security
}

// ConfigWatcher watches for config file changes and triggers reload
type ConfigWatcher struct {
	path         string
	callback     func(*Config)
	stopCh       chan struct{}
	lastSnapshot string
	mu           sync.Mutex
}

// NewConfigWatcher creates a new config file watcher
func NewConfigWatcher(callback func(*Config)) *ConfigWatcher {
	return &ConfigWatcher{
		path:     GetConfigPath(),
		callback: callback,
		stopCh:   make(chan struct{}),
	}
}

// Start begins watching the config file for changes
func (w *ConfigWatcher) Start() {
	go w.watch()
}

// Stop stops watching the config file
func (w *ConfigWatcher) Stop() {
	close(w.stopCh)
}

// watch polls the config file for changes
func (w *ConfigWatcher) watch() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	w.lastSnapshot = configTreeSnapshot()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.checkForChanges()
		}
	}
}

// checkForChanges checks if the config file has been modified
func (w *ConfigWatcher) checkForChanges() {
	snapshot := configTreeSnapshot()
	w.mu.Lock()
	defer w.mu.Unlock()
	if snapshot != w.lastSnapshot {
		w.lastSnapshot = snapshot
		// Reload config
		cfg, err := Load()
		if err != nil {
			return
		}

		// Call callback with new config
		if w.callback != nil {
			w.callback(cfg)
		}
	}
}

// configTreeSnapshot covers the complete watched configuration tree, including
// additions, removals and edits under yaml.d/ and assets/.
func configTreeSnapshot() string {
	var entries []string
	add := func(path string, info os.FileInfo) {
		entries = append(entries, fmt.Sprintf("%s:%d:%d", path, info.Size(), info.ModTime().UnixNano()))
	}
	for _, path := range []string{
		filepath.Join(paths.ConfigDir(), "server.yml"),
		filepath.Join(paths.ConfigDir(), "secrets.yml"),
	} {
		if info, err := os.Stat(path); err == nil {
			add(path, info)
		} else {
			entries = append(entries, path+":missing")
		}
	}
	for _, dir := range []string{
		filepath.Join(paths.ConfigDir(), "yaml.d"),
		filepath.Join(paths.ConfigDir(), "assets"),
	} {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				entries = append(entries, path+":error")
				return nil
			}
			add(path, info)
			return nil
		})
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

// OnChange registers a callback for config changes
// Returns a function to start watching
func OnChange(callback func(*Config)) func() {
	watcher := NewConfigWatcher(callback)
	return func() {
		watcher.Start()
	}
}
