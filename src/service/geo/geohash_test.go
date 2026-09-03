package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers GeohashEncode/GeohashDecode: invalid coordinate and out-of-range
// precision rejected on encode, invalid characters rejected on decode, and
// an encode-then-decode round trip returns approximately the original
// coordinate.
func TestGeohashEncode(t *testing.T) {
	s := New()

	t.Run("invalid coordinate", func(t *testing.T) {
		hash, err := s.GeohashEncode(999, 0, 9)
		require.Error(t, err)
		assert.Empty(t, hash)
		assert.Contains(t, err.Error(), "invalid coordinate")
	})

	t.Run("precision out of range", func(t *testing.T) {
		_, err := s.GeohashEncode(40.7128, -74.0060, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "precision must be between")

		_, err = s.GeohashEncode(40.7128, -74.0060, 21)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "precision must be between")
	})

	t.Run("known encoding", func(t *testing.T) {
		hash, err := s.GeohashEncode(40.7128, -74.0060, 9)
		require.NoError(t, err)
		assert.Len(t, hash, 9)
		assert.True(t, hash[:5] == "dr5re")
	})
}

func TestGeohashDecode(t *testing.T) {
	s := New()

	t.Run("empty hash", func(t *testing.T) {
		coord, err := s.GeohashDecode("")
		require.Error(t, err)
		assert.Nil(t, coord)
		assert.Contains(t, err.Error(), "geohash is required")
	})

	t.Run("invalid character", func(t *testing.T) {
		coord, err := s.GeohashDecode("abc")
		require.Error(t, err)
		assert.Nil(t, coord)
		assert.Contains(t, err.Error(), "invalid geohash character")
	})

	t.Run("round trip", func(t *testing.T) {
		hash, err := s.GeohashEncode(40.7128, -74.0060, 9)
		require.NoError(t, err)

		coord, err := s.GeohashDecode(hash)
		require.NoError(t, err)
		require.NotNil(t, coord)
		assert.InDelta(t, 40.7128, coord.Latitude, 0.001)
		assert.InDelta(t, -74.0060, coord.Longitude, 0.001)
	})
}
