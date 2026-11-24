package server

import (
	"net/http"
	"strings"
)

var offlinePublicPages = map[string]struct{}{
	"/": {}, "/categories": {},
	"/text": {}, "/crypto": {}, "/convert": {}, "/datetime": {},
	"/generate": {}, "/validate": {}, "/math": {}, "/dev": {},
	"/network": {}, "/parse": {}, "/geo": {}, "/osint": {},
	"/weather": {}, "/image": {}, "/fun": {}, "/lorem": {},
	"/language": {}, "/research": {}, "/docker": {}, "/testing": {},
	"/system": {}, "/server/about": {}, "/server/help": {},
	"/server/terms": {},
}

func offlinePublicCacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RawQuery != "" ||
			r.Header.Get("Cookie") != "" || r.Header.Get("Accept-Language") != "" {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := offlinePublicPages[r.URL.Path]; !ok {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("X-Offline-Cache", "public")
		next.ServeHTTP(&offlinePublicResponseWriter{ResponseWriter: w}, r)
	})
}

type offlinePublicResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *offlinePublicResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	if status != http.StatusOK || !strings.HasPrefix(strings.ToLower(w.Header().Get("Content-Type")), "text/html") {
		w.Header().Del("X-Offline-Cache")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *offlinePublicResponseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(body))
		}
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
