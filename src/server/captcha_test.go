package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCaptchaNumberBounds(t *testing.T) {
	for range 100 {
		value, err := randomCaptchaNumber(20)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, value, int64(1))
		assert.LessOrEqual(t, value, int64(20))
	}
}

func TestVerifySimpleCaptchaRejectsMalformedInput(t *testing.T) {
	for _, test := range []struct{ id, answer string }{
		{"short", "1"},
		{"0123456789abcdef0123456789abcdef", ""},
		{"0123456789abcdef0123456789abcdef", "1x"},
		{"0123456789abcdef0123456789abcdef", "1234"},
	} {
		valid, err := VerifySimpleCaptcha(t.Context(), test.id, test.answer)
		assert.NoError(t, err)
		assert.False(t, valid)
	}
}
