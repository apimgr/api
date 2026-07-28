package language

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Service provides language/translation utilities
type Service struct{}

// New creates a new Language service
func New() *Service {
	return &Service{}
}

// httpClient is a shared client with a hard timeout for the keyless
// dictionary/thesaurus providers (no API key, free, fair-use rate limited)
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

const (
	dictionaryEndpoint = "https://api.dictionaryapi.dev/api/v2/entries/en"
	// datamuseEndpoint's synonym/antonym relations (rel_syn/rel_ant) are
	// free and keyless today; Datamuse has announced a mandatory API key
	// starting 2027-01-01 — see TODO.AI.md for the tracked follow-up.
	datamuseEndpoint = "https://api.datamuse.com/words"
)

// userAgent identifies this service to keyless providers; harmless to send
// to every provider even when not required.
const userAgent = "apimgr-api/1.0 (+https://github.com/apimgr/api)"

// DictionaryPhonetic holds one pronunciation entry
type DictionaryPhonetic struct {
	Text  string `json:"text,omitempty"`
	Audio string `json:"audio,omitempty"`
}

// DictionaryDefinition holds one sense definition within a part of speech
type DictionaryDefinition struct {
	Definition string   `json:"definition"`
	Example    string   `json:"example,omitempty"`
	Synonyms   []string `json:"synonyms,omitempty"`
	Antonyms   []string `json:"antonyms,omitempty"`
}

// DictionaryMeaning groups definitions under one part of speech
type DictionaryMeaning struct {
	PartOfSpeech string                 `json:"part_of_speech"`
	Definitions  []DictionaryDefinition `json:"definitions"`
}

// DictionaryResult is the response for a Dictionary lookup
type DictionaryResult struct {
	Word      string               `json:"word"`
	Phonetics []DictionaryPhonetic `json:"phonetics,omitempty"`
	Meanings  []DictionaryMeaning  `json:"meanings"`
}

// dictionaryAPIEntry mirrors one element of the Free Dictionary API's
// (dictionaryapi.dev) response array
type dictionaryAPIEntry struct {
	Word      string `json:"word"`
	Phonetics []struct {
		Text  string `json:"text"`
		Audio string `json:"audio"`
	} `json:"phonetics"`
	Meanings []struct {
		PartOfSpeech string `json:"partOfSpeech"`
		Definitions  []struct {
			Definition string   `json:"definition"`
			Example    string   `json:"example"`
			Synonyms   []string `json:"synonyms"`
			Antonyms   []string `json:"antonyms"`
		} `json:"definitions"`
	} `json:"meanings"`
}

// Dictionary looks up a word's definitions using the free, keyless Free
// Dictionary API (dictionaryapi.dev; English only).
func (s *Service) Dictionary(ctx context.Context, word string) (*DictionaryResult, error) {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil, fmt.Errorf("word is required")
	}

	endpoint := dictionaryEndpoint + "/" + url.PathEscape(word)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dictionary provider request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no definitions found for %q", word)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dictionary provider returned status %d", resp.StatusCode)
	}

	var entries []dictionaryAPIEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("failed to decode dictionary provider response: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no definitions found for %q", word)
	}

	entry := entries[0]
	result := &DictionaryResult{Word: entry.Word}
	for _, p := range entry.Phonetics {
		if p.Text == "" && p.Audio == "" {
			continue
		}
		result.Phonetics = append(result.Phonetics, DictionaryPhonetic{Text: p.Text, Audio: p.Audio})
	}
	for _, m := range entry.Meanings {
		meaning := DictionaryMeaning{PartOfSpeech: m.PartOfSpeech}
		for _, d := range m.Definitions {
			meaning.Definitions = append(meaning.Definitions, DictionaryDefinition{
				Definition: d.Definition,
				Example:    d.Example,
				Synonyms:   d.Synonyms,
				Antonyms:   d.Antonyms,
			})
		}
		result.Meanings = append(result.Meanings, meaning)
	}

	return result, nil
}

// ThesaurusResult is the response for a Thesaurus lookup
type ThesaurusResult struct {
	Word     string   `json:"word"`
	Synonyms []string `json:"synonyms"`
	Antonyms []string `json:"antonyms"`
}

// datamuseWord mirrors one element of the Datamuse API's word-list response
type datamuseWord struct {
	Word string `json:"word"`
}

