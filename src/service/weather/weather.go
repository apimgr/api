package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Service provides weather utilities
type Service struct{}

// New creates a new Weather service
func New() *Service {
	return &Service{}
}

// Weather data structures
type CurrentWeather struct {
	Temperature   float64   `json:"temperature"`
	FeelsLike     float64   `json:"feels_like"`
	Humidity      int       `json:"humidity"`
	Pressure      int       `json:"pressure"`
	WindSpeed     float64   `json:"wind_speed"`
	WindDirection int       `json:"wind_direction"`
	Clouds        int       `json:"clouds"`
	Visibility    int       `json:"visibility"`
	Description   string    `json:"description"`
	Icon          string    `json:"icon"`
	Timestamp     time.Time `json:"timestamp"`
}

type Forecast struct {
	Date        time.Time `json:"date"`
	TempMin     float64   `json:"temp_min"`
	TempMax     float64   `json:"temp_max"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	Rain        float64   `json:"rain"`
	Snow        float64   `json:"snow"`
}

type Location struct {
	City        string  `json:"city"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Timezone    string  `json:"timezone"`
}

// httpClient is a shared client with a hard timeout for the keyless
// Open-Meteo provider (no API key, free, rate-limited by fair use)
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

const (
	geocodeEndpoint    = "https://geocoding-api.open-meteo.com/v1/search"
	forecastEndpoint   = "https://api.open-meteo.com/v1/forecast"
	airQualityEndpoint = "https://air-quality-api.open-meteo.com/v1/air-quality"
	marineEndpoint     = "https://marine-api.open-meteo.com/v1/marine"
	archiveEndpoint    = "https://archive-api.open-meteo.com/v1/archive"
)

// userAgent identifies this service to keyless government providers (the
// US National Weather Service API requires a descriptive User-Agent in
// place of an API key); harmless to send to every other provider too.
const userAgent = "apimgr-api/1.0 (+https://github.com/apimgr/api)"

// geocodeResult mirrors the Open-Meteo geocoding API response
type geocodeResult struct {
	Results []struct {
		Name        string  `json:"name"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Country     string  `json:"country"`
		CountryCode string  `json:"country_code"`
		Timezone    string  `json:"timezone"`
	} `json:"results"`
}

// currentForecastResult mirrors the Open-Meteo "current" forecast block
type currentForecastResult struct {
	Current struct {
		Time                string  `json:"time"`
		Temperature2m       float64 `json:"temperature_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		RelativeHumidity2m  int     `json:"relative_humidity_2m"`
		PressureMSL         float64 `json:"pressure_msl"`
		WindSpeed10m        float64 `json:"wind_speed_10m"`
		WindDirection10m    int     `json:"wind_direction_10m"`
		CloudCover          int     `json:"cloud_cover"`
		WeatherCode         int     `json:"weather_code"`
	} `json:"current"`
}

// dailyForecastResult mirrors the Open-Meteo "daily" forecast block
type dailyForecastResult struct {
	Daily struct {
		Time             []string  `json:"time"`
		WeatherCode      []int     `json:"weather_code"`
		Temperature2mMax []float64 `json:"temperature_2m_max"`
		Temperature2mMin []float64 `json:"temperature_2m_min"`
		RainSum          []float64 `json:"rain_sum"`
		SnowfallSum      []float64 `json:"snowfall_sum"`
	} `json:"daily"`
}

// fetchJSON performs a GET request against a trusted, keyless provider
// endpoint and decodes the JSON response into out
func fetchJSON(ctx context.Context, endpoint string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("weather provider request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("weather provider returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode weather provider response: %w", err)
	}
	return nil
}

// SearchLocation searches for locations by name using the free, keyless
// Open-Meteo geocoding API
func (s *Service) SearchLocation(query string) ([]*Location, error) {
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("name", query)
	params.Set("count", "10")
	params.Set("language", "en")
	params.Set("format", "json")

	var result geocodeResult
	if err := fetchJSON(ctx, geocodeEndpoint+"?"+params.Encode(), &result); err != nil {
		return nil, err
	}

	if len(result.Results) == 0 {
		return nil, fmt.Errorf("no locations found for %q", query)
	}

	locations := make([]*Location, 0, len(result.Results))
	for _, r := range result.Results {
		locations = append(locations, &Location{
			City:        r.Name,
			Country:     r.Country,
			CountryCode: r.CountryCode,
			Latitude:    r.Latitude,
			Longitude:   r.Longitude,
			Timezone:    r.Timezone,
		})
	}
	return locations, nil
}

