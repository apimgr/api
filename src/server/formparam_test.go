package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFormTestRouter builds a router carrying the same form-input middleware
// and fallback registration the real server uses, over a representative slice
// of the API surface: a path-parameter route, a raw-body route, and a
// parameterless route that a fallback would otherwise collide with.
func newFormTestRouter() *chi.Mux {
	router := chi.NewRouter()
	router.Use(formInputMiddleware)
	router.Route("/api/v1", func(r chi.Router) {
		r.Route("/text", func(r chi.Router) {
			r.Get("/hash/{algorithm}/{input}", apiHashHandler)
			r.Get("/uuid", apiUUIDHandler)
			r.Get("/uuid/{version}/{count}", apiUUIDBatchHandler)
		})
		r.Route("/docker", func(r chi.Router) {
			r.Post("/lint", apiDockerLintHandler)
		})
	})
	registerFormFallbacks(router)
	return router
}

// serveFormTestRequest runs one request through the test router and returns
// the recorded response.
func serveFormTestRequest(router *chi.Mux, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// formFallbackPattern must derive the parameterless prefix a plain HTML form
// submits to, preserving a trailing .txt extension and ignoring routes that
// take no path parameters.
func TestFormFallbackPattern(t *testing.T) {
	cases := []struct {
		pattern string
		want    string
	}{
		{"/api/v1/text/hash/{algorithm}/{input}", "/api/v1/text/hash"},
		{"/api/v1/text/hash/{algorithm}/{input}.txt", "/api/v1/text/hash.txt"},
		{"/api/v1/research/doi/*", "/api/v1/research/doi"},
		{"/api/v1/text/uuid", ""},
		{"/{slug}", ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, formFallbackPattern(tc.pattern), tc.pattern)
	}
}

// The existing path-parameter call shape must keep working unchanged, and a
// plain HTML form GET carrying the same values as query parameters must reach
// the same handler and produce the same result.
func TestPathParamAndQueryParamProduceSameResult(t *testing.T) {
	router := newFormTestRouter()

	pathReq := httptest.NewRequest(http.MethodGet, "/api/v1/text/hash/sha256/hello", nil)
	pathRes := serveFormTestRequest(router, pathReq)
	require.Equal(t, http.StatusOK, pathRes.Code)

	queryReq := httptest.NewRequest(http.MethodGet, "/api/v1/text/hash?algorithm=sha256&input=hello", nil)
	queryRes := serveFormTestRequest(router, queryReq)
	require.Equal(t, http.StatusOK, queryRes.Code)

	assert.Equal(t, pathRes.Body.String(), queryRes.Body.String())
	assert.Contains(t, pathRes.Body.String(), "sha256")
}

// A urlencoded form POST must reach a raw-body handler with the same payload a
// JSON/CLI client sends as the request body.
func TestFormBodyMatchesRawBody(t *testing.T) {
	router := newFormTestRouter()
	dockerfile := "FROM alpine:latest\nRUN apk add --no-cache curl\n"

	rawReq := httptest.NewRequest(http.MethodPost, "/api/v1/docker/lint", strings.NewReader(dockerfile))
	rawReq.Header.Set("Content-Type", "text/plain")
	rawRes := serveFormTestRequest(router, rawReq)
	require.Equal(t, http.StatusOK, rawRes.Code)

	form := url.Values{}
	form.Set(formBodyField, dockerfile)
	formReq := httptest.NewRequest(http.MethodPost, "/api/v1/docker/lint", strings.NewReader(form.Encode()))
	formReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formRes := serveFormTestRequest(router, formReq)
	require.Equal(t, http.StatusOK, formRes.Code)

	assert.Equal(t, rawRes.Body.String(), formRes.Body.String())
}

// A form submission missing its required field must return the canonical
// error envelope rather than panicking or returning a partial success.
func TestFormSubmissionMissingFieldReturnsEnvelope(t *testing.T) {
	router := newFormTestRouter()

	form := url.Values{}
	form.Set("unrelated", "value")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/lint", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := serveFormTestRequest(router, req)

	require.Equal(t, http.StatusBadRequest, res.Code)
	envelope := decodeEnvelope(t, res.Body.Bytes())
	assert.Equal(t, false, envelope["ok"])
	assert.Equal(t, "VALIDATION_FAILED", envelope["error"])
}

// A malformed urlencoded submission must fall through to the handler's own
// validation path and still return the canonical envelope.
func TestMalformedFormSubmissionReturnsEnvelope(t *testing.T) {
	router := newFormTestRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/lint", strings.NewReader("%zz=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := serveFormTestRequest(router, req)

	require.Equal(t, http.StatusBadRequest, res.Code)
	envelope := decodeEnvelope(t, res.Body.Bytes())
	assert.Equal(t, false, envelope["ok"])
}

// A fallback must never replace an explicitly registered route: /text/uuid
// keeps its own handler even though /text/uuid/{version}/{count} would
// otherwise claim the same parameterless pattern.
func TestFallbackNeverReplacesExplicitRoute(t *testing.T) {
	router := newFormTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/text/uuid?version=4&count=5", nil)
	res := serveFormTestRequest(router, req)

	require.Equal(t, http.StatusOK, res.Code)
	assert.Contains(t, res.Body.String(), "\"uuid\"")
	assert.NotContains(t, res.Body.String(), "\"uuids\"")
}

// An explicit query parameter must win over a form field of the same name so
// existing query-driven call shapes keep their exact behaviour.
func TestQueryParameterWinsOverFormField(t *testing.T) {
	form := url.Values{}
	form.Set("algorithm", "md5")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/text/hash?algorithm=sha256", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	require.NoError(t, parseSubmittedForm(httptest.NewRecorder(), req))

	merged := requestWithFormInput(req)
	assert.Equal(t, "sha256", merged.URL.Query().Get("algorithm"))
	assert.Equal(t, "sha256", req.URL.Query().Get("algorithm"))
}

// Non-form content types must be passed through untouched so JSON and raw
// text clients keep reading their own request body.
func TestNonFormContentTypeIsUntouched(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/lint", strings.NewReader("{\"body\":\"FROM alpine\"}"))
	req.Header.Set("Content-Type", "application/json")
	assert.False(t, isFormSubmission(req))

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/text/hash?algorithm=md5&input=x", nil)
	assert.False(t, isFormSubmission(getReq))

	webReq := httptest.NewRequest(http.MethodPost, "/server/contact", strings.NewReader("name=x"))
	webReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	assert.False(t, isFormSubmission(webReq))
}
