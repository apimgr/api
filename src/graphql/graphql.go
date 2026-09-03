package graphql

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/apimgr/api/src/server/handler"
	"github.com/apimgr/api/src/service/crypto"
	"github.com/apimgr/api/src/service/datetime"
	"github.com/apimgr/api/src/service/text"
)

// Schema represents a basic GraphQL schema
type Schema struct {
	Query    *ObjectType
	Mutation *ObjectType
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
	Data   interface{} `json:"data,omitempty"`
	Errors []Error     `json:"errors,omitempty"`
}

// Error represents a GraphQL error
type Error struct {
	Message string   `json:"message"`
	Path    []string `json:"path,omitempty"`
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
		Mutation: &ObjectType{
			Name: "Mutation",
			Fields: map[string]*Field{
				"textUppercase": {
					Type:        "TextResult",
					Description: "Convert text to uppercase",
					Args: map[string]*Argument{
						"text": {
							Type:        "String!",
							Description: "Text to convert",
						},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						input, ok := args["text"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"text\" is required")
						}
						return map[string]interface{}{
							"result": text.ToUpper(input),
						}, nil
					},
				},
				"textLowercase": {
					Type:        "TextResult",
					Description: "Convert text to lowercase",
					Args: map[string]*Argument{
						"text": {Type: "String!", Description: "Text to convert"},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						input, ok := args["text"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"text\" is required")
						}
						return map[string]interface{}{"result": text.ToLower(input)}, nil
					},
				},
				"bcryptHash": {
					Type:        "TextResult",
					Description: "Hash password with bcrypt",
					Args: map[string]*Argument{
						"password": {Type: "String!", Description: "Password to hash"},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						password, ok := args["password"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"password\" is required")
						}
						hash, err := crypto.BcryptHash(password, 12)
						if err != nil {
							return nil, err
						}
						return map[string]interface{}{"result": hash}, nil
					},
				},
				"textReverse": {
					Type:        "TextResult",
					Description: "Reverse text",
					Args: map[string]*Argument{
						"text": {Type: "String!", Description: "Text to reverse"},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						input, ok := args["text"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"text\" is required")
						}
						return map[string]interface{}{"result": text.Reverse(input)}, nil
					},
				},
				"textBase64Encode": {
					Type:        "TextResult",
					Description: "Base64-encode text",
					Args: map[string]*Argument{
						"text": {Type: "String!", Description: "Text to encode"},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						input, ok := args["text"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"text\" is required")
						}
						return map[string]interface{}{"result": text.Base64Encode(input)}, nil
					},
				},
				"textBase64Decode": {
					Type:        "TextResult",
					Description: "Base64-decode text",
					Args: map[string]*Argument{
						"text": {Type: "String!", Description: "Text to decode"},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						input, ok := args["text"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"text\" is required")
						}
						result, err := text.Base64Decode(input)
						if err != nil {
							return nil, err
						}
						return map[string]interface{}{"result": result}, nil
					},
				},
				"textSlug": {
					Type:        "TextResult",
					Description: "Slugify text",
					Args: map[string]*Argument{
						"text": {Type: "String!", Description: "Text to slugify"},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						input, ok := args["text"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"text\" is required")
						}
						return map[string]interface{}{"result": text.Slugify(input)}, nil
					},
				},
				"textHash": {
					Type:        "TextResult",
					Description: "Hash text with SHA-256",
					Args: map[string]*Argument{
						"text": {Type: "String!", Description: "Text to hash"},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						input, ok := args["text"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"text\" is required")
						}
						result, err := text.Hash("sha256", input)
						if err != nil {
							return nil, err
						}
						return map[string]interface{}{"result": result}, nil
					},
				},
				"convertTimezone": {
					Type:        "TextResult",
					Description: "Convert a Unix timestamp between timezones",
					Args: map[string]*Argument{
						"timestamp": {Type: "String!", Description: "Unix timestamp (seconds)"},
						"from":      {Type: "String!", Description: "Source IANA timezone"},
						"to":        {Type: "String!", Description: "Target IANA timezone"},
					},
					Resolve: func(args map[string]interface{}) (interface{}, error) {
						ts, ok := args["timestamp"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"timestamp\" is required")
						}
						from, ok := args["from"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"from\" is required")
						}
						to, ok := args["to"].(string)
						if !ok {
							return nil, fmt.Errorf("argument \"to\" is required")
						}
						unix, err := strconv.ParseInt(ts, 10, 64)
						if err != nil {
							return nil, fmt.Errorf("invalid timestamp %q", ts)
						}
						converted, err := datetime.ConvertTimezone(unix, from, to)
						if err != nil {
							return nil, err
						}
						return map[string]interface{}{"result": converted["to"]}, nil
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

type Mutation {
	# Text utilities
	textUppercase(text: String!): TextResult!
	textLowercase(text: String!): TextResult!
	textReverse(text: String!): TextResult!
	textBase64Encode(text: String!): TextResult!
	textBase64Decode(text: String!): TextResult!
	textSlug(text: String!): TextResult!
	textHash(text: String!): TextResult!

	# Crypto utilities
	bcryptHash(password: String!): TextResult!

	# DateTime utilities
	convertTimezone(timestamp: String!, from: String!, to: String!): TextResult!
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

type TextResult {
	result: String!
}

type CryptoResult {
	result: String!
}

type DateTimeResult {
	result: String!
}
`
}

// HandleQuery handles GraphQL queries
func HandleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	resp := executeQuery(req.Query, req.Variables)

	body, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		slog.Error("graphql: failed to marshal response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	body = append(body, '\n')

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		slog.Error("graphql: failed to write response", "error", err)
	}
}

// executeQuery parses and resolves a GraphQL query/mutation against the
// schema built by BuildSchema(), returning real resolver output rather than
// pattern-matched placeholders.
func executeQuery(query string, variables map[string]interface{}) Response {
	op, err := newDocParser(query).parseDocument()
	if err != nil {
		return Response{Errors: []Error{{Message: err.Error()}}}
	}

	schema := BuildSchema()
	root := schema.Query
	rootName := "Query"
	if op.Type == "mutation" {
		root = schema.Mutation
		rootName = "Mutation"
	}

	if variables == nil {
		variables = map[string]interface{}{}
	}

	data := map[string]interface{}{}
	var errs []Error

	for _, sel := range op.Selections {
		field, ok := root.Fields[sel.Name]
		if !ok {
			errs = append(errs, Error{
				Message: fmt.Sprintf("Cannot query field %q on type %q.", sel.Name, rootName),
				Path:    []string{sel.Name},
			})
			continue
		}

		args := map[string]interface{}{}
		for name, val := range sel.Arguments {
			args[name] = val.resolve(variables)
		}

		result, err := field.Resolve(args)
		if err != nil {
			errs = append(errs, Error{Message: err.Error(), Path: []string{sel.Name}})
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
