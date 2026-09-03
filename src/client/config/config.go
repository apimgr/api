// Package config loads and saves the api-cli client configuration
// (cli.yml), per AI.md PART 32.
package config

import (
	"fmt"
	"os"
	"runtime"

	"github.com/apimgr/api/src/client/paths"
	svcconfig "github.com/apimgr/api/src/config"
	"gopkg.in/yaml.v3"
)

// ServerConfig holds server connection settings.
type ServerConfig struct {
	Primary   string `yaml:"primary"`
	VerifySSL string `yaml:"verify_ssl"`
	// APIVersion is the route prefix segment and must match the server.
	APIVersion string `yaml:"api_version"`
	// Timeout is a Go duration string bounding each request.
	Timeout string `yaml:"timeout"`
	// Retry is the number of attempts made after a failed request.
	Retry int `yaml:"retry"`
	// RetryDelay is a Go duration string spacing successive retries.
	RetryDelay string `yaml:"retry_delay"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	Token string `yaml:"token"`
	// TokenFile reads the token from disk instead of storing it inline.
	TokenFile string `yaml:"token_file"`
}

// OutputConfig holds output rendering preferences.
type OutputConfig struct {
	// Format is one of table, json, yaml, plain, csv.
	Format string `yaml:"format"`
	// Color is auto, yes, or no, parsed with the shared truthy table.
	Color string `yaml:"color"`
	// Pager is auto, always, or never.
	Pager   string `yaml:"pager"`
	Quiet   bool   `yaml:"quiet"`
	Verbose bool   `yaml:"verbose"`
}

// LoggingConfig holds client log file settings.
type LoggingConfig struct {
	// Level is debug, info, warn, or error.
	Level string `yaml:"level"`
	// File is the log path; empty resolves to {log_dir}/cli.log.
	File     string `yaml:"file"`
	MaxSize  string `yaml:"max_size"`
	MaxFiles int    `yaml:"max_files"`
}

// CacheConfig holds client-side response cache settings.
type CacheConfig struct {
	Enabled bool `yaml:"enabled"`
	// TTL is a Go duration string bounding cached response lifetime.
	TTL     string `yaml:"ttl"`
	MaxSize string `yaml:"max_size"`
}

// UpdateConfig holds CLI self-update settings.
type UpdateConfig struct {
	Auto          string `yaml:"auto"`
	CheckInterval string `yaml:"check_interval"`
	Channel       string `yaml:"channel"`
}

// DisplayConfig holds UI mode override settings.
type DisplayConfig struct {
	Mode string `yaml:"mode"`
}

// TUIConfig holds TUI theming settings.
type TUIConfig struct {
	// Enabled false restricts the client to CLI mode even on a TTY.
	Enabled bool   `yaml:"enabled"`
	Theme   string `yaml:"theme"`
	Mouse   bool   `yaml:"mouse"`
	// Unicode false forces ASCII-only box drawing.
	Unicode bool `yaml:"unicode"`
}

// Defaults holds default values for flags.
type Defaults struct {
	Lang   string `yaml:"lang"`
	Output string `yaml:"output"`
	Limit  int    `yaml:"limit"`
}

// CLIConfig is the on-disk shape of cli.yml.
type CLIConfig struct {
	Server  ServerConfig  `yaml:"server"`
	Auth    AuthConfig    `yaml:"auth"`
	Output  OutputConfig  `yaml:"output"`
	Update  UpdateConfig  `yaml:"update"`
	Display DisplayConfig `yaml:"display"`
	TUI     TUIConfig     `yaml:"tui"`
	Logging LoggingConfig `yaml:"logging"`
	Cache   CacheConfig   `yaml:"cache"`
	// Debug mirrors the --debug flag and is a top-level key per the spec.
	Debug    bool     `yaml:"debug"`
	Defaults Defaults `yaml:"defaults"`
}

// Default returns a CLIConfig populated with the spec's documented
// defaults.
func Default() *CLIConfig {
	return &CLIConfig{
		Server: ServerConfig{
			APIVersion: "v1",
			Timeout:    "30s",
			Retry:      3,
			RetryDelay: "1s",
		},
		Output: OutputConfig{
			Format: "table",
			Color:  "auto",
			Pager:  "auto",
		},
		Update: UpdateConfig{
			Auto:          "no",
			CheckInterval: "per_invocation",
			Channel:       "stable",
		},
		Display: DisplayConfig{Mode: "auto"},
		TUI: TUIConfig{
			Enabled: true,
			Theme:   "dark",
			Mouse:   true,
			Unicode: true,
		},
		Logging: LoggingConfig{
			Level:    "warn",
			MaxSize:  "10MB",
			MaxFiles: 5,
		},
		Cache: CacheConfig{
			Enabled: true,
			TTL:     "5m",
			MaxSize: "100MB",
		},
		Defaults: Defaults{
			Lang:   "auto",
			Output: "table",
			Limit:  20,
		},
	}
}

// Load reads the named config profile (empty/"cli" = default cli.yml).
// A missing file is not an error; Default() is returned instead.
func Load(profile string) (*CLIConfig, error) {
	path := paths.NamedConfigFile(profile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config profile to disk with user-only permissions
// (0600), creating parent directories (0700) first.
func Save(profile string, cfg *CLIConfig) error {
	if err := paths.EnsureDirs(); err != nil {
		return err
	}

	path := paths.NamedConfigFile(profile)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("chmod config %s: %w", path, err)
		}
	}
	return nil
}

// SaveIfEmptyOrInvalid returns the value to use for this invocation and
// whether it should be persisted to cli.yml, per the PART 32
// flag-to-config save rules: a flag value only overwrites the saved
// config when the current value is empty or fails validate().
func SaveIfEmptyOrInvalid(current, flagValue string, validate func(string) bool) (use string, persist bool) {
	if flagValue == "" {
		return current, false
	}
	if validate != nil && !validate(flagValue) {
		return current, false
	}
	if current == "" {
		return flagValue, true
	}
	if validate != nil && !validate(current) {
		return flagValue, true
	}
	return flagValue, false
}

// IsTruthy re-exports the server's shared boolean parser so CLI flags and
// cli.yml booleans use the exact same truthy/falsey rules as the server.
func IsTruthy(s string) bool {
	return svcconfig.IsTruthy(s)
}
