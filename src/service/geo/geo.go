package geo

import (
	"fmt"
	"math"
)

// Service provides geographic utilities
type Service struct{}

// New creates a new Geo service
func New() *Service {
	return &Service{}
}

// Coordinate represents a geographic location
type Coordinate struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Distance calculates distance between two points (Haversine formula)
func (s *Service) Distance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371 // km
	
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	deltaLat := (lat2 - lat1) * math.Pi / 180
	deltaLon := (lon2 - lon1) * math.Pi / 180
	
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	
	return earthRadius * c
}

// DistanceInMiles calculates distance in miles
func (s *Service) DistanceInMiles(lat1, lon1, lat2, lon2 float64) float64 {
	return s.Distance(lat1, lon1, lat2, lon2) * 0.621371
}

// Midpoint calculates the midpoint between two coordinates
func (s *Service) Midpoint(lat1, lon1, lat2, lon2 float64) *Coordinate {
	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180
	
	bx := math.Cos(lat2Rad) * math.Cos(lon2Rad-lon1Rad)
	by := math.Cos(lat2Rad) * math.Sin(lon2Rad-lon1Rad)
	
	lat3 := math.Atan2(
		math.Sin(lat1Rad)+math.Sin(lat2Rad),
		math.Sqrt((math.Cos(lat1Rad)+bx)*(math.Cos(lat1Rad)+bx)+by*by),
	)
	lon3 := lon1Rad + math.Atan2(by, math.Cos(lat1Rad)+bx)
	
	return &Coordinate{
		Latitude:  lat3 * 180 / math.Pi,
		Longitude: lon3 * 180 / math.Pi,
	}
}

// Bearing calculates initial bearing between two points
func (s *Service) Bearing(lat1, lon1, lat2, lon2 float64) float64 {
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	deltaLon := (lon2 - lon1) * math.Pi / 180
	
	y := math.Sin(deltaLon) * math.Cos(lat2Rad)
	x := math.Cos(lat1Rad)*math.Sin(lat2Rad) -
		math.Sin(lat1Rad)*math.Cos(lat2Rad)*math.Cos(deltaLon)
	
	bearing := math.Atan2(y, x) * 180 / math.Pi
	return math.Mod(bearing+360, 360)
}

// IsValidCoordinate checks if coordinates are valid
func (s *Service) IsValidCoordinate(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// Destination calculates destination point given distance and bearing
func (s *Service) Destination(lat, lon, distance, bearing float64) *Coordinate {
	const earthRadius = 6371 // km
	
	latRad := lat * math.Pi / 180
	lonRad := lon * math.Pi / 180
	bearingRad := bearing * math.Pi / 180
	
	lat2 := math.Asin(math.Sin(latRad)*math.Cos(distance/earthRadius) +
		math.Cos(latRad)*math.Sin(distance/earthRadius)*math.Cos(bearingRad))
	
	lon2 := lonRad + math.Atan2(
		math.Sin(bearingRad)*math.Sin(distance/earthRadius)*math.Cos(latRad),
		math.Cos(distance/earthRadius)-math.Sin(latRad)*math.Sin(lat2),
	)
	
	return &Coordinate{
		Latitude:  lat2 * 180 / math.Pi,
		Longitude: lon2 * 180 / math.Pi,
	}
}

// Coordinate format conversions
func (s *Service) ToDMS(decimal float64) string {
	abs := math.Abs(decimal)
	degrees := int(abs)
	minutes := int((abs - float64(degrees)) * 60)
	seconds := ((abs - float64(degrees)) * 60 - float64(minutes)) * 60
	
	direction := ""
	if decimal >= 0 {
		direction = "N/E"
	} else {
		direction = "S/W"
	}
	
	return fmt.Sprintf("%d°%d'%.2f\"%s", degrees, minutes, seconds, direction)
}

// Parse DMS to decimal (simplified)
func (s *Service) DMSToDecimal(degrees, minutes, seconds float64, direction string) float64 {
	decimal := degrees + minutes/60 + seconds/3600
	if direction == "S" || direction == "W" {
		decimal = -decimal
	}
	return decimal
}
