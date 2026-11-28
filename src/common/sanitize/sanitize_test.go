package sanitize

import (
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type unregisteredMarshaler struct{}

func (unregisteredMarshaler) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{"sensitive_extension": "must not pass"})
}

func TestIsCredentialKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"token", "token", true},
		{"password uppercase", "PASSWORD", true},
		{"suffixed", "owner_token", true},
		{"suffixed secret", "cookie_signing_secret", true},
		{"padded", "  api_key  ", true},
		{"plain field", "username", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCredentialKey(tt.input))
		})
	}
}

func TestFilterResponseFieldsUsesExplicitJSONTags(t *testing.T) {
	type nested struct {
		Visible string `json:"visible"`
		Hidden  string
		Debug   string `json:"_debug" dev_only:"true"`
	}
	encoded, err := MarshalTaggedResponse(nested{Visible: "ok", Hidden: "sensitive", Debug: "details"}, true)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"visible":"ok"}`, string(encoded))
}

func TestFilterResponseFieldsRequiresDeclaredMapFields(t *testing.T) {
	type response struct {
		Data interface{} `json:"data"`
	}
	value := response{Data: TaggedObject{
		{Name: "visible", Value: "safe"},
		{Name: "nested", Value: TaggedObject{{Name: "allowed", Value: true}}},
	}}
	encoded, err := MarshalTaggedResponse(value, true)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"data":{"visible":"safe","nested":{"allowed":true}}}`, string(encoded))

	untagged := response{Data: map[string]interface{}{"credential": "must not pass", "secret_extension": true}}
	encoded, err = MarshalTaggedResponse(untagged, true)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"data":null}`, string(encoded))
}

func TestOpaqueUserJSONPreservesOnlyInsideDeclaredFieldAndRedactsRecursively(t *testing.T) {
	type response struct {
		Data   OpaqueUserJSON `json:"data"`
		Hidden map[string]string
	}
	encoded, err := MarshalTaggedResponse(response{
		Data: OpaqueUserJSON{Value: map[string]interface{}{
			"event": "created",
			"nested": map[string]interface{}{
				"password":    "do-not-leak",
				"token":       "also-do-not-leak",
				"private_key": "never-show",
				"count":       json.Number("2"),
				"url":         "https://example.test/?token=secret&page=1",
			},
		}},
		Hidden: map[string]string{"secret": "drop-this-object"},
	}, true)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"data":{"event":"created","nested":{"count":2,"url":"https://example.test/?page=1"}}}`, string(encoded))
}

func TestCredentialFieldsAreOmittedFromTaggedResponses(t *testing.T) {
	type response struct {
		PrivateKey string `json:"private_key"`
		PublicKey  string `json:"public_key"`
	}
	encoded, err := MarshalTaggedResponse(response{PrivateKey: "never-show", PublicKey: "public"}, false)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"public_key":"public"}`, string(encoded))

	encoded, err = MarshalTaggedResponse(TaggedObject{
		{Name: "private_key", Value: "never-show"},
		{Name: "public_key", Value: "public"},
	}, false)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"public_key":"public"}`, string(encoded))
}

func TestFilterResponseFieldsRejectsUnregisteredCustomMarshalers(t *testing.T) {
	type response struct {
		Data interface{} `json:"data"`
	}
	encoded, err := MarshalTaggedResponse(response{Data: unregisteredMarshaler{}}, true)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"data":{}}`, string(encoded))
}

func TestFilterResponseFieldsAllowsAuditedScalarMarshalers(t *testing.T) {
	type response struct {
		Timestamp time.Time       `json:"timestamp"`
		IP        net.IP          `json:"ip"`
		URL       url.URL         `json:"url"`
		Number    json.Number     `json:"number"`
		Raw       json.RawMessage `json:"raw"`
	}
	value := response{
		Timestamp: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		IP:        net.ParseIP("192.0.2.1"),
		URL:       url.URL{Scheme: "https", Host: "example.test", Path: "/status"},
		Number:    json.Number("42"),
		Raw:       json.RawMessage(`true`),
	}
	encoded, err := MarshalTaggedResponse(value, true)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"timestamp":"2026-01-02T03:04:05Z","ip":"192.0.2.1","url":"https://example.test/status","number":42,"raw":true}`, string(encoded))

	value.Raw = json.RawMessage(`{"unregistered":"must not pass"}`)
	encoded, err = MarshalTaggedResponse(value, true)
	assert.NoError(t, err)
	assert.NotContains(t, string(encoded), "unregistered")
}

