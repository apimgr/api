package config

import (
	"os"
	"path/filepath"

	"github.com/apimgr/api/src/paths"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	WebUI       WebUIConfig       `yaml:"web_ui"`
	WebRobots   WebRobotsConfig   `yaml:"web_robots"`
	WebSecurity WebSecurityConfig `yaml:"web_security"`
}

type ServerConfig struct {
	Port    string        `yaml:"port"`
	FQDN    string        `yaml:"fqdn"`
	Address string        `yaml:"address"`
	Logging LoggingConfig `yaml:"logging"`
}

type LoggingConfig struct {
	AccessFormat string `yaml:"access_format"`
	Level        string `yaml:"level"`
}

type WebUIConfig struct {
	Theme   string `yaml:"theme"`
	Logo    string `yaml:"logo"`
	Favicon string `yaml:"favicon"`
}

type WebRobotsConfig struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
}

type WebSecurityConfig struct {
	Admin string `yaml:"admin"`
	CORS  string `yaml:"cors"`
}

// Default configuration
func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:    "64365",
			FQDN:    "",
			Address: "0.0.0.0",
			Logging: LoggingConfig{
				AccessFormat: "apache",
				Level:        "info",
			},
		},
		WebUI: WebUIConfig{
			Theme:   "dark",
			Logo:    "",
			Favicon: "",
		},
		WebRobots: WebRobotsConfig{
			Allow: []string{"/", "/api"},
			Deny:  []string{"/admin"},
		},
		WebSecurity: WebSecurityConfig{
			Admin: "security@api.apimgr.us",
			CORS:  "*",
		},
	}
}

// Load loads configuration from file or creates default
func Load() (*Config, error) {
	cfg := defaultConfig()

	configFile := filepath.Join(paths.ConfigDir(), "server.yaml")

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

	return cfg, nil
}

// Save saves configuration to file
func Save(cfg *Config) error {
	configDir := paths.ConfigDir()

	// Create config directory if needed
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configFile := filepath.Join(configDir, "server.yaml")

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	// Add header comment
	content := "# CasTools Configuration\n# https://api.apimgr.us\n\n" + string(data)

	return os.WriteFile(configFile, []byte(content), 0644)
}
