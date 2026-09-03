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
	assert.Contains(t, spec.Paths, "/healthz")
	assert.Contains(t, spec.Paths, "/api/v1/version")

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
	item := spec.Paths["/healthz"]
	require.NotNil(t, item.Get)
	assert.Equal(t, "healthCheck", item.Get.OperationID)
	assert.Contains(t, item.Get.Responses, "200")
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
}
