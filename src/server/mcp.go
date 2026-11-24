package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/swagger"
	"github.com/go-chi/chi/v5"
)

const mcpProtocolVersion = "2026-07-28"

type mcpDispatchContextKey struct{}

// isMCPDispatch reports whether a request is an in-process MCP tool call.
// Middleware can avoid double-counting request-level controls while still
// applying its ordinary validation and dispatch logic.
func isMCPDispatch(r *http.Request) bool {
	return r != nil && r.Context().Value(mcpDispatchContextKey{}) == true
}

type mcpAuthFunc func(context.Context, string) (database.OwnerCredential, error)

func configuredMCPAuthenticator(cfg *config.Config) mcpAuthFunc {
	return func(ctx context.Context, token string) (database.OwnerCredential, error) {
		return validateConfiguredOwnerToken(ctx, cfg, token)
	}
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  map[string]any  `json:"params"`
	Meta    map[string]any  `json:"_meta"`
}

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema swagger.Schema         `json:"inputSchema"`
	Annotations map[string]interface{} `json:"annotations"`
	method      string
	path        string
	operation   swagger.Operation
}

// mcpHandler implements the stateless JSON-RPC Streamable HTTP endpoint.
// dispatch is the existing server router so tool calls re-enter the ordinary
// middleware and handler chain in process.
func mcpHandler(cfg *config.Config, routes chi.Routes, dispatch http.Handler, authenticate mcpAuthFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeMCPTransportError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "errors.method_not_allowed")
			return
		}
		if !mcpOriginAllowed(cfg, r) {
			writeMCPTransportError(w, r, http.StatusForbidden, "FORBIDDEN", "errors.forbidden")
			return
		}
		credential, err := authenticateMCPRequest(r.Context(), r, authenticate)
		if err != nil {
			_ = database.WriteAuditEventWithSource(r.Context(), "mcp.auth_failed", "", getClientIP(r), "mcp", map[string]any{"reason": "invalid_token"})
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeMCPError(w, r, nil, -32001, "errors.unauthorized", http.StatusUnauthorized, nil)
			return
		}
		var request mcpRequest
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil || json.Unmarshal(body, &request) != nil || request.JSONRPC != "2.0" || request.Method == "" {
			writeMCPTransportError(w, r, http.StatusBadRequest, "BAD_REQUEST", "errors.bad_request")
			return
		}
		if request.Method == "initialize" || r.Header.Get("MCP-Protocol-Version") != mcpProtocolVersion {
			requested := r.Header.Get("MCP-Protocol-Version")
			if requested == "" {
				requested, _ = request.Params["protocolVersion"].(string)
			}
			writeMCPError(w, r, request.ID, -32022, "errors.bad_request", http.StatusBadRequest, map[string]any{"supported": []string{mcpProtocolVersion}, "requested": requested})
			return
		}
		metaVersion, _ := request.Meta["io.modelcontextprotocol/protocolVersion"].(string)
		if metaVersion != mcpProtocolVersion || r.Header.Get("Mcp-Method") != request.Method {
			writeMCPError(w, r, request.ID, -32020, "errors.bad_request", http.StatusBadRequest, nil)
			return
		}
		routeList, err := collectMCPRoutes(routes)
		if err != nil {
			writeMCPTransportError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "errors.server_error")
			return
		}
		tools := mcpTools(cfg, routeList)
		if needsMCPName(request.Method) {
			name, ok := mcpRequestName(request)
			if !ok || decodeMCPName(r.Header.Get("Mcp-Name")) != name {
				writeMCPError(w, r, request.ID, -32020, "errors.bad_request", http.StatusBadRequest, nil)
				return
			}
		}
		actor := credential.OwnerID
		if credential.Kind == "operator" {
			actor = "operator"
		}
		details := map[string]any{"method": request.Method}
		if credential.Prefix != "" {
			details["token_prefix"] = credential.Prefix
		}
		if name, ok := mcpRequestName(request); ok {
			details["name"] = name
		}
		_ = database.WriteAuditEventWithSource(r.Context(), "mcp.request", actor, getClientIP(r), "mcp", details)
		switch request.Method {
		case "tools/list":
			writeMCPSuccess(w, request.ID, map[string]any{"tools": sortedMCPTools(tools)})
		case "tools/call":
			name, _ := mcpRequestName(request)
			tool, ok := tools[name]
			if !ok {
				writeMCPError(w, r, request.ID, -32602, "errors.not_found", http.StatusBadRequest, nil)
				return
			}
			result, callErr := invokeMCPTool(r, dispatch, tool, request.Params)
			if callErr != nil {
				writeMCPSuccess(w, request.ID, map[string]any{"content": []any{map[string]string{"type": "text", "text": i18n.Translate(i18n.LangFromRequest(r), "errors.server_error")}}, "isError": true})
				return
			}
			writeMCPSuccess(w, request.ID, map[string]any{"content": []any{map[string]string{"type": "text", "text": result}}})
		default:
			writeMCPError(w, r, request.ID, -32601, "errors.not_found", http.StatusNotFound, nil)
		}
	}
}

