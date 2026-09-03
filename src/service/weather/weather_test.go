package weather

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectingTransport rewrites every outgoing request's scheme/host to
// point at a local httptest.Server while preserving path and query, so
// the package's hardcoded provider endpoint constants can be exercised
// against a fully-controlled fake server without any code changes to
// the package under test.
type redirectingTransport struct {
	targetURL *url.URL
	base      http.RoundTripper
}

func (rt *redirectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.targetURL.Scheme
	req.URL.Host = rt.targetURL.Host
	req.Host = rt.targetURL.Host
	return rt.base.RoundTrip(req)
}

// withMockServer swaps the package-level httpClient for one that
// redirects all traffic to the given httptest.Server for the duration
// of the test, restoring the original client on cleanup.
func withMockServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	original := httpClient
	target, err := url.Parse(server.URL)
	require.NoError(t, err)

	httpClient = &http.Client{
		Timeout:   original.Timeout,
		Transport: &redirectingTransport{targetURL: target, base: http.DefaultTransport},
	}

	t.Cleanup(func() {
		httpClient = original
	})
}

// Covers New: returns a non-nil, usable Service.
func TestNew(t *testing.T) {
	s := New()
	require.NotNil(t, s)
}

// Covers SearchLocation: empty query rejected without any network call,
// a successful geocoding response, an empty-results response (mapped
// to a "no locations found" error), and a non-200 upstream status.
func TestSearchLocation(t *testing.T) {
	s := New()

	t.Run("empty query", func(t *testing.T) {
		locs, err := s.SearchLocation("")
		require.Error(t, err)
		assert.Nil(t, locs)
		assert.Contains(t, err.Error(), "query is required")
	})

	t.Run("successful results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[{"name":"London","latitude":51.5,"longitude":-0.12,"country":"United Kingdom","timezone":"Europe/London"}]}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		locs, err := s.SearchLocation("London")
		require.NoError(t, err)
		require.Len(t, locs, 1)
		assert.Equal(t, "London", locs[0].City)
		assert.Equal(t, "United Kingdom", locs[0].Country)
		assert.Equal(t, 51.5, locs[0].Latitude)
	})

	t.Run("no results found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"results":[]}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		locs, err := s.SearchLocation("Nowhereville")
		require.Error(t, err)
		assert.Nil(t, locs)
		assert.Contains(t, err.Error(), "no locations found")
	})

	t.Run("upstream error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()
		withMockServer(t, server)

		locs, err := s.SearchLocation("London")
		require.Error(t, err)
		assert.Nil(t, locs)
		assert.Contains(t, err.Error(), "status 500")
	})

	t.Run("malformed json body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{not-json`))
		}))
		defer server.Close()
		withMockServer(t, server)

		locs, err := s.SearchLocation("London")
		require.Error(t, err)
		assert.Nil(t, locs)
		assert.Contains(t, err.Error(), "failed to decode")
	})
}

// Covers GetWeatherByCoordinates: successful decode/mapping including
// the weather-code-to-description translation, and the fallback to
// time.Now() when the provider's "current.time" field fails to parse.
func TestGetWeatherByCoordinates(t *testing.T) {
	s := New()

	t.Run("successful current weather", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"current":{"time":"2024-01-01T12:00","temperature_2m":21.5,"apparent_temperature":20.1,"relative_humidity_2m":55,"pressure_msl":1012.3,"wind_speed_10m":12.4,"wind_direction_10m":180,"cloud_cover":40,"weather_code":2}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		w, err := s.GetWeatherByCoordinates(51.5, -0.12)
		require.NoError(t, err)
		assert.Equal(t, 21.5, w.Temperature)
		assert.Equal(t, 55, w.Humidity)
		assert.Equal(t, "Partly cloudy", w.Description)
		assert.Equal(t, "⛅", w.Icon)
		assert.Equal(t, 2024, w.Timestamp.Year())
	})

	t.Run("unparseable timestamp falls back to now", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"current":{"time":"not-a-time","temperature_2m":10,"weather_code":0}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		before := time.Now().Add(-time.Minute)
		w, err := s.GetWeatherByCoordinates(0, 0)
		require.NoError(t, err)
		assert.True(t, w.Timestamp.After(before))
	})

	t.Run("upstream error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()
		withMockServer(t, server)

		w, err := s.GetWeatherByCoordinates(0, 0)
		require.Error(t, err)
		assert.Nil(t, w)
	})
}

