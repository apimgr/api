// Package i18n provides the single shared internationalization implementation
// used by every apimgr/api binary (server and CLI). Translations are stored
// as embedded JSON locale files under locales/ and are resolved through the
// exported Translate/TranslateFormat/TranslatePlural functions.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/language"
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

type languageRegistration struct {
	code, name, nativeName, direction string
	pluralCategories                  []string
	required                          bool
	tag                               language.Tag
}

// languageRegistry is the single source of supported-language metadata.
// Locale files, runtime negotiation, formatters, and the language selector
// are checked against these registrations.
var languageRegistry = []languageRegistration{
	{code: "en", name: "English", nativeName: "English", direction: "ltr", pluralCategories: []string{"one", "other"}, required: true, tag: language.AmericanEnglish},
	{code: "es", name: "Spanish", nativeName: "Español", direction: "ltr", pluralCategories: []string{"one", "other"}, required: true, tag: language.Spanish},
	{code: "zh", name: "Chinese", nativeName: "中文", direction: "ltr", pluralCategories: []string{"other"}, tag: language.SimplifiedChinese},
	{code: "fr", name: "French", nativeName: "Français", direction: "ltr", pluralCategories: []string{"one", "other"}, required: true, tag: language.French},
	{code: "ar", name: "Arabic", nativeName: "العربية", direction: "rtl", pluralCategories: []string{"zero", "one", "two", "few", "many", "other"}, required: true, tag: language.Arabic},
	{code: "de", name: "German", nativeName: "Deutsch", direction: "ltr", pluralCategories: []string{"one", "other"}, required: true, tag: language.German},
	{code: "it", name: "Italian", nativeName: "Italiano", direction: "ltr", pluralCategories: []string{"one", "many", "other"}, required: true, tag: language.Italian},
	{code: "ja", name: "Japanese", nativeName: "日本語", direction: "ltr", pluralCategories: []string{"other"}, tag: language.Japanese},
}

func registeredLanguage(code string) (languageRegistration, bool) {
	for _, registration := range languageRegistry {
		if registration.code == code {
			return registration, true
		}
	}
	return languageRegistration{}, false
}

func languageCodes(requiredOnly bool) []string {
	codes := make([]string, 0, len(languageRegistry))
	for _, registration := range languageRegistry {
		if !requiredOnly || registration.required {
			codes = append(codes, registration.code)
		}
	}
	return codes
}

// SupportedLanguageCodes returns every locale embedded in each binary, in
// registry order.
func SupportedLanguageCodes() []string { return languageCodes(false) }

// RequiredLanguageCodes returns the minimum six languages that operators
// cannot disable.
func RequiredLanguageCodes() []string { return languageCodes(true) }

var supportedLanguages = languageCodes(false)

var locales map[string]*locale
var localeJSON map[string][]byte

func init() {
	MustLoad()
}

// MustLoad loads embedded locale files, falling back to English when an
// optional locale is missing or malformed. Validate remains the explicit
// diagnostic for build and test checks; a broken translation must not prevent
// the server or CLI from starting.
func MustLoad() {
	loaded, files := loadLocales(localeFS)
	locales = loaded
	localeJSON = files
}

func loadLocales(source fs.FS) (map[string]*locale, map[string][]byte) {
	english, englishErr := loadLocaleFrom(source, DefaultLanguage)
	if englishErr != nil {
		english = &locale{meta: Meta{Language: DefaultLanguage, Name: "English", NativeName: "English", Direction: "ltr"}, flat: map[string]string{}, plain: map[string]interface{}{}}
	}
	loaded := make(map[string]*locale, len(supportedLanguages))
	jsonByLanguage := make(map[string][]byte, len(supportedLanguages))
	englishJSON, _ := fs.ReadFile(source, "locales/"+DefaultLanguage+".json")
	for _, lang := range supportedLanguages {
		l := english
		data := englishJSON
		if lang == DefaultLanguage && englishErr == nil {
			l = english
		} else if lang != DefaultLanguage {
			localized, err := loadLocaleFrom(source, lang)
			if err == nil {
				l = localized
				data, err = fs.ReadFile(source, "locales/"+lang+".json")
				if err != nil {
					l = english
					data = englishJSON
				}
			}
		}
		loaded[lang] = l
		jsonByLanguage[lang] = append([]byte(nil), data...)
	}
	return loaded, jsonByLanguage
}

func validateEmbeddedRegistry() error {
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		return err
	}
	registered := make(map[string]bool, len(supportedLanguages))
	for _, code := range supportedLanguages {
		registered[code+".json"] = true
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if !registered[entry.Name()] {
			return fmt.Errorf("locale file %q has no registry entry", entry.Name())
		}
		delete(registered, entry.Name())
	}
	for file := range registered {
		return fmt.Errorf("registered locale %q has no embedded file", file)
	}
	return nil
}

func loadLocaleFrom(source fs.FS, lang string) (*locale, error) {
	data, err := fs.ReadFile(source, "locales/"+lang+".json")
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

// IsSupported reports whether lang is registered and shipped by every binary.
func IsSupported(lang string) bool {
	_, ok := locales[lang]
	return ok
}

// LocaleJSON returns the embedded source JSON for a registered language.
// The returned bytes are suitable for the browser's dynamic translation
// loader; unsupported names never become filesystem paths.
func LocaleJSON(lang string) ([]byte, error) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if !IsSupported(lang) {
		return nil, fmt.Errorf("unsupported locale %q", lang)
	}
	data, ok := localeJSON[lang]
	if !ok || len(data) == 0 {
		return nil, fmt.Errorf("locale %q and English fallback are unavailable", lang)
	}
	return append([]byte(nil), data...), nil
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
	return AvailableLanguagesFor(supportedLanguages)
}

// AvailableLanguagesFor returns enabled languages sorted by code.
func AvailableLanguagesFor(enabled []string) []LanguageInfo {
	allowed := make(map[string]bool, len(enabled))
	for _, code := range enabled {
		allowed[code] = true
	}
	out := make([]LanguageInfo, 0, len(supportedLanguages))
	for _, code := range languageCodes(false) {
		if !allowed[code] {
			continue
		}
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
var interpolationVariable = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func interpolationVars(s string) map[string]bool {
	vars := make(map[string]bool)
	for _, match := range interpolationVariable.FindAllStringSubmatch(s, -1) {
		vars[match[1]] = true
	}
	return vars
}
