package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDistance(t *testing.T) {
	s := New()

	// Same point has zero distance.
	assert.InDelta(t, 0.0, s.Distance(40.7128, -74.0060, 40.7128, -74.0060), 0.0001)

	// New York to Los Angeles is approximately 3936 km.
	dist := s.Distance(40.7128, -74.0060, 34.0522, -118.2437)
	assert.InDelta(t, 3936, dist, 20)
}

func TestDistanceInMiles(t *testing.T) {
	s := New()

	km := s.Distance(40.7128, -74.0060, 34.0522, -118.2437)
	miles := s.DistanceInMiles(40.7128, -74.0060, 34.0522, -118.2437)
	assert.InDelta(t, km*0.621371, miles, 0.0001)
}

func TestMidpoint(t *testing.T) {
	s := New()

	// Midpoint of a point with itself is itself.
	mid := s.Midpoint(0, 0, 0, 0)
	assert.InDelta(t, 0.0, mid.Latitude, 0.0001)
	assert.InDelta(t, 0.0, mid.Longitude, 0.0001)

	// Midpoint of two points on the equator straddling the prime meridian.
	mid = s.Midpoint(0, -10, 0, 10)
	assert.InDelta(t, 0.0, mid.Latitude, 0.0001)
	assert.InDelta(t, 0.0, mid.Longitude, 0.0001)
}

func TestBearing(t *testing.T) {
	s := New()

	// Due north: from (0,0) to (10,0) bearing should be 0 degrees.
	bearing := s.Bearing(0, 0, 10, 0)
	assert.InDelta(t, 0.0, bearing, 0.5)

	// Due east: from (0,0) to (0,10) bearing should be 90 degrees.
	bearing = s.Bearing(0, 0, 0, 10)
	assert.InDelta(t, 90.0, bearing, 0.5)

	// Bearing always normalized into [0, 360).
	assert.GreaterOrEqual(t, bearing, 0.0)
	assert.Less(t, bearing, 360.0)
}

func TestIsValidCoordinate(t *testing.T) {
	s := New()
	assert.True(t, s.IsValidCoordinate(0, 0))
	assert.True(t, s.IsValidCoordinate(90, 180))
	assert.True(t, s.IsValidCoordinate(-90, -180))
	assert.False(t, s.IsValidCoordinate(91, 0))
	assert.False(t, s.IsValidCoordinate(0, 181))
	assert.False(t, s.IsValidCoordinate(-91, 0))
	assert.False(t, s.IsValidCoordinate(0, -181))
}

func TestDestination(t *testing.T) {
	s := New()

	// Travel 0 km leaves the point unchanged.
	dest := s.Destination(40.7128, -74.0060, 0, 0)
	assert.InDelta(t, 40.7128, dest.Latitude, 0.0001)
	assert.InDelta(t, -74.0060, dest.Longitude, 0.0001)

	// Traveling due north (bearing 0) increases latitude.
	dest = s.Destination(0, 0, 100, 0)
	assert.Greater(t, dest.Latitude, 0.0)
	assert.InDelta(t, 0.0, dest.Longitude, 0.01)

	// Destination followed by Distance back should roughly equal traveled distance.
	dest = s.Destination(40.7128, -74.0060, 500, 45)
	dist := s.Distance(40.7128, -74.0060, dest.Latitude, dest.Longitude)
	assert.InDelta(t, 500, dist, 1)
}

func TestToDMS(t *testing.T) {
	s := New()

	dms := s.ToDMS(40.7128)
	assert.Contains(t, dms, "40°")
	assert.Contains(t, dms, "N/E")

	dms = s.ToDMS(-74.0060)
	assert.Contains(t, dms, "74°")
	assert.Contains(t, dms, "S/W")

	dms = s.ToDMS(0)
	assert.Contains(t, dms, "0°")
	assert.Contains(t, dms, "N/E")
}

func TestDMSToDecimal(t *testing.T) {
	s := New()

	dec := s.DMSToDecimal(40, 42, 46.08, "N")
	assert.InDelta(t, 40.7128, dec, 0.001)

	dec = s.DMSToDecimal(74, 0, 21.6, "W")
	assert.InDelta(t, -74.006, dec, 0.001)

	dec = s.DMSToDecimal(10, 0, 0, "E")
	assert.InDelta(t, 10.0, dec, 0.0001)

	// Boundary: zero degrees/minutes/seconds.
	dec = s.DMSToDecimal(0, 0, 0, "N")
	assert.InDelta(t, 0.0, dec, 0.0001)
}
