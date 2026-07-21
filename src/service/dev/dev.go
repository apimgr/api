package dev

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Service provides development utilities
type Service struct{}

// New creates a new Dev service
func New() *Service {
	return &Service{}
}

// Code formatting
func (s *Service) FormatJSON(jsonStr string) (string, error) {
	var obj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return "", err
	}
	formatted, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", err
	}
	return string(formatted), nil
}

func (s *Service) MinifyJSON(jsonStr string) (string, error) {
	var obj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return "", err
	}
	minified, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(minified), nil
}

// String manipulation for code
func (s *Service) ToCamelCase(str string) string {
	words := strings.FieldsFunc(str, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	if len(words) == 0 {
		return ""
	}

	result := strings.ToLower(words[0])
	for _, word := range words[1:] {
		if len(word) > 0 {
			result += strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	return result
}

func (s *Service) ToPascalCase(str string) string {
	words := strings.FieldsFunc(str, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	var result string
	for _, word := range words {
		if len(word) > 0 {
			result += strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	return result
}

func (s *Service) ToSnakeCase(str string) string {
	// Handle camelCase and PascalCase
	re := regexp.MustCompile("([a-z0-9])([A-Z])")
	str = re.ReplaceAllString(str, "${1}_${2}")

	// Handle spaces and hyphens
	str = strings.ReplaceAll(str, " ", "_")
	str = strings.ReplaceAll(str, "-", "_")

	// Convert to lowercase
	return strings.ToLower(str)
}

func (s *Service) ToKebabCase(str string) string {
	// Handle camelCase and PascalCase
	re := regexp.MustCompile("([a-z0-9])([A-Z])")
	str = re.ReplaceAllString(str, "${1}-${2}")

	// Handle spaces and underscores
	str = strings.ReplaceAll(str, " ", "-")
	str = strings.ReplaceAll(str, "_", "-")

	// Convert to lowercase
	return strings.ToLower(str)
}

func (s *Service) ToConstantCase(str string) string {
	return strings.ToUpper(s.ToSnakeCase(str))
}

// Code escaping
func (s *Service) EscapeHTML(str string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(str)
}

func (s *Service) UnescapeHTML(str string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&apos;", "'",
	)
	return replacer.Replace(str)
}

func (s *Service) EscapeSQL(str string) string {
	return strings.ReplaceAll(str, "'", "''")
}

func (s *Service) EscapeRegex(str string) string {
	special := []string{".", "+", "*", "?", "^", "$", "(", ")", "[", "]", "{", "}", "|", "\\"}
	result := str
	for _, char := range special {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}
	return result
}

// Comment formatting
func (s *Service) AddLineComments(code string, commentStyle string) string {
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, commentStyle+" "+line)
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func (s *Service) RemoveLineComments(code string, commentStyle string) string {
	lines := strings.Split(code, "\n")
	var result []string

	prefix := commentStyle + " "
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			result = append(result, strings.TrimPrefix(trimmed, prefix))
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// Indentation
func (s *Service) Indent(code string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, indent+line)
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func (s *Service) Dedent(code string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			result = append(result, strings.TrimPrefix(line, prefix))
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// Template string
func (s *Service) TemplateReplace(template string, values map[string]string) string {
	result := template
	for key, value := range values {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// Line operations
func (s *Service) CountLines(code string) int {
	return len(strings.Split(code, "\n"))
}

func (s *Service) RemoveEmptyLines(code string) string {
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func (s *Service) NumberLines(code string) string {
	lines := strings.Split(code, "\n")
	var result []string

	for i, line := range lines {
		result = append(result, fmt.Sprintf("%4d | %s", i+1, line))
	}

	return strings.Join(result, "\n")
}