// GetWeatherByCoordinates gets weather for specific coordinates using the
// free, keyless Open-Meteo forecast API
func (s *Service) GetWeatherByCoordinates(lat, lon float64) (*CurrentWeather, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(lat, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(lon, 'f', -1, 64))
	params.Set("current", "temperature_2m,apparent_temperature,relative_humidity_2m,pressure_msl,wind_speed_10m,wind_direction_10m,cloud_cover,weather_code")
	params.Set("timezone", "auto")

	var result currentForecastResult
	if err := fetchJSON(ctx, forecastEndpoint+"?"+params.Encode(), &result); err != nil {
		return nil, err
	}

	timestamp, err := time.Parse("2006-01-02T15:04", result.Current.Time)
	if err != nil {
		timestamp = time.Now()
	}

	description, icon := weatherCodeInfo(result.Current.WeatherCode)

	return &CurrentWeather{
		Temperature:   result.Current.Temperature2m,
		FeelsLike:     result.Current.ApparentTemperature,
		Humidity:      result.Current.RelativeHumidity2m,
		Pressure:      int(result.Current.PressureMSL),
		WindSpeed:     result.Current.WindSpeed10m,
		WindDirection: result.Current.WindDirection10m,
		Clouds:        result.Current.CloudCover,
		Description:   description,
		Icon:          icon,
		Timestamp:     timestamp,
	}, nil
}

// GetCurrentWeather retrieves current weather for a location by name
func (s *Service) GetCurrentWeather(city string) (*CurrentWeather, error) {
	locations, err := s.SearchLocation(city)
	if err != nil {
		return nil, err
	}
	return s.GetWeatherByCoordinates(locations[0].Latitude, locations[0].Longitude)
}

