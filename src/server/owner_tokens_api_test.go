package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteTokenAPIErrorUsesTranslatedCanonicalEnvelope(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens?lang=es", nil)
	recorder := httptest.NewRecorder()
	writeTokenAPIError(recorder, request, "BAD_REQUEST", "errors.bad_request", http.StatusBadRequest)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["ok"] != false || response["error"] != "BAD_REQUEST" || response["message"] != "Formato de solicitud inválido" {
		t.Fatalf("response = %#v", response)
	}
	if _, exists := response["details"]; exists {
		t.Fatalf("unexpected details field: %#v", response)
	}
}
