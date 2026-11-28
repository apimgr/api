package swagger

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GenerateSpec must populate the top-level OpenAPI fields from its
// arguments and wire up all the sub-endpoint groups.
func TestGenerateSpec(t *testing.T) {
	spec := GenerateSpec("1.2.3", "https://api.example.com")

	require.NotNil(t, spec)
	assert.Equal(t, "3.0.3", spec.OpenAPI)
	assert.Equal(t, "API Toolkit", spec.Info.Title)
	assert.Equal(t, "1.2.3", spec.Info.Version)
	require.Len(t, spec.Servers, 1)
	assert.Equal(t, "https://api.example.com", spec.Servers[0].URL)

	// Base paths.
	assert.NotContains(t, spec.Paths, "/healthz")
	assert.Contains(t, spec.Paths, "/api/v1/server/healthz")
	assert.Contains(t, spec.Paths, "/api/healthz")
	assert.NotContains(t, spec.Paths, "/api/v1/version")

	// Text endpoints.
	assert.Contains(t, spec.Paths, "/api/v1/text/uuid")
	assert.Contains(t, spec.Paths, "/api/v1/text/hash/{algorithm}/{input}")
	assert.Contains(t, spec.Paths, "/api/v1/text/encode/{encoding}/{input}")
	assert.Contains(t, spec.Paths, "/api/v1/text/decode/{encoding}/{input}")

	// Crypto endpoints.
	assert.Contains(t, spec.Paths, "/api/v1/crypto/bcrypt/{password}")
	assert.Contains(t, spec.Paths, "/api/v1/crypto/password")
	assert.Contains(t, spec.Paths, "/api/v1/crypto/totp/secret")

	// DateTime endpoints.
	assert.Contains(t, spec.Paths, "/api/v1/datetime/now")
	assert.Contains(t, spec.Paths, "/api/v1/datetime/convert/{timestamp}")
	assert.Contains(t, spec.Paths, "/api/v1/datetime/timezones")

	// Network endpoints.
	assert.Contains(t, spec.Paths, "/api/v1/network/ip")
	assert.Contains(t, spec.Paths, "/api/v1/network/headers")

	// Reusable schema components.
	require.Contains(t, spec.Components.Schemas, "Error")
	assert.Equal(t, "object", spec.Components.Schemas["Error"].Type)
}

// The health check path must document a GET operation with a 200 response.
func TestGenerateSpecHealthzOperation(t *testing.T) {
	spec := GenerateSpec("1.0.0", "http://localhost")
	item := spec.Paths["/api/v1/server/healthz"]
	require.NotNil(t, item.Get)
	assert.Equal(t, "healthCheck", item.Get.OperationID)
	assert.Contains(t, item.Get.Responses, "200")
	alias := spec.Paths["/api/healthz"]
	require.NotNil(t, alias.Get)
	assert.Equal(t, "healthCheck", alias.Get.OperationID)
}

// Path parameters (e.g. {algorithm}, {input}) must be declared as required
// "path" parameters on the corresponding operation.
func TestGenerateSpecHashPathParameters(t *testing.T) {
	spec := GenerateSpec("1.0.0", "http://localhost")
	item := spec.Paths["/api/v1/text/hash/{algorithm}/{input}"]
	require.NotNil(t, item.Get)
	require.Len(t, item.Get.Parameters, 2)
	for _, p := range item.Get.Parameters {
		assert.Equal(t, "path", p.In)
		assert.True(t, p.Required)
	}
}

// ServeSpec must write a JSON-encoded spec with the version/baseURL baked
// in and the correct content type header.
func TestServeSpec(t *testing.T) {
	handler := ServeSpec("9.9.9", "https://example.com")

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	res := rec.Result()
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "application/json", res.Header.Get("Content-Type"))

	var decoded Spec
	require.NoError(t, json.NewDecoder(res.Body).Decode(&decoded))
	assert.Equal(t, "9.9.9", decoded.Info.Version)
	require.Len(t, decoded.Servers, 1)
	assert.Equal(t, "https://example.com", decoded.Servers[0].URL)
	operation := decoded.Paths["/api/v1/text/uuid"].Get
	require.NotNil(t, operation)
	require.Contains(t, operation.Responses, "200")
	assert.Equal(t, "Operation completed", operation.Responses["200"].Description)
}

func TestServeSpecIncludesRouteTextMetadata(t *testing.T) {
	handler := ServeSpecWithRoutes("1.0.0", "", "v1", []Route{{
		Method:      http.MethodGet,
		Path:        "/api/v1/fun/joke",
		Primary:     "text",
		TextMessage: "Retrieved successfully",
	}})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/swagger", nil))

	var spec Spec
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&spec))
	operation := spec.Paths["/api/v1/fun/joke"].Get
	require.NotNil(t, operation)
	assert.Equal(t, "text", operation.Primary)
	assert.Equal(t, "Retrieved successfully", operation.TextMessage)
	assert.Equal(t, "always", operation.MountedWhen)
}

func TestGenerateSupersetSpecCarriesMountConditions(t *testing.T) {
	spec := GenerateSupersetSpec("1.0.0", "", "v1", []Route{{
		Method:      http.MethodGet,
		Path:        "/api/v1/optional",
		MountedWhen: "feature.enabled",
	}})
	operation := spec.Paths["/api/v1/optional"].Get
	require.NotNil(t, operation)
	assert.Equal(t, "feature.enabled", operation.MountedWhen)
}

func TestEmbeddedSupersetIsImmutableWhenDisabledOperationsAreFiltered(t *testing.T) {
	superset, err := EmbeddedSuperset("1.0.0", "https://api.example.test", "v1")
	require.NoError(t, err)
	var conditionalPath string
	var conditionalCondition string
	for path, item := range superset.Paths {
		for _, operation := range []*Operation{item.Get, item.Head, item.Post, item.Put, item.Patch, item.Delete, item.Options} {
			if operation != nil && operation.MountedWhen != "" && operation.MountedWhen != "always" {
				conditionalPath = path
				conditionalCondition = operation.MountedWhen
				break
			}
		}
		if conditionalPath != "" {
			break
		}
	}
	require.NotEmpty(t, conditionalPath, "build-generated superset must retain conditional operations")
	assert.Contains(t, superset.Paths, conditionalPath)

	handler := ServeEmbeddedSuperset("1.0.0", "https://api.example.test", "v1", func(condition string) bool {
		return condition != conditionalCondition
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/swagger", nil))
	var served Spec
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&served))
	assert.NotContains(t, served.Paths, conditionalPath)
	assert.Contains(t, superset.Paths, conditionalPath, "filtering must not mutate the embedded superset")
}

func TestServeSpecFiltersDisabledMountedCondition(t *testing.T) {
	handler := ServeSpecWithRoutesAndCondition("1.0.0", "", "v1", []Route{{
		Method:      http.MethodGet,
		Path:        "/api/v1/optional",
		MountedWhen: "feature.enabled",
	}, {
		Method: http.MethodGet,
		Path:   "/api/v1/always",
	}}, func(condition string) bool { return condition == "always" })
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/swagger", nil))
	var spec Spec
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&spec))
	assert.NotContains(t, spec.Paths, "/api/v1/optional")
	assert.Contains(t, spec.Paths, "/api/v1/always")
}

func TestGenerateSupersetSpecOmitsInternalRoutes(t *testing.T) {
	spec := GenerateSupersetSpec("1.0.0", "", "v1", []Route{{
		Method:   http.MethodGet,
		Path:     "/api/v1/internal",
		Internal: true,
	}})
	assert.NotContains(t, spec.Paths, "/api/v1/internal")
}
