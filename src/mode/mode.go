package mode

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/config"
)

// Mode represents the application execution mode
type Mode string

const (
	// Production mode - optimized for performance and security
	Production Mode = "production"
	// Development mode - optimized for debugging and development
	Development Mode = "development"
	// Debug mode - maximum verbosity and minimal sanitization. Explicit
	// opt-in only (via the mode flag or environment variable), never
	// implied or auto-enabled, and it never disables authentication or
	// security checks. Selecting it defaults the debug flag on.
	Debug Mode = "debug"
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

// GetCurrentMode returns the current application mode
func GetCurrentMode() Mode {
	mu.RLock()
	defer mu.RUnlock()
	return currentMode
}

// SetMode sets the application mode.
// Valid values: "production"/"prod", "development"/"dev"/"devel", "debug".
// Selecting "debug" here does NOT default the debug flag on — use
// SetWithDebugDefault for the full mode resolution behavior.
func SetMode(mode string) error {
	parsed, err := ParseMode(mode)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()
	currentMode = parsed
	return nil
}

// ParseMode parses a mode string into a Mode constant.
// Accepts (case-insensitive, surrounding whitespace ignored):
// "production"/"prod", "development"/"dev"/"devel", and "debug".
// "debug" is a first-class third mode, not an alias for development, and
// it has no shortcut form — it must be spelled out.
func ParseMode(s string) (Mode, error) {
	normalized := strings.ToLower(strings.TrimSpace(s))

	switch normalized {
	case "development", "dev", "devel":
		return Development, nil
	case "production", "prod":
		return Production, nil
	case "debug":
		return Debug, nil
	default:
		return "", fmt.Errorf("invalid mode: %q (expected: {production|development|debug}, or the shortcuts prod, dev, devel)", s)
	}
}

// SetWithDebugDefault sets the application mode and applies debug mode's
// flag default: selecting the debug mode also defaults the debug flag to
// on. An explicitly set --debug flag or DEBUG environment variable
// (applied afterward by the caller) still wins, so choosing debug mode
// together with a falsy DEBUG value runs debug mode with the debug
// endpoints turned off.
func SetWithDebugDefault(mode string) error {
	if err := SetMode(mode); err != nil {
		return err
	}

	if GetCurrentMode() == Debug {
		SetDebugEnabled(true)
	}

	return nil
}

// IsDevelopment returns true if the current mode is Development
func IsDevelopment() bool {
	return GetCurrentMode() == Development
}

// IsProduction returns true if the current mode is Production
func IsProduction() bool {
	return GetCurrentMode() == Production
}

// IsDebugMode returns true if the current mode is Debug. This reports the
// selected mode, not the debug flag: call IsDebugEnabled to test whether
// the debug diagnostics and endpoints are active.
func IsDebugMode() bool {
	return GetCurrentMode() == Debug
}

// IsVerboseMode returns true for the two non-production modes
// (development and debug), which share verbose logging, detailed errors,
// verbose panic recovery, and hot-reload-friendly cache defaults.
func IsVerboseMode() bool {
	m := GetCurrentMode()
	return m == Development || m == Debug
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
//  3. Default: production
//
// Debug:
//  1. cliDebug (--debug flag), highest priority
//  2. DEBUG environment variable (truthy/falsy, if explicitly set)
//  3. debug mode's own default (on)
//  4. Default: false
func Initialize(cliMode string, cliDebug bool, cliDebugSet bool) error {
	// Priority 1/2/3 for mode (also applies debug mode's flag default)
	switch {
	case cliMode != "":
		if err := SetWithDebugDefault(cliMode); err != nil {
			return err
		}
	default:
		if envMode := os.Getenv("MODE"); envMode != "" {
			if err := SetWithDebugDefault(envMode); err != nil {
				return err
			}
		}
	}

	// Priority 1 for debug: explicit --debug flag always wins
	if cliDebugSet {
		SetDebugEnabled(cliDebug)
		return nil
	}

	// Priority 2 for debug: an explicitly set DEBUG environment variable
	// wins over the value debug mode defaulted to, in both directions
	if v, set := os.LookupEnv("DEBUG"); set {
		SetDebugEnabled(config.IsTruthy(v))
	}

	// Otherwise: leave whatever the mode default (or false) produced
	return nil
}

// SanitizationLevel describes how much internal detail may leave the
// process, per AI.md PART 6.
type SanitizationLevel string

const (
	// SanitizationFull is the enforced level in production AND development:
	// the Output Sanitization Pipeline runs in full and sensitive data is
	// never shown.
	SanitizationFull SanitizationLevel = "full"
	// SanitizationMinimal is debug mode's level: internals, dumps, and
	// stack traces may be exposed. Credentials are still always redacted.
	SanitizationMinimal SanitizationLevel = "minimal"
)

// GetSanitizationLevel returns the sanitization level for the current mode.
// Only debug mode relaxes to minimal sanitization — development is fully
// sanitized exactly like production.
func GetSanitizationLevel() SanitizationLevel {
	if IsDebugMode() {
		return SanitizationMinimal
	}
	return SanitizationFull
}

// MayExposeInternals returns true when internal detail (dumps, stack
// traces, raw error chains) may be included in a response. Only debug
// mode allows it; credentials remain redacted even then.
func MayExposeInternals() bool {
	return GetSanitizationLevel() == SanitizationMinimal
}

// Sanitize applies the always-on credential redaction to text that is
// about to leave the process. Credentials are redacted in every mode,
// including debug — no exceptions.
func Sanitize(s string) string {
	return sanitize.RedactCredentials(s)
}

// GetErrorDetail returns error details based on the current mode.
// Development and debug modes return the error text (credentials always
// redacted); production returns a generic message with no internal detail.
func GetErrorDetail(err error) string {
	if err == nil {
		return ""
	}

	if IsVerboseMode() {
		// Development/debug: detailed error text, credentials redacted
		return Sanitize(err.Error())
	}

	// Production mode: return generic error message
	return "An internal error occurred. Please contact support if the problem persists."
}

// ShouldShowDebugEndpoints returns true if debug endpoints should be enabled.
// The debug endpoints (/debug/pprof/*, /debug/vars, /debug/config, etc.)
// are gated by the debug flag, NOT by the selected mode — they return 404
// whenever the flag is off, in every mode including debug mode.
func ShouldShowDebugEndpoints() bool {
	return IsDebugEnabled()
}

// CacheHeaders represents HTTP cache control headers
type CacheHeaders struct {
	CacheControl string
	Pragma       string
	Expires      string
}

// ResolveCacheSetting applies the config-driven caching rule from AI.md
// PART 6/26: every mode uses the cache when one is configured, so an
// operator-set value is honored in every mode and the mode only supplies
// the default when the key is unset (nil).
func ResolveCacheSetting(configured *bool, modeDefault bool) bool {
	if configured != nil {
		return *configured
	}
	return modeDefault
}

// DefaultCacheEnabled returns the caching default for the current mode:
// enabled in production, disabled in development and debug so templates
// and static assets hot-reload. It is only a DEFAULT — an explicit
// config value overrides it in every mode.
func DefaultCacheEnabled() bool {
	return !IsVerboseMode()
}

// ShouldCacheTemplates returns true if parsed templates should be cached.
// Pass the operator's server.cache.templates value (nil when unset).
func ShouldCacheTemplates(configured *bool) bool {
	return ResolveCacheSetting(configured, DefaultCacheEnabled())
}

// ShouldCacheStatic returns true if static assets should be served with
// long-lived caching. Pass the operator's server.cache.static value (nil
// when unset).
func ShouldCacheStatic(configured *bool) bool {
	return ResolveCacheSetting(configured, DefaultCacheEnabled())
}

// GetCacheHeaders returns the static-asset cache headers for the resolved
// server.cache.static setting (nil = use the current mode's default).
func GetCacheHeaders(configured *bool) CacheHeaders {
	if !ShouldCacheStatic(configured) {
		// Caching off: instruct clients not to store the asset
		return CacheHeaders{
			CacheControl: "no-cache, no-store, must-revalidate",
			Pragma:       "no-cache",
			Expires:      "0",
		}
	}

	// Caching on: 1 year for fingerprinted static assets
	return CacheHeaders{
		CacheControl: "public, max-age=31536000, immutable",
		Pragma:       "",
		Expires:      "",
	}
}

// GetLogLevel returns the recommended log level for the current mode
func GetLogLevel() string {
	if IsVerboseMode() {
		return "debug"
	}
	return "info"
}

// ShouldEnableAutoReload returns true if auto-reload should be enabled
func ShouldEnableAutoReload() bool {
	return IsVerboseMode()
}

// ShouldEnableProfiling returns true if runtime profiling (block/mutex
// profiling, pprof) should be enabled. Gated by the debug flag, not by mode.
func ShouldEnableProfiling() bool {
	return IsDebugEnabled()
}

// GetPanicRecoveryMode returns the panic recovery behavior for the current mode
// Returns "verbose" for development/debug, "graceful" for production
func GetPanicRecoveryMode() string {
	if IsVerboseMode() {
		return "verbose"
	}
	return "graceful"
}

// String returns the string representation of the Mode
func (m Mode) String() string {
	return string(m)
}

// GetModeString returns the mode string with a debug suffix when the debug
// flag is active, for the startup banner (AI.md PART 6 console output).
func GetModeString() string {
	s := GetCurrentMode().String()
	if IsDebugEnabled() {
		s += " [debugging]"
	}
	return s
}

// Validate returns an error if the mode is not valid
func (m Mode) Validate() error {
	switch m {
	case Production, Development, Debug:
		return nil
	default:
		return errors.New("invalid mode")
	}
}
