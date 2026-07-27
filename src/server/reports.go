package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// maxReportBodyBytes caps the body accepted by the browser reporting
// endpoints, per AI.md PART 11 "Reporting API (Modern + Legacy)" — reports
// are small JSON documents; anything larger is rejected before parsing to
// avoid the endpoint being used as an amplification/flooding vector.
const maxReportBodyBytes = 64 * 1024

// reportsAllowedContentTypes are the two delivery formats browsers use for
// this family of endpoints, per AI.md PART 11: the legacy single-object
// `application/csp-report` body, and the modern `application/reports+json`
// batch-array body (also accepted as `application/json` for tooling/tests
// that omit the exact media type).
var reportsAllowedContentTypes = map[string]bool{
	"application/csp-report":   true,
	"application/reports+json": true,
	"application/json":         true,
}

// reportsHandler receives browser-emitted reports (CSP violations, NEL
// network errors, deprecation/intervention/crash reports, and the generic
// "default" group) POSTed to /api/v1/server/reports/{name}, per AI.md
// PART 11 "Reporting API (Modern + Legacy)": "All report endpoints share
// the same public reports rules — same rate limits [applied globally by
// RateLimitMiddleware's write class], same Output Sanitization Pipeline,
// same Tier 2 visibility (no PII echoed back)." One handler serves every
// report name (default, csp, nel, deprecation, intervention, crash, ...)
// since they share the same scope and shape, rather than one-off handlers
// per report type.
func reportsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if name == "" {
			name = "default"
		}

		contentType := r.Header.Get("Content-Type")
		baseType := contentType
		for i, c := range contentType {
			if c == ';' {
				baseType = contentType[:i]
				break
			}
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxReportBodyBytes)
		body, err := io.ReadAll(r.Body)

		details := map[string]interface{}{
			"report_type":  name,
			"content_type": baseType,
			"path":         r.URL.Path,
		}

		switch {
		case err != nil:
			details["parse_error"] = "body_too_large_or_unreadable"
		case !reportsAllowedContentTypes[baseType]:
			details["parse_error"] = "unsupported_content_type"
		case len(body) == 0:
			details["parse_error"] = "empty_body"
		default:
			var payload interface{}
			if jsonErr := json.Unmarshal(body, &payload); jsonErr != nil {
				details["parse_error"] = "invalid_json"
			} else {
				summarizeReportPayload(name, payload, details)
			}
		}

		event := "security.report_received"
		if name == "csp" {
			event = "security.csp_violation"
		}

		if logger := GetLogger(); logger != nil {
			logger.LogSecurity(event, getClientIP(r), details)
		}

		// Per AI.md PART 11: reports are fire-and-forget from the
		// browser's perspective, no body is ever echoed back (Tier 2
		// visibility - no PII in the response), and a malformed report
		// is logged, not rejected, so browsers never retry-storm this
		// endpoint.
		w.WriteHeader(http.StatusNoContent)
	}
}

// summarizeReportPayload extracts a small, known-safe set of fields from a
// parsed report body into details for the security log, rather than
// dumping the raw payload verbatim — keeps log entries bounded and
// consistent across the "default" (array-of-objects, reports+json) and
// legacy "csp" (single {"csp-report": {...}}) shapes.
func summarizeReportPayload(name string, payload interface{}, details map[string]interface{}) {
	switch v := payload.(type) {
	case []interface{}:
		details["count"] = len(v)
		if len(v) > 0 {
			if first, ok := v[0].(map[string]interface{}); ok {
				copyReportFields(first, details)
			}
		}
	case map[string]interface{}:
		details["count"] = 1
		if name == "csp" {
			if report, ok := v["csp-report"].(map[string]interface{}); ok {
				copyReportFields(report, details)
				return
			}
		}
		copyReportFields(v, details)
	}
}

// reportFieldAllowlist are the report-body fields safe to persist to
// security.log — CSP violation fields plus the generic Reporting API
// envelope fields (type/age/url), never arbitrary/unbounded report bodies.
var reportFieldAllowlist = []string{
	"type", "age", "url",
	"blocked-uri", "violated-directive", "document-uri", "effective-directive",
	"disposition", "status-code",
}

func copyReportFields(m map[string]interface{}, details map[string]interface{}) {
	for _, key := range reportFieldAllowlist {
		if val, ok := m[key]; ok {
			details[key] = val
		}
	}
	if body, ok := m["body"].(map[string]interface{}); ok {
		for _, key := range reportFieldAllowlist {
			if val, ok := body[key]; ok {
				details[key] = val
			}
		}
	}
}
