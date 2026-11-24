package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/swagger"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestMCPRejectsMissingBearerAndLegacyInitialize(t *testing.T) {
	cfg := config.DefaultConfig()
	auth := func(_ context.Context, token string) (database.OwnerCredential, error) {
		if token != "valid" {
			return database.OwnerCredential{}, database.ErrInvalidOwnerToken
		}
		return database.OwnerCredential{OwnerID: "owner", Kind: "primary"}, nil
	}
	handler := mcpHandler(cfg, nil, chi.NewRouter(), auth)

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	handler.ServeHTTP(unauthorized, request)
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)
	require.Equal(t, "Bearer", unauthorized.Header().Get("WWW-Authenticate"))

	legacy := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "http://example.test/mcp?lang=es", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	request.Header.Set("Authorization", "Bearer valid")
	handler.ServeHTTP(legacy, request)
	require.Equal(t, http.StatusBadRequest, legacy.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(legacy.Body.Bytes(), &response))
	require.Equal(t, "Formato de solicitud inválido", response["error"].(map[string]any)["message"])
	require.Equal(t, float64(-32022), response["error"].(map[string]any)["code"])
	require.Equal(t, []any{"2026-07-28"}, response["error"].(map[string]any)["data"].(map[string]any)["supported"])
}

func TestMCPToolsUseRegisteredRoutesAndMethodAnnotations(t *testing.T) {
	cfg := config.DefaultConfig()
	routes := []swagger.Route{
		{Method: http.MethodGet, Path: "/api/v1/health"},
		{Method: http.MethodDelete, Path: "/api/v1/items/{id}"},
		{Method: http.MethodPost, Path: "/api/v1/server/preferences/tokens/primary"},
		{Method: http.MethodGet, Path: "/api/v1/server/metrics"},
	}
	tools := mcpTools(cfg, routes)
	require.Contains(t, tools, "get_api_v1_health")
	require.Contains(t, tools, "delete_api_v1_items_id")
	require.NotContains(t, tools, "post_api_v1_server_preferences_tokens_primary")
	require.NotContains(t, tools, "get_api_v1_server_metrics")
	require.Equal(t, true, tools["get_api_v1_health"].Annotations["readOnlyHint"])
	require.Equal(t, true, tools["delete_api_v1_items_id"].Annotations["destructiveHint"])
	require.Equal(t, true, tools["delete_api_v1_items_id"].Annotations["idempotentHint"])
}

func TestDecodeMCPNameBase64EncodedHeader(t *testing.T) {
	require.Equal(t, "é", decodeMCPName("=?base64?w6k=?="))
}

func TestMCPHeaderMismatchAndSuccessfulToolDispatch(t *testing.T) {
	cfg := config.DefaultConfig()
	dispatch := chi.NewRouter()
	dispatch.Get("/api/v1/echo", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	auth := func(_ context.Context, token string) (database.OwnerCredential, error) {
		if token == "valid" {
			return database.OwnerCredential{OwnerID: "owner", Kind: "primary"}, nil
		}
		return database.OwnerCredential{}, database.ErrInvalidOwnerToken
	}
	handler := mcpHandler(cfg, dispatch, dispatch, auth)
	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_api_v1_echo","arguments":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`
	request := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("MCP-Protocol-Version", "2026-07-28")
	request.Header.Set("Mcp-Method", "tools/call")
	request.Header.Set("Mcp-Name", "wrong")
	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, request)
	require.Equal(t, http.StatusBadRequest, failed.Code)

	request = httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("MCP-Protocol-Version", "2026-07-28")
	request.Header.Set("Mcp-Method", "tools/call")
	request.Header.Set("Mcp-Name", "get_api_v1_echo")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Len(t, result.Result.Content, 1)
	require.JSONEq(t, `{"ok":true}`, result.Result.Content[0].Text)
}
