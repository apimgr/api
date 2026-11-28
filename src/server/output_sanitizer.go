package server

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
)

const responseTimingFloor = 100 * time.Millisecond

// outputSanitizationMiddleware is the final JSON response chokepoint. It
// captures JSON bodies before compression, sanitizes them, and lets streaming
// and non-JSON responses pass through unchanged.
func outputSanitizationMiddleware(cfg *config.ServerConfig) func(http.Handler) http.Handler {
	production := mode.IsAppModeProd()
	if cfg != nil && strings.TrimSpace(cfg.Server.Mode) != "" {
		production = strings.EqualFold(strings.TrimSpace(cfg.Server.Mode), "production")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			capture := &responseSanitizingWriter{ResponseWriter: w, started: started, timingFloor: needsResponseTimingFloor(r)}
			next.ServeHTTP(capture, r)
			capture.finish()
			capture.waitForTimingFloor()
			if !capture.passthrough {
				body := capture.body.Bytes()
				contentType := strings.ToLower(capture.Header().Get("Content-Type"))
				changed := false
				if strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json") {
					if cleaned, err := sanitize.SanitizeResponseJSON(body, production); err == nil {
						body = cleaned
						changed = true
					} else {
						body = []byte("{\"ok\":false,\"error\":\"INTERNAL_ERROR\",\"message\":\"Internal Server Error\"}\n")
						capture.status = http.StatusInternalServerError
						capture.Header().Set("Content-Type", "application/json; charset=utf-8")
						capture.Header().Set("Cache-Control", "private, no-store")
						capture.Header().Del("ETag")
						changed = true
					}
				} else if strings.Contains(contentType, "text/html") {
					if cleaned, err := sanitize.SanitizeHTMLResponse(body, production); err == nil {
						body = cleaned
						changed = true
					}
				} else if strings.HasPrefix(contentType, "text/") {
					body = []byte(sanitize.SanitizeResponseText(string(body), ""))
					changed = true
				}
				if changed {
					capture.Header().Del("Content-Length")
					if strings.Contains(contentType, "text/html") {
						capture.Header().Set("ETag", buildETag())
					} else {
						capture.Header().Del("ETag")
					}
				}
				status := capture.status
				if status == 0 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
				if r.Method != http.MethodHead {
					_, _ = w.Write(body)
				}
			}
		})
	}
}

func needsResponseTimingFloor(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		return true
	}
	path := strings.ToLower(r.URL.Path)
	for _, sensitive := range []string{"token", "contact", "security/report", "preferences/sync", "ccpa", "consent"} {
		if strings.Contains(path, sensitive) {
			return true
		}
	}
	return false
}

type responseSanitizingWriter struct {
	http.ResponseWriter
	body        bytes.Buffer
	stream      bytes.Buffer
	status      int
	passthrough bool
	streamEvent bool
	started     time.Time
	timingFloor bool
}

func (w *responseSanitizingWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseSanitizingWriter) WriteHeader(status int) {
	if w.passthrough {
		w.waitForTimingFloor()
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status == 0 {
		w.status = status
	}
}

func (w *responseSanitizingWriter) Write(body []byte) (int, error) {
	if w.passthrough {
		w.waitForTimingFloor()
		if w.streamEvent {
			return w.writeEventStream(body, false)
		}
		return w.ResponseWriter.Write(body)
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	contentType := strings.ToLower(w.Header().Get("Content-Type"))
	if strings.HasPrefix(contentType, "text/event-stream") {
		w.passthrough = true
		w.streamEvent = true
		w.waitForTimingFloor()
		w.ResponseWriter.WriteHeader(w.status)
		return w.writeEventStream(body, false)
	}
	if contentType == "" {
		sample := body
		if len(sample) > 512 {
			sample = sample[:512]
		}
		contentType = strings.ToLower(http.DetectContentType(sample))
		w.Header().Set("Content-Type", contentType)
	}
	if !isSanitizableResponseType(contentType) {
		w.passthrough = true
		w.ResponseWriter.WriteHeader(w.status)
		if w.body.Len() > 0 {
			if _, err := w.ResponseWriter.Write(w.body.Bytes()); err != nil {
				return 0, err
			}
		}
		return w.ResponseWriter.Write(body)
	}
	return w.body.Write(body)
}

func isSanitizableResponseType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json") ||
		strings.Contains(contentType, "text/html") || (strings.HasPrefix(contentType, "text/") &&
		!strings.Contains(contentType, "text/event-stream") && !strings.Contains(contentType, "text/css") && !strings.Contains(contentType, "javascript"))
}

func (w *responseSanitizingWriter) Flush() {
	if w.streamEvent {
		_, _ = w.writeEventStream(nil, true)
	}
	if !w.passthrough {
		contentType := strings.ToLower(w.Header().Get("Content-Type"))
		if strings.HasPrefix(contentType, "text/plain") {
			w.passthrough = true
			if w.status == 0 {
				w.status = http.StatusOK
			}
			w.waitForTimingFloor()
			w.ResponseWriter.WriteHeader(w.status)
			_, _ = w.ResponseWriter.Write([]byte(sanitize.SanitizeResponseText(w.body.String(), "")))
			w.body.Reset()
		} else if isSanitizableResponseType(contentType) {
			return
		}
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseSanitizingWriter) writeEventStream(body []byte, flushRemainder bool) (int, error) {
	inputLength := len(body)
	if _, err := w.stream.Write(body); err != nil {
		return 0, err
	}
	for {
		pending := w.stream.Bytes()
		lineEnd := bytes.IndexByte(pending, '\n')
		if lineEnd < 0 && !flushRemainder {
			break
		}
		if lineEnd < 0 {
			lineEnd = len(pending)
		}
		line := append([]byte(nil), pending[:lineEnd]...)
		separator := []byte{}
		if lineEnd < len(pending) {
			separator = []byte{'\n'}
		}
		w.stream.Next(lineEnd + len(separator))
		if bytes.HasPrefix(line, []byte("data:")) {
			line = []byte(sanitize.SanitizeResponseText(string(line), "message"))
		} else {
			line = []byte(sanitize.SanitizeResponseText(string(line), ""))
		}
		if _, err := w.ResponseWriter.Write(append(line, separator...)); err != nil {
			return 0, err
		}
		if lineEnd == len(pending) {
			break
		}
	}
	return inputLength, nil
}

func (w *responseSanitizingWriter) finish() {
	if w.streamEvent && w.stream.Len() > 0 {
		_, _ = w.writeEventStream(nil, true)
	}
}

func (w *responseSanitizingWriter) waitForTimingFloor() {
	if !w.timingFloor {
		return
	}
	if remaining := responseTimingFloor - time.Since(w.started); remaining > 0 {
		time.Sleep(remaining)
	}
	w.timingFloor = false
}
