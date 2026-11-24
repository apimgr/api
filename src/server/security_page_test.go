package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/assert"
)

func TestSecurityHandlerIncludesRequiredRFC9116Fields(t *testing.T) {
	cfg := newTestConfig(t)
	if err := database.Init(cfg.Server.Database, paths.DataDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.EnsureAppSecrets(t.Context()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://example.com/.well-known/security.txt", nil)
	rec := httptest.NewRecorder()

	securityHandler(cfg)(rec, req)

	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "Contact: https://github.com/apimgr/api/security/advisories/new\n")
	assert.Contains(t, rec.Body.String(), "Contact: mailto:")
	assert.Contains(t, rec.Body.String(), "/server/contact?security_id=")
	assert.Contains(t, rec.Body.String(), "Expires: "+cfg.Web.Security.Expires.UTC().Format(time.RFC3339)+"\n")
	assert.Contains(t, rec.Body.String(), "Policy: ")
	assert.Contains(t, rec.Body.String(), "/server/security/policy\n")
	assert.True(t, strings.HasSuffix(rec.Body.String(), "Preferred-Languages: en\n"))

	apiRec := httptest.NewRecorder()
	securityAPIHandler(cfg)(apiRec, httptest.NewRequest("GET", "http://example.com/api/v1/server/security", nil))
	api := decodeJSONResponseBody(t, apiRec)
	data, ok := api["data"].(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, data, "report_url")
	pgp, ok := data["pgp"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, false, pgp["available"])
	assert.Nil(t, pgp["url"])
	assert.Nil(t, pgp["fingerprint"])
}

func TestSecurityMailtoRemovesExistingScheme(t *testing.T) {
	assert.Equal(t, "security@example.test", securityMailto("MAILTO:security@example.test"))
}
