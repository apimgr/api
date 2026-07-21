package mode

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/apimgr/api/src/config"
)

const appName = "api"

// Mode represents the application execution mode
type Mode string

const (
	// Production mode - optimized for performance and security
	Production Mode = "production"
	// Development mode - optimized for debugging and development
	Development Mode = "development"
)

var (
	// currentMode stores the active application mode
	currentMode Mode = Production
	// debugEnabled stores whether --debug/DEBUG=true diagnostics are active
	debugEnabled bool
	// currentLang stores the active interface language code (--lang/LANG)
	currentLang string
	// mu protects concurrent access to currentMode, debugEnabled, and currentLang
	mu sync.RWMutex
)

// Get returns the current application mode
func Get() Mode {
	mu.RLock()
	defer mu.RUnlock()
	return currentMode
}

// Set sets the application mode
// Valid values: "production", "prod", "development", "dev"
func Set(mode string) error {
	parsed, err := ParseMode(mode)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()
	currentMode = parsed
	return nil
}

// ParseMode parses a mode string into a Mode constant
// Accepts: "dev", "devel", "development", "prod", "production", "debug" (case-insensitive)
// "debug" is an alias for "development" — callers that need the debug-flag
// side effect of the alias should use SetWithDebugAlias instead.
func ParseMode(s string) (Mode, error) {
	normalized := strings.ToLower(strings.TrimSpace(s))

	switch normalized {
	case "development", "dev", "devel", "debug":
		return Development, nil
	case "production", "prod":
		return Production, nil
	default:
		return "", fmt.Errorf("invalid mode: %q (expected: production, prod, development, dev, devel, or debug)", s)
	}
}

// SetWithDebugAlias sets the application mode, applying the "debug" alias:
// "--mode debug" / "MODE=debug" expands to mode=development + debug=on.
// An explicitly set --debug flag or DEBUG env var (applied afterward by the
// caller) still wins over the alias.
func SetWithDebugAlias(mode string) error {
	if err := Set(mode); err != nil {
		return err
	}

	if strings.EqualFold(strings.TrimSpace(mode), "debug") {
		SetDebugEnabled(true)
	}

	return nil
}

// IsDevelopment returns true if the current mode is Development
func IsDevelopment() bool {
	return Get() == Development
}

// IsProduction returns true if the current mode is Production
func IsProduction() bool {
	return Get() == Production
}

// SetDebugEnabled enables or disables --debug/DEBUG=true diagnostics.
// Debug mode affects verbosity and diagnostics ONLY — it never bypasses
// authentication or security checks, in any mode, including production.
func SetDebugEnabled(enabled bool) {
	mu.Lock()
	defer mu.Unlock()
	debugEnabled = enabled
}

// IsDebugEnabled returns true if --debug/DEBUG=true diagnostics are active
func IsDebugEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return debugEnabled
}

// SetLang sets the active interface language code (--lang flag or LANG
// environment variable, per PART 8 shared flags).
func SetLang(lang string) {
	mu.Lock()
	defer mu.Unlock()
	currentLang = lang
}

// GetLang returns the active interface language code, or "" if none was set
func GetLang() string {
	mu.RLock()
	defer mu.RUnlock()
	return currentLang
}

// Initialize sets the mode and debug flag based on priority order:
//
// Mode:
//  1. cliMode (--mode flag), highest priority
//  2. MODE environment variable
//  3. Default: production ("--mode debug" / "MODE=debug" is an alias for
//     development + debug on)
//
// Debug:
//  1. cliDebug (--debug flag), highest priority
//  2. DEBUG environment variable (truthy/falsy, if explicitly set)
//  3. "debug" mode alias
//  4. Default: false
func Initialize(cliMode string, cliDebug bool, cliDebugSet bool) error {
	// Priority 1/2/3 for mode (also applies the "debug" alias)
	switch {
	case cliMode != "":
		if err := SetWithDebugAlias(cliMode); err != nil {
			return err
		}
	default:
		if envMode := os.Getenv("MODE"); envMode != "" {
			if err := SetWithDebugAlias(envMode); err != nil {
				return err
			}
		}
	}

	// Priority 1 for debug: explicit --debug flag always wins
	if cliDebugSet {
		SetDebugEnabled(cliDebug)
		return nil
	}

	// Priority 2 for debug: explicitly set DEBUG env var wins over the alias
	if v, set := os.LookupEnv("DEBUG"); set {
		SetDebugEnabled(config.IsTruthy(v))
	}

	// Otherwise: leave whatever the mode alias (or default false) produced
	return nil
}

// GetErrorDetail returns error details based on the current mode
// In development mode: returns full error details with stack traces
// In production mode: returns generic error message without internal details
func GetErrorDetail(err error) string {
	if err == nil {
		return ""
	}

	if IsDevelopment() {
		// Development mode: return full error details
		return err.Error()
	}

	// Production mode: return generic error message
	return "An internal error occurred. Please contact support if the problem persists."
}

// ShouldShowDebugEndpoints returns true if debug endpoints should be enabled.
// Debug endpoints (/debug/pprof/*, /debug/vars, /debug/config, etc.) are
// gated by the debug flag (--debug/DEBUG=true), NOT by development mode —
// they return 404 otherwise, in both production and development.
func ShouldShowDebugEndpoints() bool {
	return IsDebugEnabled()
}

// CacheHeaders represents HTTP cache control headers
type CacheHeaders struct {
	CacheControl string
	Pragma       string
	Expires      string
}

// GetCacheHeaders returns appropriate cache headers based on the current mode
// Development mode: no-cache headers to prevent caching
// Production mode: aggressive caching headers for static files
func GetCacheHeaders() CacheHeaders {
	if IsDevelopment() {
		// Development mode: disable caching
		return CacheHeaders{
			CacheControl: "no-cache, no-store, must-revalidate",
			Pragma:       "no-cache",
			Expires:      "0",
		}
	}

	// Production mode: enable caching (1 year for static assets)
	return CacheHeaders{
		CacheControl: "public, max-age=31536000, immutable",
		Pragma:       "",
		Expires:      "",
	}
}

// GetLogLevel returns the recommended log level for the current mode
func GetLogLevel() string {
	if IsDevelopment() {
		return "debug"
	}
	return "info"
}

// ShouldCacheTemplates returns true if templates should be cached
func ShouldCacheTemplates() bool {
	return IsProduction()
}

// ShouldEnableAutoReload returns true if auto-reload should be enabled
func ShouldEnableAutoReload() bool {
	return IsDevelopment()
}

// ShouldEnableProfiling returns true if runtime profiling (block/mutex
// profiling, pprof) should be enabled. Gated by the debug flag, not by mode.
func ShouldEnableProfiling() bool {
	return IsDebugEnabled()
}

// GetPanicRecoveryMode returns the panic recovery behavior for the current mode
// Returns "verbose" for development, "graceful" for production
func GetPanicRecoveryMode() string {
	if IsDevelopment() {
		return "verbose"
	}
	return "graceful"
}

// String returns the string representation of the Mode
func (m Mode) String() string {
	return string(m)
}

// Validate returns an error if the mode is not valid
func (m Mode) Validate() error {
	switch m {
	case Production, Development:
		return nil
	default:
		return errors.New("invalid mode")
	}
}
