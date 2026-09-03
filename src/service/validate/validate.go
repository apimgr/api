package validate

import (
	"fmt"
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

// IsIBAN validates an International Bank Account Number using the ISO
// 13616 mod-97 checksum (rearrange, letters -> digits A=10..Z=35, mod 97 == 1)
func (s *Service) IsIBAN(iban string) bool {
	iban = strings.ToUpper(strings.ReplaceAll(iban, " ", ""))

	ibanFormat := regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}$`)
	if !ibanFormat.MatchString(iban) {
		return false
	}

	rearranged := iban[4:] + iban[:4]

	var numeric strings.Builder
	for _, r := range rearranged {
		switch {
		case r >= '0' && r <= '9':
			numeric.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			numeric.WriteString(fmt.Sprintf("%d", int(r-'A')+10))
		default:
			return false
		}
	}

	remainder := 0
	digits := numeric.String()
	for i := 0; i < len(digits); i++ {
		remainder = (remainder*10 + int(digits[i]-'0')) % 97
	}

	return remainder == 1
}

// IsISBN validates an ISBN-10 (check digit mod 11, X = 10) or ISBN-13
// (check digit mod 10, alternating weights 1/3) book number
func (s *Service) IsISBN(isbn string) bool {
	cleaned := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(isbn, "-", ""), " ", ""))

	switch len(cleaned) {
	case 10:
		sum := 0
		for i := 0; i < 10; i++ {
			var digit int
			if cleaned[i] == 'X' && i == 9 {
				digit = 10
			} else if cleaned[i] >= '0' && cleaned[i] <= '9' {
				digit = int(cleaned[i] - '0')
			} else {
				return false
			}
			sum += digit * (10 - i)
		}
		return sum%11 == 0
	case 13:
		sum := 0
		for i := 0; i < 13; i++ {
			if cleaned[i] < '0' || cleaned[i] > '9' {
				return false
			}
			digit := int(cleaned[i] - '0')
			if i%2 == 0 {
				sum += digit
			} else {
				sum += digit * 3
			}
		}
		return sum%10 == 0
	default:
		return false
	}
}

// vatCountryFormats maps EU (plus UK, CH, NO) VAT prefixes to the fixed
// number of alphanumeric characters that follow the 2-letter country code
var vatCountryFormats = map[string]int{
	"AT": 9, "BE": 10, "BG": 10, "CH": 9, "CY": 9, "CZ": 9, "DE": 9,
	"DK": 8, "EE": 9, "EL": 9, "ES": 9, "FI": 8, "FR": 11, "GB": 9,
	"HR": 11, "HU": 8, "IE": 9, "IT": 11, "LT": 9, "LU": 8, "LV": 11,
	"MT": 8, "NL": 12, "NO": 9, "PL": 10, "PT": 9, "RO": 10, "SE": 12,
	"SI": 8, "SK": 10,
}

// IsVAT validates an EU/UK/CH/NO VAT registration number's structural
// format: a known 2-letter country prefix followed by that country's
// fixed-length alphanumeric body (no country-specific checksum is applied)
func (s *Service) IsVAT(vat string) bool {
	cleaned := strings.ToUpper(strings.ReplaceAll(vat, " ", ""))
	if len(cleaned) < 3 {
		return false
	}

	prefix := cleaned[:2]
	body := cleaned[2:]

	expectedLen, ok := vatCountryFormats[prefix]
	if !ok {
		return false
	}
	if len(body) != expectedLen {
		return false
	}

	bodyFormat := regexp.MustCompile(`^[A-Z0-9]+$`)
	return bodyFormat.MatchString(body)
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
