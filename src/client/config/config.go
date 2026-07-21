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
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	Token string `yaml:"token"`
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
	Theme string `yaml:"theme"`
}

// Defaults holds default values for flags.
type Defaults struct {
	Lang   string `yaml:"lang"`
	Output string `yaml:"output"`
	Limit  int    `yaml:"limit"`
}

// CLIConfig is the on-disk shape of cli.yml.
type CLIConfig struct {
	Server   ServerConfig  `yaml:"server"`
	Auth     AuthConfig    `yaml:"auth"`
	Update   UpdateConfig  `yaml:"update"`
	Display  DisplayConfig `yaml:"display"`
	TUI      TUIConfig     `yaml:"tui"`
	Defaults Defaults      `yaml:"defaults"`
}

// Default returns a CLIConfig populated with the spec's documented
// defaults.
func Default() *CLIConfig {
	return &CLIConfig{
		Update: UpdateConfig{
			Auto:          "no",
			CheckInterval: "per_invocation",
			Channel:       "stable",
		},
		Display: DisplayConfig{Mode: "auto"},
		TUI:     TUIConfig{Theme: "dark"},
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
