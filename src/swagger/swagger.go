package swagger

import (
	"encoding/json"
	"net/http"
)

// Spec represents the OpenAPI specification structure
type Spec struct {
	OpenAPI string                 `json:"openapi"`
	Info    Info                   `json:"info"`
	Servers []Server               `json:"servers"`
	Paths   map[string]PathItem    `json:"paths"`
	Components Components           `json:"components,omitempty"`
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
	Tags        []string            `json:"tags,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
}

// Parameter represents an operation parameter
type Parameter struct {
	Name        string      `json:"name"`
	In          string      `json:"in"` // query, header, path, cookie
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Schema      Schema      `json:"schema,omitempty"`
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

// GenerateSpec creates the OpenAPI specification for the API
func GenerateSpec(version, baseURL string) *Spec {
	return &Spec{
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
		Paths: generatePaths(),
		Components: Components{
			Schemas: generateSchemas(),
		},
	}
}

// generatePaths creates the path definitions
func generatePaths() map[string]PathItem {
	paths := make(map[string]PathItem)

	// Health endpoint
	paths["/healthz"] = PathItem{
		Get: &Operation{
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
		},
	}

	// Version endpoint
	paths["/api/v1/version"] = PathItem{
		Get: &Operation{
			Summary:     "Get version information",
			Description: "Returns API version details",
			Tags:        []string{"System"},
			OperationID: "getVersion",
			Responses: map[string]Response{
				"200": {
					Description: "Version information",
					Content: map[string]MediaType{
						"application/json": {
							Schema: Schema{
								Type: "object",
								Properties: map[string]Schema{
									"version":    {Type: "string", Example: "1.0.0"},
									"commit_id":  {Type: "string", Example: "abc123"},
									"build_date": {Type: "string", Example: "2025-01-01T00:00:00Z"},
								},
							},
						},
					},
				},
			},
		},
	}

	// Text utilities endpoints
	addTextEndpoints(paths)

	// Crypto utilities endpoints
	addCryptoEndpoints(paths)

	// DateTime utilities endpoints
	addDateTimeEndpoints(paths)

	// Network utilities endpoints
	addNetworkEndpoints(paths)

	return paths
}

// addTextEndpoints adds text utility endpoints to paths
func addTextEndpoints(paths map[string]PathItem) {
	// UUID endpoints
	paths["/api/v1/text/uuid"] = PathItem{
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
	paths["/api/v1/text/hash/{algorithm}/{input}"] = PathItem{
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
	paths["/api/v1/text/encode/{encoding}/{input}"] = PathItem{
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

	paths["/api/v1/text/decode/{encoding}/{input}"] = PathItem{
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
func addCryptoEndpoints(paths map[string]PathItem) {
	// Bcrypt endpoints
	paths["/api/v1/crypto/bcrypt/{password}"] = PathItem{
		Get: &Operation{
			Summary:     "Generate bcrypt hash",
			Tags:        []string{"Crypto"},
			OperationID: "bcryptHash",
			Parameters:  []Parameter{{Name: "password", In: "path", Required: true, Schema: Schema{Type: "string"}}},
			Responses:   map[string]Response{"200": {Description: "Bcrypt hash generated"}},
		},
	}

	// Password generation
	paths["/api/v1/crypto/password"] = PathItem{
		Get: &Operation{
			Summary:     "Generate secure password",
			Tags:        []string{"Crypto"},
			OperationID: "generatePassword",
			Responses:   map[string]Response{"200": {Description: "Password generated"}},
		},
	}

	// TOTP endpoints
	paths["/api/v1/crypto/totp/secret"] = PathItem{
		Get: &Operation{
			Summary:     "Generate TOTP secret",
			Tags:        []string{"Crypto"},
			OperationID: "generateTOTPSecret",
			Responses:   map[string]Response{"200": {Description: "TOTP secret generated"}},
		},
	}
}

// addDateTimeEndpoints adds datetime utility endpoints to paths
func addDateTimeEndpoints(paths map[string]PathItem) {
	// Current time
	paths["/api/v1/datetime/now"] = PathItem{
		Get: &Operation{
			Summary:     "Get current timestamp",
			Tags:        []string{"DateTime"},
			OperationID: "getCurrentTime",
			Responses:   map[string]Response{"200": {Description: "Current timestamp"}},
		},
	}

	// Timestamp conversion
	paths["/api/v1/datetime/convert/{timestamp}"] = PathItem{
		Get: &Operation{
			Summary:     "Convert timestamp",
			Tags:        []string{"DateTime"},
			OperationID: "convertTimestamp",
			Parameters:  []Parameter{{Name: "timestamp", In: "path", Required: true, Schema: Schema{Type: "string"}}},
			Responses:   map[string]Response{"200": {Description: "Timestamp converted"}},
		},
	}

	// Timezone list
	paths["/api/v1/datetime/timezones"] = PathItem{
		Get: &Operation{
			Summary:     "List all timezones",
			Tags:        []string{"DateTime"},
			OperationID: "listTimezones",
			Responses:   map[string]Response{"200": {Description: "Timezone list"}},
		},
	}
}

// addNetworkEndpoints adds network utility endpoints to paths
func addNetworkEndpoints(paths map[string]PathItem) {
	// Client IP
	paths["/api/v1/network/ip"] = PathItem{
		Get: &Operation{
			Summary:     "Get client IP address",
			Tags:        []string{"Network"},
			OperationID: "getClientIP",
			Responses:   map[string]Response{"200": {Description: "Client IP address"}},
		},
	}

	// Headers
	paths["/api/v1/network/headers"] = PathItem{
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
				"error":   {Type: "string", Example: "Error message"},
				"status":  {Type: "integer", Example: 400},
				"request_id": {Type: "string", Example: "abc123"},
			},
		},
	}
}

// ServeSpec serves the OpenAPI specification as JSON
func ServeSpec(version, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		spec := GenerateSpec(version, baseURL)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(spec)
	}
}
