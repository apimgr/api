package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContactAPIDescriptorsPublishConditionalRules(t *testing.T) {
	encoded, err := json.Marshal(contactAPIFields())
	require.NoError(t, err)
	var fields []map[string]interface{}
	require.NoError(t, json.Unmarshal(encoded, &fields))

	var reportedURL, typeField map[string]interface{}
	for _, field := range fields {
		switch field["name"] {
		case "reported_url":
			reportedURL = field
		case "type":
			typeField = field
		}
	}
	require.NotNil(t, reportedURL)
	assert.Equal(t, false, reportedURL["required"])
	assert.Equal(t, float64(2048), reportedURL["max_length"])
	assert.Equal(t, map[string]interface{}{"field": "type", "in": []interface{}{"abuse", "dmca"}}, reportedURL["required_when"])
	require.NotNil(t, typeField)
	assert.Nil(t, typeField["max_length"])
	assert.Nil(t, typeField["required_when"])
}

func TestContactReviewFormsCarrySingleUseFormToken(t *testing.T) {
	templateSet, err := contactWorkflowTemplate()
	require.NoError(t, err)
	data := contactWorkflowPageData{
		PageData:    PageData{Lang: "en", CSRFToken: "csrf-token"},
		Type:        "abuse",
		TitleKey:    "contact.type.abuse",
		Fields:      []contactWorkflowField{{Name: "email", Value: "researcher@example.org"}, {Name: "subject", Value: "Report"}},
		FormToken:   "single-use-token",
		ReviewToken: "review-token",
		IsReview:    true,
	}
	var output bytes.Buffer
	require.NoError(t, templateSet.ExecuteTemplate(&output, "content", data))
	assert.Equal(t, 2, strings.Count(output.String(), `name="form_token" value="single-use-token"`))
}

func TestSecurityReportValidationAcceptsHTTPSResearcherKeyURL(t *testing.T) {
	fields := map[string]string{
		"name": "Researcher", "email": "researcher@example.org", "researcher_gpg": "https://keys.example.org/researcher.asc",
		"component": "API", "severity": "low", "summary": "Key URL validation", "steps": "Reproduce the issue.",
		"impact": "Limited impact.", "credit_preference": "no", "disclosure_agreement": "on",
	}
	assert.NoError(t, validateSecurityReportFields(fields))
}

func TestContactReportedURLRouteMatcherUsesRegisteredRoutes(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/resource/{id}", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	assert.True(t, contactRouteExists(router, "/resource/123"))
	assert.False(t, contactRouteExists(router, "/missing/123"))
}

func TestSecurityContactAPIDescriptorCapsKeyURLs(t *testing.T) {
	encoded, err := json.Marshal(securityAPIFields())
	require.NoError(t, err)
	var fields []map[string]interface{}
	require.NoError(t, json.Unmarshal(encoded, &fields))
	for _, field := range fields {
		if field["name"] != "researcher_gpg" {
			continue
		}
		assert.Equal(t, float64(16384), field["max_length"])
		assert.Equal(t, []interface{}{map[string]interface{}{"prefix": "https://", "max_length": float64(2048)}}, field["max_length_when"])
		return
	}
	t.Fatal("researcher_gpg descriptor not found")
}