func collectMCPRoutes(routes chi.Routes) ([]swagger.Route, error) {
	if routes == nil {
		return nil, nil
	}
	var result []swagger.Route
	err := chi.Walk(routes, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(path, "/api/") {
			result = append(result, apiRouteDefinition(method, path))
		}
		return nil
	})
	return result, err
}

func authenticateMCPRequest(ctx context.Context, r *http.Request, authenticate mcpAuthFunc) (database.OwnerCredential, error) {
	if authenticate == nil {
		return database.OwnerCredential{}, database.ErrInvalidOwnerToken
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return database.OwnerCredential{}, database.ErrInvalidOwnerToken
	}
	return authenticate(ctx, parts[1])
}

func mcpOriginAllowed(cfg *config.Config, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if base, err := url.Parse(BuildURL(r, "")); err == nil && base.Scheme != "" && base.Host != "" {
		if origin == base.Scheme+"://"+base.Host {
			return true
		}
	}
	allowed, _ := resolveCORSOrigins(cfg, r)
	for _, candidate := range allowed {
		if candidate != "*" && candidate == origin {
			return true
		}
	}
	return false
}

func mcpTools(cfg *config.Config, routes []swagger.Route) map[string]mcpTool {
	apiVersion := strings.TrimPrefix(APIBasePath(cfg), "/api/")
	baseSpec := swagger.GenerateSpecForAPIVersion(Version, "", apiVersion)
	spec := &swagger.Spec{Paths: make(map[string]swagger.PathItem)}
	for _, route := range routes {
		if route.Path == "/api/mcp" || route.Path == "/mcp" || !strings.HasPrefix(route.Path, APIBasePath(cfg)+"/") || strings.Contains(route.Path, "/metrics") || strings.Contains(route.Path, "/admin/") || strings.Contains(route.Path, "/preferences/tokens") || strings.HasSuffix(route.Path, "/server/mcp") {
			continue
		}
		path := strings.ReplaceAll(route.Path, "*", "{path}")
		item := spec.Paths[path]
		operation := operationForMethod(baseSpec.Paths[path], route.Method)
		if operation == nil {
			operation = &swagger.Operation{
				Summary:     strings.Trim(strings.ReplaceAll(strings.TrimPrefix(path, APIBasePath(cfg)), "/", " "), " "),
				OperationID: strings.ToLower(route.Method + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(path)),
				Tags:        []string{"API"}, Responses: map[string]swagger.Response{"200": {Description: "Operation completed"}},
			}
		}
		setMCPOperation(&item, route.Method, *operation)
		spec.Paths[path] = item
	}
	tools := make(map[string]mcpTool)
	for path, item := range spec.Paths {
		for _, pair := range []struct {
			method    string
			operation *swagger.Operation
		}{{"GET", item.Get}, {"POST", item.Post}, {"PUT", item.Put}, {"PATCH", item.Patch}, {"DELETE", item.Delete}} {
			if pair.operation == nil || pair.operation.OperationID == "" || path == "/api/mcp" || path == "/mcp" || strings.Contains(path, "/preferences/tokens") || strings.HasSuffix(path, "/server/mcp") || strings.Contains(path, "/metrics") || strings.Contains(path, "/admin/") {
				continue
			}
			name := pair.operation.OperationID
			tool := mcpTool{Name: name, Description: pair.operation.Summary, InputSchema: mcpInputSchema(*pair.operation), Annotations: mcpAnnotations(pair.method), method: pair.method, path: path, operation: *pair.operation}
			tools[name] = tool
		}
	}
	return tools
}

