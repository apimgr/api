package geo

import (
	"fmt"
	"strings"
)

// geohashBase32 is the standard geohash base32 alphabet (excludes a, i, l, o
// to avoid visual ambiguity)
const geohashBase32 = "0123456789bcdefghjkmnpqrstuvwxyz"

// GeohashEncode encodes a coordinate to a base32 geohash string of the
// given precision (number of characters)
func (s *Service) GeohashEncode(lat, lon float64, precision int) (string, error) {
	if !s.IsValidCoordinate(lat, lon) {
		return "", fmt.Errorf("invalid coordinate: latitude must be -90..90 and longitude -180..180")
	}
	if precision < 1 || precision > 20 {
		return "", fmt.Errorf("precision must be between 1 and 20")
	}

	latRange := [2]float64{-90, 90}
	lonRange := [2]float64{-180, 180}

	var buf strings.Builder
	bit := 0
	ch := 0
	evenBit := true

	for buf.Len() < precision {
		if evenBit {
			mid := (lonRange[0] + lonRange[1]) / 2
			if lon >= mid {
				ch |= 1 << (4 - bit)
				lonRange[0] = mid
			} else {
				lonRange[1] = mid
			}
		} else {
			mid := (latRange[0] + latRange[1]) / 2
			if lat >= mid {
				ch |= 1 << (4 - bit)
				latRange[0] = mid
			} else {
				latRange[1] = mid
			}
		}
		evenBit = !evenBit

		if bit < 4 {
			bit++
		} else {
			buf.WriteByte(geohashBase32[ch])
			bit = 0
			ch = 0
		}
	}

	return buf.String(), nil
}

// GeohashDecode decodes a base32 geohash string back to a coordinate,
// returning the resolved latitude/longitude at the center of the geohash
// cell
func (s *Service) GeohashDecode(hash string) (*Coordinate, error) {
	if hash == "" {
		return nil, fmt.Errorf("geohash is required")
	}

	latRange := [2]float64{-90, 90}
	lonRange := [2]float64{-180, 180}
	evenBit := true

	for i := 0; i < len(hash); i++ {
		idx := strings.IndexByte(geohashBase32, hash[i])
		if idx < 0 {
			return nil, fmt.Errorf("invalid geohash character %q", string(hash[i]))
		}

		for n := 4; n >= 0; n-- {
			bitN := (idx >> uint(n)) & 1
			if evenBit {
				mid := (lonRange[0] + lonRange[1]) / 2
				if bitN == 1 {
					lonRange[0] = mid
				} else {
					lonRange[1] = mid
				}
			} else {
				mid := (latRange[0] + latRange[1]) / 2
				if bitN == 1 {
					latRange[0] = mid
				} else {
					latRange[1] = mid
				}
			}
			evenBit = !evenBit
		}
	}

	return &Coordinate{
		Latitude:  (latRange[0] + latRange[1]) / 2,
		Longitude: (lonRange[0] + lonRange[1]) / 2,
	}, nil
}