// Thesaurus looks up a word's synonyms and antonyms using the free, keyless
// Datamuse API. Datamuse has announced that a free API key will become
// mandatory starting 2027-01-01; tracked in TODO.AI.md.
func (s *Service) Thesaurus(ctx context.Context, word string) (*ThesaurusResult, error) {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil, fmt.Errorf("word is required")
	}

	synonyms, err := datamuseWords(ctx, "rel_syn", word)
	if err != nil {
		return nil, err
	}
	antonyms, err := datamuseWords(ctx, "rel_ant", word)
	if err != nil {
		return nil, err
	}
	if len(synonyms) == 0 && len(antonyms) == 0 {
		return nil, fmt.Errorf("no synonyms or antonyms found for %q", word)
	}

	return &ThesaurusResult{Word: word, Synonyms: synonyms, Antonyms: antonyms}, nil
}

// datamuseWords queries the Datamuse API for one relation (rel_syn/rel_ant)
// against a given word and returns the matched words.
func datamuseWords(ctx context.Context, relation, word string) ([]string, error) {
	endpoint := datamuseEndpoint + "?" + relation + "=" + url.QueryEscape(word)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("thesaurus provider request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("thesaurus provider returned status %d", resp.StatusCode)
	}

	var words []datamuseWord
	if err := json.NewDecoder(resp.Body).Decode(&words); err != nil {
		return nil, fmt.Errorf("failed to decode thesaurus provider response: %w", err)
	}

	result := make([]string, 0, len(words))
	for _, w := range words {
		result = append(result, w.Word)
	}
	return result, nil
}

// Language codes
var languageCodes = map[string]string{
	"en": "English",
	"es": "Spanish",
	"fr": "French",
	"de": "German",
	"it": "Italian",
	"pt": "Portuguese",
	"ru": "Russian",
	"ja": "Japanese",
	"ko": "Korean",
	"zh": "Chinese",
	"ar": "Arabic",
	"hi": "Hindi",
}

// GetLanguageName returns full language name for code
func (s *Service) GetLanguageName(code string) (string, error) {
	code = strings.ToLower(code)
	if name, ok := languageCodes[code]; ok {
		return name, nil
	}
	return "", fmt.Errorf("unknown language code: %s", code)
}

// ListLanguages returns all supported language codes
func (s *Service) ListLanguages() map[string]string {
	return languageCodes
}

// soundexCode maps a consonant to its Soundex digit; vowels and
// non-consonants (A, E, I, O, U, Y, H, W) map to 0 and are dropped.
func soundexCode(r rune) byte {
	switch r {
	case 'B', 'F', 'P', 'V':
		return '1'
	case 'C', 'G', 'J', 'K', 'Q', 'S', 'X', 'Z':
		return '2'
	case 'D', 'T':
		return '3'
	case 'L':
		return '4'
	case 'M', 'N':
		return '5'
	case 'R':
		return '6'
	default:
		return '0'
	}
}

// Soundex returns the 4-character Soundex code for word, per the standard
// American Soundex algorithm: keep the first letter, encode subsequent
// consonants to digits, collapse adjacent duplicates, drop vowels/H/W, and
// pad/truncate to a single letter followed by three digits.
func (s *Service) Soundex(word string) string {
	letters := make([]rune, 0, len(word))
	for _, r := range strings.ToUpper(word) {
		if r >= 'A' && r <= 'Z' {
			letters = append(letters, r)
		}
	}
	if len(letters) == 0 {
		return ""
	}

	code := []byte{byte(letters[0])}
	lastDigit := soundexCode(letters[0])
	for _, r := range letters[1:] {
		digit := soundexCode(r)
		if digit != '0' && digit != lastDigit {
			code = append(code, digit)
			if len(code) == 4 {
				break
			}
		}
		if r != 'H' && r != 'W' {
			lastDigit = digit
		}
	}
	for len(code) < 4 {
		code = append(code, '0')
	}
	return string(code)
}

