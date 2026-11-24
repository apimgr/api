package i18n

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsSupported(t *testing.T) {
	for _, lang := range []string{"en", "es", "zh", "fr", "ar", "de", "it", "ja"} {
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

func TestLocaleJSONUsesOnlyEmbeddedSupportedLocales(t *testing.T) {
	for _, lang := range []string{"en", "fr", "ar"} {
		data, err := LocaleJSON(lang)
		if err != nil {
			t.Fatalf("LocaleJSON(%q): %v", lang, err)
		}
		if len(data) == 0 {
			t.Fatalf("LocaleJSON(%q) returned empty data", lang)
		}
	}
	if _, err := LocaleJSON("../../etc/passwd"); err == nil {
		t.Fatal("unsupported locale path accepted")
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("Validate() returned an error for the shipped locale files: %v", err)
	}
}

func TestValidatePluralCategoriesRejectsOrphansAndAllowsZero(t *testing.T) {
	zh := &locale{flat: map[string]string{
		"plurals.items.other": "{count} items",
		"plurals.items.zero":  "No items",
	}}
	if got := validatePluralCategories("zh", zh); len(got) != 0 {
		t.Fatalf("validatePluralCategories(zh) = %v, want optional zero accepted", got)
	}
	zh.flat["plurals.items.one"] = "{count} item"
	if got := validatePluralCategories("zh", zh); len(got) != 1 || !strings.Contains(got[0], "orphaned category \"one\"") {
		t.Fatalf("validatePluralCategories(zh) = %v, want orphaned one category", got)
	}
}

func TestInterpolationVarsIgnoreStructuredExamples(t *testing.T) {
	got := interpolationVars(`Hello, {name}: {"mode":"generate"}; CSS { color: red; }`)
	if len(got) != 1 || !got["name"] {
		t.Fatalf("interpolationVars() = %v, want only the named placeholder name", got)
	}
}

func TestDirRTL(t *testing.T) {
	if got := Dir("ar"); got != "rtl" {
		t.Errorf("Dir(\"ar\") = %q, want \"rtl\"", got)
	}
	for _, lang := range []string{"en", "es", "zh", "fr", "de", "it", "ja"} {
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
	if len(langs) != 8 {
		t.Fatalf("AvailableLanguages() returned %d languages, want 8", len(langs))
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

func TestAvailableLanguagesForFiltersOptionalLocales(t *testing.T) {
	langs := AvailableLanguagesFor([]string{"en", "es", "fr", "de", "it", "ar"})
	if len(langs) != 6 {
		t.Fatalf("AvailableLanguagesFor returned %d languages, want 6", len(langs))
	}
	for _, lang := range langs {
		if lang.Code == "zh" || lang.Code == "ja" {
			t.Errorf("optional locale %q was not filtered", lang.Code)
		}
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

func TestPluralCategoryItalian(t *testing.T) {
	cases := map[int]string{0: "other", 1: "one", 2: "other", 1_000_000: "many", 2_000_000: "many"}
	for n, want := range cases {
		if got := PluralCategory("it", n); got != want {
			t.Errorf("PluralCategory(\"it\", %d) = %q, want %q", n, got, want)
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

func TestLangFromEmailRequestUsesCookieThenHeaderAndIgnoresQuery(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?lang=fr", nil)
	r.Header.Set("Accept-Language", "de")
	if got := LangFromEmailRequest(r); got != "de" {
		t.Fatalf("LangFromEmailRequest without cookie = %q, want de", got)
	}
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "es"})
	if got := LangFromEmailRequest(r); got != "es" {
		t.Fatalf("LangFromEmailRequest with cookie = %q, want es", got)
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

func TestLanguageMiddlewareForUsesConfiguredLanguages(t *testing.T) {
	var got string
	handler := LanguageMiddlewareFor([]string{"en", "es", "fr", "de", "it", "ar"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = LangFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/?lang=it", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got != "it" {
		t.Fatalf("configured Italian request language = %q, want it", got)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != "it" {
		t.Fatalf("language cookies = %v, want one cookie with it", cookies)
	}

	got = ""
	req = httptest.NewRequest(http.MethodGet, "/?lang=ja", nil)
	req.Header.Set("Accept-Language", "ja, en;q=0.8")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got != "en" {
		t.Fatalf("disabled language fallback = %q, want en", got)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("disabled query language must not be persisted")
	}
}

func TestLangFromContextDefaultsWhenAbsent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := LangFromContext(r.Context()); got != DefaultLanguage {
		t.Errorf("LangFromContext with no middleware run = %q, want %q", got, DefaultLanguage)
	}
}
