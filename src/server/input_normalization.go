package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/go-chi/chi/v5"
)

const maxNormalizedRequestBody = 1 << 20

func inputNormalizationMiddleware(next http.Handler, configs ...*config.Config) http.Handler {
	limit := int64(maxNormalizedRequestBody)
	if len(configs) > 0 && configs[0] != nil {
		limit = configs[0].Server.Limits.MaxBodySizeBytes()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		copied := r.Clone(r.Context())
		clonedURL := *r.URL
		query := clonedURL.Query()
		if !normalizeInputValues(query) {
			writePasswordWhitespaceError(w, r)
			return
		}
		clonedURL.RawQuery = query.Encode()
		copied.URL = &clonedURL
		if !normalizePathInputValues(copied) {
			writePasswordWhitespaceError(w, copied)
			return
		}
		if !normalizeJSONRequestBody(w, copied, limit) {
			return
		}
		if !strings.HasPrefix(copied.URL.Path, "/api/") && isURLEncodedForm(copied) {
			copied.Body = http.MaxBytesReader(w, copied.Body, limit)
			if err := copied.ParseForm(); err == nil && (!normalizeInputValues(copied.PostForm) || !normalizeInputValues(copied.Form)) {
				writePasswordWhitespaceError(w, copied)
				return
			}
		}
		next.ServeHTTP(w, copied)
	})
}

func isURLEncodedForm(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/x-www-form-urlencoded"
}

func normalizePathInputValues(r *http.Request) bool {
	routeContext := chi.RouteContext(r.Context())
	if routeContext == nil {
		return true
	}
	for index, name := range routeContext.URLParams.Keys {
		if index >= len(routeContext.URLParams.Values) {
			break
		}
		value := routeContext.URLParams.Values[index]
		trimmed := strings.TrimSpace(value)
		if isPasswordInputName(name) && trimmed != value {
			return false
		}
		routeContext.URLParams.Values[index] = trimmed
	}
	return true
}

func normalizeJSONRequestBody(w http.ResponseWriter, r *http.Request, limit int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json")) || r.Body == nil {
		return true
	}
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		writeLocalizedAPIError(w, r, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE")
		return false
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		return true
	}
	normalized, valid := normalizeJSONInputValue("", value)
	if !valid {
		writePasswordWhitespaceError(w, r)
		return false
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(encoded))
	r.ContentLength = int64(len(encoded))
	return true
}

func normalizeJSONInputValue(field string, value any) (any, bool) {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			normalized, valid := normalizeJSONInputValue(key, item)
			if !valid {
				return nil, false
			}
			current[key] = normalized
		}
		return current, true
	case []any:
		for index, item := range current {
			normalized, valid := normalizeJSONInputValue(field, item)
			if !valid {
				return nil, false
			}
			current[index] = normalized
		}
		return current, true
	case string:
		trimmed := strings.TrimSpace(current)
		if isPasswordInputName(field) && trimmed != current {
			return nil, false
		}
		return trimmed, true
	default:
		return value, true
	}
}

func normalizeInputValues(values url.Values) bool {
	for name, entries := range values {
		for index, value := range entries {
			trimmed := strings.TrimSpace(value)
			if isPasswordInputName(name) && trimmed != value {
				return false
			}
			values[name][index] = trimmed
		}
	}
	return true
}

func isPasswordInputName(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	return normalized == "password" || normalized == "passphrase" || strings.HasSuffix(normalized, "_password") || strings.HasSuffix(normalized, "_passphrase")
}

func writePasswordWhitespaceError(w http.ResponseWriter, r *http.Request) {
	writeLocalizedAPIError(w, r, http.StatusBadRequest, "VALIDATION_FAILED")
}
