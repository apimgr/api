package swagger

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/apimgr/api/src/common/sanitize"
)

// Route is a method and path collected from the running server router.
type Route struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Primary     string `json:"primary,omitempty"`
	TextMessage string `json:"text_message,omitempty"`
	MountedWhen string `json:"x-mounted-when"`
	Internal    bool   `json:"internal,omitempty"`
}

//go:embed openapi_superset.json
var embeddedOpenAPISuperset []byte

// EmbeddedSuperset returns a fresh configured copy of the build-generated
// OpenAPI superset. The embedded bytes remain immutable while each server
// filters its own copy for the routes enabled in that run.
func EmbeddedSuperset(version, baseURL, apiVersion string) (*Spec, error) {
	var spec Spec
	if err := json.Unmarshal(embeddedOpenAPISuperset, &spec); err != nil {
		return nil, fmt.Errorf("decode embedded OpenAPI superset: %w", err)
	}
	spec.Info.Version = version
	if len(spec.Servers) == 0 {
		spec.Servers = []Server{{URL: baseURL, Description: "API Server"}}
	} else {
		spec.Servers[0].URL = baseURL
	}
	if apiVersion != "" && apiVersion != "{api_version}" {
		paths := make(map[string]PathItem, len(spec.Paths))
		for path, item := range spec.Paths {
			paths[strings.ReplaceAll(path, "{api_version}", apiVersion)] = item
		}
		spec.Paths = paths
	}
	return &spec, nil
}

// Spec represents the OpenAPI specification structure
type Spec struct {
	OpenAPI    string              `json:"openapi"`
	Info       Info                `json:"info"`
	Servers    []Server            `json:"servers"`
	Paths      map[string]PathItem `json:"paths"`
	Components Components          `json:"components,omitempty"`
}

// Info contains API metadata
type Info struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Version     string  `json:"version"`
	Contact     Contact `json:"contact,omitempty"`
	License     License `json:"license,omitempty"`
}

// Contact information
type Contact struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// License information
type License struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// Server represents an API server
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// PathItem represents operations available on a path
type PathItem struct {
	Get     *Operation `json:"get,omitempty"`
	Head    *Operation `json:"head,omitempty"`
	Post    *Operation `json:"post,omitempty"`
	Put     *Operation `json:"put,omitempty"`
	Delete  *Operation `json:"delete,omitempty"`
	Patch   *Operation `json:"patch,omitempty"`
	Options *Operation `json:"options,omitempty"`
}

// Operation represents a single API operation
type Operation struct {
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	OperationID string              `json:"operationId,omitempty"`
	Primary     string              `json:"x-primary,omitempty"`
	TextMessage string              `json:"x-text-message,omitempty"`
	MountedWhen string              `json:"x-mounted-when"`
	Tags        []string            `json:"tags,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
}

// Parameter represents an operation parameter
type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"` // query, header, path, cookie
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Schema      Schema `json:"schema,omitempty"`
}

// RequestBody represents request body
type RequestBody struct {
	Description string               `json:"description,omitempty"`
	Required    bool                 `json:"required,omitempty"`
	Content     map[string]MediaType `json:"content"`
}

// MediaType represents a media type
type MediaType struct {
	Schema Schema `json:"schema,omitempty"`
}

// Response represents an operation response
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// Schema represents a JSON Schema
type Schema struct {
	Type       string            `json:"type,omitempty"`
	Format     string            `json:"format,omitempty"`
	Properties map[string]Schema `json:"properties,omitempty"`
	Items      *Schema           `json:"items,omitempty"`
	Required   []string          `json:"required,omitempty"`
	Example    interface{}       `json:"example,omitempty"`
}

// Components holds reusable schemas
type Components struct {
	Schemas map[string]Schema `json:"schemas,omitempty"`
}

func (spec Spec) TaggedResponseFields() sanitize.TaggedObject {
	fields := sanitize.TaggedObject{{Name: "openapi", Value: spec.OpenAPI}, {Name: "info", Value: spec.Info}, {Name: "servers", Value: spec.Servers}, {Name: "paths", Value: taggedObjectMap(spec.Paths)}}
	if len(spec.Components.Schemas) > 0 {
		fields = append(fields, sanitize.TaggedField{Name: "components", Value: spec.Components})
	}
	return fields
}