// GetForecast retrieves a daily weather forecast for a location by name
func (s *Service) GetForecast(city string, days int) ([]*Forecast, error) {
	if days < 1 || days > 16 {
		return nil, fmt.Errorf("days must be between 1 and 16")
	}

	locations, err := s.SearchLocation(city)
	if err != nil {
		return nil, err
	}
	loc := locations[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(loc.Latitude, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(loc.Longitude, 'f', -1, 64))
	params.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,rain_sum,snowfall_sum")
	params.Set("forecast_days", strconv.Itoa(days))
	params.Set("timezone", "auto")

	var result dailyForecastResult
	if err := fetchJSON(ctx, forecastEndpoint+"?"+params.Encode(), &result); err != nil {
		return nil, err
	}

	forecasts := make([]*Forecast, 0, len(result.Daily.Time))
	for i := range result.Daily.Time {
		date, err := time.Parse("2006-01-02", result.Daily.Time[i])
		if err != nil {
			continue
		}

		var code int
		if i < len(result.Daily.WeatherCode) {
			code = result.Daily.WeatherCode[i]
		}
		description, icon := weatherCodeInfo(code)

		f := &Forecast{
			Date:        date,
			Description: description,
			Icon:        icon,
		}
		if i < len(result.Daily.Temperature2mMax) {
			f.TempMax = result.Daily.Temperature2mMax[i]
		}
		if i < len(result.Daily.Temperature2mMin) {
			f.TempMin = result.Daily.Temperature2mMin[i]
		}
		if i < len(result.Daily.RainSum) {
			f.Rain = result.Daily.RainSum[i]
		}
		if i < len(result.Daily.SnowfallSum) {
			f.Snow = result.Daily.SnowfallSum[i]
		}
		forecasts = append(forecasts, f)
	}
	return forecasts, nil
}

// resolveLocation searches for city by name and returns the best match,
// shared by every sub-tool that accepts a location name instead of raw
// coordinates
func (s *Service) resolveLocation(city string) (*Location, error) {
	locations, err := s.SearchLocation(city)
	if err != nil {
		return nil, err
	}
	return locations[0], nil
}

// AirQuality holds pollutant concentrations and air quality indices for a
// location, sourced from the free, keyless Open-Meteo Air Quality API
type AirQuality struct {
	Location        string  `json:"location"`
	Time            string  `json:"time"`
	EuropeanAQI     int     `json:"european_aqi"`
	USAQI           int     `json:"us_aqi"`
	PM10            float64 `json:"pm10"`
	PM2_5           float64 `json:"pm2_5"`
	CarbonMonoxide  float64 `json:"carbon_monoxide"`
	NitrogenDioxide float64 `json:"nitrogen_dioxide"`
	SulphurDioxide  float64 `json:"sulphur_dioxide"`
	Ozone           float64 `json:"ozone"`
}

// UVIndex holds the current and clear-sky UV index for a location
type UVIndex struct {
	Location        string  `json:"location"`
	Time            string  `json:"time"`
	UVIndex         float64 `json:"uv_index"`
	UVIndexClearSky float64 `json:"uv_index_clear_sky"`
}

// Pollen holds pollen concentrations for a location; coverage is limited to
// the European domain by the upstream Open-Meteo model — values outside
// that domain are zero, not an error
type Pollen struct {
	Location       string  `json:"location"`
	Time           string  `json:"time"`
	AlderPollen    float64 `json:"alder_pollen"`
	BirchPollen    float64 `json:"birch_pollen"`
	GrassPollen    float64 `json:"grass_pollen"`
	MugwortPollen  float64 `json:"mugwort_pollen"`
	OlivePollen    float64 `json:"olive_pollen"`
	RagweedPollen  float64 `json:"ragweed_pollen"`
	CoverageRegion string  `json:"coverage_region"`
}

// airQualityCurrentResult mirrors the Open-Meteo Air Quality API "current" block
type airQualityCurrentResult struct {
	Current struct {
		Time            string  `json:"time"`
		EuropeanAQI     int     `json:"european_aqi"`
		USAQI           int     `json:"us_aqi"`
		PM10            float64 `json:"pm10"`
		PM2_5           float64 `json:"pm2_5"`
		CarbonMonoxide  float64 `json:"carbon_monoxide"`
		NitrogenDioxide float64 `json:"nitrogen_dioxide"`
		SulphurDioxide  float64 `json:"sulphur_dioxide"`
		Ozone           float64 `json:"ozone"`
		UVIndex         float64 `json:"uv_index"`
		UVIndexClearSky float64 `json:"uv_index_clear_sky"`
		AlderPollen     float64 `json:"alder_pollen"`
		BirchPollen     float64 `json:"birch_pollen"`
		GrassPollen     float64 `json:"grass_pollen"`
		MugwortPollen   float64 `json:"mugwort_pollen"`
		OlivePollen     float64 `json:"olive_pollen"`
		RagweedPollen   float64 `json:"ragweed_pollen"`
	} `json:"current"`
}

// fetchAirQuality is shared by GetAirQuality, GetUVIndex, and GetPollen —
// all three read from the same Open-Meteo Air Quality API "current" block
func (s *Service) fetchAirQuality(city string) (*Location, *airQualityCurrentResult, error) {
	loc, err := s.resolveLocation(city)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(loc.Latitude, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(loc.Longitude, 'f', -1, 64))
	params.Set("current", "european_aqi,us_aqi,pm10,pm2_5,carbon_monoxide,nitrogen_dioxide,sulphur_dioxide,ozone,uv_index,uv_index_clear_sky,alder_pollen,birch_pollen,grass_pollen,mugwort_pollen,olive_pollen,ragweed_pollen")
	params.Set("timezone", "auto")

	var result airQualityCurrentResult
	if err := fetchJSON(ctx, airQualityEndpoint+"?"+params.Encode(), &result); err != nil {
		return nil, nil, err
	}
	return loc, &result, nil
}

// GetAirQuality retrieves current air quality indices and pollutant
// concentrations for a location by name
func (s *Service) GetAirQuality(city string) (*AirQuality, error) {
	loc, result, err := s.fetchAirQuality(city)
	if err != nil {
		return nil, err
	}
	return &AirQuality{
		Location:        loc.City,
		Time:            result.Current.Time,
		EuropeanAQI:     result.Current.EuropeanAQI,
		USAQI:           result.Current.USAQI,
		PM10:            result.Current.PM10,
		PM2_5:           result.Current.PM2_5,
		CarbonMonoxide:  result.Current.CarbonMonoxide,
		NitrogenDioxide: result.Current.NitrogenDioxide,
		SulphurDioxide:  result.Current.SulphurDioxide,
		Ozone:           result.Current.Ozone,
	}, nil
}

// GetUVIndex retrieves the current and clear-sky UV index for a location by name
func (s *Service) GetUVIndex(city string) (*UVIndex, error) {
	loc, result, err := s.fetchAirQuality(city)
	if err != nil {
		return nil, err
	}
	return &UVIndex{
		Location:        loc.City,
		Time:            result.Current.Time,
		UVIndex:         result.Current.UVIndex,
		UVIndexClearSky: result.Current.UVIndexClearSky,
	}, nil
}

// GetPollen retrieves current pollen concentrations for a location by name;
// the upstream model only covers the European domain, so non-European
// locations return zeroed values rather than an error
func (s *Service) GetPollen(city string) (*Pollen, error) {
	loc, result, err := s.fetchAirQuality(city)
	if err != nil {
		return nil, err
	}
	region := "europe"
	if loc.CountryCode != "" && !isEuropeanCountryCode(loc.CountryCode) {
		region = "unsupported (Open-Meteo pollen data covers Europe only; values will read zero)"
	}
	return &Pollen{
		Location:       loc.City,
		Time:           result.Current.Time,
		AlderPollen:    result.Current.AlderPollen,
		BirchPollen:    result.Current.BirchPollen,
		GrassPollen:    result.Current.GrassPollen,
		MugwortPollen:  result.Current.MugwortPollen,
		OlivePollen:    result.Current.OlivePollen,
		RagweedPollen:  result.Current.RagweedPollen,
		CoverageRegion: region,
	}, nil
}

// isEuropeanCountryCode reports whether an ISO 3166-1 alpha-2 country code
// falls within the Open-Meteo pollen model's European coverage domain
func isEuropeanCountryCode(code string) bool {
	europeanCodes := map[string]bool{
		"AL": true, "AD": true, "AT": true, "BY": true, "BE": true, "BA": true,
		"BG": true, "HR": true, "CY": true, "CZ": true, "DK": true, "EE": true,
		"FI": true, "FR": true, "DE": true, "GR": true, "HU": true, "IS": true,
		"IE": true, "IT": true, "XK": true, "LV": true, "LI": true, "LT": true,
		"LU": true, "MT": true, "MD": true, "MC": true, "ME": true, "NL": true,
		"MK": true, "NO": true, "PL": true, "PT": true, "RO": true, "RU": true,
		"SM": true, "RS": true, "SK": true, "SI": true, "ES": true, "SE": true,
		"CH": true, "UA": true, "GB": true, "VA": true,
	}
	return europeanCodes[code]
}

// Astronomy holds sunrise/sunset and daylight data for a location
type Astronomy struct {
	Location         string  `json:"location"`
	Date             string  `json:"date"`
	Sunrise          string  `json:"sunrise"`
	Sunset           string  `json:"sunset"`
	DaylightDuration float64 `json:"daylight_duration_seconds"`
	SunshineDuration float64 `json:"sunshine_duration_seconds"`
	UVIndexMax       float64 `json:"uv_index_max"`
}

// astronomyResult mirrors the Open-Meteo forecast API "daily" block used for astronomy data
type astronomyResult struct {
	Daily struct {
		Time             []string  `json:"time"`
		Sunrise          []string  `json:"sunrise"`
		Sunset           []string  `json:"sunset"`
		DaylightDuration []float64 `json:"daylight_duration"`
		SunshineDuration []float64 `json:"sunshine_duration"`
		UVIndexMax       []float64 `json:"uv_index_max"`
	} `json:"daily"`
}

// GetAstronomy retrieves sunrise, sunset, and daylight data for today at a
// location by name
func (s *Service) GetAstronomy(city string) (*Astronomy, error) {
	loc, err := s.resolveLocation(city)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(loc.Latitude, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(loc.Longitude, 'f', -1, 64))
	params.Set("daily", "sunrise,sunset,daylight_duration,sunshine_duration,uv_index_max")
	params.Set("forecast_days", "1")
	params.Set("timezone", "auto")

	var result astronomyResult
	if err := fetchJSON(ctx, forecastEndpoint+"?"+params.Encode(), &result); err != nil {
		return nil, err
	}
	if len(result.Daily.Time) == 0 {
		return nil, fmt.Errorf("no astronomy data returned for %q", city)
	}

	a := &Astronomy{
		Location: loc.City,
		Date:     result.Daily.Time[0],
	}
	if len(result.Daily.Sunrise) > 0 {
		a.Sunrise = result.Daily.Sunrise[0]
	}
	if len(result.Daily.Sunset) > 0 {
		a.Sunset = result.Daily.Sunset[0]
	}
	if len(result.Daily.DaylightDuration) > 0 {
		a.DaylightDuration = result.Daily.DaylightDuration[0]
	}
	if len(result.Daily.SunshineDuration) > 0 {
		a.SunshineDuration = result.Daily.SunshineDuration[0]
	}
	if len(result.Daily.UVIndexMax) > 0 {
		a.UVIndexMax = result.Daily.UVIndexMax[0]
	}
	return a, nil
}

// HistoricalDay holds one day of historical weather observations
type HistoricalDay struct {
	Date            string  `json:"date"`
	TempMax         float64 `json:"temp_max"`
	TempMin         float64 `json:"temp_min"`
	PrecipitationMM float64 `json:"precipitation_mm"`
	WindSpeedMaxKMH float64 `json:"wind_speed_max_kmh"`
}

// historicalResult mirrors the Open-Meteo Historical Weather (archive) API "daily" block
type historicalResult struct {
	Daily struct {
		Time             []string  `json:"time"`
		Temperature2mMax []float64 `json:"temperature_2m_max"`
		Temperature2mMin []float64 `json:"temperature_2m_min"`
		PrecipitationSum []float64 `json:"precipitation_sum"`
		WindSpeed10mMax  []float64 `json:"windspeed_10m_max"`
	} `json:"daily"`
}

// GetHistorical retrieves historical daily weather observations for a
// location by name between startDate and endDate (both "YYYY-MM-DD")
func (s *Service) GetHistorical(city, startDate, endDate string) ([]*HistoricalDay, error) {
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start_date and end_date are required (YYYY-MM-DD)")
	}
	if _, err := time.Parse("2006-01-02", startDate); err != nil {
		return nil, fmt.Errorf("invalid start_date: %w", err)
	}
	if _, err := time.Parse("2006-01-02", endDate); err != nil {
		return nil, fmt.Errorf("invalid end_date: %w", err)
	}

	loc, err := s.resolveLocation(city)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(loc.Latitude, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(loc.Longitude, 'f', -1, 64))
	params.Set("start_date", startDate)
	params.Set("end_date", endDate)
	params.Set("daily", "temperature_2m_max,temperature_2m_min,precipitation_sum,windspeed_10m_max")
	params.Set("timezone", "auto")

	var result historicalResult
	if err := fetchJSON(ctx, archiveEndpoint+"?"+params.Encode(), &result); err != nil {
		return nil, err
	}

	days := make([]*HistoricalDay, 0, len(result.Daily.Time))
	for i, date := range result.Daily.Time {
		d := &HistoricalDay{Date: date}
		if i < len(result.Daily.Temperature2mMax) {
			d.TempMax = result.Daily.Temperature2mMax[i]
		}
		if i < len(result.Daily.Temperature2mMin) {
			d.TempMin = result.Daily.Temperature2mMin[i]
		}
		if i < len(result.Daily.PrecipitationSum) {
			d.PrecipitationMM = result.Daily.PrecipitationSum[i]
		}
		if i < len(result.Daily.WindSpeed10mMax) {
			d.WindSpeedMaxKMH = result.Daily.WindSpeed10mMax[i]
		}
		days = append(days, d)
	}
	return days, nil
}