// Covers GetCurrentWeather: the geocode-then-forecast chain, and
// propagation of a geocoding error without attempting the forecast call.
func TestGetCurrentWeather(t *testing.T) {
	s := New()

	t.Run("chains geocode then forecast", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"Paris","latitude":48.85,"longitude":2.35,"country":"France"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"current":{"time":"2024-01-01T12:00","temperature_2m":15,"weather_code":0}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		w, err := s.GetCurrentWeather("Paris")
		require.NoError(t, err)
		assert.Equal(t, 15.0, w.Temperature)
	})

	t.Run("propagates geocode error", func(t *testing.T) {
		w, err := s.GetCurrentWeather("")
		require.Error(t, err)
		assert.Nil(t, w)
		assert.Contains(t, err.Error(), "query is required")
	})
}

// Covers GetForecast: out-of-range day counts rejected before any
// network call, and a successful multi-day forecast decode including
// the index-bounds-checked optional fields (rain/snow) and a malformed
// date entry being skipped rather than aborting the whole forecast.
func TestGetForecast(t *testing.T) {
	s := New()

	t.Run("days too low", func(t *testing.T) {
		f, err := s.GetForecast("London", 0)
		require.Error(t, err)
		assert.Nil(t, f)
		assert.Contains(t, err.Error(), "between 1 and 16")
	})

	t.Run("days too high", func(t *testing.T) {
		f, err := s.GetForecast("London", 17)
		require.Error(t, err)
		assert.Nil(t, f)
	})

	t.Run("successful multi-day forecast", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"London","latitude":51.5,"longitude":-0.12,"country":"UK"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"daily":{"time":["2024-01-01","bad-date","2024-01-03"],"weather_code":[0,1,61],"temperature_2m_max":[10,11,12],"temperature_2m_min":[2,3,4],"rain_sum":[0,0.5],"snowfall_sum":[]}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		forecasts, err := s.GetForecast("London", 3)
		require.NoError(t, err)
		// The malformed "bad-date" entry is skipped, leaving 2 valid days.
		require.Len(t, forecasts, 2)
		assert.Equal(t, 10.0, forecasts[0].TempMax)
		assert.Equal(t, 2.0, forecasts[0].TempMin)
		assert.Equal(t, 0.0, forecasts[0].Rain)
		assert.Equal(t, 0.0, forecasts[0].Snow)
		assert.Equal(t, "Rain", forecasts[1].Description)
	})
}

// Covers every temperature and wind-speed conversion helper, including
// round-trip consistency between inverse pairs.
func TestConversions(t *testing.T) {
	s := New()

	assert.InDelta(t, 32.0, s.CelsiusToFahrenheit(0), 0.001)
	assert.InDelta(t, 212.0, s.CelsiusToFahrenheit(100), 0.001)
	assert.InDelta(t, 0.0, s.FahrenheitToCelsius(32), 0.001)
	assert.InDelta(t, 100.0, s.FahrenheitToCelsius(212), 0.001)
	assert.InDelta(t, 273.15, s.CelsiusToKelvin(0), 0.001)
	assert.InDelta(t, 0.0, s.KelvinToCelsius(273.15), 0.001)

	assert.InDelta(t, 16.0934, s.MPHToKMH(10), 0.001)
	assert.InDelta(t, 10.0, s.KMHToMPH(16.0934), 0.001)
	assert.InDelta(t, 36.0, s.MSToKMH(10), 0.001)
	assert.InDelta(t, 10.0, s.KMHToMS(36), 0.001)

	// Round-trip sanity: converting there and back returns the original.
	c := 23.4
	assert.InDelta(t, c, s.FahrenheitToCelsius(s.CelsiusToFahrenheit(c)), 0.0001)
	assert.InDelta(t, c, s.KelvinToCelsius(s.CelsiusToKelvin(c)), 0.0001)
}

// Covers GetWeatherEmoji: every mapped condition string, case
// sensitivity of the lookup (unmapped uppercase falls through to
// default), and the unmapped-condition default fallback.
func TestGetWeatherEmoji(t *testing.T) {
	s := New()

	tests := []struct {
		condition string
		want      string
	}{
		{"clear", "☀️"},
		{"sunny", "☀️"},
		{"clouds", "☁️"},
		{"rain", "🌧️"},
		{"snow", "❄️"},
		{"thunderstorm", "⛈️"},
		{"fog", "🌫️"},
		{"wind", "💨"},
		{"partly cloudy", "⛅"},
		{"unknown-condition", "🌡️"},
		{"", "🌡️"},
		{"CLEAR", "🌡️"},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			assert.Equal(t, tt.want, s.GetWeatherEmoji(tt.condition))
		})
	}
}

