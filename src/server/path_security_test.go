package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPathSecurityMiddleware(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		rawPath      string
		wantPath     string
		wantLocation string
		wantStatus   int
	}{
		{name: "repeated slash", path: "/server/admin//config//settings", wantPath: "/server/admin/config/settings", wantStatus: http.StatusOK},
		{name: "root slashes", path: "///", wantPath: "/", wantStatus: http.StatusOK},
		{name: "redirect trailing slash", path: "/server/admin/", wantLocation: "/server/admin", wantStatus: http.StatusMovedPermanently},
		{name: "preserve query on trailing slash redirect", path: "/server/admin/?tab=keys", wantLocation: "/server/admin?tab=keys", wantStatus: http.StatusMovedPermanently},
		{name: "decoded traversal", path: "/static/../server/admin", wantStatus: http.StatusBadRequest},
		{name: "encoded traversal", path: "/api/v1/files/../../etc/passwd", rawPath: "/api/v1/files/%2e%2e/%2e%2e/etc/passwd", wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := urlNormalizationMiddleware(pathSecurityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Path", r.URL.Path)
				w.WriteHeader(http.StatusOK)
			})))
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			req.URL.RawPath = test.rawPath
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, test.wantStatus)
			}
			if test.wantLocation != "" && rec.Header().Get("Location") != test.wantLocation {
				t.Errorf("redirect location = %q, want %q", rec.Header().Get("Location"), test.wantLocation)
			}
			if test.wantPath != "" && rec.Header().Get("X-Path") != test.wantPath {
				t.Errorf("normalized path = %q, want %q", rec.Header().Get("X-Path"), test.wantPath)
			}
		})
	}
}