func (operation Operation) TaggedResponseFields() sanitize.TaggedObject {
	fields := sanitize.TaggedObject{}
	if operation.Summary != "" {
		fields = append(fields, sanitize.TaggedField{Name: "summary", Value: operation.Summary})
	}
	if operation.Description != "" {
		fields = append(fields, sanitize.TaggedField{Name: "description", Value: operation.Description})
	}
	if operation.OperationID != "" {
		fields = append(fields, sanitize.TaggedField{Name: "operationId", Value: operation.OperationID})
	}
	if operation.Primary != "" {
		fields = append(fields, sanitize.TaggedField{Name: "x-primary", Value: operation.Primary})
	}
	if operation.TextMessage != "" {
		fields = append(fields, sanitize.TaggedField{Name: "x-text-message", Value: operation.TextMessage})
	}
	fields = append(fields, sanitize.TaggedField{Name: "x-mounted-when", Value: operation.MountedWhen})
	if len(operation.Tags) > 0 {
		fields = append(fields, sanitize.TaggedField{Name: "tags", Value: operation.Tags})
	}
	if len(operation.Parameters) > 0 {
		fields = append(fields, sanitize.TaggedField{Name: "parameters", Value: operation.Parameters})
	}
	if operation.RequestBody != nil {
		fields = append(fields, sanitize.TaggedField{Name: "requestBody", Value: operation.RequestBody})
	}
	fields = append(fields, sanitize.TaggedField{Name: "responses", Value: taggedObjectMap(operation.Responses)})
	return fields
}

func (body RequestBody) TaggedResponseFields() sanitize.TaggedObject {
	fields := sanitize.TaggedObject{}
	if body.Description != "" {
		fields = append(fields, sanitize.TaggedField{Name: "description", Value: body.Description})
	}
	if body.Required {
		fields = append(fields, sanitize.TaggedField{Name: "required", Value: true})
	}
	fields = append(fields, sanitize.TaggedField{Name: "content", Value: taggedObjectMap(body.Content)})
	return fields
}

func (response Response) TaggedResponseFields() sanitize.TaggedObject {
	fields := sanitize.TaggedObject{{Name: "description", Value: response.Description}}
	if len(response.Content) > 0 {
		fields = append(fields, sanitize.TaggedField{Name: "content", Value: taggedObjectMap(response.Content)})
	}
	return fields
}

func (schema Schema) TaggedResponseFields() sanitize.TaggedObject {
	fields := sanitize.TaggedObject{}
	if schema.Type != "" {
		fields = append(fields, sanitize.TaggedField{Name: "type", Value: schema.Type})
	}
	if schema.Format != "" {
		fields = append(fields, sanitize.TaggedField{Name: "format", Value: schema.Format})
	}
	if len(schema.Properties) > 0 {
		fields = append(fields, sanitize.TaggedField{Name: "properties", Value: taggedObjectMap(schema.Properties)})
	}
	if schema.Items != nil {
		fields = append(fields, sanitize.TaggedField{Name: "items", Value: schema.Items})
	}
	if len(schema.Required) > 0 {
		fields = append(fields, sanitize.TaggedField{Name: "required", Value: schema.Required})
	}
	if schema.Example != nil {
		fields = append(fields, sanitize.TaggedField{Name: "example", Value: schema.Example})
	}
	return fields
}

func (components Components) TaggedResponseFields() sanitize.TaggedObject {
	return sanitize.TaggedObject{{Name: "schemas", Value: taggedObjectMap(components.Schemas)}}
}

func taggedObjectMap[T any](values map[string]T) sanitize.TaggedObject {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make(sanitize.TaggedObject, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, sanitize.TaggedField{Name: key, Value: values[key]})
	}
	return fields
}

// GenerateSpec creates the OpenAPI specification for the API
func GenerateSpec(version, baseURL string) *Spec {
	return GenerateSpecForAPIVersion(version, baseURL, "v1")
}

// GenerateSpecForAPIVersion creates an OpenAPI specification using the
// configured API version prefix.
func GenerateSpecForAPIVersion(version, baseURL, apiVersion string) *Spec {
	apiBase := "/api/" + apiVersion
	spec := &Spec{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:       "API Toolkit",
			Description: "Universal API toolkit providing utility services (text, crypto, datetime, network)",
			Version:     version,
			Contact: Contact{
				Name: "API Manager",
				URL:  "https://github.com/apimgr/api",
			},
			License: License{
				Name: "MIT",
				URL:  "https://opensource.org/licenses/MIT",
			},
		},
		Servers: []Server{
			{
				URL:         baseURL,
				Description: "API Server",
			},
		},
		Paths: generatePaths(apiBase),
		Components: Components{
			Schemas: generateSchemas(),
		},
	}
	for path, item := range spec.Paths {
		setMountedWhen(&item, "always")
		spec.Paths[path] = item
	}
	return spec
}

