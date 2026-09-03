package metrics

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// LogEntry is one log line offered to the Loki push-API handler.
type LogEntry struct {
	Time   time.Time
	Line   string
	Labels map[string]string
}

// LogSource supplies recent log entries for the Loki metrics handler. The
// metrics package has no access to the server's actual logging
// implementation, so callers inject one, backed by whatever log store
// src/server already maintains.
type LogSource interface {
	// RecentEntries returns entries no older than maxAge, most recent
	// last, capped at maxEntries.
	RecentEntries(maxAge time.Duration, maxEntries int) []LogEntry
}

// PrometheusHandler returns the Prometheus text-exposition-format handler
// bound to this Metrics instance's registry.
func (m *Metrics) PrometheusHandler() http.Handler {
	return http.HandlerFunc(m.ServePrometheus)
}

// GrafanaHandler returns a handler serving the fixed 9-panel dashboard
// document for projectName as JSON.
func (m *Metrics) GrafanaHandler(projectName string) http.Handler {
	dashboard := buildGrafanaDashboard(projectName)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(dashboard)
	})
}

// lokiStream is one label set and its associated log lines, in the Loki
// push-API JSON stream shape.
type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

// lokiPushBody is the top-level Loki push-API document.
type lokiPushBody struct {
	Streams []lokiStream `json:"streams"`
}

// LokiHandler returns a handler that serves recent log entries from source
// in the Loki push-API JSON format, bounded by maxEntries/maxAge per
// server.metrics.loki.max_entries / .max_age.
func (m *Metrics) LokiHandler(source LogSource, maxEntries int, maxAge time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entries := source.RecentEntries(maxAge, maxEntries)

		streamsByKey := make(map[string]*lokiStream)
		var order []string

		for _, e := range entries {
			key := labelKey(e.Labels)
			s, ok := streamsByKey[key]
			if !ok {
				s = &lokiStream{Stream: e.Labels}
				streamsByKey[key] = s
				order = append(order, key)
			}
			ts := e.Time.UnixNano()
			s.Values = append(s.Values, [2]string{strconv.FormatInt(ts, 10), e.Line})
		}

		body := lokiPushBody{}
		for _, key := range order {
			body.Streams = append(body.Streams, *streamsByKey[key])
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(body)
	})
}

// labelKey builds a stable map key from a label set so entries sharing the
// same labels are grouped into one Loki stream.
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	b, _ := json.Marshal(labels)
	return string(b)
}
