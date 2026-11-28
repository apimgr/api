package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/database"
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

func TestLinkedTokenMutationErrorStatuses(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "duplicate name", err: database.ErrLinkedTokenNameConflict, want: http.StatusConflict},
		{name: "maximum reached", err: database.ErrLinkedTokenLimit, want: http.StatusConflict},
		{name: "missing token", err: database.ErrInvalidOwnerToken, want: http.StatusNotFound},
		{name: "invalid fields", err: database.ErrLinkedTokenValidation, want: http.StatusBadRequest},
		{name: "database failure", err: errors.New("database unavailable"), want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens", nil)
			recorder := httptest.NewRecorder()
			writeLinkedTokenMutationError(recorder, request, test.err)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
		})
	}
}

func TestOwnerTokenExpiryRequestAcceptsSpecFields(t *testing.T) {
	expires := "never"
	got, err := (ownerTokenExpiryRequest{Expires: &expires}).parseExpiry()
	if err != nil || got != nil {
		t.Fatalf("expires field result = %v, %v; want nil, nil", got, err)
	}

	expiresOn := "2099-01-31"
	got, err = (ownerTokenExpiryRequest{ExpiresOn: &expiresOn}).parseExpiry()
	wantExpiry := "2099-02-01"
	if err != nil || got == nil || got.UTC().Format("2006-01-02") != wantExpiry {
		t.Fatalf("expires_on field result = %v, %v; want %s", got, err, wantExpiry)
	}
	today := time.Now().UTC().Format("2006-01-02")
	got, err = (ownerTokenExpiryRequest{ExpiresOn: &today}).parseExpiry()
	if err != nil || got == nil || got.UTC().Format("2006-01-02") != time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02") {
		t.Fatalf("today's expires_on field result = %v, %v", got, err)
	}
	dateAsDuration := "2099-01-31"
	if _, err := (ownerTokenExpiryRequest{Expires: &dateAsDuration}).parseExpiry(); err == nil {
		t.Fatal("a date was accepted in expires instead of expires_on")
	}

	if _, err := (ownerTokenExpiryRequest{Expires: &expires, ExpiresOn: &expiresOn}).parseExpiry(); err == nil {
		t.Fatal("both expires and expires_on were accepted")
	}
}

func TestOwnerTokenAPIItemMasksCredentialFields(t *testing.T) {
	item := ownerTokenAPIItemFrom(database.OwnerToken{
		ID: "018f0000-0000-7000-8000-000000000001", Kind: "linked", Name: "ci",
		Scope: "read", Prefix: "own_abcdefgh", Last4: "WXYZ", Status: "active",
	})
	if item.TokenMasked != "own_"+strings.Repeat("•", 28)+"WXYZ" {
		t.Fatalf("masked token = %q", item.TokenMasked)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("encode API token item: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatalf("decode API token item: %v", err)
	}
	if response["id"] != item.ID || response["token_masked"] != item.TokenMasked || response["scope"] != "read" {
		t.Fatalf("token item = %#v", response)
	}
	for _, key := range []string{"prefix", "last4", "owner_id"} {
		if _, exists := response[key]; exists {
			t.Fatalf("unexpected token field %q in %#v", key, response)
		}
	}
}

func TestOwnerTokenAPIExposesOneTimeCredentials(t *testing.T) {
	const primary = "own_abcdefghijklmnopqrstuvwxyz123456"
	primaryRecorder := httptest.NewRecorder()
	writeEnvelopeOK(primaryRecorder, http.StatusCreated,
		responseObject(responseField("owner_token", sanitize.PublicResponseValue{Value: primary})))
	var primaryResponse struct {
		OK   bool `json:"ok"`
		Data struct {
			OwnerToken string `json:"owner_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(primaryRecorder.Body.Bytes(), &primaryResponse); err != nil {
		t.Fatalf("decode primary-token response: %v", err)
	}
	if primaryRecorder.Code != http.StatusCreated || !primaryResponse.OK || primaryResponse.Data.OwnerToken != primary {
		t.Fatalf("primary token response = %s", primaryRecorder.Body.String())
	}

	const linked = "own_zyxwvutsrqponmlkjihgfedcba654321"
	linkedRecorder := httptest.NewRecorder()
	item := ownerTokenAPIItem{ID: "018f0000-0000-7000-8000-000000000001", Kind: "linked", Scope: "read-write", Status: "active", LinkedToken: linked}
	writeEnvelopeOK(linkedRecorder, http.StatusCreated, item)
	var linkedResponse struct {
		Data struct {
			LinkedToken string `json:"linked_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(linkedRecorder.Body.Bytes(), &linkedResponse); err != nil {
		t.Fatalf("decode linked-token response: %v", err)
	}
	if linkedRecorder.Code != http.StatusCreated || linkedResponse.Data.LinkedToken != linked {
		t.Fatalf("linked token response = %s", linkedRecorder.Body.String())
	}
}
