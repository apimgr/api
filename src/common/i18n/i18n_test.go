package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsSupported(t *testing.T) {
	for _, lang := range []string{"en", "es", "zh", "fr", "ar", "de", "ja"} {
		if !IsSupported(lang) {
			t.Errorf("IsSupported(%q) = false, want true", lang)
		}
	}
	if IsSupported("xx") {
		t.Errorf("IsSupported(\"xx\") = true, want false")
	}
	if IsSupported("") {
		t.Errorf("IsSupported(\"\") = true, want false")
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("Validate() returned an error for the shipped locale files: %v", err)
	}
}

func TestDirRTL(t *testing.T) {
	if got := Dir("ar"); got != "rtl" {
		t.Errorf("Dir(\"ar\") = %q, want \"rtl\"", got)
	}
	for _, lang := range []string{"en", "es", "zh", "fr", "de", "ja"} {
		if got := Dir(lang); got != "ltr" {
			t.Errorf("Dir(%q) = %q, want \"ltr\"", lang, got)
		}
	}
	if got := Dir("xx"); got != "ltr" {
		t.Errorf("Dir(\"xx\") (unsupported, falls back to en) = %q, want \"ltr\"", got)
	}
}

func TestAvailableLanguages(t *testing.T) {
	langs := AvailableLanguages()
	if len(langs) != 7 {
		t.Fatalf("AvailableLanguages() returned %d languages, want 7", len(langs))
	}
	for i := 1; i < len(langs); i++ {
		if langs[i-1].Code >= langs[i].Code {
			t.Errorf("AvailableLanguages() not sorted by code: %q >= %q", langs[i-1].Code, langs[i].Code)
		}
	}
	var sawArabic bool
	for _, l := range langs {
		if l.Code == "ar" {
			sawArabic = true
			if l.Direction != "rtl" {
				t.Errorf("ar direction = %q, want \"rtl\"", l.Direction)
			}
		}
	}
	if !sawArabic {
		t.Error("AvailableLanguages() missing \"ar\"")
	}
}

func TestTranslateFallsBackToEnglishOnMissingKey(t *testing.T) {
	got := Translate("de", "no.such.key.at.all")
	if got != "no.such.key.at.all" {
		t.Errorf("Translate for a nonexistent key = %q, want the raw key back", got)
	}
}

func TestTranslateUnsupportedLanguageFallsBackToEnglish(t *testing.T) {
	enValue := Translate("en", "common.go_home")
	unsupportedValue := Translate("xx", "common.go_home")
	if unsupportedValue != enValue {
		t.Errorf("Translate(\"xx\", key) = %q, want English fallback %q", unsupportedValue, enValue)
	}
}

func TestTranslateFormatInterpolation(t *testing.T) {
	got := TranslateFormat("en", "common.go_home", map[string]string{"unused": "value"})
	want := Translate("en", "common.go_home")
	if got != want {
		t.Errorf("TranslateFormat with unrelated args changed the string: got %q, want %q", got, want)
	}
}

func TestPluralCategoryEnglish(t *testing.T) {
	cases := map[int]string{0: "other", 1: "one", 2: "other", 100: "other"}
	for n, want := range cases {
		if got := PluralCategory("en", n); got != want {
			t.Errorf("PluralCategory(\"en\", %d) = %q, want %q", n, got, want)
		}
	}
}

func TestPluralCategoryFrenchTreatsZeroAsOne(t *testing.T) {
	if got := PluralCategory("fr", 0); got != "one" {
		t.Errorf("PluralCategory(\"fr\", 0) = %q, want \"one\"", got)
	}
	if got := PluralCategory("fr", 1); got != "one" {
		t.Errorf("PluralCategory(\"fr\", 1) = %q, want \"one\"", got)
	}
	if got := PluralCategory("fr", 2); got != "other" {
		t.Errorf("PluralCategory(\"fr\", 2) = %q, want \"other\"", got)
	}
}

func TestPluralCategoryChineseAndJapaneseAlwaysOther(t *testing.T) {
	for _, lang := range []string{"zh", "ja"} {
		for _, n := range []int{0, 1, 2, 100} {
			if got := PluralCategory(lang, n); got != "other" {
				t.Errorf("PluralCategory(%q, %d) = %q, want \"other\"", lang, n, got)
			}
		}
	}
}

func TestPluralCategoryArabicFullCLDRRule(t *testing.T) {
	cases := map[int]string{
		0:   "zero",
		1:   "one",
		2:   "two",
		3:   "few",
		10:  "few",
		11:  "many",
		99:  "many",
		100: "other",
		101: "other",
		103: "few",
	}
	for n, want := range cases {
		if got := PluralCategory("ar", n); got != want {
			t.Errorf("PluralCategory(\"ar\", %d) = %q, want %q", n, got, want)
		}
	}
}

func TestTranslatePluralInterpolatesCount(t *testing.T) {
	got := TranslatePlural("en", "items", 1, nil)
	if got == "" {
		t.Fatal("TranslatePlural returned an empty string")
	}
	if got == "plurals.items.one" {
		t.Errorf("TranslatePlural returned the raw key %q; locale data is missing this group", got)
	}
}

func TestLangFromRequestQueryParamWins(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?lang=fr", nil)
	r.Header.Set("Accept-Language", "de")
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "es"})
	if got := LangFromRequest(r); got != "fr" {
		t.Errorf("LangFromRequest = %q, want \"fr\" (query param has top priority)", got)
	}
}

func TestLangFromRequestCookieBeatsHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "de")
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "es"})
	if got := LangFromRequest(r); got != "es" {
		t.Errorf("LangFromRequest = %q, want \"es\" (cookie beats header)", got)
	}
}

func TestLangFromRequestAcceptLanguageHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "fr;q=0.5, de;q=0.9, en;q=0.1")
	if got := LangFromRequest(r); got != "de" {
		t.Errorf("LangFromRequest = %q, want \"de\" (highest q-value)", got)
	}
}

func TestLangFromRequestDefaultsToEnglish(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := LangFromRequest(r); got != DefaultLanguage {
		t.Errorf("LangFromRequest with nothing set = %q, want %q", got, DefaultLanguage)
	}
}

func TestLangFromRequestUnsupportedFallsBackSilently(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?lang=xx", nil)
	if got := LangFromRequest(r); got != DefaultLanguage {
		t.Errorf("LangFromRequest with unsupported ?lang= = %q, want %q", got, DefaultLanguage)
	}
}

func TestLanguageMiddlewareSetsContextAndCookie(t *testing.T) {
	var seen string
	handler := LanguageMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = LangFromContext(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/?lang=de", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if seen != "de" {
		t.Errorf("LangFromContext inside handler = %q, want \"de\"", seen)
	}

	res := w.Result()
	var cookieSet bool
	for _, c := range res.Cookies() {
		if c.Name == CookieName && c.Value == "de" {
			cookieSet = true
		}
	}
	if !cookieSet {
		t.Error("LanguageMiddleware did not set the lang cookie for an explicit ?lang= request")
	}
}

func TestLangFromContextDefaultsWhenAbsent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := LangFromContext(r.Context()); got != DefaultLanguage {
		t.Errorf("LangFromContext with no middleware run = %q, want %q", got, DefaultLanguage)
	}
}
