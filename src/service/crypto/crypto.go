package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"hash"
	"math/big"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// BcryptHash creates a bcrypt hash
func BcryptHash(password string, cost int) (string, error) {
	if cost < 4 || cost > 31 {
		cost = 12
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// BcryptVerify verifies a bcrypt hash
func BcryptVerify(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// BcryptCost extracts the cost from a bcrypt hash
func BcryptCost(hash string) int {
	cost, _ := bcrypt.Cost([]byte(hash))
	return cost
}

// GeneratePassword generates a random password
func GeneratePassword(length int, options PasswordOptions) (string, error) {
	if length < 4 {
		length = 16
	}
	if length > 256 {
		length = 256
	}

	var charset string

	if options.Uppercase {
		charset += "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	}
	if options.Lowercase {
		charset += "abcdefghijklmnopqrstuvwxyz"
	}
	if options.Numbers {
		charset += "0123456789"
	}
	if options.Symbols {
		charset += "!@#$%^&*()_+-=[]{}|;:,.<>?"
	}

	if charset == "" {
		charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	}

	// Remove similar characters if requested
	if options.ExcludeSimilar {
		charset = strings.ReplaceAll(charset, "0", "")
		charset = strings.ReplaceAll(charset, "O", "")
		charset = strings.ReplaceAll(charset, "1", "")
		charset = strings.ReplaceAll(charset, "l", "")
		charset = strings.ReplaceAll(charset, "I", "")
	}

	password := make([]byte, length)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", err
		}
		password[i] = charset[num.Int64()]
	}

	return string(password), nil
}

type PasswordOptions struct {
	Uppercase      bool
	Lowercase      bool
	Numbers        bool
	Symbols        bool
	ExcludeSimilar bool
}

// DefaultPasswordOptions returns default password options
func DefaultPasswordOptions() PasswordOptions {
	return PasswordOptions{
		Uppercase:      true,
		Lowercase:      true,
		Numbers:        true,
		Symbols:        true,
		ExcludeSimilar: false,
	}
}

// GeneratePIN generates a random PIN
func GeneratePIN(length int) (string, error) {
	if length < 3 {
		length = 4
	}
	if length > 12 {
		length = 12
	}

	pin := make([]byte, length)
	charsetLen := big.NewInt(10)

	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", err
		}
		pin[i] = byte('0' + num.Int64())
	}

	return string(pin), nil
}

// RandomBytes generates random bytes
func RandomBytes(count int) ([]byte, error) {
	if count <= 0 {
		count = 32
	}
	if count > 10000 {
		count = 10000
	}

	bytes := make([]byte, count)
	_, err := rand.Read(bytes)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

// TOTP

// GenerateTOTPSecret generates a new TOTP secret
func GenerateTOTPSecret(length int) (string, error) {
	if length < 16 {
		length = 20
	}
	if length > 64 {
		length = 64
	}

	bytes := make([]byte, length)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}

	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes), nil
}

// GenerateTOTP generates a TOTP code
func GenerateTOTP(secret string, digits int, period int64) (string, error) {
	if digits < 6 {
		digits = 6
	}
	if digits > 8 {
		digits = 8
	}
	if period <= 0 {
		period = 30
	}

	// Decode secret
	secret = strings.ToUpper(strings.TrimSpace(secret))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		// Try with padding
		key, err = base32.StdEncoding.DecodeString(secret)
		if err != nil {
			return "", fmt.Errorf("invalid secret")
		}
	}

	// Calculate time counter
	counter := uint64(time.Now().Unix() / period)

	// Generate HOTP
	return generateHOTP(key, counter, digits), nil
}

// VerifyTOTP verifies a TOTP code
func VerifyTOTP(secret, code string, digits int, period int64, window int) bool {
	if window <= 0 {
		window = 1
	}

	// Check current and adjacent time periods
	for i := -window; i <= window; i++ {
		expectedCode, err := generateTOTPAtOffset(secret, digits, period, int64(i))
		if err != nil {
			continue
		}
		if expectedCode == code {
			return true
		}
	}

	return false
}

func generateTOTPAtOffset(secret string, digits int, period int64, offset int64) (string, error) {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		key, err = base32.StdEncoding.DecodeString(secret)
		if err != nil {
			return "", fmt.Errorf("invalid secret")
		}
	}

	counter := uint64((time.Now().Unix() / period) + offset)
	return generateHOTP(key, counter, digits), nil
}

func generateHOTP(key []byte, counter uint64, digits int) string {
	// Convert counter to bytes
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	// Generate HMAC-SHA1
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	hash := mac.Sum(nil)

	// Dynamic truncation
	offset := hash[len(hash)-1] & 0x0F
	truncated := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7FFFFFFF

	// Generate OTP
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	otp := truncated % mod

	return fmt.Sprintf("%0*d", digits, otp)
}

// GenerateTOTPURI generates an otpauth URI
func GenerateTOTPURI(secret, issuer, account string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		issuer, account, secret, issuer)
}

// HMAC

// HMACGenerate generates an HMAC
func HMACGenerate(algorithm, key, message string) (string, error) {
	var mac hash.Hash

	switch strings.ToLower(algorithm) {
	case "sha1":
		mac = hmac.New(sha1.New, []byte(key))
	case "sha256":
		mac = hmac.New(sha256.New, []byte(key))
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", algorithm)
	}

	mac.Write([]byte(message))

	return fmt.Sprintf("%x", mac.Sum(nil)), nil
}

// PasswordStrength analyzes password strength
func PasswordStrength(password string) map[string]interface{} {
	length := len(password)

	hasUpper := strings.ContainsAny(password, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	hasLower := strings.ContainsAny(password, "abcdefghijklmnopqrstuvwxyz")
	hasDigit := strings.ContainsAny(password, "0123456789")
	hasSymbol := strings.ContainsAny(password, "!@#$%^&*()_+-=[]{}|;:,.<>?")

	charsetSize := 0
	if hasLower {
		charsetSize += 26
	}
	if hasUpper {
		charsetSize += 26
	}
	if hasDigit {
		charsetSize += 10
	}
	if hasSymbol {
		charsetSize += 32
	}

	entropy := float64(length) * (float64(charsetSize) / 4.0) // Simplified entropy calculation

	var strength string
	var score int

	switch {
	case entropy >= 100:
		strength = "very_strong"
		score = 5
	case entropy >= 80:
		strength = "strong"
		score = 4
	case entropy >= 60:
		strength = "good"
		score = 3
	case entropy >= 40:
		strength = "fair"
		score = 2
	default:
		strength = "weak"
		score = 1
	}

	return map[string]interface{}{
		"score":          score,
		"strength":       strength,
		"length":         length,
		"entropy_bits":   entropy,
		"has_uppercase":  hasUpper,
		"has_lowercase":  hasLower,
		"has_numbers":    hasDigit,
		"has_symbols":    hasSymbol,
		"charset_size":   charsetSize,
	}
}
