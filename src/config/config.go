package config

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/paths"
	"gopkg.in/yaml.v3"
)

// Config represents the complete server configuration
type Config struct {
	Server ServerConfig `yaml:"server"`
	Web    WebConfig    `yaml:"web"`
	Output OutputConfig `yaml:"output"`
}

// OutputConfig holds CLI color/emoji output settings per AI.md PART 8
// "NO_COLOR Support". Color and Emoji are pointers so an absent key is
// distinguishable from an explicit false, matching the CLI flag > config
// file > NO_COLOR env var > auto-detect priority order.
type OutputConfig struct {
	Color *bool `yaml:"color,omitempty"`
	Emoji *bool `yaml:"emoji,omitempty"`
}

// ServerConfig holds server-related settings
type ServerConfig struct {
	Port           string               `yaml:"port"`
	FQDN           string               `yaml:"fqdn"`
	Address        string               `yaml:"address"`
	Mode           string               `yaml:"mode"`
	APIVersion     string               `yaml:"api_version"`
	BaseURL        string               `yaml:"baseurl"`
	Branding       BrandingConfig       `yaml:"branding"`
	SSL            SSLConfig            `yaml:"ssl"`
	Schedule       ScheduleConfig       `yaml:"schedule"`
	TrustedProxies TrustedProxiesConfig `yaml:"trusted_proxies"`
	RateLimit      RateLimitConfig      `yaml:"rate_limit"`
	Database       DatabaseConfig       `yaml:"database"`
	Cache          CacheConfig          `yaml:"cache"`
	Healthz        HealthzConfig        `yaml:"healthz"`
	Logs           LogsConfig           `yaml:"logs"`
	Users          UsersConfig          `yaml:"users"`
	Update         UpdateConfig         `yaml:"update"`
	Tor            TorConfig            `yaml:"tor"`
	Metrics        MetricsConfig        `yaml:"metrics"`
}

// MetricsConfig holds Prometheus metrics endpoint settings, per AI.md
// PART 20. The endpoint is internal-only (firewall/proxy/NetworkPolicy
// restricted per PART 20 Access Control) - Token is an optional additional
// layer, never a substitute for network-level restriction.
type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
	// Endpoint is the path metrics are served on.
	Endpoint string `yaml:"endpoint"`
	// Token, when non-empty, requires "Authorization: Bearer <token>" on
	// every request to Endpoint. Empty means no token check (firewall-only).
	Token string `yaml:"token"`
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
	VirtualPort int `yaml:"virtual_port"`
}

// UpdateConfig holds release-channel and auto-update settings
type UpdateConfig struct {
	// Branch selects the release channel: stable, beta, or daily
	Branch string `yaml:"branch"`
	// AutoInstall auto-installs updates found by the update_check task.
	// Default OFF: the task only notifies; installing is always an
	// explicit operator decision
	AutoInstall bool `yaml:"auto_install"`
	// DeferDays is the defer window (0-365): a release is only eligible
	// once it is this many days old
	DeferDays int `yaml:"defer_days"`
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
}

// SSLConfig holds SSL/TLS settings
type SSLConfig struct {
	Enabled     bool              `yaml:"enabled"`
	CertPath    string            `yaml:"cert_path"`
	LetsEncrypt LetsEncryptConfig `yaml:"letsencrypt"`
}

// LetsEncryptConfig holds Let's Encrypt settings
type LetsEncryptConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Email     string `yaml:"email"`
	Challenge string `yaml:"challenge"`
}

// ScheduleConfig holds scheduler settings
type ScheduleConfig struct {
	Enabled bool `yaml:"enabled"`
}

