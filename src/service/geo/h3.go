package geo

import (
	"fmt"

	"github.com/ziprecruiter/h3-go/pkg/h3"
)

// H3Result represents an Uber H3 hexagonal cell index for a coordinate
type H3Result struct {
	Index      string  `json:"index"`
	Resolution int     `json:"resolution"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
}

// H3Encode encodes a coordinate to an Uber H3 hexagonal cell index at the
// given resolution (0-15, coarsest to finest)
func (s *Service) H3Encode(lat, lon float64, resolution int) (*H3Result, error) {
	if !s.IsValidCoordinate(lat, lon) {
		return nil, fmt.Errorf("invalid coordinate: latitude must be -90..90 and longitude -180..180")
	}
	if resolution < 0 || resolution > 15 {
		return nil, fmt.Errorf("resolution must be between 0 and 15")
	}

	cell, err := h3.NewCellFromLatLng(h3.NewLatLng(lat, lon), resolution)
	if err != nil {
		return nil, fmt.Errorf("failed to encode h3 cell: %w", err)
	}

	return &H3Result{
		Index:      cell.String(),
		Resolution: resolution,
		Latitude:   lat,
		Longitude:  lon,
	}, nil
}
