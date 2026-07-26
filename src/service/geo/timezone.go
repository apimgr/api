package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// openMeteoForecastEndpoint reuses the same free, keyless Open-Meteo
// forecast API already used by the weather service, with timezone=auto to
// resolve the IANA timezone name for a coordinate cheaply
const openMeteoForecastEndpoint = "https://api.open-meteo.com/v1/forecast"

// openMeteoTimezoneResult mirrors the minimal Open-Meteo forecast response
// needed to read the resolved timezone
type openMeteoTimezoneResult struct {
	Timezone         string `json:"timezone"`
	TimezoneAbbrev   string `json:"timezone_abbreviation"`
	UTCOffsetSeconds int    `json:"utc_offset_seconds"`
}

// TimezoneResult represents a resolved IANA timezone for a coordinate
type TimezoneResult struct {
	Timezone         string `json:"timezone"`
	Abbreviation     string `json:"abbreviation"`
	UTCOffsetSeconds int    `json:"utc_offset_seconds"`
}

// Timezone resolves the IANA timezone name for a coordinate using the free,
// keyless Open-Meteo forecast API's timezone=auto resolution
func (s *Service) Timezone(lat, lon float64) (*TimezoneResult, error) {
	if !s.IsValidCoordinate(lat, lon) {
		return nil, fmt.Errorf("invalid coordinate: latitude must be -90..90 and longitude -180..180")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(lat, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(lon, 'f', -1, 64))
	params.Set("current_weather", "true")
	params.Set("timezone", "auto")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openMeteoForecastEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open-meteo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open-meteo returned status %d", resp.StatusCode)
	}

	var result openMeteoTimezoneResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode open-meteo response: %w", err)
	}

	if result.Timezone == "" {
		return nil, fmt.Errorf("no timezone found for coordinates %g, %g", lat, lon)
	}

	return &TimezoneResult{
		Timezone:         result.Timezone,
		Abbreviation:     result.TimezoneAbbrev,
		UTCOffsetSeconds: result.UTCOffsetSeconds,
	}, nil
}
