package i18n

import "strings"

// Translate returns the string stored at key for lang. Resolution order:
//  1. key in the requested lang (if lang is supported)
//  2. key in English (missing-key fallback, per spec)
//  3. the raw key itself, as a last resort so callers never render an empty
//     string
//
// An unsupported lang silently falls back to English before key lookup even
// begins, per the spec's "never error or crash" rule.
func Translate(lang, key string) string {
	l := resolveLocale(lang)
	if v, ok := l.flat[key]; ok {
		return v
	}
	if l.meta.Language != DefaultLanguage {
		if v, ok := locales[DefaultLanguage].flat[key]; ok {
			return v
		}
	}
	return key
}

// TranslateFormat returns the string stored at key for lang with every
// "{token}" placeholder replaced by args["token"]. Interpolation is a
// literal, named-token substitution only — never printf-style verbs and
// never positional placeholders.
func TranslateFormat(lang, key string, args map[string]string) string {
	s := Translate(lang, key)
	if len(args) == 0 {
		return s
	}
	replacements := make([]string, 0, len(args)*2)
	for name, value := range args {
		replacements = append(replacements, "{"+name+"}", value)
	}
	return strings.NewReplacer(replacements...).Replace(s)
}
