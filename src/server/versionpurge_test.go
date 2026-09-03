package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// findCookie returns the Set-Cookie entry with the given name, or nil.
func findCookie(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// TestVersionPurge_FirstVisitSetsCookieWithoutPurging covers the AI.md PART 9
// rule that a browser with no build-stamp cookie is never purged.
func TestVersionPurge_FirstVisitSetsCookieWithoutPurging(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	rec := httptest.NewRecorder()

	versionPurge(rec, req)

	res := rec.Result()
	assert.Empty(t, res.Header.Get("Clear-Site-Data"))

	c := findCookie(res, buildCookieName)
	if assert.NotNil(t, c) {
		assert.Equal(t, assetStamp(), c.Value)
		assert.Equal(t, "/", c.Path)
		assert.Equal(t, buildCookieMaxAge, c.MaxAge)
		assert.True(t, c.Secure)
		assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	}
}

// TestVersionPurge_PlainHTTPCookieIsNotSecure covers the Tor/I2P case: those
// front ends serve plain HTTP by design, and a Secure cookie there would be
// dropped by the browser, permanently disabling the purge.
func TestVersionPurge_PlainHTTPCookieIsNotSecure(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.onion/", nil)
	rec := httptest.NewRecorder()

	versionPurge(rec, req)

	c := findCookie(rec.Result(), buildCookieName)
	if assert.NotNil(t, c) {
		assert.False(t, c.Secure)
	}
}

// TestVersionPurge_MatchingStampDoesNotPurge covers the "naturally one-shot"
// rule: once the cookie matches the running stamp no further purge occurs.
func TestVersionPurge_MatchingStampDoesNotPurge(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: buildCookieName, Value: assetStamp()})
	rec := httptest.NewRecorder()

	versionPurge(rec, req)

	assert.Empty(t, rec.Result().Header.Get("Clear-Site-Data"))
}

// TestVersionPurge_StaleStampPurgesCacheAndStorageOnly covers the mismatch
// rule and the hard prohibition on "cookies" in the version purge, which
// would otherwise destroy owner_token and the theme/language/consent cookies.
func TestVersionPurge_StaleStampPurgesCacheAndStorageOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: buildCookieName, Value: "0.0.1-deadbee"})
	rec := httptest.NewRecorder()

	versionPurge(rec, req)

	res := rec.Result()
	assert.Equal(t, `"cache", "storage"`, res.Header.Get("Clear-Site-Data"))
	assert.NotContains(t, res.Header.Get("Clear-Site-Data"), "cookies")

	c := findCookie(res, buildCookieName)
	if assert.NotNil(t, c) {
		assert.Equal(t, assetStamp(), c.Value)
	}
}

// TestVersionPurgeMiddleware_HTMLOnly covers the rule that the purge runs on
// HTML documents only, leaving static assets, API JSON, the service worker,
// and the web app manifest untouched.
func TestVersionPurgeMiddleware_HTMLOnly(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		wantPurge   bool
	}{
		{"html document", "text/html; charset=utf-8", true},
		{"api json", "application/json", false},
		{"service worker", "application/javascript; charset=utf-8", false},
		{"manifest", "application/manifest+json", false},
		{"stylesheet", "text/css; charset=utf-8", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := versionPurgeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("body"))
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: buildCookieName, Value: "0.0.1-deadbee"})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			res := rec.Result()
			if tc.wantPurge {
				assert.Equal(t, `"cache", "storage"`, res.Header.Get("Clear-Site-Data"))
				assert.NotNil(t, findCookie(res, buildCookieName))
			} else {
				assert.Empty(t, res.Header.Get("Clear-Site-Data"))
				assert.Nil(t, findCookie(res, buildCookieName))
			}
		})
	}
}

// TestAssetStamp covers the version-plus-short-commit stamp format.
func TestAssetStamp(t *testing.T) {
	origVersion, origCommit := Version, CommitID
	t.Cleanup(func() {
		Version, CommitID = origVersion, origCommit
	})

	Version, CommitID = "1.2.3", "abcdef0"
	assert.Equal(t, "1.2.3-abcdef0", assetStamp())
}
