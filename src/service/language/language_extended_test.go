package language

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// rewriteTransport redirects every outbound request to a local httptest
// server regardless of the hardcoded provider host, so Dictionary/Thesaurus
// can be exercised without live network access.
type rewriteTransport struct {
	target *url.URL
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

// withFakeProvider swaps the package-level httpClient for the duration of
// the calling test, routing all requests to srv, and restores the original
// client on cleanup.
func withFakeProvider(t *testing.T, srv *httptest.Server) {
	t.Helper()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("failed to parse httptest URL: %v", err)
	}

	orig := httpClient
	httpClient = &http.Client{
		Timeout:   5 * time.Second,
		Transport: &rewriteTransport{target: target},
	}
	t.Cleanup(func() { httpClient = orig })
}

// TestDictionaryEmptyWord verifies the empty/whitespace-only input guard,
// which returns before any HTTP call is made.
func TestDictionaryEmptyWord(t *testing.T) {
	s := New()
	if _, err := s.Dictionary(context.Background(), "   "); err == nil {
		t.Error("expected an error for an empty word")
	}
}

// TestDictionarySuccess verifies a well-formed provider response is decoded
// into the public DictionaryResult shape, including phonetic filtering.
func TestDictionarySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/hello") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"word": "hello",
			"phonetics": [{"text": "", "audio": ""}, {"text": "/həˈloʊ/", "audio": "a.mp3"}],
			"meanings": [{
				"partOfSpeech": "exclamation",
				"definitions": [{"definition": "A greeting.", "example": "Hello!", "synonyms": ["hi"], "antonyms": []}]
			}]
		}]`))
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	result, err := s.Dictionary(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Dictionary failed: %v", err)
	}
	if result.Word != "hello" {
		t.Errorf("Word = %q, want hello", result.Word)
	}
	// The empty phonetic entry must be filtered out.
	if len(result.Phonetics) != 1 {
		t.Fatalf("Phonetics = %d entries, want 1", len(result.Phonetics))
	}
	if result.Phonetics[0].Audio != "a.mp3" {
		t.Errorf("Phonetics[0].Audio = %q, want a.mp3", result.Phonetics[0].Audio)
	}
	if len(result.Meanings) != 1 || result.Meanings[0].PartOfSpeech != "exclamation" {
		t.Fatalf("unexpected meanings: %+v", result.Meanings)
	}
	if len(result.Meanings[0].Definitions) != 1 || result.Meanings[0].Definitions[0].Definition != "A greeting." {
		t.Fatalf("unexpected definitions: %+v", result.Meanings[0].Definitions)
	}
}

// TestDictionaryNotFound verifies a 404 from the provider maps to a
// not-found error rather than a decode error.
func TestDictionaryNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	_, err := s.Dictionary(context.Background(), "zzzznotaword")
	if err == nil || !strings.Contains(err.Error(), "no definitions found") {
		t.Errorf("err = %v, want a 'no definitions found' error", err)
	}
}

// TestDictionaryNonOKStatus verifies a non-200/404 status surfaces the
// status code in the error.
func TestDictionaryNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	_, err := s.Dictionary(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want an error mentioning status 500", err)
	}
}

// TestDictionaryEmptyArray verifies a 200 response with an empty JSON array
// is treated as "not found", not a successful empty result.
func TestDictionaryEmptyArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	_, err := s.Dictionary(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "no definitions found") {
		t.Errorf("err = %v, want a 'no definitions found' error", err)
	}
}

// TestDictionaryBadJSON verifies malformed provider JSON surfaces a decode
// error rather than panicking.
func TestDictionaryBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	_, err := s.Dictionary(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "failed to decode") {
		t.Errorf("err = %v, want a decode error", err)
	}
}

// TestDictionaryRequestFailure verifies a canceled context surfaces a
// request-failure error from httpClient.Do rather than a panic.
func TestDictionaryRequestFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New()
	_, err := s.Dictionary(ctx, "hello")
	if err == nil {
		t.Error("expected an error for a canceled context")
	}
}

// TestThesaurusEmptyWord verifies the empty/whitespace-only input guard.
func TestThesaurusEmptyWord(t *testing.T) {
	s := New()
	if _, err := s.Thesaurus(context.Background(), " "); err == nil {
		t.Error("expected an error for an empty word")
	}
}

// TestThesaurusSuccess verifies synonym and antonym relations are both
// queried and merged into the result.
func TestThesaurusSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("rel_syn") != "":
			_, _ = w.Write([]byte(`[{"word":"happy"},{"word":"glad"}]`))
		case r.URL.Query().Get("rel_ant") != "":
			_, _ = w.Write([]byte(`[{"word":"sad"}]`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	result, err := s.Thesaurus(context.Background(), "cheerful")
	if err != nil {
		t.Fatalf("Thesaurus failed: %v", err)
	}
	if len(result.Synonyms) != 2 || result.Synonyms[0] != "happy" {
		t.Errorf("Synonyms = %v, want [happy glad]", result.Synonyms)
	}
	if len(result.Antonyms) != 1 || result.Antonyms[0] != "sad" {
		t.Errorf("Antonyms = %v, want [sad]", result.Antonyms)
	}
}

// TestThesaurusNoResults verifies both relations returning empty lists is
// treated as a not-found error.
func TestThesaurusNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	_, err := s.Thesaurus(context.Background(), "zzzznotaword")
	if err == nil || !strings.Contains(err.Error(), "no synonyms or antonyms") {
		t.Errorf("err = %v, want a 'no synonyms or antonyms' error", err)
	}
}

// TestThesaurusSynonymProviderError verifies a failed synonym lookup short-
// circuits before the antonym lookup ever happens.
func TestThesaurusSynonymProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	_, err := s.Thesaurus(context.Background(), "cheerful")
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Errorf("err = %v, want an error mentioning status 500", err)
	}
}

// TestThesaurusAntonymProviderError verifies a successful synonym lookup
// followed by a failed antonym lookup still surfaces an error.
func TestThesaurusAntonymProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("rel_syn") != "" {
			_, _ = w.Write([]byte(`[{"word":"happy"}]`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	_, err := s.Thesaurus(context.Background(), "cheerful")
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Errorf("err = %v, want an error mentioning status 500", err)
	}
}

// TestThesaurusBadJSON verifies malformed provider JSON surfaces a decode
// error.
func TestThesaurusBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	withFakeProvider(t, srv)

	s := New()
	_, err := s.Thesaurus(context.Background(), "cheerful")
	if err == nil || !strings.Contains(err.Error(), "failed to decode") {
		t.Errorf("err = %v, want a decode error", err)
	}
}

// TestThesaurusRequestFailure verifies a canceled context surfaces a
// request-failure error rather than a panic.
func TestThesaurusRequestFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New()
	_, err := s.Thesaurus(ctx, "cheerful")
	if err == nil {
		t.Error("expected an error for a canceled context")
	}
}

// TestSoundex traces the standard American Soundex algorithm against
// hand-verified inputs, including the classic Census Bureau "Ashcraft"
// example (H does not reset the previous digit, so the following C
// collapses into S's digit).
func TestSoundex(t *testing.T) {
	tests := []struct {
		name string
		word string
		want string
	}{
		{"empty string", "", ""},
		{"no letters", "123", ""},
		{"robert", "Robert", "R163"},
		{"rupert", "Rupert", "R163"},
		{"ashcraft h-w rule", "Ashcraft", "A261"},
		{"short word padded", "Lee", "L000"},
		{"single letter", "A", "A000"},
	}

	s := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Soundex(tt.word); got != tt.want {
				t.Errorf("Soundex(%q) = %q, want %q", tt.word, got, tt.want)
			}
		})
	}
}

// TestMetaphone traces the classic (single) Metaphone algorithm against
// hand-verified inputs covering the initial-letter substitutions (GN/KN/WR/
// X/WH), the CH/TH/PH digraphs, and duplicate-consonant collapsing.
func TestMetaphone(t *testing.T) {
	tests := []struct {
		name string
		word string
		want string
	}{
		{"empty string", "", ""},
		{"no letters", "123", ""},
		{"cat", "cat", "KT"},
		{"gnat initial GN drop", "gnat", "NT"},
		{"knife initial KN drop", "knife", "NF"},
		{"wrap initial WR drop", "wrap", "RP"},
		{"check CH digraph", "check", "XK"},
	}

	s := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Metaphone(tt.word); got != tt.want {
				t.Errorf("Metaphone(%q) = %q, want %q", tt.word, got, tt.want)
			}
		})
	}
}

// TestWordCount covers the empty, single-line, and multi-line/multi-
// sentence branches.
func TestWordCount(t *testing.T) {
	tests := []struct {
		name string
		text string
		want WordStats
	}{
		{
			name: "empty text",
			text: "",
			want: WordStats{},
		},
		{
			name: "whitespace only",
			text: "   ",
			want: WordStats{Characters: 3, CharactersNoSpaces: 0, Lines: 1},
		},
		{
			name: "single sentence",
			text: "Hi there!",
			want: WordStats{Words: 2, Characters: 9, CharactersNoSpaces: 8, Lines: 1, Sentences: 1},
		},
		{
			name: "multi line, multi sentence",
			text: "Line one. Line two?\nLine three!",
			want: WordStats{Words: 6, Characters: 31, CharactersNoSpaces: 26, Lines: 2, Sentences: 3},
		},
	}

	s := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.WordCount(tt.text)
			if got != tt.want {
				t.Errorf("WordCount(%q) = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}

// TestKeywords verifies stopword filtering, frequency ordering, alphabetic
// tie-breaking, the default limit, and a caller-supplied limit.
func TestKeywords(t *testing.T) {
	s := New()

	got := s.Keywords("the cat sat on the mat the cat ran", 0)
	want := []Keyword{{Word: "cat", Count: 2}, {Word: "mat", Count: 1}, {Word: "ran", Count: 1}, {Word: "sat", Count: 1}}
	if len(got) != len(want) {
		t.Fatalf("Keywords length = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Keywords[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	limited := s.Keywords("the cat sat on the mat the cat ran", 2)
	if len(limited) != 2 {
		t.Fatalf("Keywords with limit=2 returned %d entries", len(limited))
	}
	if limited[0].Word != "cat" || limited[0].Count != 2 {
		t.Errorf("top keyword = %+v, want cat:2", limited[0])
	}

	if got := s.Keywords("", 5); len(got) != 0 {
		t.Errorf("Keywords(\"\") = %+v, want empty", got)
	}

	if got := s.Keywords("the a an", 5); len(got) != 0 {
		t.Errorf("Keywords of pure stopwords = %+v, want empty", got)
	}
}

// TestReadability covers the empty-text early return and sanity-checks the
// computed scores are non-zero for real text (the exact float formulas are
// standard published constants, verified structurally rather than
// re-deriving every decimal by hand).
func TestReadability(t *testing.T) {
	s := New()

	empty := s.Readability("")
	if empty != (ReadabilityStats{}) {
		t.Errorf("Readability(\"\") = %+v, want zero value", empty)
	}

	stats := s.Readability("The cat sat on the mat. It was a sunny day.")
	if stats.Words != 11 {
		t.Errorf("Words = %d, want 11", stats.Words)
	}
	if stats.Sentences != 2 {
		t.Errorf("Sentences = %d, want 2", stats.Sentences)
	}
	if stats.Syllables == 0 {
		t.Error("expected a non-zero syllable count")
	}
	if stats.FleschReadingEase == 0 {
		t.Error("expected a non-zero Flesch Reading Ease score")
	}

	// No terminal punctuation still yields one implied sentence.
	noPunct := s.Readability("word word word")
	if noPunct.Sentences != 1 {
		t.Errorf("Sentences with no punctuation = %d, want 1", noPunct.Sentences)
	}
}

// TestReadingTime covers the default words-per-minute fallback, ceiling
// rounding for minutes, and an explicit custom rate.
func TestReadingTime(t *testing.T) {
	tests := []struct {
		name           string
		wordCount      int
		wordsPerMinute int
		want           ReadingTimeStats
	}{
		{"default wpm exact", 200, 0, ReadingTimeStats{Words: 200, Minutes: 1, Seconds: 60}},
		{"default wpm rounds up", 201, 0, ReadingTimeStats{Words: 201, Minutes: 2, Seconds: 60}},
		{"custom wpm", 100, 50, ReadingTimeStats{Words: 100, Minutes: 2, Seconds: 120}},
		{"zero words", 0, 200, ReadingTimeStats{Words: 0, Minutes: 0, Seconds: 0}},
		{"negative wpm falls back to default", 200, -5, ReadingTimeStats{Words: 200, Minutes: 1, Seconds: 60}},
	}

	s := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.ReadingTime(tt.wordCount, tt.wordsPerMinute)
			if got != tt.want {
				t.Errorf("ReadingTime(%d, %d) = %+v, want %+v", tt.wordCount, tt.wordsPerMinute, got, tt.want)
			}
		})
	}
}

// TestSentiment covers the empty-text guard and the positive/negative/
// neutral classification thresholds.
func TestSentiment(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantLabel string
	}{
		{"empty text", "", "neutral"},
		{"clearly positive", "This is a great, wonderful, amazing product", "positive"},
		{"clearly negative", "This is a terrible, awful, horrible product", "negative"},
		{"neutral no matches", "The quick brown fox jumps", "neutral"},
	}

	s := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Sentiment(tt.text)
			if got.Label != tt.wantLabel {
				t.Errorf("Sentiment(%q).Label = %q, want %q", tt.text, got.Label, tt.wantLabel)
			}
		})
	}

	mixed := s.Sentiment("good bad good good")
	if mixed.PositiveMatches != 3 || mixed.NegativeMatches != 1 {
		t.Errorf("mixed matches = +%d/-%d, want +3/-1", mixed.PositiveMatches, mixed.NegativeMatches)
	}
	if mixed.Label != "positive" {
		t.Errorf("mixed Label = %q, want positive", mixed.Label)
	}
}