// generatePaths creates the path definitions
func generatePaths(apiBase string) map[string]PathItem {
	paths := make(map[string]PathItem)

	// Health endpoints are API routes under the versioned server prefix and
	// the unversioned API alias. The optional root /healthz frontend alias is
	// registered only when configured and is not an API route.
	healthOperation := Operation{
		Summary:     "Health check",
		Description: "Returns the health status of the API",
		Tags:        []string{"Health"},
		OperationID: "healthCheck",
		Responses: map[string]Response{
			"200": {
				Description: "Service is healthy",
				Content: map[string]MediaType{
					"application/json": {
						Schema: Schema{
							Type: "object",
							Properties: map[string]Schema{
								"status": {Type: "string", Example: "ok"},
								"uptime": {Type: "number", Example: 3600},
							},
						},
					},
				},
			},
		},
	}
	paths[apiBase+"/server/healthz"] = PathItem{Get: &healthOperation}
	apiHealthOperation := healthOperation
	paths["/api/healthz"] = PathItem{Get: &apiHealthOperation}

	// Text utilities endpoints
	addTextEndpoints(paths, apiBase)

	// Crypto utilities endpoints
	addCryptoEndpoints(paths, apiBase)

	// DateTime utilities endpoints
	addDateTimeEndpoints(paths, apiBase)

	// Network utilities endpoints
	addNetworkEndpoints(paths, apiBase)

	return paths
}

// addTextEndpoints adds text utility endpoints to paths
func addTextEndpoints(paths map[string]PathItem, apiBase string) {
	// UUID endpoints
	paths[apiBase+"/text/uuid"] = PathItem{
		Get: &Operation{
			Summary:     "Generate UUID",
			Tags:        []string{"Text"},
			OperationID: "generateUUID",
			Responses: map[string]Response{
				"200": {Description: "UUID generated"},
			},
		},
	}

	// Hash endpoints
	paths[apiBase+"/text/hash/{algorithm}/{input}"] = PathItem{
		Get: &Operation{
			Summary:     "Hash text",
			Tags:        []string{"Text"},
			OperationID: "hashText",
			Parameters: []Parameter{
				{Name: "algorithm", In: "path", Required: true, Schema: Schema{Type: "string"}},
				{Name: "input", In: "path", Required: true, Schema: Schema{Type: "string"}},
			},
			Responses: map[string]Response{
				"200": {Description: "Hash generated"},
			},
		},
	}

	// Encode/Decode endpoints
	paths[apiBase+"/text/encode/{encoding}/{input}"] = PathItem{
		Get: &Operation{
			Summary:     "Encode text",
			Tags:        []string{"Text"},
			OperationID: "encodeText",
			Parameters: []Parameter{
				{Name: "encoding", In: "path", Required: true, Schema: Schema{Type: "string"}},
				{Name: "input", In: "path", Required: true, Schema: Schema{Type: "string"}},
			},
			Responses: map[string]Response{
				"200": {Description: "Text encoded"},
			},
		},
	}

	paths[apiBase+"/text/decode/{encoding}/{input}"] = PathItem{
		Get: &Operation{
			Summary:     "Decode text",
			Tags:        []string{"Text"},
			OperationID: "decodeText",
			Parameters: []Parameter{
				{Name: "encoding", In: "path", Required: true, Schema: Schema{Type: "string"}},
				{Name: "input", In: "path", Required: true, Schema: Schema{Type: "string"}},
			},
			Responses: map[string]Response{
				"200": {Description: "Text decoded"},
			},
		},
	}
}

// addCryptoEndpoints adds crypto utility endpoints to paths
func addCryptoEndpoints(paths map[string]PathItem, apiBase string) {
	// Bcrypt endpoints
	paths[apiBase+"/crypto/bcrypt/{password}"] = PathItem{
		Get: &Operation{
			Summary:     "Generate bcrypt hash",
			Tags:        []string{"Crypto"},
			OperationID: "bcryptHash",
			Parameters:  []Parameter{{Name: "password", In: "path", Required: true, Schema: Schema{Type: "string"}}},
			Responses:   map[string]Response{"200": {Description: "Bcrypt hash generated"}},
		},
	}

	// Password generation
	paths[apiBase+"/crypto/password"] = PathItem{
		Get: &Operation{
			Summary:     "Generate secure password",
			Tags:        []string{"Crypto"},
			OperationID: "generatePassword",
			Responses:   map[string]Response{"200": {Description: "Password generated"}},
		},
	}

	// TOTP endpoints
	paths[apiBase+"/crypto/totp/secret"] = PathItem{
		Get: &Operation{
			Summary:     "Generate TOTP secret",
			Tags:        []string{"Crypto"},
			OperationID: "generateTOTPSecret",
			Responses:   map[string]Response{"200": {Description: "TOTP secret generated"}},
		},
	}
}

