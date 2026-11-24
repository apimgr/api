package graphql

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/server/handler"
	"github.com/apimgr/api/src/service/text"
)

// Schema represents a basic GraphQL schema
type Schema struct {
	Query *ObjectType
}

// ObjectType represents a GraphQL object type
type ObjectType struct {
	Name   string
	Fields map[string]*Field
}

// Field represents a GraphQL field
type Field struct {
	Type        string
	Description string
	Args        map[string]*Argument
	Resolve     ResolveFunc
}

// Argument represents a field argument
type Argument struct {
	Type        string
	Description string
}

// ResolveFunc is a function that resolves a field value
type ResolveFunc func(args map[string]interface{}) (interface{}, error)

// Request represents a GraphQL request
type Request struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName,omitempty"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
}

// Response represents a GraphQL response
type Response struct {
	Data   interface{} `json:"data"`
	Errors []Error     `json:"errors,omitempty"`
}

// Error represents a GraphQL error
type Error struct {
	Message    string            `json:"message"`
	Path       []string          `json:"path,omitempty"`
	Extensions map[string]string `json:"extensions"`
}

// BuildSchema creates the GraphQL schema for the API
func BuildSchema() *Schema {
	return &Schema{
		Query: &ObjectType{
			Name: "Query",
			Fields: map[string]*Field{
				"health": {
					Type:        "Health",
					Description: "Health check",
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						return map[string]interface{}{
							"status": handler.Status(),
							"uptime": handler.UptimeSeconds(),
						}, nil
					},
				},
				"version": {
					Type:        "Version",
					Description: "Version information",
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						return map[string]interface{}{
							"version":    handler.Version,
							"commit_id":  handler.CommitID,
							"build_date": handler.BuildDate,
						}, nil
					},
				},
				"textUppercase": {
					Type:        "String",
					Description: "Convert text to uppercase",
					Args: map[string]*Argument{
						"text": {Type: "String!", Description: "Text to convert"},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						input, ok := args["text"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"text\" is required")
						}
						return text.ToUpper(input), nil
					},
				},
				"generateUUID": {
					Type:        "String",
					Description: "Generate UUID",
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						return text.UUID(4)
					},
				},
			},
		},
	}
}

// GenerateSchemaSDL generates the GraphQL SDL (Schema Definition Language)
func GenerateSchemaSDL() string {
	return `
type Query {
	# Health check
	health: Health!

	# Version information
	version: Version!

	# Text utilities
	textUppercase(text: String!): String!
	generateUUID: String!

	# Crypto utilities (implement as needed)

	# DateTime utilities (implement as needed)

	# Network utilities (implement as needed)
}


type Health {
	status: String!
	uptime: Int!
}

type Version {
	version: String!
	commit_id: String!
	build_date: String!
}

`
}

// HandleQuery handles read-only GraphQL queries over GET or POST.
func HandleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeTransportError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	var req Request
	if r.Method == http.MethodGet {
		req.Query = r.URL.Query().Get("query")
		req.OperationName = r.URL.Query().Get("operationName")
		if rawVariables := r.URL.Query().Get("variables"); rawVariables != "" {
			if err := json.Unmarshal([]byte(rawVariables), &req.Variables); err != nil {
				writeTransportError(w, http.StatusBadRequest, "INVALID_JSON", i18n.Translate(i18n.LangFromRequest(r), "graphql.invalid_variables"))
				return
			}
		}
		if req.Query == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(i18n.Translate(i18n.LangFromRequest(r), "graphql.endpoint_usage") + "\n"))
			return
		}
	} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTransportError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	resp := executeQuery(req.Query, req.Variables, req.OperationName, i18n.LangFromRequest(r))

	body, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		slog.Error("graphql: failed to marshal response", "error", err)
		writeTransportError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	body = append(body, '\n')

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		slog.Error("graphql: failed to write response", "error", err)
	}
}

func writeTransportError(w http.ResponseWriter, status int, code, message string) {
	body, _ := json.MarshalIndent(map[string]interface{}{
		"ok":      false,
		"error":   code,
		"message": message,
		"details": map[string]interface{}{},
	}, "", "  ")
	body = append(body, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// executeQuery parses and resolves a GraphQL query against the
// schema built by BuildSchema(), returning real resolver output rather than
// pattern-matched placeholders.
func executeQuery(query string, variables map[string]interface{}, operationName, language string) Response {
	op, err := newDocParser(query).parseDocument()
	if err != nil {
		return Response{Errors: []Error{{Message: err.Error(), Extensions: map[string]string{"code": "GRAPHQL_PARSE_FAILED"}}}}
	}

	if operationName != "" && operationName != op.Name {
		return Response{Errors: []Error{{Message: i18n.TranslateFormat(language, "graphql.unknown_operation", map[string]string{"operation_name": operationName}), Extensions: map[string]string{"code": "GRAPHQL_VALIDATION_FAILED"}}}}
	}
	if op.Type != "query" {
		return Response{Errors: []Error{{Message: i18n.Translate(language, "graphql.queries_only"), Extensions: map[string]string{"code": "GRAPHQL_VALIDATION_FAILED"}}}}
	}

	root := BuildSchema().Query

	if variables == nil {
		variables = map[string]interface{}{}
	}

	data := map[string]interface{}{}
	var errs []Error

	for _, sel := range op.Selections {
		field, ok := root.Fields[sel.Name]
		if !ok {
			errs = append(errs, Error{
				Message:    fmt.Sprintf("Cannot query field %q on type %q.", sel.Name, root.Name),
				Path:       []string{sel.Name},
				Extensions: map[string]string{"code": "GRAPHQL_VALIDATION_FAILED"},
			})
			continue
		}

		args := map[string]interface{}{}
		for name, val := range sel.Arguments {
			args[name] = val.resolve(variables)
		}

		result, err := field.Resolve(args)
		if err != nil {
			errs = append(errs, Error{Message: err.Error(), Path: []string{sel.Name}, Extensions: map[string]string{"code": "INTERNAL_SERVER_ERROR"}})
			continue
		}

		key := sel.Name
		if sel.Alias != "" {
			key = sel.Alias
		}
		data[key] = applySelection(result, sel.Selections)
	}

	resp := Response{Errors: errs}
	if len(data) > 0 {
		resp.Data = data
	}
	return resp
}

// applySelection filters a resolver's result down to only the requested
// GraphQL sub-fields, mirroring the query shape. Scalars and results with no
// requested sub-selection are returned as-is.
func applySelection(result interface{}, sels []selection) interface{} {
	if len(sels) == 0 {
		return result
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		return result
	}

	filtered := map[string]interface{}{}
	for _, sel := range sels {
		value, present := m[sel.Name]
		if !present {
			continue
		}
		key := sel.Name
		if sel.Alias != "" {
			key = sel.Alias
		}
		filtered[key] = applySelection(value, sel.Selections)
	}
	return filtered
}

// ServeSchema serves the GraphQL schema (introspection)
func ServeSchema(w http.ResponseWriter, r *http.Request) {
	schema := GenerateSchemaSDL()
	w.Header().Set("Content-Type", "text/plain")
	if _, err := w.Write([]byte(schema)); err != nil {
		slog.Error("graphql: failed to write schema", "error", err)
	}
}
