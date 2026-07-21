package main

import (
	"os"
	"strings"
)

// colorEnabled holds the resolved --color/NO_COLOR state for this process.
var colorEnabled = true

// envOrFlag resolves a CLI flag value against its environment variable
// fallback, per AI.md PART 8 "Environment Variable Fallbacks": an
// explicitly set CLI flag always wins, otherwise the env var is used, and
// otherwise the empty string is returned so the caller can apply its own
// default.
func envOrFlag(flagValue, envKey string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv(envKey)
}

// applyColorMode resolves --color against NO_COLOR and TTY/TERM
// auto-detection, per AI.md PART 8 "NO_COLOR Support" priority order:
// CLI flag > NO_COLOR env var > auto-detect. Config-file overrides do not
// apply here (server has no `output.color` config key).
func applyColorMode(colorFlag string) {
	switch strings.ToLower(strings.TrimSpace(colorFlag)) {
	case "yes", "true", "on":
		colorEnabled = true
		return
	case "no", "false", "off":
		colorEnabled = false
		return
	}

	if os.Getenv("NO_COLOR") != "" {
		colorEnabled = false
		return
	}
	if os.Getenv("TERM") == "dumb" {
		colorEnabled = false
		return
	}
	colorEnabled = true
}