// addDateTimeEndpoints adds datetime utility endpoints to paths
func addDateTimeEndpoints(paths map[string]PathItem, apiBase string) {
	// Current time
	paths[apiBase+"/datetime/now"] = PathItem{
		Get: &Operation{
			Summary:     "Get current timestamp",
			Tags:        []string{"DateTime"},
			OperationID: "getCurrentTime",
			Responses:   map[string]Response{"200": {Description: "Current timestamp"}},
		},
	}

	// Timestamp conversion
	paths[apiBase+"/datetime/convert/{timestamp}"] = PathItem{
		Get: &Operation{
			Summary:     "Convert timestamp",
			Tags:        []string{"DateTime"},
			OperationID: "convertTimestamp",
			Parameters:  []Parameter{{Name: "timestamp", In: "path", Required: true, Schema: Schema{Type: "string"}}},
			Responses:   map[string]Response{"200": {Description: "Timestamp converted"}},
		},
	}

	// Timezone list
	paths[apiBase+"/datetime/timezones"] = PathItem{
		Get: &Operation{
			Summary:     "List all timezones",
			Tags:        []string{"DateTime"},
			OperationID: "listTimezones",
			Responses:   map[string]Response{"200": {Description: "Timezone list"}},
		},
	}
}

// addNetworkEndpoints adds network utility endpoints to paths
func addNetworkEndpoints(paths map[string]PathItem, apiBase string) {
	// Client IP
	paths[apiBase+"/network/ip"] = PathItem{
		Get: &Operation{
			Summary:     "Get client IP address",
			Tags:        []string{"Network"},
			OperationID: "getClientIP",
			Responses:   map[string]Response{"200": {Description: "Client IP address"}},
		},
	}

	// Headers
	paths[apiBase+"/network/headers"] = PathItem{
		Get: &Operation{
			Summary:     "Get request headers",
			Tags:        []string{"Network"},
			OperationID: "getHeaders",
			Responses:   map[string]Response{"200": {Description: "Request headers"}},
		},
	}
}

// generateSchemas creates reusable schema definitions
func generateSchemas() map[string]Schema {
	return map[string]Schema{
		"Error": {
			Type: "object",
			Properties: map[string]Schema{
				"error":      {Type: "string", Example: "Error message"},
				"status":     {Type: "integer", Example: 400},
				"request_id": {Type: "string", Example: "abc123"},
			},
		},
	}
}

// ServeSpec serves the OpenAPI specification as JSON
func ServeSpec(version, baseURL string) http.HandlerFunc {
	return ServeEmbeddedSuperset(version, baseURL, "v1", nil)
}

// ServeSpecWithRoutes serves a spec generated from the configured version and
// the routes registered by the server.
func ServeSpecWithRoutes(version, baseURL, apiVersion string, routes []Route) http.HandlerFunc {
	return ServeSpecWithRoutesAndCondition(version, baseURL, apiVersion, routes, func(string) bool { return true })
}

// ServeSpecWithRoutesAndCondition builds the embedded superset from the
// generated API schema and route inventory, then removes operations whose
// x-mounted-when condition is false for the current server run.
func ServeSpecWithRoutesAndCondition(version, baseURL, apiVersion string, routes []Route, enabled func(string) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if enabled == nil {
			enabled = func(string) bool { return true }
		}
		spec := GenerateSupersetSpec(version, baseURL, apiVersion, routes)
		filterSpec(spec, enabled)
		w.Header().Set("Content-Type", "application/json")
		if err := sanitize.WriteTaggedJSON(w, spec, true); err != nil {
			return
		}
	}
}