func operationForMethod(item swagger.PathItem, method string) *swagger.Operation {
	switch strings.ToUpper(method) {
	case http.MethodGet:
		return item.Get
	case http.MethodPost:
		return item.Post
	case http.MethodPut:
		return item.Put
	case http.MethodPatch:
		return item.Patch
	case http.MethodDelete:
		return item.Delete
	default:
		return nil
	}
}

func setMCPOperation(item *swagger.PathItem, method string, operation swagger.Operation) {
	switch strings.ToUpper(method) {
	case http.MethodGet:
		if item.Get == nil {
			item.Get = &operation
		}
	case http.MethodPost:
		if item.Post == nil {
			item.Post = &operation
		}
	case http.MethodPut:
		if item.Put == nil {
			item.Put = &operation
		}
	case http.MethodPatch:
		if item.Patch == nil {
			item.Patch = &operation
		}
	case http.MethodDelete:
		if item.Delete == nil {
			item.Delete = &operation
		}
	}
}

func mcpInputSchema(operation swagger.Operation) swagger.Schema {
	properties := make(map[string]swagger.Schema)
	var required []string
	for _, parameter := range operation.Parameters {
		properties[parameter.Name] = parameter.Schema
		if parameter.Required {
			required = append(required, parameter.Name)
		}
	}
	if operation.RequestBody != nil {
		if media, ok := operation.RequestBody.Content["application/json"]; ok {
			properties["body"] = media.Schema
			if operation.RequestBody.Required {
				required = append(required, "body")
			}
		}
	}
	return swagger.Schema{Type: "object", Properties: properties, Required: required}
}

func mcpAnnotations(method string) map[string]interface{} {
	annotations := map[string]interface{}{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false}
	switch method {
	case http.MethodGet:
		annotations["readOnlyHint"] = true
	case http.MethodDelete:
		annotations["destructiveHint"] = true
		annotations["idempotentHint"] = true
	case http.MethodPut:
		annotations["idempotentHint"] = true
	}
	return annotations
}

