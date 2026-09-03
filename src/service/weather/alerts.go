package weather

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Alert is a normalized weather alert/warning, uniform across every
// government provider this service queries
type Alert struct {
	Provider    string `json:"provider"`
	Event       string `json:"event"`
	Severity    string `json:"severity"`
	Certainty   string `json:"certainty"`
	Urgency     string `json:"urgency"`
	Headline    string `json:"headline"`
	Description string `json:"description"`
	AreaDesc    string `json:"area_desc"`
	Effective   string `json:"effective"`
	Expires     string `json:"expires"`
}

// GetAlerts retrieves active weather alerts for a location by name,
// routing to the appropriate free, keyless government provider for that
// location's country: NWS for the US, Environment Canada/MSC GeoMet for
// Canada, and MeteoAlarm for European countries. Locations outside these
// coverage areas return an empty (not error) result, since no global
// keyless alerts provider exists.
func (s *Service) GetAlerts(city string) ([]*Alert, error) {
	loc, err := s.resolveLocation(city)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch strings.ToUpper(loc.CountryCode) {
	case "US":
		return fetchNWSAlerts(ctx, loc)
	case "CA":
		return fetchECCCAlerts(ctx, loc)
	default:
		if isEuropeanCountryCode(strings.ToUpper(loc.CountryCode)) {
			return fetchMeteoAlarmAlerts(ctx, loc)
		}
		return []*Alert{}, nil
	}
}

// nwsAlertsResponse mirrors the National Weather Service "active alerts by
// point" GeoJSON FeatureCollection
type nwsAlertsResponse struct {
	Features []struct {
		Properties struct {
			Event       string `json:"event"`
			Severity    string `json:"severity"`
			Certainty   string `json:"certainty"`
			Urgency     string `json:"urgency"`
			Headline    string `json:"headline"`
			Description string `json:"description"`
			AreaDesc    string `json:"areaDesc"`
			Effective   string `json:"effective"`
			Expires     string `json:"expires"`
		} `json:"properties"`
	} `json:"features"`
}

// fetchNWSAlerts retrieves active alerts from api.weather.gov for a US location
func fetchNWSAlerts(ctx context.Context, loc *Location) ([]*Alert, error) {
	point := strconv.FormatFloat(loc.Latitude, 'f', 4, 64) + "," + strconv.FormatFloat(loc.Longitude, 'f', 4, 64)
	endpoint := "https://api.weather.gov/alerts/active?point=" + url.QueryEscape(point)

	var result nwsAlertsResponse
	if err := fetchJSON(ctx, endpoint, &result); err != nil {
		return nil, fmt.Errorf("NWS alerts lookup failed: %w", err)
	}

	alerts := make([]*Alert, 0, len(result.Features))
	for _, f := range result.Features {
		p := f.Properties
		alerts = append(alerts, &Alert{
			Provider:    "NWS (US National Weather Service)",
			Event:       p.Event,
			Severity:    p.Severity,
			Certainty:   p.Certainty,
			Urgency:     p.Urgency,
			Headline:    p.Headline,
			Description: p.Description,
			AreaDesc:    p.AreaDesc,
			Effective:   p.Effective,
			Expires:     p.Expires,
		})
	}
	return alerts, nil
}

// ecccAlertsResponse mirrors the Environment Canada / MSC GeoMet-OGC-API
// "weather-alerts" GeoJSON FeatureCollection
type ecccAlertsResponse struct {
	Features []struct {
		Properties struct {
			AlertNameEn         string `json:"alert_name_en"`
			AlertTextEn         string `json:"alert_text_en"`
			FeatureNameEn       string `json:"feature_name_en"`
			PublicationDatetime string `json:"publication_datetime"`
			ExpirationDatetime  string `json:"expiration_datetime"`
			RiskColourEn        string `json:"risk_colour_en"`
			ConfidenceEn        string `json:"confidence_en"`
			DisplayStatus       string `json:"display_status"`
		} `json:"properties"`
	} `json:"features"`
}

