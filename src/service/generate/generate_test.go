package generate

import (
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUUID(t *testing.T) {
	s := New()
	uuidRegex := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	id1 := s.UUID()
	assert.Regexp(t, uuidRegex, id1)

	id2 := s.UUIDv4()
	assert.Regexp(t, uuidRegex, id2)

	assert.NotEqual(t, id1, id2)
}

func TestRandomStringVariants(t *testing.T) {
	s := New()

	str, err := s.RandomString(20)
	require.NoError(t, err)
	assert.Len(t, str, 20)

	alpha, err := s.RandomAlpha(20)
	require.NoError(t, err)
	assert.Len(t, alpha, 20)
	assert.Regexp(t, `^[a-zA-Z]+$`, alpha)

	numeric, err := s.RandomNumeric(20)
	require.NoError(t, err)
	assert.Len(t, numeric, 20)
	assert.Regexp(t, `^[0-9]+$`, numeric)

	alnum, err := s.RandomAlphanumeric(20)
	require.NoError(t, err)
	assert.Len(t, alnum, 20)
	assert.Regexp(t, `^[a-zA-Z0-9]+$`, alnum)

	// Zero length is a valid boundary case producing an empty string.
	zero, err := s.RandomString(0)
	require.NoError(t, err)
	assert.Equal(t, "", zero)
}

func TestPassword(t *testing.T) {
	s := New()

	pw, err := s.Password(16, true)
	require.NoError(t, err)
	assert.Len(t, pw, 16)
	assert.Regexp(t, `[a-z]`, pw)
	assert.Regexp(t, `[A-Z]`, pw)
	assert.Regexp(t, `[0-9]`, pw)

	pwNoSpecial, err := s.Password(16, false)
	require.NoError(t, err)
	assert.Len(t, pwNoSpecial, 16)

	// Below-minimum length is clamped up to 8.
	short, err := s.Password(3, false)
	require.NoError(t, err)
	assert.Len(t, short, 8)
}

func TestRandomBytesAndHex(t *testing.T) {
	s := New()

	b, err := s.RandomBytes(16)
	require.NoError(t, err)
	assert.Len(t, b, 16)

	hexStr, err := s.RandomHex(16)
	require.NoError(t, err)
	assert.Len(t, hexStr, 32)
	assert.Regexp(t, `^[0-9a-f]+$`, hexStr)

	// Zero length bytes is a valid boundary case.
	empty, err := s.RandomBytes(0)
	require.NoError(t, err)
	assert.Len(t, empty, 0)
}

func TestToken(t *testing.T) {
	s := New()

	token, err := s.Token(32)
	require.NoError(t, err)
	// Token(n) hex-encodes n/2 random bytes, producing an n-char hex string.
	assert.Len(t, token, 32)
}

func TestAPIKey(t *testing.T) {
	s := New()

	key, err := s.APIKey()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(key, "key_"))
	// "key_" prefix + 64 hex chars (32 random bytes).
	assert.Len(t, key, 4+64)
}

func TestSlug(t *testing.T) {
	s := New()
	cases := []struct {
		in   string
		want string
	}{
		{"Hello World", "hello-world"},
		{"  Leading and Trailing  ", "leading-and-trailing"},
		{"Multiple---Hyphens", "multiple-hyphens"},
		{"Special!@# Characters$%^", "special-characters"},
		{"already-a-slug", "already-a-slug"},
		{"123 Numbers", "123-numbers"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, s.Slug(c.in), "Slug(%q)", c.in)
	}
}

func TestRandomColor(t *testing.T) {
	s := New()
	color := s.RandomColor()
	assert.Regexp(t, `^#[0-9a-f]{6}$`, color)
}

func TestRandomMAC(t *testing.T) {
	s := New()
	mac, err := s.RandomMAC()
	require.NoError(t, err)
	assert.Regexp(t, `^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`, mac)

	// Locally administered bit set, multicast bit cleared on first octet.
	firstByteBytes, err := hex.DecodeString(mac[:2])
	require.NoError(t, err)
	firstByte := int(firstByteBytes[0])
	assert.Equal(t, 0, firstByte&1, "multicast bit should be cleared")
	assert.Equal(t, 2, firstByte&2, "locally administered bit should be set")
}

func TestRandomIPv4(t *testing.T) {
	s := New()
	ip, err := s.RandomIPv4()
	require.NoError(t, err)
	assert.Regexp(t, `^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`, ip)
}

func TestTimestamps(t *testing.T) {
	s := New()
	ts := s.Timestamp()
	assert.Greater(t, ts, int64(0))

	tsMillis := s.TimestampMillis()
	assert.Greater(t, tsMillis, int64(0))
	// Millis should be roughly 1000x seconds (within a generous tolerance).
	assert.InDelta(t, ts*1000, tsMillis, 5000)
}

func TestNonce(t *testing.T) {
	s := New()
	nonce, err := s.Nonce()
	require.NoError(t, err)
	assert.Len(t, nonce, 32)
	assert.Regexp(t, `^[0-9a-f]+$`, nonce)
}