// Covers GetAirQuality, GetUVIndex, and GetPollen: all three share
// fetchAirQuality, so exercise the successful decode path once per
// public method plus the European-vs-non-European pollen coverage note.
func TestGetAirQualityUVPollen(t *testing.T) {
	s := New()

	airQualityServer := func(country, countryCode string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"City","latitude":1,"longitude":2,"country":"` + country + `","country_code":"` + countryCode + `"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"current":{"time":"2024-01-01T12:00","european_aqi":25,"us_aqi":30,"pm10":5.1,"pm2_5":2.3,"carbon_monoxide":100,"nitrogen_dioxide":10,"sulphur_dioxide":1,"ozone":50,"uv_index":4.5,"uv_index_clear_sky":5.1,"alder_pollen":1,"birch_pollen":2,"grass_pollen":3,"mugwort_pollen":4,"olive_pollen":5,"ragweed_pollen":6}}`))
		}))
	}

	t.Run("GetAirQuality success", func(t *testing.T) {
		server := airQualityServer("United Kingdom", "GB")
		defer server.Close()
		withMockServer(t, server)

		aq, err := s.GetAirQuality("London")
		require.NoError(t, err)
		assert.Equal(t, "City", aq.Location)
		assert.Equal(t, 25, aq.EuropeanAQI)
		assert.Equal(t, 30, aq.USAQI)
		assert.Equal(t, 5.1, aq.PM10)
	})

	t.Run("GetUVIndex success", func(t *testing.T) {
		server := airQualityServer("United Kingdom", "GB")
		defer server.Close()
		withMockServer(t, server)

		uv, err := s.GetUVIndex("London")
		require.NoError(t, err)
		assert.Equal(t, 4.5, uv.UVIndex)
		assert.Equal(t, 5.1, uv.UVIndexClearSky)
	})

	t.Run("GetPollen European coverage", func(t *testing.T) {
		server := airQualityServer("United Kingdom", "GB")
		defer server.Close()
		withMockServer(t, server)

		p, err := s.GetPollen("London")
		require.NoError(t, err)
		assert.Equal(t, "europe", p.CoverageRegion)
		assert.Equal(t, 3.0, p.GrassPollen)
	})

	t.Run("GetPollen non-European coverage note", func(t *testing.T) {
		server := airQualityServer("United States", "US")
		defer server.Close()
		withMockServer(t, server)

		p, err := s.GetPollen("Miami")
		require.NoError(t, err)
		assert.Contains(t, p.CoverageRegion, "unsupported")
	})

	t.Run("propagates geocode error", func(t *testing.T) {
		_, err := s.GetAirQuality("")
		require.Error(t, err)
	})
}

// Covers GetAstronomy: successful decode of the daily astronomy block and
// the "no data returned" error path.
func TestGetAstronomy(t *testing.T) {
	s := New()

	t.Run("successful astronomy", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"London","latitude":51.5,"longitude":-0.12,"country":"UK"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"daily":{"time":["2024-01-01"],"sunrise":["2024-01-01T08:00"],"sunset":["2024-01-01T16:00"],"daylight_duration":[28800],"sunshine_duration":[20000],"uv_index_max":[3.2]}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		a, err := s.GetAstronomy("London")
		require.NoError(t, err)
		assert.Equal(t, "2024-01-01", a.Date)
		assert.Equal(t, "2024-01-01T08:00", a.Sunrise)
		assert.Equal(t, "2024-01-01T16:00", a.Sunset)
		assert.Equal(t, 28800.0, a.DaylightDuration)
		assert.Equal(t, 3.2, a.UVIndexMax)
	})

	t.Run("no data returned", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"London","latitude":51.5,"longitude":-0.12,"country":"UK"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"daily":{"time":[]}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		a, err := s.GetAstronomy("London")
		require.Error(t, err)
		assert.Nil(t, a)
		assert.Contains(t, err.Error(), "no astronomy data")
	})
}

// Covers GetHistorical: missing/invalid date validation rejected before
// any network call, and a successful multi-day decode.
func TestGetHistorical(t *testing.T) {
	s := New()

	t.Run("missing dates", func(t *testing.T) {
		days, err := s.GetHistorical("London", "", "2024-01-07")
		require.Error(t, err)
		assert.Nil(t, days)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("invalid start date", func(t *testing.T) {
		days, err := s.GetHistorical("London", "not-a-date", "2024-01-07")
		require.Error(t, err)
		assert.Nil(t, days)
		assert.Contains(t, err.Error(), "invalid start_date")
	})

	t.Run("invalid end date", func(t *testing.T) {
		days, err := s.GetHistorical("London", "2024-01-01", "not-a-date")
		require.Error(t, err)
		assert.Nil(t, days)
		assert.Contains(t, err.Error(), "invalid end_date")
	})

	t.Run("successful historical range", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"London","latitude":51.5,"longitude":-0.12,"country":"UK"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"daily":{"time":["2024-01-01","2024-01-02"],"temperature_2m_max":[10,11],"temperature_2m_min":[2,3],"precipitation_sum":[0,1.2],"windspeed_10m_max":[15,20]}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		days, err := s.GetHistorical("London", "2024-01-01", "2024-01-02")
		require.NoError(t, err)
		require.Len(t, days, 2)
		assert.Equal(t, 10.0, days[0].TempMax)
		assert.Equal(t, 1.2, days[1].PrecipitationMM)
		assert.Equal(t, 20.0, days[1].WindSpeedMaxKMH)
	})
}

// Covers GetHourly: out-of-range hour counts rejected before any network
// call, and a successful decode trimmed to the requested hour count.
func TestGetHourly(t *testing.T) {
	s := New()

	t.Run("hours too low", func(t *testing.T) {
		entries, err := s.GetHourly("London", 0)
		require.Error(t, err)
		assert.Nil(t, entries)
	})

	t.Run("hours too high", func(t *testing.T) {
		entries, err := s.GetHourly("London", 49)
		require.Error(t, err)
		assert.Nil(t, entries)
	})

	t.Run("successful hourly forecast trimmed to requested hours", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"London","latitude":51.5,"longitude":-0.12,"country":"UK"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"hourly":{"time":["2024-01-01T00:00","2024-01-01T01:00","2024-01-01T02:00"],"temperature_2m":[5,6,7],"precipitation_probability":[0,10,20],"weather_code":[0,1,2],"wind_speed_10m":[3,4,5]}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		entries, err := s.GetHourly("London", 2)
		require.NoError(t, err)
		require.Len(t, entries, 2)
		assert.Equal(t, 5.0, entries[0].Temperature)
		assert.Equal(t, "Clear sky", entries[0].Description)
		assert.Equal(t, 6.0, entries[1].Temperature)
	})
}

// Covers GetMarine: successful decode of the current marine conditions block.
func TestGetMarine(t *testing.T) {
	s := New()

	t.Run("successful marine conditions", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"Miami","latitude":25.7,"longitude":-80.1,"country":"United States"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"current":{"time":"2024-01-01T12:00","wave_height":1.2,"wave_direction":180,"wave_period":6.5,"swell_wave_height":0.8,"wind_wave_height":0.4,"ocean_current_velocity":0.3,"sea_surface_temperature":24.1}}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		m, err := s.GetMarine("Miami")
		require.NoError(t, err)
		assert.Equal(t, "Miami", m.Location)
		assert.Equal(t, 1.2, m.WaveHeightM)
		assert.Equal(t, 24.1, m.SeaSurfaceTempC)
	})

	t.Run("propagates geocode error", func(t *testing.T) {
		_, err := s.GetMarine("")
		require.Error(t, err)
	})
}

// Covers weatherCodeInfo across every documented WMO code range/exact
// match plus an unmapped code, verifying every switch branch fires.
func TestWeatherCodeInfo(t *testing.T) {
	tests := []struct {
		code     int
		wantDesc string
		wantIcon string
	}{
		{0, "Clear sky", "☀️"},
		{1, "Mainly clear", "🌤️"},
		{2, "Partly cloudy", "⛅"},
		{3, "Overcast", "☁️"},
		{45, "Fog", "🌫️"},
		{48, "Fog", "🌫️"},
		{53, "Drizzle", "🌦️"},
		{63, "Rain", "🌧️"},
		{75, "Snow", "❄️"},
		{81, "Rain showers", "🌧️"},
		{85, "Snow showers", "🌨️"},
		{95, "Thunderstorm", "⛈️"},
		{96, "Thunderstorm with hail", "⛈️"},
		{99, "Thunderstorm with hail", "⛈️"},
		{9999, "Unknown", "🌡️"},
	}

	for _, tt := range tests {
		desc, icon := weatherCodeInfo(tt.code)
		assert.Equal(t, tt.wantDesc, desc, "code %d description", tt.code)
		assert.Equal(t, tt.wantIcon, icon, "code %d icon", tt.code)
	}
}
