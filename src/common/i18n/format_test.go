package i18n

import (
	"strings"
	"testing"
	"time"
)

func TestFormatNumberGrouping(t *testing.T) {
	got := FormatNumber("en", 1234.5)
	if got == "" {
		t.Fatal("FormatNumber(\"en\", 1234.5) returned an empty string")
	}
	if got := FormatNumber("it", 1234.5); got != "1.234,5" {
		t.Errorf("FormatNumber(\"it\", 1234.5) = %q, want \"1.234,5\"", got)
	}
}

func TestFormatPercentAndCurrencyAreLocaleAware(t *testing.T) {
	if got := FormatPercent("it", 0.455); !strings.Contains(got, ",") || !strings.Contains(got, "%") {
		t.Errorf("FormatPercent(\"it\", 0.455) = %q, want localized decimal and percent sign", got)
	}
	got, err := FormatCurrency("de", "EUR", 1234.56)
	if err != nil {
		t.Fatalf("FormatCurrency: %v", err)
	}
	if got == "" || !strings.Contains(got, "€") {
		t.Errorf("FormatCurrency(\"de\", \"EUR\", 1234.56) = %q, want localized currency output", got)
	}
	if _, err := FormatCurrency("en", "not-a-code", 1); err == nil {
		t.Fatal("FormatCurrency accepted an invalid ISO currency code")
	}
}

func TestFormatDateUsesLanguageLayout(t *testing.T) {
	ref := time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC)
	if got := FormatDate("en", ref); got != "03/04/2026" {
		t.Errorf("FormatDate(\"en\", ref) = %q, want \"03/04/2026\"", got)
	}
	if got := FormatDate("de", ref); got != "04.03.2026" {
		t.Errorf("FormatDate(\"de\", ref) = %q, want \"04.03.2026\"", got)
	}
	if got := FormatDate("zh", ref); got != "2026/03/04" {
		t.Errorf("FormatDate(\"zh\", ref) = %q, want \"2026/03/04\"", got)
	}
	if got := FormatDate("it", ref); got != "04/03/2026" {
		t.Errorf("FormatDate(\"it\", ref) = %q, want \"04/03/2026\"", got)
	}
}

func TestFormatTimeTwelveVsTwentyFourHour(t *testing.T) {
	ref := time.Date(2026, time.March, 4, 15, 4, 0, 0, time.UTC)
	if got := FormatTime("en", ref); got != "3:04 PM" {
		t.Errorf("FormatTime(\"en\", ref) = %q, want \"3:04 PM\"", got)
	}
	if got := FormatTime("de", ref); got != "15:04" {
		t.Errorf("FormatTime(\"de\", ref) = %q, want \"15:04\"", got)
	}
	if got := FormatTime("it", ref); got != "15:04" {
		t.Errorf("FormatTime(\"it\", ref) = %q, want \"15:04\"", got)
	}
}

func TestFormatDateTimeUsesLocaleFormatsAndDigits(t *testing.T) {
	ref := time.Date(2026, time.March, 4, 15, 4, 0, 0, time.UTC)
	if got := FormatDateTime("en", ref); got != "03/04/2026 3:04 PM" {
		t.Errorf("FormatDateTime(\"en\", ref) = %q, want \"03/04/2026 3:04 PM\"", got)
	}
	if got := FormatDateTime("ar", ref); got != "٠٤/٠٣/٢٠٢٦ ١٥:٠٤" {
		t.Errorf("FormatDateTime(\"ar\", ref) = %q, want Arabic-Indic digits", got)
	}
}
