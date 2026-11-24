package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/require"
)

func TestOwnerTokenCookieNameRequiresRecordedSuffixShape(t *testing.T) {
	const prefix = "api_owner_token_"
	suffix := strings.TrimPrefix(paths.OwnerTokenCookieName, prefix)
	name, err := ownerTokenCookieName(paths.ProjectName, suffix)
	require.NoError(t, err)
	require.Equal(t, paths.OwnerTokenCookieName, name)
	_, err = ownerTokenCookieName(paths.ProjectName, "qwhzkft1")
	require.Error(t, err)
	_, err = ownerTokenCookieName(paths.ProjectName, "short")
	require.Error(t, err)
}

func TestSetOwnerTokenCookieUsesStrictHttpOnlySecureScope(t *testing.T) {
	cfg := config.DefaultConfig()
	request := httptest.NewRequest("GET", "https://site.example/server/preferences/tokens", nil)
	recorder := httptest.NewRecorder()
	setOwnerTokenCookie(recorder, request, cfg, paths.OwnerTokenCookieName, "own_token-value")
	setCookie := recorder.Header().Get("Set-Cookie")
	require.True(t, strings.Contains(setCookie, "Path=/"))
	require.True(t, strings.Contains(setCookie, "HttpOnly"))
	require.True(t, strings.Contains(setCookie, "Secure"))
	require.True(t, strings.Contains(setCookie, "SameSite=Strict"))
}
