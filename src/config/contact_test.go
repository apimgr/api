package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveDPOUsesOnlyConfiguredDPOOrGeneralEmail(t *testing.T) {
	cfg := ContactConfig{
		Admin:   ContactRoleConfig{Email: "admin@example.test"},
		General: ContactRoleConfig{Email: "general@example.test"},
	}
	assert.Equal(t, cfg.General, cfg.ResolveDPO())

	cfg.DPO = ContactRoleConfig{Email: "dpo@example.test"}
	assert.Equal(t, cfg.DPO, cfg.ResolveDPO())

	cfg.DPO.Email = ""
	cfg.General.Email = ""
	assert.Empty(t, cfg.ResolveDPO().Email)
}

func TestResolveDPOFallsBackWhenOnlyWebhookIsConfigured(t *testing.T) {
	cfg := ContactConfig{
		DPO:     ContactRoleConfig{Webhooks: ContactWebhooksConfig{"generic": "https://dpo.example.test"}},
		General: ContactRoleConfig{Email: "general@example.test"},
	}
	assert.Equal(t, cfg.General, cfg.ResolveDPO())
}
