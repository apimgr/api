package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOfflinePublicCacheMiddlewareMarksOnlyAnonymousHTMLPages(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		query      string
		cookie     string
		acceptLang string
		status     int
		content    string
		wantHeader string
	}{
		{name: "public category", method: http.MethodGet, path: "/categories", status: http.StatusOK, content: "text/html; charset=utf-8", wantHeader: "public"},
		{name: "public about", method: http.MethodGet, path: "/server/about", status: http.StatusOK, content: "text/html; charset=utf-8", wantHeader: "public"},
		{name: "tool page", method: http.MethodGet, path: "/text/hash", status: http.StatusOK, content: "text/html; charset=utf-8"},
		{name: "cookie", method: http.MethodGet, path: "/categories", cookie: "lang=fr", status: http.StatusOK, content: "text/html; charset=utf-8"},
		{name: "language negotiation", method: http.MethodGet, path: "/categories", acceptLang: "fr", status: http.StatusOK, content: "text/html; charset=utf-8"},
		{name: "query", method: http.MethodGet, path: "/categories", query: "lang=fr", status: http.StatusOK, content: "text/html; charset=utf-8"},
		{name: "post", method: http.MethodPost, path: "/categories", status: http.StatusOK, content: "text/html; charset=utf-8"},
		{name: "not found", method: http.MethodGet, path: "/categories", status: http.StatusNotFound, content: "text/html; charset=utf-8"},
		{name: "json", method: http.MethodGet, path: "/categories", status: http.StatusOK, content: "application/json"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := offlinePublicCacheMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.content)
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("response"))
			}))
			request := httptest.NewRequest(test.method, test.path+"?"+test.query, nil)
			if test.query == "" {
				request = httptest.NewRequest(test.method, test.path, nil)
			}
			if test.cookie != "" {
				request.Header.Set("Cookie", test.cookie)
			}
			if test.acceptLang != "" {
				request.Header.Set("Accept-Language", test.acceptLang)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if got := recorder.Header().Get("X-Offline-Cache"); got != test.wantHeader {
				t.Fatalf("X-Offline-Cache = %q, want %q", got, test.wantHeader)
			}
		})
	}
}