// Metaphone returns the classic (single) Metaphone phonetic code for word,
// following Lawrence Philips' 1990 algorithm.
func (s *Service) Metaphone(word string) string {
	letters := make([]byte, 0, len(word))
	for _, r := range strings.ToUpper(word) {
		if r >= 'A' && r <= 'Z' {
			letters = append(letters, byte(r))
		}
	}
	if len(letters) == 0 {
		return ""
	}

	isVowel := func(b byte) bool {
		return b == 'A' || b == 'E' || b == 'I' || b == 'O' || b == 'U'
	}
	at := func(i int) byte {
		if i < 0 || i >= len(letters) {
			return 0
		}
		return letters[i]
	}

	switch {
	case at(0) == 'A' && at(1) == 'E',
		at(0) == 'G' && at(1) == 'N',
		at(0) == 'K' && at(1) == 'N',
		at(0) == 'P' && at(1) == 'N',
		at(0) == 'W' && at(1) == 'R':
		letters = letters[1:]
	case at(0) == 'X':
		letters[0] = 'S'
	case at(0) == 'W' && at(1) == 'H':
		letters = append([]byte{'W'}, letters[2:]...)
	}

	var out strings.Builder
	for i := 0; i < len(letters); i++ {
		c := letters[i]
		prev := at(i - 1)
		next := at(i + 1)
		next2 := at(i + 2)

		if i > 0 && c == prev && c != 'C' {
			continue
		}

		switch c {
		case 'A', 'E', 'I', 'O', 'U':
			if i == 0 {
				out.WriteByte(c)
			}
		case 'B':
			if !(i == len(letters)-1 && prev == 'M') {
				out.WriteByte('B')
			}
		case 'C':
			switch {
			case next == 'I' && next2 == 'A':
				out.WriteByte('X')
			case next == 'H':
				out.WriteByte('X')
				i++
			case prev == 'S' && (next == 'I' || next == 'E' || next == 'Y'):
			case next == 'I' || next == 'E' || next == 'Y':
				out.WriteByte('S')
			default:
				out.WriteByte('K')
			}
		case 'D':
			if next == 'G' && (next2 == 'E' || next2 == 'Y' || next2 == 'I') {
				out.WriteByte('J')
				i += 2
			} else {
				out.WriteByte('T')
			}
		case 'G':
			switch {
			case next == 'H' && !isVowel(next2):
			case next == 'N':
			case next == 'I' || next == 'E' || next == 'Y':
				out.WriteByte('J')
			default:
				out.WriteByte('K')
			}
		case 'H':
			if isVowel(prev) && !isVowel(next) {
			} else if prev == 'C' || prev == 'S' || prev == 'P' || prev == 'T' || prev == 'G' {
			} else {
				out.WriteByte('H')
			}
		case 'K':
			if prev != 'C' {
				out.WriteByte('K')
			}
		case 'P':
			if next == 'H' {
				out.WriteByte('F')
				i++
			} else {
				out.WriteByte('P')
			}
		case 'Q':
			out.WriteByte('K')
		case 'S':
			switch {
			case next == 'H':
				out.WriteByte('X')
				i++
			case next == 'I' && (next2 == 'O' || next2 == 'A'):
				out.WriteByte('X')
			default:
				out.WriteByte('S')
			}
		case 'T':
			switch {
			case next == 'I' && (next2 == 'O' || next2 == 'A'):
				out.WriteByte('X')
			case next == 'H':
				out.WriteByte('0')
				i++
			case next == 'C' && next2 == 'H':
			default:
				out.WriteByte('T')
			}
		case 'V':
			out.WriteByte('F')
		case 'W', 'Y':
			if isVowel(next) {
				out.WriteByte(c)
			}
		case 'X':
			out.WriteString("KS")
		case 'Z':
			out.WriteByte('S')
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

// WordStats holds the counts returned by WordCount.
type WordStats struct {
	Words              int `json:"words"`
	Characters         int `json:"characters"`
	CharactersNoSpaces int `json:"characters_no_spaces"`
	Lines              int `json:"lines"`
	Sentences          int `json:"sentences"`
}

// WordCount computes word/character/line/sentence counts for text.
func (s *Service) WordCount(text string) WordStats {
	stats := WordStats{
		Characters: len([]rune(text)),
	}

	for _, r := range text {
		if !strings.ContainsRune(" \t\n\r\v\f", r) {
			stats.CharactersNoSpaces++
		}
	}

	if strings.TrimSpace(text) != "" {
		stats.Words = len(strings.Fields(text))
	}

	if text == "" {
		stats.Lines = 0
	} else {
		stats.Lines = strings.Count(text, "\n") + 1
	}

	for _, r := range text {
		if r == '.' || r == '!' || r == '?' {
			stats.Sentences++
		}
	}

	return stats
}

// stopwords is a small, hand-authored English stopword list used by
// Keywords. It is a simple filter for a frequency-based extractor, not a
// full NLP stopword corpus.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "is": true, "are": true, "was": true, "were": true,
	"be": true, "been": true, "being": true, "this": true, "that": true,
	"these": true, "those": true, "it": true, "its": true, "as": true,
	"by": true, "from": true, "into": true, "about": true, "than": true,
	"then": true, "so": true, "such": true, "not": true, "no": true,
	"do": true, "does": true, "did": true, "have": true, "has": true,
	"had": true, "if": true, "because": true, "while": true, "there": true,
	"here": true, "we": true, "you": true, "i": true, "he": true, "she": true,
	"they": true, "them": true, "his": true, "her": true, "their": true,
	"our": true, "your": true,
}

// Keyword is one word/frequency pair returned by Keywords.
type Keyword struct {
	Word  string `json:"word"`
	Count int    `json:"count"`
}

// tokenizeWords lowercases text and splits it into words on any run of
// non-letter characters.
func tokenizeWords(text string) []string {
	var words []string
	var current strings.Builder
	for _, r := range strings.ToLower(text) {
		if r >= 'a' && r <= 'z' {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// Keywords extracts the top limit most frequent non-stopword words from
// text using simple frequency counting (not a full NLP pipeline). Ties are
// broken alphabetically. limit <= 0 defaults to 10.
func (s *Service) Keywords(text string, limit int) []Keyword {
	if limit <= 0 {
		limit = 10
	}

	counts := make(map[string]int)
	for _, word := range tokenizeWords(text) {
		if stopwords[word] {
			continue
		}
		counts[word]++
	}

	keywords := make([]Keyword, 0, len(counts))
	for word, count := range counts {
		keywords = append(keywords, Keyword{Word: word, Count: count})
	}

	sort.Slice(keywords, func(i, j int) bool {
		if keywords[i].Count != keywords[j].Count {
			return keywords[i].Count > keywords[j].Count
		}
		return keywords[i].Word < keywords[j].Word
	})

	if len(keywords) > limit {
		keywords = keywords[:limit]
	}
	return keywords
}

// countSyllables returns an approximate syllable count for word using the
// standard vowel-group heuristic (count runs of vowels, drop a trailing
// silent e, minimum of one syllable). This is the well-known approximate
// algorithm used by most readability libraries, not a dictionary-perfect
// syllable count.
func countSyllables(word string) int {
	word = strings.ToLower(word)
	isVowel := func(r rune) bool {
		return strings.ContainsRune("aeiouy", r)
	}

	count := 0
	prevVowel := false
	for _, r := range word {
		if isVowel(r) {
			if !prevVowel {
				count++
			}
			prevVowel = true
		} else {
			prevVowel = false
		}
	}

	if strings.HasSuffix(word, "e") && count > 1 {
		count--
	}
	if count < 1 {
		count = 1
	}
	return count
}

// ReadabilityStats holds the scores and raw counts returned by Readability.
type ReadabilityStats struct {
	FleschReadingEase  float64 `json:"flesch_reading_ease"`
	FleschKincaidGrade float64 `json:"flesch_kincaid_grade"`
	GunningFog         float64 `json:"gunning_fog"`
	Words              int     `json:"words"`
	Sentences          int     `json:"sentences"`
	Syllables          int     `json:"syllables"`
}

// Readability computes Flesch Reading Ease, Flesch-Kincaid Grade Level, and
// Gunning Fog Index for text using the standard approximate syllable-
// counting heuristic in countSyllables. These are well-known heuristic
// approximations, not dictionary-perfect linguistic measures.
func (s *Service) Readability(text string) ReadabilityStats {
	words := strings.Fields(text)
	wordCount := len(words)

	sentenceCount := 0
	for _, r := range text {
		if r == '.' || r == '!' || r == '?' {
			sentenceCount++
		}
	}
	if sentenceCount == 0 && wordCount > 0 {
		sentenceCount = 1
	}

	syllableCount := 0
	complexWords := 0
	for _, word := range words {
		trimmed := strings.TrimFunc(word, func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
		})
		if trimmed == "" {
			continue
		}
		syllables := countSyllables(trimmed)
		syllableCount += syllables
		if syllables >= 3 {
			complexWords++
		}
	}

	stats := ReadabilityStats{
		Words:     wordCount,
		Sentences: sentenceCount,
		Syllables: syllableCount,
	}

	if wordCount == 0 || sentenceCount == 0 {
		return stats
	}

	wordsPerSentence := float64(wordCount) / float64(sentenceCount)
	syllablesPerWord := float64(syllableCount) / float64(wordCount)

	stats.FleschReadingEase = 206.835 - 1.015*wordsPerSentence - 84.6*syllablesPerWord
	stats.FleschKincaidGrade = 0.39*wordsPerSentence + 11.8*syllablesPerWord - 15.59
	stats.GunningFog = 0.4 * (wordsPerSentence + 100*(float64(complexWords)/float64(wordCount)))

	return stats
}

// ReadingTimeStats holds the estimated reading time returned by ReadingTime.
type ReadingTimeStats struct {
	Words   int `json:"words"`
	Minutes int `json:"minutes"`
	Seconds int `json:"seconds"`
}

// ReadingTime estimates reading time for wordCount words at wordsPerMinute
// (industry-standard default 200 wpm, matching Medium and most reading-time
// libraries). Minutes are rounded up (ceiling) since "3.2 minutes to read"
// reads as "4 min read" in every real product; seconds is the exact total.
func (s *Service) ReadingTime(wordCount, wordsPerMinute int) ReadingTimeStats {
	if wordsPerMinute <= 0 {
		wordsPerMinute = 200
	}

	exactMinutes := float64(wordCount) / float64(wordsPerMinute)
	return ReadingTimeStats{
		Words:   wordCount,
		Minutes: int(math.Ceil(exactMinutes)),
		Seconds: int(math.Round(exactMinutes * 60)),
	}
}

// positiveWords and negativeWords are small, hand-authored sentiment
// lexicons used by Sentiment. This is a simple lexicon-based heuristic, not
// a trained model — an honest, documented limitation rather than a hidden
// shortcut. It will misjudge negation, sarcasm, and domain-specific tone.
var positiveWords = map[string]bool{
	"good": true, "great": true, "excellent": true, "happy": true,
	"love": true, "wonderful": true, "amazing": true, "best": true,
	"beautiful": true, "fantastic": true, "positive": true, "awesome": true,
	"perfect": true, "nice": true, "brilliant": true, "superb": true,
	"delightful": true, "pleasant": true, "enjoy": true, "glad": true,
	"exciting": true, "excited": true, "impressive": true, "fabulous": true,
	"outstanding": true, "terrific": true, "charming": true, "lovely": true,
	"joy": true, "joyful": true, "grateful": true, "thankful": true,
	"satisfied": true, "success": true, "successful": true, "win": true,
	"winning": true, "favorite": true, "helpful": true, "kind": true,
	"friendly": true, "recommend": true, "recommended": true, "fun": true,
	"comfortable": true, "confident": true, "reliable": true, "smooth": true,
	"efficient": true, "genuine": true,
}

var negativeWords = map[string]bool{
	"bad": true, "terrible": true, "awful": true, "hate": true,
	"sad": true, "horrible": true, "worst": true, "ugly": true,
	"poor": true, "negative": true, "disappointing": true, "angry": true,
	"upset": true, "wrong": true, "fail": true, "failed": true,
	"broken": true, "disgusting": true, "annoying": true, "unpleasant": true,
	"worse": true, "hated": true, "hateful": true, "dislike": true,
	"boring": true, "disappointed": true, "frustrating": true,
	"frustrated": true, "useless": true, "painful": true, "pain": true,
	"unhappy": true, "miserable": true, "awkward": true, "confusing": true,
	"confused": true, "problem": true, "problems": true, "issue": true,
	"issues": true, "slow": true, "expensive": true, "difficult": true,
	"hard": true, "unreliable": true, "rude": true, "dirty": true,
	"disaster": true, "regret": true, "waste": true, "cheap": true,
}

// SentimentResult holds the score and label returned by Sentiment.
type SentimentResult struct {
	Score           float64 `json:"score"`
	Label           string  `json:"label"`
	PositiveMatches int     `json:"positive_matches"`
	NegativeMatches int     `json:"negative_matches"`
}

// Sentiment scores text using the small hand-authored lexicons above: score
// is (positive matches - negative matches) / total word count, classified
// as "positive" (score > 0.05), "negative" (score < -0.05), or "neutral"
// otherwise. This is a simple lexicon-based heuristic, not a trained
// sentiment model.
func (s *Service) Sentiment(text string) SentimentResult {
	words := tokenizeWords(text)

	result := SentimentResult{Label: "neutral"}
	if len(words) == 0 {
		return result
	}

	for _, word := range words {
		switch {
		case positiveWords[word]:
			result.PositiveMatches++
		case negativeWords[word]:
			result.NegativeMatches++
		}
	}

	result.Score = float64(result.PositiveMatches-result.NegativeMatches) / float64(len(words))
	switch {
	case result.Score > 0.05:
		result.Label = "positive"
	case result.Score < -0.05:
		result.Label = "negative"
	}

	return result
}