// ServeEmbeddedSuperset filters a fresh copy of the generated build-time
// superset for the current server run and writes JSON only.
func ServeEmbeddedSuperset(version, baseURL, apiVersion string, enabled func(string) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if enabled == nil {
			enabled = func(string) bool { return true }
		}
		spec, err := EmbeddedSuperset(version, baseURL, apiVersion)
		if err != nil {
			http.Error(w, "OpenAPI specification unavailable", http.StatusInternalServerError)
			return
		}
		filterSpec(spec, enabled)
		w.Header().Set("Content-Type", "application/json")
		if err := sanitize.WriteTaggedJSON(w, spec, true); err != nil {
			return
		}
	}
}

// GenerateSupersetSpec combines the static generated schema and every route
// supplied by the server's route inventory. It preserves x-mounted-when on
// each operation so the served document can be filtered per run.
func GenerateSupersetSpec(version, baseURL, apiVersion string, routes []Route) *Spec {
	spec := &Spec{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:       "API Toolkit",
			Description: "API operations registered by the server router",
			Version:     version,
			Contact:     Contact{Name: "API Manager", URL: "https://github.com/apimgr/api"},
			License:     License{Name: "MIT", URL: "https://opensource.org/licenses/MIT"},
		},
		Servers:    []Server{{URL: baseURL, Description: "API Server"}},
		Paths:      make(map[string]PathItem),
		Components: Components{Schemas: generateSchemas()},
	}
	for _, route := range routes {
		if route.Internal || (!strings.HasPrefix(route.Path, "/api/") && route.Path != "/healthz" && route.Path != "/mcp") {
			continue
		}
		path := strings.ReplaceAll(route.Path, "*", "{path}")
		item := spec.Paths[path]
		operation := &Operation{
			Summary:     strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(path, "/api/"), "/", " ")),
			OperationID: strings.ToLower(route.Method + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(path)),
			Primary:     route.Primary,
			TextMessage: route.TextMessage,
			MountedWhen: mountedWhen(route.MountedWhen),
			Tags:        []string{"API"},
			Responses:   map[string]Response{"200": {Description: "Operation completed"}},
		}
		var registered *Operation
		switch strings.ToUpper(route.Method) {
		case http.MethodGet:
			if item.Get == nil {
				item.Get = operation
			}
			registered = item.Get
		case http.MethodHead:
			if item.Head == nil {
				item.Head = operation
			}
			registered = item.Head
		case http.MethodPost:
			if item.Post == nil {
				item.Post = operation
			}
			registered = item.Post
		case http.MethodPut:
			if item.Put == nil {
				item.Put = operation
			}
			registered = item.Put
		case http.MethodPatch:
			if item.Patch == nil {
				item.Patch = operation
			}
			registered = item.Patch
		case http.MethodDelete:
			if item.Delete == nil {
				item.Delete = operation
			}
			registered = item.Delete
		case http.MethodOptions:
			if item.Options == nil {
				item.Options = operation
			}
			registered = item.Options
		default:
			continue
		}
		if registered != nil {
			registered.Primary = route.Primary
			registered.TextMessage = route.TextMessage
			registered.MountedWhen = mountedWhen(route.MountedWhen)
		}
		spec.Paths[path] = item
	}
	return spec
}

func mountedWhen(condition string) string {
	if strings.TrimSpace(condition) == "" {
		return "always"
	}
	return condition
}

func setMountedWhen(item *PathItem, condition string) {
	for _, operation := range []*Operation{item.Get, item.Head, item.Post, item.Put, item.Patch, item.Delete, item.Options} {
		if operation != nil && operation.MountedWhen == "" {
			operation.MountedWhen = condition
		}
	}
}

func filterSpec(spec *Spec, enabled func(string) bool) {
	for path, item := range spec.Paths {
		changed := false
		changed = filterOperation(&item.Get, enabled) || changed
		changed = filterOperation(&item.Head, enabled) || changed
		changed = filterOperation(&item.Post, enabled) || changed
		changed = filterOperation(&item.Put, enabled) || changed
		changed = filterOperation(&item.Patch, enabled) || changed
		changed = filterOperation(&item.Delete, enabled) || changed
		changed = filterOperation(&item.Options, enabled) || changed
		if changed {
			spec.Paths[path] = item
		}
		if item.Get == nil && item.Head == nil && item.Post == nil && item.Put == nil && item.Patch == nil && item.Delete == nil && item.Options == nil {
			delete(spec.Paths, path)
		}
	}
}

func filterOperation(operation **Operation, enabled func(string) bool) bool {
	if *operation == nil {
		return false
	}
	if enabled((*operation).MountedWhen) {
		return true
	}
	*operation = nil
	return true
}
