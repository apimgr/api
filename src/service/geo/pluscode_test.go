package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers PlusCodeEncode/PlusCodeDecode: invalid coordinate rejected on
// encode, empty and malformed codes rejected on decode, and an
// encode-then-decode round trip returns approximately the original
// coordinate.
func TestPlusCodeEncode(t *testing.T) {
	s := New()

	t.Run("invalid coordinate", func(t *testing.T) {
		result, err := s.PlusCodeEncode(999, 0)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid coordinate")
	})

	t.Run("valid encode", func(t *testing.T) {
		result, err := s.PlusCodeEncode(40.7128, -74.0060)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Code)
		assert.Contains(t, result.Code, "+")
	})
}

func TestPlusCodeDecode(t *testing.T) {
	s := New()

	t.Run("empty code", func(t *testing.T) {
		coord, err := s.PlusCodeDecode("")
		require.Error(t, err)
		assert.Nil(t, coord)
		assert.Contains(t, err.Error(), "plus code is required")
	})

	t.Run("malformed code", func(t *testing.T) {
		coord, err := s.PlusCodeDecode("not-a-code")
		require.Error(t, err)
		assert.Nil(t, coord)
		assert.Contains(t, err.Error(), "invalid plus code")
	})

	t.Run("round trip", func(t *testing.T) {
		encoded, err := s.PlusCodeEncode(40.7128, -74.0060)
		require.NoError(t, err)

		coord, err := s.PlusCodeDecode(encoded.Code)
		require.NoError(t, err)
		require.NotNil(t, coord)
		assert.InDelta(t, 40.7128, coord.Latitude, 0.001)
		assert.InDelta(t, -74.0060, coord.Longitude, 0.001)
	})
}
