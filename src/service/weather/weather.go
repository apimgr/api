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
	City      string  `json:"city"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
}

// httpClient is a shared client with a hard timeout for the keyless
// Open-Meteo provider (no API key, free, rate-limited by fair use)
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

const (
	geocodeEndpoint  = "https://geocoding-api.open-meteo.com/v1/search"
	forecastEndpoint = "https://api.open-meteo.com/v1/forecast"
)

// geocodeResult mirrors the Open-Meteo geocoding API response
type geocodeResult struct {
	Results []struct {
		Name      string  `json:"name"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Country   string  `json:"country"`
		Timezone  string  `json:"timezone"`
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
			City:      r.Name,
			Country:   r.Country,
			Latitude:  r.Latitude,
			Longitude: r.Longitude,
			Timezone:  r.Timezone,
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
