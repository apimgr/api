package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers H3Encode: invalid coordinate and out-of-range resolution rejected,
// and a valid encode returns a non-empty index echoing back the requested
// resolution and coordinate.
func TestH3Encode(t *testing.T) {
	s := New()

	t.Run("invalid coordinate", func(t *testing.T) {
		result, err := s.H3Encode(999, 0, 9)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid coordinate")
	})

	t.Run("resolution out of range", func(t *testing.T) {
		_, err := s.H3Encode(40.7128, -74.0060, -1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolution must be between")

		_, err = s.H3Encode(40.7128, -74.0060, 16)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolution must be between")
	})

	t.Run("valid encode", func(t *testing.T) {
		result, err := s.H3Encode(40.7128, -74.0060, 9)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Index)
		assert.Equal(t, 9, result.Resolution)
		assert.InDelta(t, 40.7128, result.Latitude, 0.0001)
		assert.InDelta(t, -74.0060, result.Longitude, 0.0001)
	})
}
