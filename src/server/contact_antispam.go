package server

import (
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/go-chi/chi/v5"
)

type contactAntispamSettings struct {
	Honeypot            bool
	MinFill             time.Duration
	MaxAge              time.Duration
	PerIP               int
	PerEmail            int
	Global              int
	Window              time.Duration
	MaxLinks            int
	RejectMarkup        bool
	BlockedWords        []string
	BlockedEmailDomains []string
	Strikes             int
	BanDuration         time.Duration
	ReviewMin           time.Duration
	ReviewMax           time.Duration
	DuplicateWindow     time.Duration
	RouteExists         func(string) bool
}

func contactRouteExists(router chi.Router, path string) bool {
	if router == nil {
		return false
	}
	context := chi.NewRouteContext()
	return router.Match(context, "GET", path)
}

func contactAntispamConfig(cfg *config.Config) contactAntispamSettings {
	settings := contactAntispamSettings{
		Honeypot: true, MinFill: 3 * time.Second, MaxAge: 12 * time.Hour,
		PerIP: 5, PerEmail: 3, Global: 60, Window: time.Hour, MaxLinks: 10,
		RejectMarkup: true, Strikes: 3, BanDuration: 24 * time.Hour,
		ReviewMin: 2 * time.Second, ReviewMax: 15 * time.Minute,
		DuplicateWindow: 24 * time.Hour,
	}
	if cfg == nil || cfg.Server.Pages == nil {
		return settings
	}
	page, ok := cfg.Server.Pages["contact"].(map[string]interface{})
	if !ok {
		return settings
	}
	values, ok := page["antispam"].(map[string]interface{})
	if !ok {
		return settings
	}
	if value, ok := values["honeypot"]; ok {
		settings.Honeypot = contactBool(value, settings.Honeypot)
	}
	if n, ok := contactInt(values["min_fill_seconds"]); ok {
		settings.MinFill = time.Duration(max(n, 0)) * time.Second
	}
	if n, ok := contactInt(values["max_age_seconds"]); ok {
		settings.MaxAge = time.Duration(max(n, 0)) * time.Second
	}
	if n, ok := contactInt(values["per_ip"]); ok {
		settings.PerIP = max(n, 0)
	}
	if n, ok := contactInt(values["per_email"]); ok {
		settings.PerEmail = max(n, 0)
	}
	if n, ok := contactInt(values["global"]); ok {
		settings.Global = max(n, 0)
	}
	if value, ok := values["window"].(string); ok {
		if duration, err := time.ParseDuration(strings.TrimSpace(value)); err == nil && duration >= 0 {
			settings.Window = duration
		}
	}
	if n, ok := contactInt(values["max_links"]); ok {
		settings.MaxLinks = max(n, 0)
	}
	if value, ok := values["reject_markup"]; ok {
		settings.RejectMarkup = contactBool(value, settings.RejectMarkup)
	}
	settings.BlockedWords = contactStringSlice(values["blocked_words"])
	settings.BlockedEmailDomains = contactStringSlice(values["blocked_email_domains"])
	if n, ok := contactInt(values["strikes"]); ok {
		settings.Strikes = max(n, 0)
	}
	if n, ok := contactInt(values["ban_hours"]); ok {
		settings.BanDuration = time.Duration(max(n, 0)) * time.Hour
	}
	if n, ok := contactInt(values["review_min_seconds"]); ok {
		settings.ReviewMin = time.Duration(max(n, 0)) * time.Second
	}
	if n, ok := contactInt(values["review_max_seconds"]); ok {
		settings.ReviewMax = time.Duration(max(n, 0)) * time.Second
	}
	if n, ok := contactInt(values["duplicate_window_hours"]); ok {
		settings.DuplicateWindow = time.Duration(max(n, 0)) * time.Hour
	}
	return settings
}

func contactInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case uint64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n, true
		}
	}
	return 0, false
}

func contactBool(value interface{}, fallback bool) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	return fallback
}

func contactStringSlice(value interface{}) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []interface{}:
		result := make([]string, 0, len(values))
		for _, item := range values {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return result
	default:
		return nil
	}
}
