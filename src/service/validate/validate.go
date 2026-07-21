package validate

import (
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Service provides validation utilities
type Service struct{}

// New creates a new Validate service
func New() *Service {
	return &Service{}
}

// Email validation
func (s *Service) IsEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// URL validation
func (s *Service) IsURL(urlStr string) bool {
	u, err := url.ParseRequestURI(urlStr)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// IP validation
func (s *Service) IsIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

func (s *Service) IsIPv4(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.To4() != nil
}

func (s *Service) IsIPv6(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.To4() == nil
}

// Domain validation
func (s *Service) IsDomain(domain string) bool {
	domainRegex := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
	return domainRegex.MatchString(domain)
}

// Phone validation (basic)
func (s *Service) IsPhone(phone string) bool {
	phoneRegex := regexp.MustCompile(`^\+?[1-9]\d{1,14}$`)
	digits := regexp.MustCompile(`\d+`).FindAllString(phone, -1)
	digitStr := strings.Join(digits, "")
	return phoneRegex.MatchString(digitStr)
}

// Credit card validation (Luhn algorithm)
func (s *Service) IsCreditCard(number string) bool {
	// Remove spaces and dashes
	number = strings.ReplaceAll(number, " ", "")
	number = strings.ReplaceAll(number, "-", "")

	if len(number) < 13 || len(number) > 19 {
		return false
	}

	sum := 0
	isSecond := false

	for i := len(number) - 1; i >= 0; i-- {
		digit := int(number[i] - '0')

		if isSecond {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}

		sum += digit
		isSecond = !isSecond
	}

	return sum%10 == 0
}

// String validations
func (s *Service) IsAlpha(str string) bool {
	for _, r := range str {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return len(str) > 0
}

func (s *Service) IsAlphanumeric(str string) bool {
	for _, r := range str {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return len(str) > 0
}

func (s *Service) IsNumeric(str string) bool {
	for _, r := range str {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(str) > 0
}

func (s *Service) IsLowercase(str string) bool {
	for _, r := range str {
		if unicode.IsUpper(r) {
			return false
		}
	}
	return len(str) > 0
}

func (s *Service) IsUppercase(str string) bool {
	for _, r := range str {
		if unicode.IsLower(r) {
			return false
		}
	}
	return len(str) > 0
}

// JSON validation
func (s *Service) IsJSON(str string) bool {
	str = strings.TrimSpace(str)
	return (strings.HasPrefix(str, "{") && strings.HasSuffix(str, "}")) ||
		(strings.HasPrefix(str, "[") && strings.HasSuffix(str, "]"))
}

// UUID validation
func (s *Service) IsUUID(str string) bool {
	uuidRegex := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	return uuidRegex.MatchString(str)
}

// MAC address validation
func (s *Service) IsMAC(mac string) bool {
	_, err := net.ParseMAC(mac)
	return err == nil
}

// Password strength
func (s *Service) PasswordStrength(password string) string {
	if len(password) < 8 {
		return "weak"
	}

	hasUpper := false
	hasLower := false
	hasDigit := false
	hasSpecial := false

	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	strength := 0
	if hasUpper {
		strength++
	}
	if hasLower {
		strength++
	}
	if hasDigit {
		strength++
	}
	if hasSpecial {
		strength++
	}

	if strength >= 4 && len(password) >= 12 {
		return "strong"
	} else if strength >= 3 {
		return "medium"
	}
	return "weak"
}

// Length validations
func (s *Service) MinLength(str string, min int) bool {
	return len(str) >= min
}

func (s *Service) MaxLength(str string, max int) bool {
	return len(str) <= max
}

func (s *Service) LengthBetween(str string, min, max int) bool {
	l := len(str)
	return l >= min && l <= max
}