func TestSanitizeHTMLResponsePreservesMarkupAndRedactsValues(t *testing.T) {
	input := []byte(`<main><p>server 10.0.0.8 at /var/private/data</p><a href="https://example.test/?token=secret&page=2">Open</a></main>`)
	cleaned, err := SanitizeHTMLResponse(input, true)
	assert.NoError(t, err)
	assert.Contains(t, string(cleaned), "<main>")
	assert.Contains(t, string(cleaned), "<p>")
	assert.Contains(t, string(cleaned), "<a href=")
	assert.NotContains(t, string(cleaned), "10.0.0.8")
	assert.NotContains(t, string(cleaned), "/var/private/data")
	assert.NotContains(t, string(cleaned), "token=secret")
	assert.Contains(t, string(cleaned), "page=2")
}

func TestSanitizeResponseJSON(t *testing.T) {
	input := `{"code":"87G7PX7V+3F","url":"https://example.test/path?token=secret&page=2","message":"failed at /root/private/file.go from 192.168.1.4","sample":"` + strings.Repeat("x", 250) + `","_debug":{"stack":"hidden"}}`
	cleaned, err := SanitizeResponseJSON([]byte(input), true)
	assert.NoError(t, err)
	var got map[string]interface{}
	assert.NoError(t, json.Unmarshal(cleaned, &got))
	assert.Equal(t, "https://example.test/path?page=2", got["url"])
	assert.Equal(t, "87G7PX7V+3F", got["code"])
	assert.NotContains(t, got["message"], "/root/private")
	assert.NotContains(t, got["message"], "192.168.1.4")
	assert.Len(t, got["sample"], 200)
	assert.NotContains(t, got, "_debug")
}

func TestSanitizeResponseJSONPreservesStructFieldOrder(t *testing.T) {
	type response struct {
		First  string `json:"first"`
		Second string `json:"second"`
		Third  string `json:"third"`
	}

	encoded, err := MarshalTaggedResponse(response{First: "a", Second: "b", Third: "c"}, true)
	assert.NoError(t, err)
	first := strings.Index(string(encoded), `"first"`)
	second := strings.Index(string(encoded), `"second"`)
	third := strings.Index(string(encoded), `"third"`)
	assert.True(t, first >= 0 && first < second && second < third, "JSON struct field order should survive sanitization: %s", encoded)
}

func TestRedactCredentials(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantMissing string
		wantPresent string
	}{
		{"equals form", "connect failed: password=hunter2", "hunter2", "password=" + RedactedPlaceholder},
		{"colon form", "token: tok_abc123", "tok_abc123", "token: " + RedactedPlaceholder},
		{"quoted value", `secret="s3cr3t value"`, "s3cr3t", "secret=" + RedactedPlaceholder},
		{"bearer header", "Authorization: Bearer abc.def.ghi", "abc.def.ghi", "Bearer " + RedactedPlaceholder},
		{"database url", "dial libsql://admin:sup3rs3cret@db.example.com/app", "sup3rs3cret", "admin:" + RedactedPlaceholder + "@"},
		{"api key query", "GET /items?api_key=zzz123&page=2", "zzz123", "page=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactCredentials(tt.input)
			assert.NotContains(t, got, tt.wantMissing)
			assert.Contains(t, got, tt.wantPresent)
		})
	}
}

// TestRedactCredentialsKeepsDiagnostics confirms redaction only removes
// credential values, so debug-mode diagnostics stay useful.
func TestRedactCredentialsKeepsDiagnostics(t *testing.T) {
	assert.Equal(t, "", RedactCredentials(""))

	input := "sqlite: no such table: api_tokens (attempt 3 of 5)"
	assert.Equal(t, input, RedactCredentials(input))
}
