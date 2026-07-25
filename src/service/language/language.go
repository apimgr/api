package language

import (
	"fmt"
	"strings"
)

// Service provides language/translation utilities
type Service struct{}

// New creates a new Language service
func New() *Service {
	return &Service{}
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
