package server

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/require"
)

func TestAboutAndPrivacyAPIUseSourceContent(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.Branding.Content = map[string]config.PageContentConfig{
		"about":   {Format: "md", Body: "About {app_name}"},
		"privacy": {Format: "md", Body: "Privacy extension for {app_name}"},
	}
	about := httptest.NewRecorder()
	aboutAPIHandler(cfg)(about, httptest.NewRequest(http.MethodGet, "https://example.test/api/v1/server/about", nil))
	require.Equal(t, http.StatusOK, about.Code)
	aboutData := decodeJSON(t, about)
	require.Equal(t, "About "+cfg.Server.Branding.Title, aboutData["content"])
	require.Equal(t, "markdown", aboutData["format"])
	require.NotContains(t, about.Body.String(), "<p>")

	privacy := httptest.NewRecorder()
	privacyAPIHandler(cfg)(privacy, httptest.NewRequest(http.MethodGet, "https://example.test/api/v1/server/privacy", nil))
	require.Equal(t, http.StatusOK, privacy.Code)
	privacyData := decodeJSON(t, privacy)
	require.Contains(t, privacyData["data_collection"], "request")
	require.Equal(t, "Privacy extension for "+cfg.Server.Branding.Title, privacyData["extension"])
	require.NotContains(t, privacy.Body.String(), "<p>")
}

func TestAuthCheckAPIIsReadOnlyAndNeverReturnsCredential(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.Token = "operator-secret"
	require.NoError(t, database.InitDatabase(cfg.Server.Database, paths.DataDir()))
	defer func() { _ = database.Close() }()
	db := database.GetServerDB()
	require.NotNil(t, db)
	ownerToken, owner, err := database.IssueOwnerToken(t.Context(), db, []byte(strings.Repeat("k", 32)))
	require.NoError(t, err)

	missing := doAuthCheck(t, cfg, "")
	require.Equal(t, http.StatusUnauthorized, missing.Code)
	invalid := doAuthCheck(t, cfg, "invalid")
	require.Equal(t, http.StatusUnauthorized, invalid.Code)
	require.NotContains(t, invalid.Body.String(), "invalid")

	operator := doAuthCheck(t, cfg, cfg.Server.Token)
	require.Equal(t, http.StatusOK, operator.Code)
	require.Equal(t, "operator", decodeJSON(t, operator)["kind"])
	require.NotContains(t, operator.Body.String(), cfg.Server.Token)

	ownerResponse := doAuthCheck(t, cfg, ownerToken)
	require.Equal(t, http.StatusOK, ownerResponse.Code)
	require.Equal(t, "owner", decodeJSON(t, ownerResponse)["kind"])
	require.NotContains(t, ownerResponse.Body.String(), ownerToken)
	var lastUsed sql.NullInt64
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT last_used_at FROM api_tokens WHERE COALESCE(public_id, id) = ?", owner.TokenID).Scan(&lastUsed))
	require.False(t, lastUsed.Valid, "auth check must not record token usage")
}

func doAuthCheck(t *testing.T, cfg *config.ServerConfig, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "https://example.test/api/v1/server/auth/check", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	authCheckAPIHandler(cfg)(recorder, req)
	return recorder
}