func invokeMCPTool(r *http.Request, dispatch http.Handler, tool mcpTool, params map[string]any) (string, error) {
	if dispatch == nil {
		return "", fmt.Errorf("API router is unavailable")
	}
	arguments, _ := params["arguments"].(map[string]any)
	if arguments == nil {
		arguments = map[string]any{}
	}
	path := tool.path
	query := url.Values{}
	extraHeaders := make(http.Header)
	parameterNames := make(map[string]struct{}, len(tool.operation.Parameters))
	for _, parameter := range tool.operation.Parameters {
		parameterNames[parameter.Name] = struct{}{}
		value, ok := arguments[parameter.Name]
		if !ok || value == nil {
			continue
		}
		switch parameter.In {
		case "path":
			text := fmt.Sprint(value)
			if parameter.Name != "path" && (strings.ContainsAny(text, "/\\") || text == "." || text == "..") {
				return "", fmt.Errorf("path argument %q is invalid", parameter.Name)
			}
			path = strings.ReplaceAll(path, "{"+parameter.Name+"}", url.PathEscape(text))
		case "query":
			query.Set(parameter.Name, fmt.Sprint(value))
		case "header":
			extraHeaders.Set(parameter.Name, fmt.Sprint(value))
		}
	}
	for key, value := range arguments {
		if _, isParameter := parameterNames[key]; isParameter || key == "body" || value == nil {
			continue
		}
		if tool.method == http.MethodGet {
			query.Set(key, fmt.Sprint(value))
		}
	}
	if strings.Contains(path, "{") {
		return "", fmt.Errorf("required path argument is missing")
	}
	var requestBody io.Reader
	if body, ok := arguments["body"]; ok && body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		requestBody = bytes.NewReader(encoded)
	} else if tool.method != http.MethodGet && len(arguments) > 0 {
		bodyArgs := make(map[string]any, len(arguments))
		for key, value := range arguments {
			if key == "body" {
				continue
			}
			if _, isParameter := parameterNames[key]; !isParameter {
				bodyArgs[key] = value
			}
		}
		if len(bodyArgs) > 0 {
			encoded, err := json.Marshal(bodyArgs)
			if err != nil {
				return "", err
			}
			requestBody = bytes.NewReader(encoded)
		}
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	clone := r.Clone(r.Context())
	ctx := database.WithAuditSource(clone.Context(), "mcp")
	clone = clone.WithContext(context.WithValue(ctx, mcpDispatchContextKey{}, true))
	clone.Method = tool.method
	target, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	clone.URL = target
	clone.RequestURI = path
	if requestBody != nil {
		clone.Body = io.NopCloser(requestBody)
	} else {
		clone.Body = http.NoBody
	}
	clone.Header = r.Header.Clone()
	for key, values := range extraHeaders {
		clone.Header[key] = values
	}
	if requestBody != nil {
		clone.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	dispatch.ServeHTTP(recorder, clone)
	result := recorder.Body.String()
	if result == "" {
		result = strconv.Itoa(recorder.Code)
	}
	if recorder.Code >= http.StatusBadRequest {
		return result, fmt.Errorf("%s", result)
	}
	return result, nil
}

func needsMCPName(method string) bool {
	return method == "tools/call" || method == "resources/read" || method == "prompts/get"
}

func mcpRequestName(request mcpRequest) (string, bool) {
	params := request.Params
	if value, ok := params["name"].(string); ok {
		return value, true
	}
	if value, ok := params["uri"].(string); ok {
		return value, true
	}
	return "", false
}

func decodeMCPName(value string) string {
	if strings.HasPrefix(strings.ToLower(value), "=?base64?") && strings.HasSuffix(value, "?=") {
		encoded := value[len("=?base64?") : len(value)-2]
		if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			return string(decoded)
		}
		if decoded, err := base64.RawStdEncoding.DecodeString(encoded); err == nil {
			return string(decoded)
		}
	}
	if strings.HasPrefix(value, "=?") {
		if decoded, err := new(mime.WordDecoder).DecodeHeader(value); err == nil {
			return decoded
		}
	}
	return value
}

func writeMCPSuccess(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rawMCPID(id), "result": result})
}

func writeMCPError(w http.ResponseWriter, r *http.Request, id json.RawMessage, code int, messageKey string, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	object := map[string]any{"code": code, "message": i18n.Translate(i18n.LangFromRequest(r), messageKey)}
	if data != nil {
		object["data"] = data
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rawMCPID(id), "error": object})
}

func writeMCPTransportError(w http.ResponseWriter, r *http.Request, status int, code, messageKey string) {
	writeEnvelopeError(w, status, code, i18n.Translate(i18n.LangFromRequest(r), messageKey), nil)
}

func rawMCPID(id json.RawMessage) any {
	if len(id) == 0 || bytes.Equal(id, []byte("null")) {
		return nil
	}
	var value any
	if json.Unmarshal(id, &value) != nil {
		return nil
	}
	return value
}

func sortedMCPTools(tools map[string]mcpTool) []mcpTool {
	keys := make([]string, 0, len(tools))
	for key := range tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]mcpTool, 0, len(keys))
	for _, key := range keys {
		result = append(result, tools[key])
	}
	return result
}
