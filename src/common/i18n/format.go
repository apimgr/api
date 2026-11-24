package i18n

import (
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
	"golang.org/x/text/number"
)

func languageTag(lang string) language.Tag {
	if registration, ok := registeredLanguage(resolveLocale(lang).meta.Language); ok {
		return registration.tag
	}
	registration, _ := registeredLanguage(DefaultLanguage)
	return registration.tag
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
	registration, ok := registeredLanguage(resolveLocale(lang).meta.Language)
	if !ok {
		registration, _ = registeredLanguage(DefaultLanguage)
	}
	return t.Format(registration.dateLayout)
}

// FormatTime renders t's time-of-day portion using lang's conventional
// clock layout (12-hour for English, 24-hour for every other supported
// language).
func FormatTime(lang string, t time.Time) string {
	registration, ok := registeredLanguage(resolveLocale(lang).meta.Language)
	if !ok {
		registration, _ = registeredLanguage(DefaultLanguage)
	}
	return t.Format(registration.timeLayout)
}
