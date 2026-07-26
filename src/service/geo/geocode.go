package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// httpClient is a shared client with a hard timeout for the keyless
// Nominatim (OpenStreetMap) provider
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

const (
	nominatimSearchEndpoint  = "https://nominatim.openstreetmap.org/search"
	nominatimReverseEndpoint = "https://nominatim.openstreetmap.org/reverse"

	// nominatimUserAgent identifies this application to the Nominatim usage
	// policy, which requires a descriptive User-Agent on every request
	nominatimUserAgent = "apimgr-api/1.0 (+https://github.com/apimgr/api)"
)

// GeocodeResult represents a single Nominatim search result
type GeocodeResult struct {
	DisplayName string  `json:"display_name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Type        string  `json:"type,omitempty"`
	Class       string  `json:"class,omitempty"`
}

// nominatimSearchEntry mirrors a single entry in the Nominatim /search response
type nominatimSearchEntry struct {
	DisplayName string `json:"display_name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	Type        string `json:"type"`
	Class       string `json:"class"`
}

// fetchNominatim performs a GET request against a Nominatim endpoint with
// the required descriptive User-Agent header and decodes the JSON response
func fetchNominatim(ctx context.Context, endpoint string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", nominatimUserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nominatim request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nominatim returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode nominatim response: %w", err)
	}
	return nil
}

// Geocode converts an address or place name to coordinates using the free,
// keyless Nominatim (OpenStreetMap) search API
func (s *Service) Geocode(query string) ([]*GeocodeResult, error) {
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("limit", "5")

	var results []nominatimSearchEntry
	if err := fetchNominatim(ctx, nominatimSearchEndpoint+"?"+params.Encode(), &results); err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results found for %q", query)
	}

	out := make([]*GeocodeResult, 0, len(results))
	for _, r := range results {
		var lat, lon float64
		if _, err := fmt.Sscanf(r.Lat, "%g", &lat); err != nil {
			continue
		}
		if _, err := fmt.Sscanf(r.Lon, "%g", &lon); err != nil {
			continue
		}
		out = append(out, &GeocodeResult{
			DisplayName: r.DisplayName,
			Latitude:    lat,
			Longitude:   lon,
			Type:        r.Type,
			Class:       r.Class,
		})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no valid results found for %q", query)
	}
	return out, nil
}
