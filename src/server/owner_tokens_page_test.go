package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
)

func TestValidOwnerCookieName(t *testing.T) {
	valid, err := ownerTokenCookieName("api", "abcdefgh")
	if err != nil {
		t.Fatal(err)
	}
	if !validOwnerCookieName(valid, "api") {
		t.Fatal("derived owner cookie name should be accepted")
	}
	for _, name := range []string{"", "api_owner_token_ABCDEFGH", "api_owner_token_abcdefg", "other_owner_token_abcdefgh", valid + "-extra"} {
		if validOwnerCookieName(name, "api") {
			t.Errorf("invalid cookie name %q accepted", name)
		}
	}
}

func TestOwnerTokensTemplateRendersLinkedTokenPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example.test/server/preferences/tokens", nil)
	data := ownerTokensPageData{
		PageData:       newPageData(config.DefaultConfig(), request, "preferences"),
		OwnerCookie:    true,
		PrimaryToken:   &database.OwnerToken{Kind: "primary", Scope: "global", Last4: "1234", Status: "active"},
		LinkedTokens:   []database.OwnerToken{{ID: "linked-id", Kind: "linked", Scope: "read", Name: "integration", Last4: "5678", Status: "active", CreatedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)}},
		ResourcePlural: i18n.Translate("en", "owner_tokens.resources"),
	}

	tmpl, err := ownerTokenPageTemplate()
	if err != nil {
		t.Fatal("parse owner-token page template:", err)
	}
	var rendered bytes.Buffer
	if err := tmpl.ExecuteTemplate(&rendered, "public.tmpl", data); err != nil {
		t.Fatal("render owner-token page with a linked token:", err)
	}
	for _, expected := range []string{"integration", "token-list-item", "Create linked token", "Never", "Revoke"} {
		if !strings.Contains(rendered.String(), expected) {
			t.Errorf("linked-token page is missing %q", expected)
		}
	}
}