// RateLimitConfig holds rate limiting settings
type RateLimitConfig struct {
	Enabled bool `yaml:"enabled"`
	// GET/HEAD requests, per minute per IP
	Read RateLimitClassConfig `yaml:"read"`
	// POST/PUT/PATCH/DELETE requests, per minute per IP
	Write RateLimitClassConfig `yaml:"write"`
	// Health/status endpoints, per minute per IP
	Health RateLimitClassConfig `yaml:"health"`
	// Absolute ceiling across all endpoint types, per minute per IP
	GlobalBurst int `yaml:"global_burst"`
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
	Driver string `yaml:"driver"`
	URL    string `yaml:"url"`
	Token  string `yaml:"token"`
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
	TLS           bool `yaml:"tls"`
	TLSSkipVerify bool `yaml:"tls_skip_verify"`
	PoolSize      int  `yaml:"pool_size"`
	MinIdle       int  `yaml:"min_idle"`
	// Timeout is the connection/command timeout as a duration string.
	Timeout string `yaml:"timeout"`
	// Prefix is prepended to every cache key to avoid collisions between
	// applications sharing the same Valkey/Redis instance.
	Prefix string `yaml:"prefix"`
	// TTL is the default entry lifetime as a duration string.
	TTL string `yaml:"ttl"`
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

// UsersConfig holds user management settings
type UsersConfig struct {
	Enabled      bool               `yaml:"enabled"`
	Registration RegistrationConfig `yaml:"registration"`
	Tokens       TokensConfig       `yaml:"tokens"`
	Profile      ProfileConfig      `yaml:"profile"`
	Auth         AuthConfig         `yaml:"auth"`
	Limits       UserLimitsConfig   `yaml:"limits"`
}

// RegistrationConfig holds user registration settings
type RegistrationConfig struct {
	Enabled                  bool     `yaml:"enabled"`
	RequireEmailVerification bool     `yaml:"require_email_verification"`
	RequireApproval          bool     `yaml:"require_approval"`
	AllowedDomains           []string `yaml:"allowed_domains"`
	BlockedDomains           []string `yaml:"blocked_domains"`
}

// TokensConfig holds API token settings
type TokensConfig struct {
	Enabled        bool `yaml:"enabled"`
	MaxPerUser     int  `yaml:"max_per_user"`
	ExpirationDays int  `yaml:"expiration_days"`
}

// ProfileConfig holds user profile settings
type ProfileConfig struct {
	AllowAvatar      bool `yaml:"allow_avatar"`
	AllowDisplayName bool `yaml:"allow_display_name"`
	AllowBio         bool `yaml:"allow_bio"`
}

// AuthConfig holds authentication settings
type AuthConfig struct {
	SessionDuration          string `yaml:"session_duration"`
	Require2FA               bool   `yaml:"require_2fa"`
	Allow2FA                 bool   `yaml:"allow_2fa"`
	PasswordMinLength        int    `yaml:"password_min_length"`
	PasswordRequireUppercase bool   `yaml:"password_require_uppercase"`
	PasswordRequireNumber    bool   `yaml:"password_require_number"`
	PasswordRequireSpecial   bool   `yaml:"password_require_special"`
}

// UserLimitsConfig holds per-user rate limits
type UserLimitsConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute"`
	RequestsPerDay    int `yaml:"requests_per_day"`
}

