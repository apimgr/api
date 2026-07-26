package geo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// nominatimReverseEntry mirrors the Nominatim /reverse response
type nominatimReverseEntry struct {
	DisplayName string `json:"display_name"`
	Address     struct {
		Road        string `json:"road"`
		City        string `json:"city"`
		Town        string `json:"town"`
		Village     string `json:"village"`
		County      string `json:"county"`
		State       string `json:"state"`
		Postcode    string `json:"postcode"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
	} `json:"address"`
	Error string `json:"error"`
}

// ReverseGeocodeResult represents a reverse-geocoded address
type ReverseGeocodeResult struct {
	DisplayName string `json:"display_name"`
	Road        string `json:"road,omitempty"`
	City        string `json:"city,omitempty"`
	County      string `json:"county,omitempty"`
	State       string `json:"state,omitempty"`
	Postcode    string `json:"postcode,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
}

// ReverseGeocode converts coordinates to a human-readable address using the
// free, keyless Nominatim (OpenStreetMap) reverse geocoding API
func (s *Service) ReverseGeocode(lat, lon float64) (*ReverseGeocodeResult, error) {
	if !s.IsValidCoordinate(lat, lon) {
		return nil, fmt.Errorf("invalid coordinate: latitude must be -90..90 and longitude -180..180")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
	params.Set("lon", strconv.FormatFloat(lon, 'f', -1, 64))
	params.Set("format", "json")

	var result nominatimReverseEntry
	if err := fetchNominatim(ctx, nominatimReverseEndpoint+"?"+params.Encode(), &result); err != nil {
		return nil, err
	}

	if result.Error != "" {
		return nil, fmt.Errorf("nominatim: %s", result.Error)
	}
	if result.DisplayName == "" {
		return nil, fmt.Errorf("no address found for coordinates %g, %g", lat, lon)
	}

	city := result.Address.City
	if city == "" {
		city = result.Address.Town
	}
	if city == "" {
		city = result.Address.Village
	}

	return &ReverseGeocodeResult{
		DisplayName: result.DisplayName,
		Road:        result.Address.Road,
		City:        city,
		County:      result.Address.County,
		State:       result.Address.State,
		Postcode:    result.Address.Postcode,
		Country:     result.Address.Country,
		CountryCode: result.Address.CountryCode,
	}, nil
}
