package geo

import (
	"fmt"

	olc "github.com/google/open-location-code/go"
)

// defaultPlusCodeLength is the standard Open Location Code length (10
// characters), giving roughly 14m x 14m precision
const defaultPlusCodeLength = 10

// PlusCodeResult represents a Google Open Location Code (Plus Code)
type PlusCodeResult struct {
	Code string `json:"code"`
}

// PlusCodeEncode encodes a coordinate to a Google Open Location Code
// (Plus Code) at the standard 10-character precision
func (s *Service) PlusCodeEncode(lat, lon float64) (*PlusCodeResult, error) {
	if !s.IsValidCoordinate(lat, lon) {
		return nil, fmt.Errorf("invalid coordinate: latitude must be -90..90 and longitude -180..180")
	}

	code := olc.Encode(lat, lon, defaultPlusCodeLength)
	return &PlusCodeResult{Code: code}, nil
}

// PlusCodeDecode decodes a Google Open Location Code (Plus Code) back to
// the coordinate at the center of its resolved area
func (s *Service) PlusCodeDecode(code string) (*Coordinate, error) {
	if code == "" {
		return nil, fmt.Errorf("plus code is required")
	}

	if err := olc.CheckFull(code); err != nil {
		return nil, fmt.Errorf("invalid plus code: %w", err)
	}

	area, err := olc.Decode(code)
	if err != nil {
		return nil, fmt.Errorf("failed to decode plus code: %w", err)
	}

	lat, lon := area.Center()
	return &Coordinate{Latitude: lat, Longitude: lon}, nil
}
