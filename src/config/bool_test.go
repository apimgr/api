package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseBool covers every truthy/falsy alias, case-insensitivity,
// whitespace trimming, the empty-string default, and the invalid-value
// error path.
func TestParseBool(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		defaultVal bool
		want       bool
		wantErr    bool
	}{
		{"canonical true", "true", false, true, false},
		{"canonical false", "false", true, false, false},
		{"numeric 1", "1", false, true, false},
		{"numeric 0", "0", true, false, false},
		{"yes", "yes", false, true, false},
		{"no", "no", true, false, false},
		{"on", "on", false, true, false},
		{"off", "off", true, false, false},
		{"uppercase TRUE", "TRUE", false, true, false},
		{"mixed case YeS", "YeS", false, true, false},
		{"whitespace padded", "  true  ", false, true, false},
		{"enable alias", "enable", false, true, false},
		{"disabled alias", "disabled", true, false, false},
		{"empty uses default true", "", true, true, false},
		{"empty uses default false", "", false, false, false},
		{"invalid value", "maybe", false, false, true},
		{"invalid numeric", "2", false, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseBool(tc.in, tc.defaultVal)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestMustParseBool covers the success path and confirms invalid input
// panics rather than silently defaulting.
func TestMustParseBool(t *testing.T) {
	assert.True(t, MustParseBool("true", false))
	assert.False(t, MustParseBool("", false))
	assert.Panics(t, func() {
		MustParseBool("not-a-bool", false)
	})
}

// TestIsTruthyIsFalsy covers both helpers across truthy, falsy, empty, and
// invalid input - each must return false (no error) for anything outside
// its own set.
func TestIsTruthyIsFalsy(t *testing.T) {
	assert.True(t, IsTruthy("yes"))
	assert.True(t, IsTruthy("  TRUE  "))
	assert.False(t, IsTruthy("no"))
	assert.False(t, IsTruthy(""))
	assert.False(t, IsTruthy("garbage"))

	assert.True(t, IsFalsy("no"))
	assert.True(t, IsFalsy("  FALSE  "))
	assert.False(t, IsFalsy("yes"))
	assert.False(t, IsFalsy(""))
	assert.False(t, IsFalsy("garbage"))
}
