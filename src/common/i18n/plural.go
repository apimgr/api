package i18n

import (
	"strconv"
	"strings"
)

// PluralCategory selects the CLDR plural category for n in lang, using the
// rules from AI.md PART 30: en/es/de use one (n==1) / other; fr treats 0 as
// "one"; zh/ja always select "other"; it uses "many" for round millions;
// and ar implements the full CLDR Arabic rule.
func PluralCategory(lang string, n int) string {
	switch lang {
	case "zh", "ja":
		return "other"
	case "fr":
		if n == 0 || n == 1 {
			return "one"
		}
		return "other"
	case "ar":
		return arabicPluralCategory(n)
	case "it":
		if n == 1 {
			return "one"
		}
		if n != 0 && n%1_000_000 == 0 {
			return "many"
		}
		return "other"
	case "en", "es", "de":
		if n == 1 {
			return "one"
		}
		return "other"
	default:
		if n == 1 {
			return "one"
		}
		return "other"
	}
}

func arabicPluralCategory(n int) string {
	if n < 0 {
		n = -n
	}
	mod100 := n % 100
	switch {
	case n == 0:
		return "zero"
	case n == 1:
		return "one"
	case n == 2:
		return "two"
	case mod100 >= 3 && mod100 <= 10:
		return "few"
	case mod100 >= 11 && mod100 <= 99:
		return "many"
	default:
		return "other"
	}
}

// TranslatePlural returns the pluralized string for group (e.g. "items",
// resolving "plurals.<group>.<category>") in lang for the given count,
// with "{count}" (and any other {token} in args) interpolated. args may be
// nil; "count" is always available even if the caller does not pass it.
func TranslatePlural(lang, group string, count int, args map[string]string) string {
	l := resolveLocale(lang)
	category := PluralCategory(l.meta.Language, count)

	key := "plurals." + group + "." + category
	value, ok := l.flat[key]
	if !ok {
		// Missing-key fallback: try English at the same category, then
		// English "other", then the raw key.
		if v, found := locales[DefaultLanguage].flat[key]; found {
			value = v
		} else if v, found := locales[DefaultLanguage].flat["plurals."+group+".other"]; found {
			value = v
		} else {
			value = key
		}
	}

	merged := make(map[string]string, len(args)+1)
	for k, v := range args {
		merged[k] = v
	}
	if _, exists := merged["count"]; !exists {
		merged["count"] = strconv.Itoa(count)
	}

	replacements := make([]string, 0, len(merged)*2)
	for name, val := range merged {
		replacements = append(replacements, "{"+name+"}", val)
	}
	return strings.NewReplacer(replacements...).Replace(value)
}

// requiredPluralCategories returns the categories lang must define for
// every plural group, per AI.md PART 30. Unknown languages default to the
// English set.
func requiredPluralCategories(lang string) []string {
	if registration, ok := registeredLanguage(lang); ok {
		return registration.pluralCategories
	}
	registration, _ := registeredLanguage(DefaultLanguage)
	return registration.pluralCategories
}
