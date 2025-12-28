package config

import (
	"crypto/rand"
	"encoding/hex"
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
	Port     string         `yaml:"port"`
	FQDN     string         `yaml:"fqdn"`
	Address  string         `yaml:"address"`
	Mode     string         `yaml:"mode"`
	Branding BrandingConfig `yaml:"branding"`
	Admin    AdminConfig    `yaml:"admin"`
	SSL      SSLConfig      `yaml:"ssl"`
	Schedule ScheduleConfig `yaml:"schedule"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	Database DatabaseConfig `yaml:"database"`
	Logs     LogsConfig     `yaml:"logs"`
	Users    UsersConfig    `yaml:"users"`
}

// BrandingConfig holds branding/SEO settings
type BrandingConfig struct {
	Title   string `yaml:"title"`
	Tagline string `yaml:"tagline"`
}

// AdminConfig holds admin authentication settings
type AdminConfig struct {
	Email    string `yaml:"email"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Token    string `yaml:"token"`
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
	Enabled  bool `yaml:"enabled"`
	Requests int  `yaml:"requests"`
	Window   int  `yaml:"window"`
}

// DatabaseConfig holds database/storage settings
type DatabaseConfig struct {
	Driver string `yaml:"driver"`
}

// LogsConfig holds logging settings
type LogsConfig struct {
	Level    string            `yaml:"level"`
	Access   LogConfig         `yaml:"access"`
	Server   LogConfig         `yaml:"server"`
	Error    LogConfig         `yaml:"error"`
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
type SecurityLogConfig struct{
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
	Enabled      bool                `yaml:"enabled"`
	Registration RegistrationConfig  `yaml:"registration"`
	Roles        RolesConfig         `yaml:"roles"`
	Tokens       TokensConfig        `yaml:"tokens"`
	Profile      ProfileConfig       `yaml:"profile"`
	Auth         AuthConfig          `yaml:"auth"`
	Limits       UserLimitsConfig    `yaml:"limits"`
}

// RegistrationConfig holds user registration settings
type RegistrationConfig struct {
	Enabled                  bool     `yaml:"enabled"`
	RequireEmailVerification bool     `yaml:"require_email_verification"`
	RequireApproval          bool     `yaml:"require_approval"`
	AllowedDomains           []string `yaml:"allowed_domains"`
	BlockedDomains           []string `yaml:"blocked_domains"`
}

// RolesConfig holds role settings
type RolesConfig struct {
	Available []string `yaml:"available"`
	Default   string   `yaml:"default"`
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

// generateRandomString generates a random hex string
func generateRandomString(length int) string {
	bytes := make([]byte, length/2)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

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
			Port:    "64365",
			FQDN:    hostname,
			Address: "0.0.0.0",
			Mode:    "production",
			Branding: BrandingConfig{
				Title:   "CasTools",
				Tagline: "Universal API Toolkit",
			},
			Admin: AdminConfig{
				Email:    "admin@" + hostname,
				Username: "administrator",
				Password: generateRandomString(32),
				Token:    generateRandomString(64),
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
			RateLimit: RateLimitConfig{
				Enabled:  true,
				Requests: 120,
				Window:   60,
			},
			Database: DatabaseConfig{
				Driver: "file",
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
				Roles: RolesConfig{
					Available: []string{"admin", "user"},
					Default:   "user",
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

	// Store in global
	configMu.Lock()
	currentConfig = cfg
	configMu.Unlock()

	return cfg, nil
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

// ParseBool parses various boolean representations
// Accepts: 1, yes, true, enable, enabled, on (and their negatives)

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