// WebConfig holds web-related settings
type WebConfig struct {
	UI                UIConfig                `yaml:"ui"`
	Robots            RobotsConfig            `yaml:"robots"`
	Security          SecurityConfig          `yaml:"security"`
	CORS              CORSConfig              `yaml:"cors"`
	HSTS              HSTSConfig              `yaml:"hsts"`
	PermissionsPolicy PermissionsPolicyConfig `yaml:"permissions_policy"`
	Reports           WebReportsConfig        `yaml:"reports"`
	CSP               CSPConfig               `yaml:"csp"`
	Headers           WebHeadersConfig        `yaml:"headers"`
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

// PermissionsPolicyConfig holds the per-feature Permissions-Policy allowlist
// (AI.md PART 11 "Permissions-Policy Configuration"); each value is the raw
// allowlist token (e.g. "()", "(self)") joined into the header.
type PermissionsPolicyConfig struct {
	Accelerometer           string `yaml:"accelerometer"`
	AmbientLightSensor      string `yaml:"ambient-light-sensor"`
	Battery                 string `yaml:"battery"`
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
	InterestCohort          string `yaml:"interest-cohort"`
	Autoplay                string `yaml:"autoplay"`
	EncryptedMedia          string `yaml:"encrypted-media"`
	Fullscreen              string `yaml:"fullscreen"`
	Payment                 string `yaml:"payment"`
	PictureInPicture        string `yaml:"picture-in-picture"`
	PublicKeyCredentialsGet string `yaml:"publickey-credentials-get"`
	StorageAccess           string `yaml:"storage-access"`
	WebShare                string `yaml:"web-share"`
}

// Header returns the Permissions-Policy header value built from every
// non-empty feature, in the fixed spec order, so output is deterministic
// across restarts (a Go map would not be).
func (p PermissionsPolicyConfig) Header() string {
	pairs := []struct{ name, value string }{
		{"accelerometer", p.Accelerometer},
		{"ambient-light-sensor", p.AmbientLightSensor},
		{"battery", p.Battery},
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
		{"interest-cohort", p.InterestCohort},
		{"autoplay", p.Autoplay},
		{"encrypted-media", p.EncryptedMedia},
		{"fullscreen", p.Fullscreen},
		{"payment", p.Payment},
		{"picture-in-picture", p.PictureInPicture},
		{"publickey-credentials-get", p.PublicKeyCredentialsGet},
		{"storage-access", p.StorageAccess},
		{"web-share", p.WebShare},
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
	Contact string    `yaml:"contact"`
	Expires time.Time `yaml:"expires"`
}

// Global config with mutex for hot reload
var (
	currentConfig *Config
	configMu      sync.RWMutex
)

// generateRandomPort generates a random port in the 64xxx range
func generateRandomPort() string {
	bytes := make([]byte, 2)
	rand.Read(bytes)
	// Generate port between 64000-64999
	port := 64000 + (int(bytes[0])<<8|int(bytes[1]))%1000
	return string(rune('0'+port/10000)) + string(rune('0'+(port/1000)%10)) + string(rune('0'+(port/100)%10)) + string(rune('0'+(port/10)%10)) + string(rune('0'+port%10))
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
		Server: ServerConfig{
			Port:       generateRandomPort(),
			FQDN:       hostname,
			Address:    "0.0.0.0",
			Mode:       "production",
			APIVersion: "v1",
			BaseURL:    "/",
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
			Schedule: ScheduleConfig{
				Enabled: true,
			},
			TrustedProxies: TrustedProxiesConfig{
				Additional: []string{},
			},
			RateLimit: RateLimitConfig{
				Enabled:     true,
				Read:        RateLimitClassConfig{Requests: 120, Window: 60},
				Write:       RateLimitClassConfig{Requests: 10, Window: 60},
				Health:      RateLimitClassConfig{Requests: 120, Window: 60},
				GlobalBurst: 240,
			},
			Database: DatabaseConfig{
				Driver: "sqlite",
				URL:    filepath.Join(paths.DataDir(), "db", "server.db"),
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
				Enabled:  true,
				Endpoint: "/metrics",
				Token:    "",
			},
			Tor: TorConfig{
				Binary:                    "",
				UseNetwork:                false,
				MaxCircuits:               32,
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
			Users: UsersConfig{
				Enabled: false,
				Registration: RegistrationConfig{
					Enabled:                  false,
					RequireEmailVerification: true,
					RequireApproval:          false,
					AllowedDomains:           []string{},
					BlockedDomains:           []string{},
				},
				Tokens: TokensConfig{
					Enabled:        true,
					MaxPerUser:     5,
					ExpirationDays: 0,
				},
				Profile: ProfileConfig{
					AllowAvatar:      true,
					AllowDisplayName: true,
					AllowBio:         true,
				},
				Auth: AuthConfig{
					SessionDuration:          "30d",
					Require2FA:               false,
					Allow2FA:                 true,
					PasswordMinLength:        8,
					PasswordRequireUppercase: false,
					PasswordRequireNumber:    false,
					PasswordRequireSpecial:   false,
				},
				Limits: UserLimitsConfig{
					RequestsPerMinute: 0,
					RequestsPerDay:    0,
				},
			},
		},
		Web: WebConfig{
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
				Contact: "security@" + hostname,
				Expires: time.Now().AddDate(1, 0, 0),
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
				AmbientLightSensor:      "()",
				Battery:                 "()",
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
				InterestCohort:          "()",
				Autoplay:                "(self)",
				EncryptedMedia:          "(self)",
				Fullscreen:              "(self)",
				Payment:                 "(self)",
				PictureInPicture:        "(self)",
				PublicKeyCredentialsGet: "(self)",
				StorageAccess:           "(self)",
				WebShare:                "(self)",
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

	configFile := filepath.Join(paths.ConfigDir(), "server.yml")

	// Check if config file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		// First-run setup: pre-fill web.headers.* from IDEA.md's declared
		// audience/compliance/data-class per AI.md "IDEA.md → Header
		// Tightening Auto-Map". Must run before Save() so the tightened
		// values (not the loose defaultConfig() values) are what gets
		// persisted; changes are recorded for the setup audit log via
		// LastAutoTightenChanges() once server.InitLogger() is up.
		applyIdeaHeaderAutoMap(cfg)

		// Create default config file
		if err := Save(cfg); err != nil {
			return cfg, err
		}
		applyDatabaseEnvOverrides(cfg)
		return cfg, nil
	}

	// Read config file
	data, err := os.ReadFile(configFile)
	if err != nil {
		return cfg, err
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}

	applyDatabaseEnvOverrides(cfg)

	// Store in global
	configMu.Lock()
	currentConfig = cfg
	configMu.Unlock()

	return cfg, nil
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

// Save saves configuration to file
func Save(cfg *Config) error {
	configDir := paths.ConfigDir()

	// Create config directory if needed
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configFile := filepath.Join(configDir, "server.yml")

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	// Add header comment
	content := "# CasTools Configuration\n# https://api.apimgr.us\n\n" + string(data)

	return os.WriteFile(configFile, []byte(content), 0644)
}

// Get returns the current configuration (thread-safe)
func Get() *Config {
	configMu.RLock()
	defer configMu.RUnlock()
	if currentConfig == nil {
		cfg, _ := Load()
		return cfg
	}
	return currentConfig
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
	path      string
	callback  func(*Config)
	stopCh    chan struct{}
	lastMtime time.Time
	mu        sync.Mutex
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

	// Get initial mtime
	if info, err := os.Stat(w.path); err == nil {
		w.mu.Lock()
		w.lastMtime = info.ModTime()
		w.mu.Unlock()
	}

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
	info, err := os.Stat(w.path)
	if err != nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if info.ModTime().After(w.lastMtime) {
		w.lastMtime = info.ModTime()

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

// OnChange registers a callback for config changes
// Returns a function to start watching
func OnChange(callback func(*Config)) func() {
	watcher := NewConfigWatcher(callback)
	return func() {
		watcher.Start()
	}
}
