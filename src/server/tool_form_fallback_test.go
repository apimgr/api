package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolFormFallbackSelectsDeclaredEndpointVariants(t *testing.T) {
	t.Run("blank network IP uses current-client endpoint", func(t *testing.T) {
		spec := toolFormEndpoint{
			Endpoint:      "{{.APIBasePath}}/network/ip/{ip}",
			EmptyEndpoint: "{{.APIBasePath}}/network/ip",
		}
		assert.Equal(t, spec.EmptyEndpoint, selectToolEndpoint(spec, url.Values{"ip": {""}}))
		assert.Equal(t, spec.Endpoint, selectToolEndpoint(spec, url.Values{"ip": {"192.0.2.4"}}))
	})

	t.Run("UUID count uses declared batch endpoint", func(t *testing.T) {
		spec := toolFormEndpoint{
			Endpoint:      "{{.APIBasePath}}/text/uuid/{version}",
			BatchEndpoint: "{{.APIBasePath}}/text/uuid/{version}/{count}",
		}
		assert.Equal(t, spec.Endpoint, selectToolEndpoint(spec, url.Values{"count": {"1"}}))
		assert.Equal(t, spec.BatchEndpoint, selectToolEndpoint(spec, url.Values{"count": {"4"}}))
	})
}

func TestToolFormFallbackMethodNotAllowedUsesRequestLanguage(t *testing.T) {
	cfg := newTestConfig(t)
	page := toolPage{category: "text", tool: "uuid", title: "UUID", description: "UUID generator"}
	handler := toolFormFallbackHandler(cfg, page, chi.NewRouter())
	request := httptest.NewRequest(http.MethodGet, "/text/uuid", nil)
	request.Header.Set("Accept-Language", "fr")
	response := httptest.NewRecorder()
	handler(response, request)
	assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
	assert.Equal(t, http.MethodPost, response.Header().Get("Allow"))
	assert.Contains(t, response.Body.String(), i18n.Translate("fr", "errors.method_not_allowed"))
}

func TestEveryToolPageDeclaresFallbackRequestMetadata(t *testing.T) {
	for _, page := range toolPages() {
		page := page
		t.Run(page.category+"/"+page.tool, func(t *testing.T) {
			spec, err := loadToolFormEndpoint(page)
			require.NoError(t, err)
			assert.NotEmpty(t, spec.Endpoint)
			assert.NotEmpty(t, spec.Kind)
			assert.Contains(t, []string{http.MethodGet, http.MethodPost}, spec.Method)
		})
	}
}

func TestBuildToolAPIRequestUsesDeclaredFormShape(t *testing.T) {
	cfg := newTestConfig(t)
	request := httptest.NewRequest(http.MethodPost, "/text/uuid", strings.NewReader(""))
	request.PostForm = url.Values{"version": {"v4"}, "count": {"3"}, "csrf_token": {"ignored"}}
	spec := toolFormEndpoint{
		Method: http.MethodGet,
		Kind:   "template",
	}
	apiRequest, err := buildToolAPIRequest(request, cfg, spec, "{{.APIBasePath}}/text/uuid/{version}/{count}")
	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, apiRequest.Method)
	assert.Equal(t, APIBasePath(cfg)+"/text/uuid/v4/3", apiRequest.URL.Path)
	assert.Empty(t, apiRequest.URL.RawQuery)
}

func TestToolFormFallbackCSRFAndRouteDispatch(t *testing.T) {
	cfg := newTestConfig(t)
	// The global middleware enforces the double-submit token on this frontend
	// POST before dispatching to the API route. A token supplied only in the
	// form is rejected when the CSRF cookie is absent.
	router := chi.NewRouter()
	router.Use(csrfMiddleware(cfg))
	apiRouter := chi.NewRouter()
	apiRouter.Get("/text/uuid/{version}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(chi.URLParam(r, "version")))
	})
	page := toolPage{category: "text", tool: "uuid", title: "UUID", description: "UUID generator"}
	router.Post("/text/uuid", toolFormFallbackHandler(cfg, page, apiRouter))

	request := httptest.NewRequest(http.MethodPost, "/text/uuid", strings.NewReader("version=v4&csrf_token=attacker"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusForbidden, response.Code)
}

func TestToolFormFallbackDispatchesThroughRootRouterAfterCSRF(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.CSRF.Enabled = true
	require.NoError(t, initTemplates())
	token, err := newCSRFToken(cfg.Server.CSRF.Normalized().TokenLength)
	require.NoError(t, err)
	var router chi.Router
	mux := chi.NewRouter()
	router = mux
	mux.Use(csrfMiddleware(cfg))
	page := toolPage{category: "text", tool: "uuid", title: "UUID", description: "UUID generator"}
	mux.Post("/text/uuid", toolFormFallbackHandler(cfg, page, router))
	mux.Get(APIBasePath(cfg)+"/text/uuid/{version}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(chi.URLParam(r, "version")))
	})

	request := httptest.NewRequest(http.MethodPost, "/text/uuid", strings.NewReader("version=4&count=1&csrf_token="+url.QueryEscape(token)))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: cfg.Server.CSRF.Normalized().CookieName, Value: token})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `<pre class="tool-result">4</pre>`)
}

func TestSafeToolImageTypeAllowsOnlyRasterImages(t *testing.T) {
	for _, contentType := range []string{"image/png", "image/jpeg; charset=binary", "image/gif", "image/webp"} {
		mediaType, ok := safeToolImageType(contentType)
		assert.True(t, ok, contentType)
		assert.True(t, strings.HasPrefix(mediaType, "image/"))
	}
	for _, contentType := range []string{"image/svg+xml", "text/html", "image/png; broken"} {
		_, ok := safeToolImageType(contentType)
		assert.False(t, ok, contentType)
	}
}

func TestSameOriginPathAppliesConfiguredBaseURL(t *testing.T) {
	assert.Equal(t, "/edge/api/server/contact", sameOriginPath("https://example.invalid/edge/api", "/server/contact"))
	assert.Equal(t, "/edge/api/server/contact", sameOriginPath("https://example.invalid/edge/api", "server/contact"))
	assert.Equal(t, "/server/contact", sameOriginPath("https://example.invalid/", "/server/contact"))
}

func TestToolFormResultRendersAPIOutputAsEscapedText(t *testing.T) {
	cfg := newTestConfig(t)
	require.NoError(t, initTemplates())
	request := httptest.NewRequest(http.MethodGet, "/text/uuid", nil)
	data := newPageData(cfg, request, "text")
	data.ToolResult = `<script>alert("x")</script>`
	data.PageTitle = "UUID"
	data.PageDescription = "UUID generator"
	response := httptest.NewRecorder()
	renderPage(response, "text/uuid", data)
	assert.NotContains(t, response.Body.String(), `<script>alert("x")</script>`)
	assert.Contains(t, response.Body.String(), `&lt;script&gt;`)
	assert.Contains(t, response.Body.String(), `&lt;/script&gt;`)
}