// HourlyEntry holds one hour of forecast data
type HourlyEntry struct {
	Time                     string  `json:"time"`
	Temperature              float64 `json:"temperature"`
	PrecipitationProbability int     `json:"precipitation_probability"`
	WindSpeed                float64 `json:"wind_speed"`
	Description              string  `json:"description"`
	Icon                     string  `json:"icon"`
}

// hourlyResult mirrors the Open-Meteo forecast API "hourly" block
type hourlyResult struct {
	Hourly struct {
		Time                     []string  `json:"time"`
		Temperature2m            []float64 `json:"temperature_2m"`
		PrecipitationProbability []int     `json:"precipitation_probability"`
		WeatherCode              []int     `json:"weather_code"`
		WindSpeed10m             []float64 `json:"wind_speed_10m"`
	} `json:"hourly"`
}

// GetHourly retrieves an hour-by-hour forecast for a location by name,
// trimmed to the requested number of hours (1-48)
func (s *Service) GetHourly(city string, hours int) ([]*HourlyEntry, error) {
	if hours < 1 || hours > 48 {
		return nil, fmt.Errorf("hours must be between 1 and 48")
	}

	loc, err := s.resolveLocation(city)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(loc.Latitude, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(loc.Longitude, 'f', -1, 64))
	params.Set("hourly", "temperature_2m,precipitation_probability,weather_code,wind_speed_10m")
	params.Set("forecast_days", "2")
	params.Set("timezone", "auto")

	var result hourlyResult
	if err := fetchJSON(ctx, forecastEndpoint+"?"+params.Encode(), &result); err != nil {
		return nil, err
	}

	limit := hours
	if limit > len(result.Hourly.Time) {
		limit = len(result.Hourly.Time)
	}

	entries := make([]*HourlyEntry, 0, limit)
	for i := 0; i < limit; i++ {
		var code int
		if i < len(result.Hourly.WeatherCode) {
			code = result.Hourly.WeatherCode[i]
		}
		description, icon := weatherCodeInfo(code)

		e := &HourlyEntry{
			Time:        result.Hourly.Time[i],
			Description: description,
			Icon:        icon,
		}
		if i < len(result.Hourly.Temperature2m) {
			e.Temperature = result.Hourly.Temperature2m[i]
		}
		if i < len(result.Hourly.PrecipitationProbability) {
			e.PrecipitationProbability = result.Hourly.PrecipitationProbability[i]
		}
		if i < len(result.Hourly.WindSpeed10m) {
			e.WindSpeed = result.Hourly.WindSpeed10m[i]
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// MarineConditions holds current sea-state observations for a location
type MarineConditions struct {
	Location           string  `json:"location"`
	Time               string  `json:"time"`
	WaveHeightM        float64 `json:"wave_height_m"`
	WaveDirectionDeg   float64 `json:"wave_direction_deg"`
	WavePeriodS        float64 `json:"wave_period_s"`
	SwellWaveHeightM   float64 `json:"swell_wave_height_m"`
	WindWaveHeightM    float64 `json:"wind_wave_height_m"`
	OceanCurrentVelKMH float64 `json:"ocean_current_velocity_kmh"`
	SeaSurfaceTempC    float64 `json:"sea_surface_temperature_c"`
}

// marineCurrentResult mirrors the Open-Meteo Marine Weather API "current" block
type marineCurrentResult struct {
	Current struct {
		Time                  string  `json:"time"`
		WaveHeight            float64 `json:"wave_height"`
		WaveDirection         float64 `json:"wave_direction"`
		WavePeriod            float64 `json:"wave_period"`
		SwellWaveHeight       float64 `json:"swell_wave_height"`
		WindWaveHeight        float64 `json:"wind_wave_height"`
		OceanCurrentVelocity  float64 `json:"ocean_current_velocity"`
		SeaSurfaceTemperature float64 `json:"sea_surface_temperature"`
	} `json:"current"`
}

// GetMarine retrieves current sea-state conditions for a coastal location by
// name; inland locations return zeroed values (no error) since the upstream
// model has no data there
func (s *Service) GetMarine(city string) (*MarineConditions, error) {
	loc, err := s.resolveLocation(city)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(loc.Latitude, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(loc.Longitude, 'f', -1, 64))
	params.Set("current", "wave_height,wave_direction,wave_period,swell_wave_height,wind_wave_height,ocean_current_velocity,sea_surface_temperature")
	params.Set("timezone", "auto")

	var result marineCurrentResult
	if err := fetchJSON(ctx, marineEndpoint+"?"+params.Encode(), &result); err != nil {
		return nil, err
	}

	return &MarineConditions{
		Location:           loc.City,
		Time:               result.Current.Time,
		WaveHeightM:        result.Current.WaveHeight,
		WaveDirectionDeg:   result.Current.WaveDirection,
		WavePeriodS:        result.Current.WavePeriod,
		SwellWaveHeightM:   result.Current.SwellWaveHeight,
		WindWaveHeightM:    result.Current.WindWaveHeight,
		OceanCurrentVelKMH: result.Current.OceanCurrentVelocity,
		SeaSurfaceTempC:    result.Current.SeaSurfaceTemperature,
	}, nil
}

// Temperature conversion utilities
func (s *Service) CelsiusToFahrenheit(celsius float64) float64 {
	return (celsius * 9 / 5) + 32
}

func (s *Service) FahrenheitToCelsius(fahrenheit float64) float64 {
	return (fahrenheit - 32) * 5 / 9
}

func (s *Service) CelsiusToKelvin(celsius float64) float64 {
	return celsius + 273.15
}

func (s *Service) KelvinToCelsius(kelvin float64) float64 {
	return kelvin - 273.15
}

// Wind speed conversions
func (s *Service) MPHToKMH(mph float64) float64 {
	return mph * 1.60934
}

func (s *Service) KMHToMPH(kmh float64) float64 {
	return kmh / 1.60934
}

func (s *Service) MSToKMH(ms float64) float64 {
	return ms * 3.6
}

func (s *Service) KMHToMS(kmh float64) float64 {
	return kmh / 3.6
}

// Weather condition helpers
func (s *Service) GetWeatherEmoji(condition string) string {
	emojiMap := map[string]string{
		"clear":         "☀️",
		"sunny":         "☀️",
		"clouds":        "☁️",
		"cloudy":        "☁️",
		"rain":          "🌧️",
		"rainy":         "🌧️",
		"snow":          "❄️",
		"snowy":         "❄️",
		"thunderstorm":  "⛈️",
		"fog":           "🌫️",
		"foggy":         "🌫️",
		"wind":          "💨",
		"windy":         "💨",
		"partly cloudy": "⛅",
	}

	if emoji, ok := emojiMap[condition]; ok {
		return emoji
	}
	return "🌡️"
}

// weatherCodeInfo maps a WMO weather interpretation code (as returned by
// Open-Meteo) to a human-readable description and an emoji icon
func weatherCodeInfo(code int) (string, string) {
	switch {
	case code == 0:
		return "Clear sky", "☀️"
	case code == 1:
		return "Mainly clear", "🌤️"
	case code == 2:
		return "Partly cloudy", "⛅"
	case code == 3:
		return "Overcast", "☁️"
	case code == 45 || code == 48:
		return "Fog", "🌫️"
	case code >= 51 && code <= 57:
		return "Drizzle", "🌦️"
	case code >= 61 && code <= 67:
		return "Rain", "🌧️"
	case code >= 71 && code <= 77:
		return "Snow", "❄️"
	case code >= 80 && code <= 82:
		return "Rain showers", "🌧️"
	case code == 85 || code == 86:
		return "Snow showers", "🌨️"
	case code == 95:
		return "Thunderstorm", "⛈️"
	case code == 96 || code == 99:
		return "Thunderstorm with hail", "⛈️"
	default:
		return "Unknown", "🌡️"
	}
}
