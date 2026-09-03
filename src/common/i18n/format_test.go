package i18n

import (
	"testing"
	"time"
)

func TestFormatNumberGrouping(t *testing.T) {
	got := FormatNumber("en", 1234.5)
	if got == "" {
		t.Fatal("FormatNumber(\"en\", 1234.5) returned an empty string")
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
}

func TestFormatTimeTwelveVsTwentyFourHour(t *testing.T) {
	ref := time.Date(2026, time.March, 4, 15, 4, 0, 0, time.UTC)
	if got := FormatTime("en", ref); got != "3:04 PM" {
		t.Errorf("FormatTime(\"en\", ref) = %q, want \"3:04 PM\"", got)
	}
	if got := FormatTime("de", ref); got != "15:04" {
		t.Errorf("FormatTime(\"de\", ref) = %q, want \"15:04\"", got)
	}
}
