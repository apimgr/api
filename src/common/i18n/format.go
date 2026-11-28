package i18n

import (
	"strings"
	"time"

	"golang.org/x/text/currency"
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

// FormatPercent renders a ratio using locale-aware grouping and percent
// placement. A value of 1 is rendered as 100 percent.
func FormatPercent(lang string, ratio float64) string {
	printer := message.NewPrinter(languageTag(lang), message.Catalog(emptyCatalog))
	return printer.Sprintf("%v", number.Percent(ratio, number.Scale(1)))
}

// FormatCurrency renders amount with an ISO 4217 currency code using the
// locale's number and currency conventions.
func FormatCurrency(lang, code string, amount float64) (string, error) {
	unit, err := currency.ParseISO(code)
	if err != nil {
		return "", err
	}
	printer := message.NewPrinter(languageTag(lang), message.Catalog(emptyCatalog))
	return printer.Sprintf("%v", currency.Symbol(unit.Amount(amount))), nil
}

// FormatDate renders t's date portion using lang's conventional numeric
// date layout.
func FormatDate(lang string, t time.Time) string {
	return localizeDigits(lang, t.Format(Translate(lang, "format.date")))
}

// FormatTime renders t's time-of-day portion using lang's conventional
// clock layout (12-hour for English, 24-hour for every other supported
// language).
func FormatTime(lang string, t time.Time) string {
	value := t.Format(Translate(lang, "format.time"))
	value = strings.ReplaceAll(value, "AM", Translate(lang, "format.am"))
	value = strings.ReplaceAll(value, "PM", Translate(lang, "format.pm"))
	return localizeDigits(lang, value)
}

// FormatDateTime renders a date and time using the locale's complete
// reference-time layout.
func FormatDateTime(lang string, t time.Time) string {
	value := t.Format(Translate(lang, "format.datetime"))
	value = strings.ReplaceAll(value, "AM", Translate(lang, "format.am"))
	value = strings.ReplaceAll(value, "PM", Translate(lang, "format.pm"))
	return localizeDigits(lang, value)
}

func localizeDigits(lang, value string) string {
	digits := Translate(lang, "format.digits")
	if len([]rune(digits)) != 10 {
		return value
	}
	var out strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			out.WriteRune([]rune(digits)[r-'0'])
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}
