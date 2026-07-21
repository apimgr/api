package generate

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service provides generation utilities
type Service struct{}

// New creates a new Generate service
func New() *Service {
	return &Service{}
}

// UUID generation
func (s *Service) UUID() string {
	return uuid.New().String()
}

func (s *Service) UUIDv4() string {
	return uuid.New().String()
}

// Random string generation
func (s *Service) RandomString(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	return s.randomStringFromCharset(length, charset)
}

func (s *Service) RandomAlpha(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	return s.randomStringFromCharset(length, charset)
}

func (s *Service) RandomNumeric(length int) (string, error) {
	const charset = "0123456789"
	return s.randomStringFromCharset(length, charset)
}

func (s *Service) RandomAlphanumeric(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	return s.randomStringFromCharset(length, charset)
}

func (s *Service) randomStringFromCharset(length int, charset string) (string, error) {
	result := make([]byte, length)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := range result {
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", err
		}
		result[i] = charset[num.Int64()]
	}

	return string(result), nil
}

// Password generation
func (s *Service) Password(length int, includeSpecial bool) (string, error) {
	if length < 8 {
		length = 8
	}

	charset := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if includeSpecial {
		charset += "!@#$%^&*()_+-=[]{}|;:,.<>?"
	}

	password, err := s.randomStringFromCharset(length, charset)
	if err != nil {
		return "", err
	}

	// Ensure at least one of each required type
	hasLower := false
	hasUpper := false
	hasDigit := false
	hasSpecial := false

	for _, r := range password {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}

	// If missing required characters, regenerate
	if !hasLower || !hasUpper || !hasDigit || (includeSpecial && !hasSpecial) {
		return s.Password(length, includeSpecial)
	}

	return password, nil
}

// Random bytes
func (s *Service) RandomBytes(length int) ([]byte, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}

func (s *Service) RandomHex(length int) (string, error) {
	bytes, err := s.RandomBytes(length)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// Token generation
func (s *Service) Token(length int) (string, error) {
	return s.RandomHex(length / 2) // hex encoding doubles length
}

func (s *Service) APIKey() (string, error) {
	bytes, err := s.RandomBytes(32)
	if err != nil {
		return "", err
	}
	return "key_" + hex.EncodeToString(bytes), nil
}

// Slug generation
func (s *Service) Slug(text string) string {
	// Convert to lowercase
	slug := strings.ToLower(text)

	// Replace spaces and special chars with hyphens
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, slug)

	// Remove multiple consecutive hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}

	// Trim hyphens from ends
	slug = strings.Trim(slug, "-")

	return slug
}

// Color generation
func (s *Service) RandomColor() string {
	r, _ := rand.Int(rand.Reader, big.NewInt(256))
	g, _ := rand.Int(rand.Reader, big.NewInt(256))
	b, _ := rand.Int(rand.Reader, big.NewInt(256))
	return fmt.Sprintf("#%02x%02x%02x", r.Int64(), g.Int64(), b.Int64())
}

// MAC address generation
func (s *Service) RandomMAC() (string, error) {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Set locally administered bit
	bytes[0] |= 2
	// Clear multicast bit
	bytes[0] &= 254
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5]), nil
}

// IP address generation
func (s *Service) RandomIPv4() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d.%d", bytes[0], bytes[1], bytes[2], bytes[3]), nil
}

// Timestamp generation
func (s *Service) Timestamp() int64 {
	return time.Now().Unix()
}

func (s *Service) TimestampMillis() int64 {
	return time.Now().UnixMilli()
}

// Nonce generation
func (s *Service) Nonce() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
