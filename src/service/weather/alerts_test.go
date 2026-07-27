package weather

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers GetAlerts: routing to NWS for a US location, Environment Canada
// for a Canadian location, MeteoAlarm for a European location, an empty
// (not error) result for an uncovered country, and geocode-error
// propagation.
func TestGetAlerts(t *testing.T) {
	s := New()

	t.Run("routes to NWS for US location", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"Miami","latitude":25.7,"longitude":-80.1,"country":"United States","country_code":"US"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"features":[{"properties":{"event":"Flood Warning","severity":"Severe","certainty":"Observed","urgency":"Immediate","headline":"Flood Warning issued","description":"Heavy rain expected","areaDesc":"Miami-Dade","effective":"2024-01-01T00:00:00Z","expires":"2024-01-02T00:00:00Z"}}]}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		alerts, err := s.GetAlerts("Miami")
		require.NoError(t, err)
		require.Len(t, alerts, 1)
		assert.Equal(t, "NWS (US National Weather Service)", alerts[0].Provider)
		assert.Equal(t, "Flood Warning", alerts[0].Event)
	})

	t.Run("routes to Environment Canada for CA location", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"Toronto","latitude":43.7,"longitude":-79.4,"country":"Canada","country_code":"CA"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"features":[{"properties":{"alert_name_en":"Winter Storm Warning","alert_text_en":"Heavy snow expected","feature_name_en":"Toronto","publication_datetime":"2024-01-01T00:00:00Z","expiration_datetime":"2024-01-02T00:00:00Z","risk_colour_en":"Red","confidence_en":"High","display_status":"visible"}}]}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		alerts, err := s.GetAlerts("Toronto")
		require.NoError(t, err)
		require.Len(t, alerts, 1)
		assert.Equal(t, "Environment Canada (MSC GeoMet)", alerts[0].Provider)
		assert.Equal(t, "Winter Storm Warning", alerts[0].Event)
	})

	t.Run("Environment Canada filters non-visible alerts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"Toronto","latitude":43.7,"longitude":-79.4,"country":"Canada","country_code":"CA"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"features":[{"properties":{"alert_name_en":"Expired Alert","display_status":"expired"}}]}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		alerts, err := s.GetAlerts("Toronto")
		require.NoError(t, err)
		assert.Empty(t, alerts)
	})

	t.Run("routes to MeteoAlarm for European location", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/search" {
				_, _ = w.Write([]byte(`{"results":[{"name":"London","latitude":51.5,"longitude":-0.12,"country":"United Kingdom","country_code":"GB"}]}`))
				return
			}
			_, _ = w.Write([]byte(`<feed><entry><title>Yellow Wind Warning</title><cap:areaDesc xmlns:cap="urn:oasis:names:tc:emergency:cap:1.2">Greater London</cap:areaDesc><cap:event xmlns:cap="urn:oasis:names:tc:emergency:cap:1.2">Wind</cap:event><cap:severity xmlns:cap="urn:oasis:names:tc:emergency:cap:1.2">Moderate</cap:severity><cap:certainty xmlns:cap="urn:oasis:names:tc:emergency:cap:1.2">Likely</cap:certainty><cap:urgency xmlns:cap="urn:oasis:names:tc:emergency:cap:1.2">Expected</cap:urgency><cap:onset xmlns:cap="urn:oasis:names:tc:emergency:cap:1.2">2024-01-01T00:00:00Z</cap:onset><cap:expires xmlns:cap="urn:oasis:names:tc:emergency:cap:1.2">2024-01-02T00:00:00Z</cap:expires></entry></feed>`))
		}))
		defer server.Close()
		withMockServer(t, server)

		alerts, err := s.GetAlerts("London")
		require.NoError(t, err)
		require.Len(t, alerts, 1)
		assert.Equal(t, "MeteoAlarm", alerts[0].Provider)
		assert.Equal(t, "Wind", alerts[0].Event)
		assert.Equal(t, "Greater London", alerts[0].AreaDesc)
	})

	t.Run("returns empty result for uncovered country", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"results":[{"name":"Tokyo","latitude":35.7,"longitude":139.7,"country":"Japan","country_code":"JP"}]}`))
		}))
		defer server.Close()
		withMockServer(t, server)

		alerts, err := s.GetAlerts("Tokyo")
		require.NoError(t, err)
		assert.Empty(t, alerts)
	})

	t.Run("propagates geocode error", func(t *testing.T) {
		alerts, err := s.GetAlerts("")
		require.Error(t, err)
		assert.Nil(t, alerts)
	})
}
