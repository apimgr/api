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