// fetchECCCAlerts retrieves active alerts from Environment Canada's MSC
// GeoMet-OGC-API for a Canadian location, using a small bounding box around
// the resolved coordinates and filtering to currently visible alerts
func fetchECCCAlerts(ctx context.Context, loc *Location) ([]*Alert, error) {
	const delta = 0.5
	bbox := fmt.Sprintf("%s,%s,%s,%s",
		strconv.FormatFloat(loc.Longitude-delta, 'f', 4, 64),
		strconv.FormatFloat(loc.Latitude-delta, 'f', 4, 64),
		strconv.FormatFloat(loc.Longitude+delta, 'f', 4, 64),
		strconv.FormatFloat(loc.Latitude+delta, 'f', 4, 64),
	)

	params := url.Values{}
	params.Set("f", "json")
	params.Set("bbox", bbox)
	params.Set("limit", "50")
	endpoint := "https://api.weather.gc.ca/collections/weather-alerts/items?" + params.Encode()

	var result ecccAlertsResponse
	if err := fetchJSON(ctx, endpoint, &result); err != nil {
		return nil, fmt.Errorf("environment Canada alerts lookup failed: %w", err)
	}

	alerts := make([]*Alert, 0, len(result.Features))
	for _, f := range result.Features {
		p := f.Properties
		if p.DisplayStatus != "" && p.DisplayStatus != "visible" {
			continue
		}
		alerts = append(alerts, &Alert{
			Provider:    "Environment Canada (MSC GeoMet)",
			Event:       p.AlertNameEn,
			Severity:    p.RiskColourEn,
			Certainty:   p.ConfidenceEn,
			Headline:    p.AlertNameEn,
			Description: p.AlertTextEn,
			AreaDesc:    p.FeatureNameEn,
			Effective:   p.PublicationDatetime,
			Expires:     p.ExpirationDatetime,
		})
	}
	return alerts, nil
}

// meteoAlarmFeed mirrors the Atom+CAP feed MeteoAlarm publishes per European country
type meteoAlarmFeed struct {
	XMLName xml.Name `xml:"feed"`
	Entries []struct {
		Title     string `xml:"title"`
		AreaDesc  string `xml:"urn:oasis:names:tc:emergency:cap:1.2 areaDesc"`
		Event     string `xml:"urn:oasis:names:tc:emergency:cap:1.2 event"`
		Severity  string `xml:"urn:oasis:names:tc:emergency:cap:1.2 severity"`
		Certainty string `xml:"urn:oasis:names:tc:emergency:cap:1.2 certainty"`
		Urgency   string `xml:"urn:oasis:names:tc:emergency:cap:1.2 urgency"`
		Onset     string `xml:"urn:oasis:names:tc:emergency:cap:1.2 onset"`
		Expires   string `xml:"urn:oasis:names:tc:emergency:cap:1.2 expires"`
	} `xml:"entry"`
}

// meteoAlarmCountrySlugs maps an ISO 3166-1 alpha-2 country code to the
// country slug MeteoAlarm uses in its per-country feed URLs
// (https://feeds.meteoalarm.org/feeds/meteoalarm-legacy-atom-{slug})
var meteoAlarmCountrySlugs = map[string]string{
	"AL": "albania", "AD": "andorra", "AT": "austria", "BY": "belarus",
	"BE": "belgium", "BA": "bosnia-herzegovina", "BG": "bulgaria",
	"HR": "croatia", "CY": "cyprus", "CZ": "czechia", "DK": "denmark",
	"EE": "estonia", "FI": "finland", "FR": "france", "DE": "germany",
	"GR": "greece", "HU": "hungary", "IS": "iceland", "IE": "ireland",
	"IT": "italy", "XK": "kosovo", "LV": "latvia", "LI": "liechtenstein",
	"LT": "lithuania", "LU": "luxembourg", "MT": "malta", "MD": "moldova",
	"MC": "monaco", "ME": "montenegro", "NL": "netherlands",
	"MK": "north-macedonia", "NO": "norway", "PL": "poland", "PT": "portugal",
	"RO": "romania", "RU": "russia", "SM": "san-marino", "RS": "serbia",
	"SK": "slovakia", "SI": "slovenia", "ES": "spain", "SE": "sweden",
	"CH": "switzerland", "UA": "ukraine", "GB": "united-kingdom",
	"VA": "vatican-city",
}

// fetchMeteoAlarmAlerts retrieves active alerts from MeteoAlarm's per-country
// Atom+CAP feed for a European location; countries without a known feed
// slug return an empty result rather than an error
func fetchMeteoAlarmAlerts(ctx context.Context, loc *Location) ([]*Alert, error) {
	slug, ok := meteoAlarmCountrySlugs[strings.ToUpper(loc.CountryCode)]
	if !ok {
		return []*Alert{}, nil
	}

	endpoint := "https://feeds.meteoalarm.org/feeds/meteoalarm-legacy-atom-" + slug

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build MeteoAlarm request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MeteoAlarm alerts lookup failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MeteoAlarm alerts lookup returned status %d", resp.StatusCode)
	}

	var feed meteoAlarmFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("failed to decode MeteoAlarm feed: %w", err)
	}

	alerts := make([]*Alert, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		alerts = append(alerts, &Alert{
			Provider:    "MeteoAlarm",
			Event:       e.Event,
			Severity:    e.Severity,
			Certainty:   e.Certainty,
			Urgency:     e.Urgency,
			Headline:    e.Title,
			Description: e.Title,
			AreaDesc:    e.AreaDesc,
			Effective:   e.Onset,
			Expires:     e.Expires,
		})
	}
	return alerts, nil
}
