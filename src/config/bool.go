package config

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

var invalidBooleanWarnings sync.Map

// Truthy values (case-insensitive)
var truthyValues = map[string]bool{
	"1": true, "y": true, "t": true,
	"yes": true, "true": true, "on": true, "ok": true,
	"enable": true, "enabled": true,
	"yep": true, "yup": true, "yeah": true,
	"aye": true, "si": true, "oui": true, "da": true, "hai": true,
	"affirmative": true, "accept": true, "allow": true, "grant": true,
	"sure": true, "totally": true,
}

// Falsy values (case-insensitive)
var falsyValues = map[string]bool{
	"0": true, "n": true, "f": true,
	"no": true, "false": true, "off": true,
	"disable": true, "disabled": true,
	"nope": true, "nah": true, "nay": true,
	"nein": true, "non": true, "niet": true, "iie": true, "lie": true,
	"negative": true, "reject": true, "block": true, "revoke": true,
	"deny": true, "never": true, "noway": true,
}

// ParseBool parses a string into a boolean using truthy/falsy values.
// Returns the parsed value and nil on success.
// Returns false and an error for invalid values.
// Empty string returns the provided default value.
func ParseBool(s string, defaultVal bool) (bool, error) {
	s = strings.TrimSpace(strings.ToLower(s))

	if s == "" {
		return defaultVal, nil
	}

	if truthyValues[s] {
		return true, nil
	}

	if falsyValues[s] {
		return false, nil
	}

	return false, fmt.Errorf("invalid boolean value: %q", s)
}

// MustParseBool is retained for source compatibility and follows the
// configuration contract: invalid values use the default and never panic.
func MustParseBool(s string, defaultVal bool) bool {
	return ParseBoolOrDefault("boolean", s, defaultVal)
}

// ParseBoolOrDefault parses a configuration boolean and uses its default on
// invalid input. Each setting emits at most one warning per process.
func ParseBoolOrDefault(name, value string, defaultValue bool) bool {
	parsed, err := ParseBool(value, defaultValue)
	if err == nil {
		return parsed
	}
	if _, loaded := invalidBooleanWarnings.LoadOrStore(name, struct{}{}); !loaded {
		slog.Warn("invalid boolean configuration; using default", "setting", name, "value", value, "default", defaultValue)
	}
	return defaultValue
}

// IsTruthy returns true if the string is a truthy value.
// Returns false for empty, invalid, or falsy values (no error).
func IsTruthy(s string) bool {
	return truthyValues[strings.TrimSpace(strings.ToLower(s))]
}

// IsFalsy returns true if the string is a falsy value.
// Returns false for empty, invalid, or truthy values (no error).
func IsFalsy(s string) bool {
	return falsyValues[strings.TrimSpace(strings.ToLower(s))]
}
