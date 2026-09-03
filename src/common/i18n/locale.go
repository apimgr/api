// Package i18n provides the single shared internationalization implementation
// used by every apimgr/api binary (server and CLI). Translations are stored
// as embedded JSON locale files under locales/ and are resolved through the
// exported Translate/TranslateFormat/TranslatePlural functions.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed locales/*.json
var localeFS embed.FS

// DefaultLanguage is the fallback language used when no preference is
// detected, when an unsupported language is requested, or when a key is
// missing from the active language.
const DefaultLanguage = "en"

// Meta describes the identity of a single locale file.
type Meta struct {
	Language   string `json:"language"`
	Name       string `json:"name"`
	NativeName string `json:"native_name"`
	Direction  string `json:"direction"`
	Version    string `json:"version"`
}

// LanguageInfo is the public, read-only summary of a supported language,
// suitable for rendering a language picker.
type LanguageInfo struct {
	Code       string
	Name       string
	NativeName string
	Direction  string
}

// locale holds a single loaded language: its metadata plus a flattened
// key -> string lookup table (dot-joined nested JSON keys).
type locale struct {
	meta  Meta
	flat  map[string]string
	plain map[string]interface{}
}

// supportedLanguages is the fixed, ordered set of languages every binary
// ships. No partial support is permitted.
var supportedLanguages = []string{"en", "es", "zh", "fr", "ar", "de", "ja"}

var locales map[string]*locale

func init() {
	MustLoad()
}

// MustLoad loads and validates every locale file embedded in the binary. It
// panics if any locale file is missing, malformed, or fails validation
// against the canonical en.json key set. It is called automatically at
// package init time; it is exported so callers (and tests) can force a
// reload/re-validation deterministically.
func MustLoad() {
	loaded := make(map[string]*locale, len(supportedLanguages))
	for _, lang := range supportedLanguages {
		l, err := loadLocale(lang)
		if err != nil {
			panic(fmt.Sprintf("i18n: failed to load locale %q: %v", lang, err))
		}
		loaded[lang] = l
	}
	locales = loaded
	if err := validateLocales(locales); err != nil {
		panic(fmt.Sprintf("i18n: locale validation failed: %v", err))
	}
}

func loadLocale(lang string) (*locale, error) {
	data, err := localeFS.ReadFile("locales/" + lang + ".json")
	if err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	metaRaw, ok := raw["meta"]
	if !ok {
		return nil, fmt.Errorf("missing required \"meta\" object")
	}
	metaBytes, err := json.Marshal(metaRaw)
	if err != nil {
		return nil, err
	}
	var meta Meta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("invalid \"meta\" object: %w", err)
	}
	if meta.Language != lang {
		return nil, fmt.Errorf("meta.language %q does not match filename %q", meta.Language, lang)
	}

	flat := make(map[string]string, 512)
	for key, value := range raw {
		if key == "meta" {
			continue
		}
		flatten(key, value, flat)
	}

	return &locale{meta: meta, flat: flat, plain: raw}, nil
}

// flatten recursively walks a decoded JSON value, writing every string leaf
// into dst keyed by its dot-joined path (e.g. "plurals.items.other").
func flatten(prefix string, value interface{}, dst map[string]string) {
	switch v := value.(type) {
	case string:
		dst[prefix] = v
	case map[string]interface{}:
		for key, child := range v {
			flatten(prefix+"."+key, child, dst)
		}
	default:
		// Non-string, non-object leaves are not part of the translation
		// surface (the schema only ever nests objects and strings under
		// each language's key tree).
	}
}

// IsSupported reports whether lang is one of the seven languages shipped by
// every binary.
func IsSupported(lang string) bool {
	_, ok := locales[lang]
	return ok
}

// resolveLocale returns the locale for lang, falling back to English when
// lang is empty or unsupported. It never returns nil once MustLoad has run
// successfully, since "en" is always present.
func resolveLocale(lang string) *locale {
	if l, ok := locales[lang]; ok {
		return l
	}
	return locales[DefaultLanguage]
}

// Dir returns "rtl" or "ltr" for lang, defaulting to the English locale's
// direction ("ltr") for an unsupported language.
func Dir(lang string) string {
	return resolveLocale(lang).meta.Direction
}

// Name returns the English display name of lang (e.g. "German").
func Name(lang string) string {
	return resolveLocale(lang).meta.Name
}

// NativeName returns the endonym for lang (e.g. "Deutsch").
func NativeName(lang string) string {
	return resolveLocale(lang).meta.NativeName
}

// AvailableLanguages returns every supported language, sorted by code, for
// rendering a language picker.
func AvailableLanguages() []LanguageInfo {
	out := make([]LanguageInfo, 0, len(supportedLanguages))
	for _, code := range supportedLanguages {
		l := locales[code]
		out = append(out, LanguageInfo{
			Code:       code,
			Name:       l.meta.Name,
			NativeName: l.meta.NativeName,
			Direction:  l.meta.Direction,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// allKeys returns every flattened key present in the locale, sorted.
func (l *locale) allKeys() []string {
	keys := make([]string, 0, len(l.flat))
	for k := range l.flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// interpolationVars extracts the set of {token} variable names referenced
// by a translation string.
func interpolationVars(s string) map[string]bool {
	vars := make(map[string]bool)
	for {
		start := strings.IndexByte(s, '{')
		if start == -1 {
			break
		}
		end := strings.IndexByte(s[start:], '}')
		if end == -1 {
			break
		}
		vars[s[start+1:start+end]] = true
		s = s[start+end+1:]
	}
	return vars
}
