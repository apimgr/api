package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// apiOwnerPrimaryIssueHandler issues an owner token only when the caller has
// no valid bearer credential, following the API token issuance contract.
func apiOwnerPrimaryIssueHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if token := bearerToken(r); token != "" {
			if _, err := validateConfiguredOwnerToken(r.Context(), cfg, token); err == nil {
				writeTokenAPIError(w, r, "CONFLICT", "errors.conflict", http.StatusConflict)
				return
			}
		}
		token, credential, err := database.IssueOwnerToken(r.Context(), database.GetServerDB(), ownerEncryptionKey(cfg))
		if err != nil {
			writeTokenAPIError(w, r, "INTERNAL_ERROR", "errors.server_error", http.StatusInternalServerError)
			return
		}
		writeTokenAudit(r, "token.created", credential, map[string]any{"source": "issue"})
		writeEnvelopeOK(w, http.StatusCreated, map[string]any{"owner_token": token})
	}
}

// apiOwnerTokensHandler implements the owner-token API mirror. Every
// management operation requires a primary owner credential; linked tokens
// retain resource access but cannot manage credentials.
func apiOwnerTokensHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		credential, ok := primaryOwnerCredential(w, r, cfg)
		if !ok {
			return
		}
		if r.Method == http.MethodGet && chi.URLParam(r, "id") == "" {
			tokens, err := database.ListOwnerTokens(r.Context(), database.GetServerDB(), credential.OwnerID)
			if err != nil {
				writeTokenAPIError(w, r, "INTERNAL_ERROR", "errors.server_error", http.StatusInternalServerError)
				return
			}
			policy, _ := cfg.Server.Tokens.Normalized()
			data := map[string]any{"primary": nil, "linked": []ownerTokenAPIItem{}, "max_linked": policy.MaxLinked}
			for _, token := range tokens {
				item := ownerTokenAPIItemFrom(token)
				if token.Kind == "primary" {
					data["primary"] = item
				} else {
					data["linked"] = append(data["linked"].([]ownerTokenAPIItem), item)
				}
			}
			writeEnvelopeOK(w, http.StatusOK, data)
			return
		}
		if r.Method == http.MethodPost && chi.URLParam(r, "id") == "" {
			apiOwnerLinkedCreate(w, r, cfg, credential)
			return
		}
		id := chi.URLParam(r, "id")
		if _, err := uuid.Parse(id); err != nil {
			writeTokenNotFound(w, r)
			return
		}
		action := strings.Trim(strings.TrimPrefix(r.URL.Path, APIBasePath(cfg)+"/server/preferences/tokens/"), "/")
		action = strings.TrimPrefix(action, id)
		action = strings.Trim(action, "/")
		switch {
		case r.Method == http.MethodPost && action == "reveal":
			token, err := database.RevealLinkedOwnerToken(r.Context(), database.GetServerDB(), credential.OwnerID, id, ownerEncryptionKeys(cfg)...)
			if err != nil {
				writeTokenNotFound(w, r)
				return
			}
			writeTokenAudit(r, "token.revealed", credential, map[string]any{"token_id": id, "token_prefix": ownerTokenPrefix(token)})
			item, ok := findLinkedToken(r.Context(), credential.OwnerID, id)
			if !ok {
				writeTokenNotFound(w, r)
				return
			}
			item.LinkedToken = token
			writeEnvelopeOK(w, http.StatusOK, item)
		case r.Method == http.MethodPost && action == "rotate":
			token, err := database.RotateLinkedOwnerToken(r.Context(), database.GetServerDB(), ownerEncryptionKey(cfg), credential.OwnerID, id)
			if err != nil {
				writeTokenNotFound(w, r)
				return
			}
			writeTokenAudit(r, "token.rotated", credential, map[string]any{"token_id": id, "token_prefix": ownerTokenPrefix(token)})
			item, ok := findLinkedToken(r.Context(), credential.OwnerID, id)
			if !ok {
				writeTokenNotFound(w, r)
				return
			}
			item.LinkedToken = token
			writeEnvelopeOK(w, http.StatusOK, item)
		case r.Method == http.MethodPatch:
			var request ownerTokenExpiryRequest
			if err := decodeOwnerTokenBody(w, r, &request); err != nil {
				return
			}
			expiryProvided := request.Expires != nil || request.ExpiresOn != nil
			if request.Name == nil && !expiryProvided {
				writeTokenAPIError(w, r, "VALIDATION_FAILED", "errors.invalid_format", http.StatusBadRequest)
				return
			}
			var expires *time.Time
			if expiryProvided {
				var err error
				expires, err = request.parseExpiry()
				if err != nil {
					writeTokenAPIError(w, r, "VALIDATION_FAILED", "errors.invalid_format", http.StatusBadRequest)
					return
				}
			}
			if err := database.PatchLinkedOwnerToken(r.Context(), database.GetServerDB(), credential.OwnerID, id, request.Name, expires, expiryProvided); err != nil {
				writeLinkedTokenMutationError(w, r, err)
				return
			}
			item, ok := findLinkedToken(r.Context(), credential.OwnerID, id)
			if !ok {
				writeTokenNotFound(w, r)
				return
			}
			writeEnvelopeOK(w, http.StatusOK, item)
		case r.Method == http.MethodDelete && action == "":
			items, listErr := database.ListOwnerTokens(r.Context(), database.GetServerDB(), credential.OwnerID)
			if listErr != nil {
				writeTokenAPIError(w, r, "INTERNAL_ERROR", "errors.server_error", http.StatusInternalServerError)
				return
			}
			var targetPrefix string
			for _, item := range items {
				if item.ID == id && item.Kind == "linked" {
					targetPrefix = item.Prefix
					break
				}
			}
			if err := database.RevokeLinkedOwnerToken(r.Context(), database.GetServerDB(), credential.OwnerID, id); err != nil {
				writeTokenNotFound(w, r)
				return
			}
			writeTokenAudit(r, "token.revoked", credential, map[string]any{"token_id": id, "token_prefix": targetPrefix})
			writeEnvelopeOK(w, http.StatusOK, map[string]string{"id": id, "status": "revoked"})
		default:
			writeTokenAPIError(w, r, "METHOD_NOT_ALLOWED", "errors.method_not_allowed", http.StatusMethodNotAllowed)
		}
	}
}

type ownerTokenAPIItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name,omitempty"`
	Kind        string     `json:"kind"`
	TokenMasked string     `json:"token_masked"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Status      string     `json:"status"`
	LinkedToken string     `json:"linked_token,omitempty"`
}

func ownerTokenAPIItemFrom(token database.OwnerToken) ownerTokenAPIItem {
	return ownerTokenAPIItem{
		ID: token.ID, Name: token.Name, Kind: token.Kind,
		TokenMasked: "own_" + strings.Repeat("•", 28) + token.Last4,
		CreatedAt:   token.CreatedAt, LastUsedAt: token.LastUsed,
		ExpiresAt: token.ExpiresAt, Status: token.Status,
	}
}

func findLinkedToken(ctx context.Context, ownerID, id string) (ownerTokenAPIItem, bool) {
	tokens, err := database.ListOwnerTokens(ctx, database.GetServerDB(), ownerID)
	if err != nil {
		return ownerTokenAPIItem{}, false
	}
	for _, token := range tokens {
		if token.ID == id && token.Kind == "linked" {
			return ownerTokenAPIItemFrom(token), true
		}
	}
	return ownerTokenAPIItem{}, false
}

// apiOwnerPrimaryHandler serves reveal, rotate, and delete of the primary.
func apiOwnerPrimaryHandler(cfg *config.Config, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		credential, ok := primaryOwnerCredential(w, r, cfg)
		if !ok {
			return
		}
		switch action {
		case "reveal":
			token, err := database.RevealOwnerToken(r.Context(), database.GetServerDB(), credential.OwnerID, credential.TokenID, ownerEncryptionKeys(cfg)...)
			if err != nil {
				writeTokenNotFound(w, r)
				return
			}
			writeTokenAudit(r, "token.revealed", credential, map[string]any{"token_id": credential.TokenID, "token_prefix": ownerTokenPrefix(token)})
			writeEnvelopeOK(w, http.StatusOK, map[string]string{"owner_token": token})
		case "rotate":
			token, err := database.RotateOwnerToken(r.Context(), database.GetServerDB(), ownerEncryptionKey(cfg), credential.OwnerID, credential.TokenID)
			if err != nil {
				writeTokenAPIError(w, r, "INTERNAL_ERROR", "errors.server_error", http.StatusInternalServerError)
				return
			}
			writeTokenAudit(r, "token.rotated", credential, map[string]any{"token_id": credential.TokenID, "token_prefix": ownerTokenPrefix(token)})
			writeEnvelopeOK(w, http.StatusOK, map[string]string{"owner_token": token})
		case "revoke":
			if err := database.RevokeOwner(r.Context(), database.GetServerDB(), credential.OwnerID); err != nil {
				writeTokenAPIError(w, r, "INTERNAL_ERROR", "errors.server_error", http.StatusInternalServerError)
				return
			}
			writeTokenAudit(r, "token.revoked", credential, map[string]any{"owner_id": credential.OwnerID})
			writeEnvelopeOK(w, http.StatusOK, map[string]bool{"revoked": true})
		default:
			writeTokenNotFound(w, r)
		}
	}
}

func apiOwnerLinkedCreate(w http.ResponseWriter, r *http.Request, cfg *config.Config, credential database.OwnerCredential) {
	var request ownerTokenExpiryRequest
	if err := decodeOwnerTokenBody(w, r, &request); err != nil {
		return
	}
	if request.Name == nil {
		writeTokenAPIError(w, r, "VALIDATION_FAILED", "errors.invalid_format", http.StatusBadRequest)
		return
	}
	expires, err := request.parseExpiry()
	if err != nil {
		writeTokenAPIError(w, r, "VALIDATION_FAILED", "errors.invalid_format", http.StatusBadRequest)
		return
	}
	policy, _ := cfg.Server.Tokens.Normalized()
	token, item, err := database.CreateLinkedOwnerToken(r.Context(), database.GetServerDB(), ownerEncryptionKey(cfg), credential.OwnerID, *request.Name, expires, policy.MaxLinked)
	if err != nil {
		writeLinkedTokenMutationError(w, r, err)
		return
	}
	writeTokenAudit(r, "token.created", credential, map[string]any{"token_id": item.ID, "source": "linked", "token_prefix": item.Prefix})
	response := ownerTokenAPIItemFrom(item)
	response.LinkedToken = token
	writeEnvelopeOK(w, http.StatusCreated, response)
}

func writeLinkedTokenMutationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, database.ErrLinkedTokenNameConflict), errors.Is(err, database.ErrLinkedTokenLimit):
		writeTokenAPIError(w, r, "CONFLICT", "errors.conflict", http.StatusConflict)
	case errors.Is(err, database.ErrInvalidOwnerToken):
		writeTokenNotFound(w, r)
	case errors.Is(err, database.ErrLinkedTokenValidation):
		writeTokenAPIError(w, r, "VALIDATION_FAILED", "errors.invalid_format", http.StatusBadRequest)
	default:
		writeTokenAPIError(w, r, "INTERNAL_ERROR", "errors.server_error", http.StatusInternalServerError)
	}
}

type ownerTokenExpiryRequest struct {
	Name      *string `json:"name"`
	Expires   *string `json:"expires"`
	ExpiresOn *string `json:"expires_on"`
}

func (request ownerTokenExpiryRequest) parseExpiry() (*time.Time, error) {
	if request.Expires != nil && request.ExpiresOn != nil {
		return nil, errors.New("expires and expires_on cannot both be set")
	}
	if request.ExpiresOn != nil {
		return parseOwnerTokenExpiry(*request.ExpiresOn)
	}
	if request.Expires == nil {
		return parseOwnerTokenExpiry("")
	}
	switch strings.TrimSpace(*request.Expires) {
	case "", "never", "7d", "30d", "90d", "1y":
	default:
		return nil, errors.New("expires must be never, 7d, 30d, 90d, or 1y")
	}
	return parseOwnerTokenExpiry(*request.Expires)
}

func primaryOwnerCredential(w http.ResponseWriter, r *http.Request, cfg *config.Config) (database.OwnerCredential, bool) {
	token := bearerToken(r)
	credential, err := validateConfiguredOwnerToken(r.Context(), cfg, token)
	if err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeTokenAPIError(w, r, "UNAUTHORIZED", "errors.unauthorized", http.StatusUnauthorized)
		return database.OwnerCredential{}, false
	}
	if credential.Kind != "primary" {
		writeTokenAPIError(w, r, "FORBIDDEN", "errors.forbidden", http.StatusForbidden)
		return database.OwnerCredential{}, false
	}
	return credential, true
}

func validateConfiguredOwnerToken(ctx context.Context, cfg *config.Config, token string) (database.OwnerCredential, error) {
	operator := ""
	if cfg != nil {
		operator = cfg.Server.Token
	}
	return database.ValidateOwnerToken(ctx, database.GetServerDB(), token, operator)
}

func bearerToken(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func ownerEncryptionKey(cfg *config.Config) []byte {
	if cfg == nil {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(cfg.Server.Security.EncryptionKey)
	if err != nil {
		return nil
	}
	return key
}

func ownerEncryptionKeys(cfg *config.Config) [][]byte {
	if cfg == nil {
		return nil
	}
	keys := make([][]byte, 0, 2)
	if current := ownerEncryptionKey(cfg); len(current) == 32 {
		keys = append(keys, current)
	}
	if len(keys) > 0 {
		if previous, err := database.LoadPreviousEncryptionKey(context.Background(), keys[0], time.Now().UTC()); err == nil {
			keys = append(keys, previous)
		}
	}
	return keys
}

func decodeOwnerTokenBody(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeTokenAPIError(w, r, "BAD_REQUEST", "errors.bad_request", http.StatusBadRequest)
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeTokenAPIError(w, r, "BAD_REQUEST", "errors.bad_request", http.StatusBadRequest)
		return errors.New("trailing JSON value")
	}
	return nil
}

func parseOwnerTokenExpiry(value string) (*time.Time, error) {
	now := time.Now().UTC()
	switch strings.TrimSpace(value) {
	case "", "never":
		return nil, nil
	case "7d":
		out := now.Add(7 * 24 * time.Hour)
		return &out, nil
	case "30d":
		out := now.Add(30 * 24 * time.Hour)
		return &out, nil
	case "90d":
		out := now.Add(90 * 24 * time.Hour)
		return &out, nil
	case "1y":
		out := now.AddDate(1, 0, 0)
		return &out, nil
	default:
		out, err := time.ParseInLocation("2006-01-02", value, time.UTC)
		if err != nil || out.Format("2006-01-02") < now.Format("2006-01-02") {
			return nil, errors.New("expiry must be never, 7d, 30d, 90d, 1y, or today or a future UTC date")
		}
		endOfDate := out.AddDate(0, 0, 1)
		return &endOfDate, nil
	}
}

func writeTokenAPIError(w http.ResponseWriter, r *http.Request, code, messageKey string, status int) {
	message := i18n.Translate(i18n.LangFromRequest(r), messageKey)
	writeEnvelopeError(w, status, code, message, nil)
}

func writeTokenNotFound(w http.ResponseWriter, r *http.Request) {
	writeTokenAPIError(w, r, "NOT_FOUND", "errors.not_found", http.StatusNotFound)
}

func writeTokenAudit(r *http.Request, event string, credential database.OwnerCredential, details map[string]any) {
	if details == nil {
		details = make(map[string]any)
	}
	if _, exists := details["token_prefix"]; !exists {
		details["token_prefix"] = credential.Prefix
	}
	if credential.OwnerID != "" {
		details["owner_id"] = credential.OwnerID
	}
	_ = database.WriteAuditEvent(r.Context(), event, credential.OwnerID, getClientIP(r), details)
}

func ownerTokenPrefix(token string) string {
	if len(token) < 12 {
		return token
	}
	return token[:12]
}
