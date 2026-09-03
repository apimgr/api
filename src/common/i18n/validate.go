package i18n

import (
	"fmt"
	"sort"
	"strings"
)

// Validate re-runs full cross-locale validation against the currently
// loaded locales, without triggering MustLoad's panic-on-failure behavior.
// Callers (tests, CLI diagnostics) can use this to get a plain error.
func Validate() error {
	return validateLocales(locales)
}

// validateLocales enforces the build-time/load-time guarantees required by
// AI.md PART 30:
//  1. every non-English locale has exactly the same flattened key set as
//     en.json (no missing keys, no orphaned/extra keys)
//  2. no translation value is an empty string
//  3. the set of {token} interpolation variables in a value matches exactly
//     between each language and English, for the same key
//  4. every "plurals.<group>.*" family defines all categories required for
//     that language (per requiredPluralCategories/pluralCategoriesFor)
func validateLocales(all map[string]*locale) error {
	englishLocale, ok := all[DefaultLanguage]
	if !ok {
		return fmt.Errorf("missing required %q locale", DefaultLanguage)
	}
	englishKeys := englishLocale.allKeys()

	var errs []string

	for _, lang := range supportedLanguages {
		l, ok := all[lang]
		if !ok {
			errs = append(errs, fmt.Sprintf("locale %q: not loaded", lang))
			continue
		}

		if msgs := validateKeySet(lang, l, englishKeys); len(msgs) > 0 {
			errs = append(errs, msgs...)
		}
		if msgs := validateNonEmptyValues(lang, l); len(msgs) > 0 {
			errs = append(errs, msgs...)
		}
		if lang != DefaultLanguage {
			if msgs := validateInterpolationVars(lang, l, englishLocale); len(msgs) > 0 {
				errs = append(errs, msgs...)
			}
		}
		if msgs := validatePluralCategories(lang, l); len(msgs) > 0 {
			errs = append(errs, msgs...)
		}
	}

	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("%d validation error(s):\n%s", len(errs), strings.Join(errs, "\n"))
	}
	return nil
}

// validateKeySet reports every non-plural key present in English but
// missing from l, and every non-plural key present in l but not in
// English (orphaned). Keys under the "plurals." namespace are excluded
// here because each language legitimately defines a different category
// set (e.g. zh/ja define only "other", ar defines all six CLDR
// categories) — that is verified separately by validatePluralCategories.
func validateKeySet(lang string, l *locale, englishKeys []string) []string {
	var msgs []string

	englishSet := make(map[string]bool, len(englishKeys))
	for _, k := range englishKeys {
		if strings.HasPrefix(k, "plurals.") {
			continue
		}
		englishSet[k] = true
	}

	for k := range englishSet {
		if _, ok := l.flat[k]; !ok {
			msgs = append(msgs, fmt.Sprintf("locale %q: missing key %q (present in en)", lang, k))
		}
	}

	for k := range l.flat {
		if strings.HasPrefix(k, "plurals.") {
			continue
		}
		if !englishSet[k] {
			msgs = append(msgs, fmt.Sprintf("locale %q: orphaned key %q (absent from en)", lang, k))
		}
	}

	return msgs
}

// validateNonEmptyValues reports any key whose value is an empty string.
func validateNonEmptyValues(lang string, l *locale) []string {
	var msgs []string
	for k, v := range l.flat {
		if v == "" {
			msgs = append(msgs, fmt.Sprintf("locale %q: key %q has an empty value", lang, k))
		}
	}
	return msgs
}

// validateInterpolationVars reports any key whose {token} placeholder set
// in lang differs from the placeholder set of the same key in English.
// Keys under "plurals." are excluded: TranslatePlural always supplies
// "count" itself regardless of whether a given category's wording uses it
// (e.g. an exact-value category like Arabic's "one" may name the count in
// words instead of interpolating the numeral), so per-category omission is
// linguistically valid rather than a translation defect.
func validateInterpolationVars(lang string, l *locale, english *locale) []string {
	var msgs []string
	for key, enValue := range english.flat {
		if strings.HasPrefix(key, "plurals.") {
			continue
		}
		value, ok := l.flat[key]
		if !ok {
			// Already reported by validateKeySet.
			continue
		}
		want := interpolationVars(enValue)
		got := interpolationVars(value)
		if !sameVarSet(want, got) {
			msgs = append(msgs, fmt.Sprintf(
				"locale %q: key %q interpolation variables %v do not match en %v",
				lang, key, sortedKeys(got), sortedKeys(want),
			))
		}
	}
	return msgs
}

// validatePluralCategories reports any "plurals.<group>" family missing a
// category required for lang.
func validatePluralCategories(lang string, l *locale) []string {
	var msgs []string

	groups := make(map[string]bool)
	const prefix = "plurals."
	for k := range l.flat {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		if i := strings.LastIndexByte(rest, '.'); i != -1 {
			groups[rest[:i]] = true
		}
	}

	required := requiredPluralCategories(lang)
	groupNames := make([]string, 0, len(groups))
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)

	for _, group := range groupNames {
		for _, category := range required {
			key := prefix + group + "." + category
			if _, ok := l.flat[key]; !ok {
				msgs = append(msgs, fmt.Sprintf(
					"locale %q: plural group %q missing required category %q (key %q)",
					lang, group, category, key,
				))
			}
		}
	}

	return msgs
}

func sameVarSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
