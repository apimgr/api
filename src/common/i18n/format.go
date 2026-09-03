package i18n

import (
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
	"golang.org/x/text/number"
)

// languageTags maps each supported language to its BCP 47 language.Tag for
// use with golang.org/x/text formatters.
var languageTags = map[string]language.Tag{
	"en": language.AmericanEnglish,
	"es": language.Spanish,
	"zh": language.SimplifiedChinese,
	"fr": language.French,
	"ar": language.Arabic,
	"de": language.German,
	"ja": language.Japanese,
}

// dateLayouts gives each supported language its conventional numeric date
// layout (Go reference-time format).
var dateLayouts = map[string]string{
	"en": "01/02/2006",
	"es": "02/01/2006",
	"zh": "2006/01/02",
	"fr": "02/01/2006",
	"ar": "02/01/2006",
	"de": "02.01.2006",
	"ja": "2006/01/02",
}

// timeLayouts gives each supported language its conventional clock layout.
// English uses a 12-hour clock with AM/PM; every other supported language
// uses a 24-hour clock.
var timeLayouts = map[string]string{
	"en": "3:04 PM",
	"es": "15:04",
	"zh": "15:04",
	"fr": "15:04",
	"ar": "15:04",
	"de": "15:04",
	"ja": "15:04",
}

func languageTag(lang string) language.Tag {
	if tag, ok := languageTags[resolveLocale(lang).meta.Language]; ok {
		return tag
	}
	return languageTags[DefaultLanguage]
}

// emptyCatalog supplies message.NewPrinter with a translation catalog that
// holds no entries, since number formatting via golang.org/x/text/number
// requires a *message.Printer but the translation text itself is always
// sourced from Translate/TranslateFormat, not from this catalog.
var emptyCatalog = catalog.NewBuilder()

// FormatNumber renders n using lang's conventional grouping and decimal
// separators (e.g. "1,234.5" for en, "1.234,5" for de).
func FormatNumber(lang string, n float64) string {
	printer := message.NewPrinter(languageTag(lang), message.Catalog(emptyCatalog))
	return printer.Sprintf("%v", number.Decimal(n))
}

// FormatDate renders t's date portion using lang's conventional numeric
// date layout.
func FormatDate(lang string, t time.Time) string {
	layout, ok := dateLayouts[resolveLocale(lang).meta.Language]
	if !ok {
		layout = dateLayouts[DefaultLanguage]
	}
	return t.Format(layout)
}

// FormatTime renders t's time-of-day portion using lang's conventional
// clock layout (12-hour for English, 24-hour for every other supported
// language).
func FormatTime(lang string, t time.Time) string {
	layout, ok := timeLayouts[resolveLocale(lang).meta.Language]
	if !ok {
		layout = timeLayouts[DefaultLanguage]
	}
	return t.Format(layout)
}
