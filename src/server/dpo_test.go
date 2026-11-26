package server

import (
	"net/http/httptest"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicDPOContactUsesOnlyDPOAndGeneralEmail(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Contact.Admin.Email = "admin@example.test"
	cfg.Server.Contact.DPO.Name = "Privacy Office"
	cfg.Server.Contact.General.Email = "general@example.test"

	name, email := publicDPOContact(cfg)
	assert.Equal(t, "Privacy Office", name)
	assert.Equal(t, "general@example.test", email)

	cfg.Server.Contact.DPO.Email = "dpo@example.test"
	name, email = publicDPOContact(cfg)
	assert.Equal(t, "Privacy Office", name)
	assert.Equal(t, "dpo@example.test", email)

	cfg.Server.Contact.General.Email = ""
	cfg.Server.Contact.DPO.Email = ""
	name, email = publicDPOContact(cfg)
	assert.Equal(t, "Privacy Office", name)
	assert.Empty(t, email)
}

func TestDPOAPIHandlerReturnsContactAndRequestResolvedPrivacyURL(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Contact.Admin.Email = "admin@example.test"
	cfg.Server.Contact.DPO.Name = "Privacy Office"
	cfg.Server.Contact.DPO.Email = "dpo@example.test"

	req := httptest.NewRequest("GET", "https://example.test/api/v1/server/dpo", nil)
	rec := httptest.NewRecorder()
	dpoAPIHandler(cfg)(rec, req)

	assert.Equal(t, 200, rec.Code)
	data := decodeJSON(t, rec)
	assert.Equal(t, "Privacy Office", data["name"])
	assert.Equal(t, "dpo@example.test", data["email"])
	assert.Equal(t, "https://example.test/server/privacy", data["privacy_url"])
}

func TestDPOAPIHandlerReturnsEmptyContactInsteadOfAdminFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Contact.Admin.Email = "admin@example.test"

	req := httptest.NewRequest("GET", "https://example.test/api/v1/server/dpo", nil)
	rec := httptest.NewRecorder()
	dpoAPIHandler(cfg)(rec, req)

	require.Equal(t, 200, rec.Code)
	data := decodeJSON(t, rec)
	assert.Equal(t, "", data["name"])
	assert.Equal(t, "", data["email"])
	assert.Equal(t, "https://example.test/server/privacy", data["privacy_url"])
}
