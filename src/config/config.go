package config

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/apimgr/api/src/paths"
	"gopkg.in/yaml.v3"
)

// Config represents the complete server configuration
type Config struct {
	Server ServerConfig `yaml:"server"`
	Web    WebConfig    `yaml:"web"`
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
	Healthz        HealthzConfig        `yaml:"healthz"`
	Logs           LogsConfig           `yaml:"logs"`
	Users          UsersConfig          `yaml:"users"`
	Update         UpdateConfig         `yaml:"update"`
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

// DatabaseConfig holds database/storage settings
type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	URL    string `yaml:"url"`
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
	UI       UIConfig       `yaml:"ui"`
	Robots   RobotsConfig   `yaml:"robots"`
	Security SecurityConfig `yaml:"security"`
	CORS     string         `yaml:"cors"`
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
			CORS: "*",
		},
	}
}

// Load loads configuration from file or creates default
func Load() (*Config, error) {
	cfg := defaultConfig()

	configFile := filepath.Join(paths.ConfigDir(), "server.yml")

	// Check if config file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
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
