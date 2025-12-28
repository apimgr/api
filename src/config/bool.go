package config

import (
	"strings"
)

// ParseBool parses a boolean value from various string formats.
// Accepts all truthy/falsy values defined in the specification.
//
// Truthy values (case-insensitive):
// - "1", "yes", "true", "on", "enable", "enabled"
// - "y", "t", "yep", "yup", "yeah", "aye", "si", "oui"
//
// Falsy values (case-insensitive):
// - "0", "no", "false", "off", "disable", "disabled"
// - "n", "f", "nope", "nah", "nay", "nein", "non"
//
// Returns false for any unrecognized value.
func ParseBool(value string) bool {
	// Normalize to lowercase for comparison
	v := strings.ToLower(strings.TrimSpace(value))

	// Truthy values
	truthy := map[string]bool{
		"1":       true,
		"yes":     true,
		"true":    true,
		"on":      true,
		"enable":  true,
		"enabled": true,
		"y":       true,
		"t":       true,
		"yep":     true,
		"yup":     true,
		"yeah":    true,
		"aye":     true,
		"si":      true,
		"oui":     true,
	}

	// Check if value is truthy
	if truthy[v] {
		return true
	}

	// All other values (including falsy and unrecognized) return false
	return false
}

// ParseBoolWithDefault parses a boolean value with a default fallback.
// If the value is empty or unrecognized, returns the default value.
func ParseBoolWithDefault(value string, defaultValue bool) bool {
	if value == "" {
		return defaultValue
	}

	// Normalize to lowercase
	v := strings.ToLower(strings.TrimSpace(value))

	// Check if it's explicitly falsy
	falsy := map[string]bool{
		"0":        true,
		"no":       true,
		"false":    true,
		"off":      true,
		"disable":  true,
		"disabled": true,
		"n":        true,
		"f":        true,
		"nope":     true,
		"nah":      true,
		"nay":      true,
		"nein":     true,
		"non":      true,
	}

	if falsy[v] {
		return false
	}

	// Check if it's explicitly truthy
	truthy := map[string]bool{
		"1":       true,
		"yes":     true,
		"true":    true,
		"on":      true,
		"enable":  true,
		"enabled": true,
		"y":       true,
		"t":       true,
		"yep":     true,
		"yup":     true,
		"yeah":    true,
		"aye":     true,
		"si":      true,
		"oui":     true,
	}

	if truthy[v] {
		return true
	}

	// Unrecognized value - use default
	return defaultValue
}
