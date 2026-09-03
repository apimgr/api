package validate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEmail(t *testing.T) {
	s := New()
	cases := []struct {
		in   string
		want bool
	}{
		{"user@example.com", true},
		{"a.b+c@sub.example.co.uk", true},
		{"not-an-email", false},
		{"", false},
		{"@example.com", false},
		{"user@", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, s.IsEmail(c.in), "IsEmail(%q)", c.in)
	}
}

func TestIsURL(t *testing.T) {
	s := New()
	cases := []struct {
		in   string
		want bool
	}{
		{"https://example.com", true},
		{"http://example.com/path?query=1", true},
		{"not a url", false},
		{"", false},
		{"/relative/path", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, s.IsURL(c.in), "IsURL(%q)", c.in)
	}
}

func TestIsIP(t *testing.T) {
	s := New()
	assert.True(t, s.IsIP("192.168.1.1"))
	assert.True(t, s.IsIP("::1"))
	assert.False(t, s.IsIP("not-an-ip"))
	assert.False(t, s.IsIP(""))
}

func TestIsIPv4AndIPv6(t *testing.T) {
	s := New()
	assert.True(t, s.IsIPv4("10.0.0.1"))
	assert.False(t, s.IsIPv4("::1"))
	assert.False(t, s.IsIPv4("not-an-ip"))

	assert.True(t, s.IsIPv6("::1"))
	assert.False(t, s.IsIPv6("10.0.0.1"))
	assert.False(t, s.IsIPv6("not-an-ip"))
}

func TestIsDomain(t *testing.T) {
	s := New()
	cases := []struct {
		in   string
		want bool
	}{
		{"example.com", true},
		{"sub.example.co.uk", true},
		{"example", false},
		{"", false},
		{"-example.com", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, s.IsDomain(c.in), "IsDomain(%q)", c.in)
	}
}

func TestIsPhone(t *testing.T) {
	s := New()
	assert.True(t, s.IsPhone("+15551234567"))
	assert.True(t, s.IsPhone("15551234567"))
	// Leading zero digit strings fail the phone regex (no leading zero allowed).
	assert.False(t, s.IsPhone("0"))
	assert.False(t, s.IsPhone(""))
}

func TestIsCreditCard(t *testing.T) {
	s := New()
	cases := []struct {
		in   string
		want bool
	}{
		// Valid Visa test number (passes Luhn check).
		{"4111111111111111", true},
		{"4111-1111-1111-1111", true},
		{"4111 1111 1111 1111", true},
		{"1234567890123", false},
		{"123", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, s.IsCreditCard(c.in), "IsCreditCard(%q)", c.in)
	}
}

func TestIsAlpha(t *testing.T) {
	s := New()
	assert.True(t, s.IsAlpha("abcXYZ"))
	assert.False(t, s.IsAlpha("abc123"))
	assert.False(t, s.IsAlpha(""))
}

func TestIsAlphanumeric(t *testing.T) {
	s := New()
	assert.True(t, s.IsAlphanumeric("abc123"))
	assert.False(t, s.IsAlphanumeric("abc-123"))
	assert.False(t, s.IsAlphanumeric(""))
}

func TestIsNumeric(t *testing.T) {
	s := New()
	assert.True(t, s.IsNumeric("12345"))
	assert.False(t, s.IsNumeric("123a5"))
	assert.False(t, s.IsNumeric(""))
}

func TestIsLowercaseUppercase(t *testing.T) {
	s := New()
	assert.True(t, s.IsLowercase("abc"))
	assert.False(t, s.IsLowercase("Abc"))
	assert.False(t, s.IsLowercase(""))

	assert.True(t, s.IsUppercase("ABC"))
	assert.False(t, s.IsUppercase("Abc"))
	assert.False(t, s.IsUppercase(""))
}

func TestIsJSON(t *testing.T) {
	s := New()
	assert.True(t, s.IsJSON(`{"a":1}`))
	assert.True(t, s.IsJSON(`[1,2,3]`))
	assert.True(t, s.IsJSON("  {\"a\":1}  "))
	assert.False(t, s.IsJSON("not json"))
	assert.False(t, s.IsJSON(""))
}

func TestIsUUID(t *testing.T) {
	s := New()
	assert.True(t, s.IsUUID("123e4567-e89b-12d3-a456-426614174000"))
	assert.False(t, s.IsUUID("not-a-uuid"))
	assert.False(t, s.IsUUID(""))
}

func TestIsMAC(t *testing.T) {
	s := New()
	assert.True(t, s.IsMAC("00:1A:2B:3C:4D:5E"))
	assert.False(t, s.IsMAC("not-a-mac"))
	assert.False(t, s.IsMAC(""))
}

func TestPasswordStrength(t *testing.T) {
	s := New()
	cases := []struct {
		in   string
		want string
	}{
		{"short", "weak"},
		{"alllowercase", "weak"},
		// Upper+lower+digit = 3 categories -> medium.
		{"Password1", "medium"},
		// Upper+lower+digit+special and length >= 12 -> strong.
		{"Password123!@#", "strong"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, s.PasswordStrength(c.in), "PasswordStrength(%q)", c.in)
	}
}

func TestLengthValidations(t *testing.T) {
	s := New()
	assert.True(t, s.MinLength("hello", 3))
	assert.False(t, s.MinLength("hi", 3))

	assert.True(t, s.MaxLength("hi", 5))
	assert.False(t, s.MaxLength("hello world", 5))

	assert.True(t, s.LengthBetween("hello", 3, 10))
	assert.False(t, s.LengthBetween("hi", 3, 10))
	assert.False(t, s.LengthBetween(strings.Repeat("a", 20), 3, 10))
}
