package geo

import "fmt"

// BoundingBox represents a geographic bounding box
type BoundingBox struct {
	MinLatitude  float64 `json:"min_latitude"`
	MinLongitude float64 `json:"min_longitude"`
	MaxLatitude  float64 `json:"max_latitude"`
	MaxLongitude float64 `json:"max_longitude"`
}

// BoundingBoxFromRadius calculates a bounding box that contains a circle of
// the given radius (km) centered at the given coordinate
func (s *Service) BoundingBoxFromRadius(lat, lon, radiusKM float64) (*BoundingBox, error) {
	if !s.IsValidCoordinate(lat, lon) {
		return nil, fmt.Errorf("invalid coordinate: latitude must be -90..90 and longitude -180..180")
	}
	if radiusKM <= 0 {
		return nil, fmt.Errorf("radius must be greater than 0")
	}

	north := s.Destination(lat, lon, radiusKM, 0)
	south := s.Destination(lat, lon, radiusKM, 180)
	east := s.Destination(lat, lon, radiusKM, 90)
	west := s.Destination(lat, lon, radiusKM, 270)

	return &BoundingBox{
		MinLatitude:  south.Latitude,
		MaxLatitude:  north.Latitude,
		MinLongitude: west.Longitude,
		MaxLongitude: east.Longitude,
	}, nil
}

// BoundingBoxFromCoordinates calculates the smallest bounding box that
// contains all of the given coordinates
func (s *Service) BoundingBoxFromCoordinates(coords []Coordinate) (*BoundingBox, error) {
	if len(coords) == 0 {
		return nil, fmt.Errorf("at least one coordinate is required")
	}

	for _, c := range coords {
		if !s.IsValidCoordinate(c.Latitude, c.Longitude) {
			return nil, fmt.Errorf("invalid coordinate: latitude must be -90..90 and longitude -180..180")
		}
	}

	box := &BoundingBox{
		MinLatitude:  coords[0].Latitude,
		MaxLatitude:  coords[0].Latitude,
		MinLongitude: coords[0].Longitude,
		MaxLongitude: coords[0].Longitude,
	}

	for _, c := range coords[1:] {
		if c.Latitude < box.MinLatitude {
			box.MinLatitude = c.Latitude
		}
		if c.Latitude > box.MaxLatitude {
			box.MaxLatitude = c.Latitude
		}
		if c.Longitude < box.MinLongitude {
			box.MinLongitude = c.Longitude
		}
		if c.Longitude > box.MaxLongitude {
			box.MaxLongitude = c.Longitude
		}
	}

	return box, nil
}
