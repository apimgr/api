package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers BoundingBoxFromRadius: invalid coordinate and non-positive radius
// rejected, and a valid radius produces a box symmetric around the center.
func TestBoundingBoxFromRadius(t *testing.T) {
	s := New()

	t.Run("invalid coordinate", func(t *testing.T) {
		box, err := s.BoundingBoxFromRadius(999, 0, 10)
		require.Error(t, err)
		assert.Nil(t, box)
		assert.Contains(t, err.Error(), "invalid coordinate")
	})

	t.Run("radius must be positive", func(t *testing.T) {
		box, err := s.BoundingBoxFromRadius(40.7128, -74.0060, 0)
		require.Error(t, err)
		assert.Nil(t, box)
		assert.Contains(t, err.Error(), "radius must be greater than 0")
	})

	t.Run("valid radius produces symmetric box", func(t *testing.T) {
		box, err := s.BoundingBoxFromRadius(40.7128, -74.0060, 10)
		require.NoError(t, err)
		require.NotNil(t, box)
		assert.Less(t, box.MinLatitude, 40.7128)
		assert.Greater(t, box.MaxLatitude, 40.7128)
		assert.Less(t, box.MinLongitude, -74.0060)
		assert.Greater(t, box.MaxLongitude, -74.0060)
	})
}

// Covers BoundingBoxFromCoordinates: empty list and any invalid coordinate
// rejected, and a valid list produces the correct min/max bounds.
func TestBoundingBoxFromCoordinates(t *testing.T) {
	s := New()

	t.Run("empty list", func(t *testing.T) {
		box, err := s.BoundingBoxFromCoordinates(nil)
		require.Error(t, err)
		assert.Nil(t, box)
		assert.Contains(t, err.Error(), "at least one coordinate is required")
	})

	t.Run("invalid coordinate in list", func(t *testing.T) {
		box, err := s.BoundingBoxFromCoordinates([]Coordinate{
			{Latitude: 40.7128, Longitude: -74.0060},
			{Latitude: 999, Longitude: 0},
		})
		require.Error(t, err)
		assert.Nil(t, box)
		assert.Contains(t, err.Error(), "invalid coordinate")
	})

	t.Run("valid list produces correct bounds", func(t *testing.T) {
		box, err := s.BoundingBoxFromCoordinates([]Coordinate{
			{Latitude: 40.7128, Longitude: -74.0060},
			{Latitude: 34.0522, Longitude: -118.2437},
		})
		require.NoError(t, err)
		require.NotNil(t, box)
		assert.InDelta(t, 34.0522, box.MinLatitude, 0.0001)
		assert.InDelta(t, 40.7128, box.MaxLatitude, 0.0001)
		assert.InDelta(t, -118.2437, box.MinLongitude, 0.0001)
		assert.InDelta(t, -74.0060, box.MaxLongitude, 0.0001)
	})
}
