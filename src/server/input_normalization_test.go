package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestInputNormalizationTrimsQueryAndPathText(t *testing.T) {
	queryHandler := inputNormalizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Query().Get("value"))
	}))
	recorder := httptest.NewRecorder()
	queryHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/test?value=%20hello%20", nil))
	require.Equal(t, "hello", recorder.Body.String())

	request := httptest.NewRequest(http.MethodGet, "/api/v1/test/%20hello%20", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("value", " hello ")
	request = request.WithContext(contextWithRouteContext(request, routeContext))
	pathHandler := inputNormalizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, chi.URLParam(r, "value"))
	}))
	recorder = httptest.NewRecorder()
	pathHandler.ServeHTTP(recorder, request)
	require.Equal(t, "hello", recorder.Body.String())
}

func TestInputNormalizationRejectsPasswordWhitespace(t *testing.T) {
	handler := inputNormalizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not receive an invalid password")
	}))
	for _, target := range []string{"/api/v1/test?password=%20secret", "/api/v1/test?passphrase=secret%20"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.NotContains(t, recorder.Body.String(), "secret")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("password", " secret")
	request = request.WithContext(contextWithRouteContext(request, routeContext))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestInputNormalizationUsesConfiguredBodyLimit(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Limits.MaxBodySize = "4B"
	handler := inputNormalizationMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("oversized request body must be rejected")
	}), cfg)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(`{"long":"value"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestInputNormalizationTrimsJSONAndRejectsPasswordWhitespace(t *testing.T) {
	handler := inputNormalizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value map[string]any
		require.NoError(t, decodeJSONBody(r, &value))
		_ = json.NewEncoder(w).Encode(value)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(`{"name":"  Ada  "}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"name":"Ada"}`, recorder.Body.String())

	request = httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(`{"password":" secret"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "secret")
}

func TestInputNormalizationTrimsFormFieldsAndRejectsPasswordWhitespace(t *testing.T) {
	handler := inputNormalizationMiddleware(formInputMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Query().Get("name"))
	})))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader("name=%20Ada%20"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, "Ada", recorder.Body.String())

	request = httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader("password=%20secret"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "secret")
}

func TestInputNormalizationTrimsServerPageForms(t *testing.T) {
	handler := inputNormalizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		_, _ = io.WriteString(w, r.FormValue("name"))
	}))
	request := httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader("name=%20Ada%20"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, "Ada", recorder.Body.String())
}

func contextWithRouteContext(request *http.Request, routeContext *chi.Context) context.Context {
	return context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
}
