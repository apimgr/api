package config

import (
	"net/url"
	"regexp"
	"strings"
)

// TrackingConfig holds the analytics settings from AI.md PART 12. An empty
// or "none" Type disables analytics entirely.
type TrackingConfig struct {
	// Type is one of google, matomo, piwik, owa, fathom, plausible, umami,
	// simple or cloudflare.
	Type string `yaml:"type"`
	// ID is the tracking/site identifier; its format depends on Type.
	ID string `yaml:"id"`
	// URL is the self-hosted instance URL, required for matomo, piwik, owa
	// and umami and unused for the cloud-only platforms.
	URL string `yaml:"url"`
}

// trackingIDPatterns validates each platform's identifier format per the
// PART 12 "Supported Analytics Platforms" table.
var trackingIDPatterns = map[string]*regexp.Regexp{
	"google":     regexp.MustCompile(`^(UA-\d+-\d+|G-[A-Z0-9]+)$`),
	"matomo":     regexp.MustCompile(`^\d+$`),
	"piwik":      regexp.MustCompile(`^\d+$`),
	"owa":        regexp.MustCompile(`(?s)^.+$`),
	"fathom":     regexp.MustCompile(`(?s)^.+$`),
	"plausible":  regexp.MustCompile(`^[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`),
	"umami":      regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`),
	"simple":     regexp.MustCompile(`^.*$`),
	"cloudflare": regexp.MustCompile(`(?s)^.+$`),
}

// trackingURLRequired lists the self-hosted-only platforms whose URL must
// be set for the integration to render.
var trackingURLRequired = map[string]bool{
	"matomo": true,
	"piwik":  true,
	"owa":    true,
	"umami":  true,
}

// Enabled reports whether analytics is configured and valid enough to
// render. An unknown type, malformed ID, or missing self-hosted URL all
// disable tracking rather than emitting a broken snippet.
func (t TrackingConfig) Enabled() bool {
	kind := t.Normalized()
	if kind == "" {
		return false
	}
	pattern, known := trackingIDPatterns[kind]
	if !known {
		return false
	}
	if kind != "simple" && strings.TrimSpace(t.ID) == "" {
		return false
	}
	if !pattern.MatchString(t.ID) {
		return false
	}
	if trackingURLRequired[kind] && strings.TrimSpace(t.URL) == "" {
		return false
	}
	if strings.TrimSpace(t.URL) != "" {
		parsed, err := url.Parse(strings.TrimSpace(t.URL))
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
			return false
		}
	}
	return true
}

// Normalized returns the lower-cased platform name, or "" when tracking is
// disabled via an empty value or the literal "none".
func (t TrackingConfig) Normalized() string {
	kind := strings.ToLower(strings.TrimSpace(t.Type))
	if kind == "" || kind == "none" {
		return ""
	}
	return kind
}
