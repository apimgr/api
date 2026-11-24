package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/notify"
)

func TestSecurityReportWebhookSummaryAndTransportFallback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Contact.Security.Webhooks["generic"] = "https://security.example.test/hook"
	cfg.Server.Contact.Security.Webhooks["generic_secret"] = "security-secret"
	cfg.Server.Contact.Admin.Webhooks["slack"] = "https://admin.example.test/hook"
	cfg.Server.Contact.Admin.Webhooks["slack_secret"] = "admin-secret"
	req := httptest.NewRequest("POST", "https://api.example.test/server/contact", nil)
	deliveries := securityReportWebhookDeliveries(cfg, req, "sec_0123456789abcdef", "critical", "example component")
	if len(deliveries) != 2 {
		t.Fatalf("deliveries = %d, want security and admin-fallback webhooks", len(deliveries))
	}

	byTransport := make(map[notify.Transport]securityReportWebhookDelivery, len(deliveries))
	for _, delivery := range deliveries {
		byTransport[delivery.webhook.Transport] = delivery
	}
	security := byTransport[notify.Generic]
	if security.webhook.Name != "security" || security.webhook.Secret != "security-secret" || security.event.Role != "security" {
		t.Fatalf("security webhook resolved incorrectly: %#v", security)
	}
	admin := byTransport[notify.Slack]
	if admin.webhook.Name != "admin" || admin.webhook.Secret != "admin-secret" || admin.event.Role != "admin" {
		t.Fatalf("admin fallback resolved incorrectly: %#v", admin)
	}
	for _, delivery := range deliveries {
		if delivery.event.Severity != "critical" || delivery.event.TrackingID != "sec_0123456789abcdef" {
			t.Fatalf("summary event metadata is incorrect: %#v", delivery.event)
		}
		for _, expected := range []string{"example component", "api --maintenance security show sec_0123456789abcdef"} {
			if !strings.Contains(delivery.event.Body, expected) || !strings.Contains(delivery.event.Message, expected) {
				t.Errorf("webhook summary missing %q: %#v", expected, delivery.event)
			}
		}
		for _, forbidden := range []string{"report body", "researcher@example.test", "/server/security/report/", "?token="} {
			if strings.Contains(delivery.event.Body, forbidden) || strings.Contains(delivery.event.AppURL, forbidden) {
				t.Errorf("webhook summary exposed %q: %#v", forbidden, delivery.event)
			}
		}
	}
}
