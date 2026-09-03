package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
)

// csrfTestConfig builds a config with CSRF enabled and the supplied exempt
// path patterns.
func csrfTestConfig(exempt []string) *config.Config {
	cfg := &config.Config{}
	cfg.Server.CSRF.Enabled = true
	cfg.Server.CSRF.ExemptPaths = exempt
	return cfg
}

// newCSRFTestHandler wraps an always-200 handler in the csrf middleware.
func newCSRFTestHandler(cfg *config.Config) http.Handler {
	return csrfMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestCSRFMiddleware_GetIssuesCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	newCSRFTestHandler(csrfTestConfig(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on a safe method, got %d", rec.Code)
	}
	setCookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "csrf_token=") {
		t.Fatalf("expected a csrf_token cookie, got %q", setCookie)
	}
	if strings.Contains(setCookie, "HttpOnly") {
		t.Fatal("the csrf_token cookie must not be HttpOnly")
	}
	if !strings.Contains(setCookie, "SameSite=Strict") {
		t.Fatalf("expected SameSite=Strict, got %q", setCookie)
	}
}

func TestCSRFMiddleware_PostWithoutTokenIsRejected(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/things", nil)

	newCSRFTestHandler(csrfTestConfig(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a tokenless POST, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "CSRF_FAILED") {
		t.Fatalf("expected the CSRF_FAILED error code, got %q", rec.Body.String())
	}
}

func TestCSRFMiddleware_MatchingHeaderAndCookiePasses(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/things", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "matching-token-value"})
	req.Header.Set("X-CSRF-Token", "matching-token-value")

	newCSRFTestHandler(csrfTestConfig(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when the token matches, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_MismatchedTokenIsRejected(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/things", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "cookie-value"})
	req.Header.Set("X-CSRF-Token", "different-value")

	newCSRFTestHandler(csrfTestConfig(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on a token mismatch, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_BearerBypassesCheck(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/things", nil)
	req.Header.Set("Authorization", "Bearer tok_example")

	newCSRFTestHandler(csrfTestConfig(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected bearer requests to bypass CSRF, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_OriginHeaderDoesNotBypass(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/things", nil)
	req.Header.Set("Origin", "https://example.test")

	newCSRFTestHandler(csrfTestConfig(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("a same-origin Origin header must never bypass validation, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_ExemptPathSkipsCheck(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil)

	newCSRFTestHandler(csrfTestConfig([]string{"/webhooks/*"})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected the exempt path to skip validation, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_FormFieldIsAccepted(t *testing.T) {
	rec := httptest.NewRecorder()
	body := strings.NewReader("csrf_token=form-token-value&name=x")
	req := httptest.NewRequest(http.MethodPost, "/things", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "form-token-value"})

	newCSRFTestHandler(csrfTestConfig(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected the hidden form field to satisfy the check, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_APIPathBypassesCheck(t *testing.T) {
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"text":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/text/uppercase", body)
	req.Header.Set("Content-Type", "application/json")

	newCSRFTestHandler(csrfTestConfig(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("programmatic API routes ignore cookies and must bypass CSRF, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_MultipartFieldIsAccepted(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("csrf_token", "multipart-token-value"); err != nil {
		t.Fatalf("failed to write the token field: %v", err)
	}
	fw, err := mw.CreateFormFile("image", "test.bin")
	if err != nil {
		t.Fatalf("failed to create the file part: %v", err)
	}
	if _, err := fw.Write([]byte("payload")); err != nil {
		t.Fatalf("failed to write the file part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("failed to close the writer: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tools/image/resize", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "multipart-token-value"})

	// The downstream handler must still see the uploaded file, so the
	// middleware's own multipart parse cannot consume it.
	handler := csrfMiddleware(csrfTestConfig(nil))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("image")
		if err != nil {
			t.Errorf("the uploaded file was not readable downstream: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer file.Close()
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected the multipart token field to satisfy the check, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_DisabledPassesThrough(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.CSRF.Enabled = false

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/things", nil)

	newCSRFTestHandler(cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected disabled CSRF to pass through, got %d", rec.Code)
	}
}
