package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/service/text"
	"github.com/go-chi/chi/v5"
)

// Helper functions for JSON responses
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	writeJSONValue(w, data)
}

func jsonError(w http.ResponseWriter, r *http.Request, messageKey string, status int) {
	w.Header().Set("Content-Type", "application/json")
	errorCode := "BAD_REQUEST"
	if status >= http.StatusInternalServerError {
		errorCode = "INTERNAL_ERROR"
	}
	body, err := sanitize.MarshalTaggedResponse(sanitize.TaggedObject{
		{Name: "ok", Value: false},
		{Name: "error", Value: errorCode},
		{Name: "message", Value: i18n.Translate(i18n.LangFromRequest(r), messageKey)},
	}, mode.IsAppModeProd())
	if err != nil {
		slog.Error("failed to encode JSON error response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	body, err = sanitize.FormatResponseJSON(body)
	if err != nil {
		slog.Error("failed to format JSON error response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write JSON error response", "error", err)
	}
}

func responseObject(fields ...sanitize.TaggedField) sanitize.TaggedObject {
	return sanitize.TaggedObject(fields)
}

func responseField(name string, value interface{}) sanitize.TaggedField {
	return sanitize.TaggedField{Name: name, Value: value}
}

func writeJSONValue(w http.ResponseWriter, value interface{}) {
	body, err := sanitize.MarshalTaggedResponse(value, mode.IsAppModeProd())
	if err != nil {
		slog.Error("failed to encode JSON response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	body, err = sanitize.FormatResponseJSON(body)
	if err != nil {
		slog.Error("failed to format JSON response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}

// UUID generates a UUID
// @Summary Generate UUID
// @Tags Text
// @Produce json
// @Param version query int false "UUID version (1, 4, 7)" default(4)
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/uuid [get]
func TextUUIDHandler(w http.ResponseWriter, r *http.Request) {
	version, _ := strconv.Atoi(r.URL.Query().Get("version"))
	if version == 0 {
		version = 4
	}

	result, err := text.UUID(version)
	if err != nil {
		jsonError(w, r, "errors.server_error", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, responseObject(responseField("uuid", result), responseField("version", version)))
}

// UUIDs generates multiple UUIDs
// @Summary Generate multiple UUIDs
// @Tags Text
// @Produce json
// @Param version query int false "UUID version (1, 4, 7)" default(4)
// @Param count query int false "Number of UUIDs" default(10)
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/uuids [get]
func TextUUIDsHandler(w http.ResponseWriter, r *http.Request) {
	version, _ := strconv.Atoi(r.URL.Query().Get("version"))
	if version == 0 {
		version = 4
	}
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count == 0 {
		count = 10
	}

	result, err := text.UUIDs(version, count)
	if err != nil {
		jsonError(w, r, "errors.server_error", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, responseObject(responseField("uuids", result), responseField("count", len(result)), responseField("version", version)))
}

// Hash computes a hash
// @Summary Compute hash
// @Tags Text
// @Produce json
// @Param algorithm query string true "Hash algorithm (md5, sha1, sha256, sha384, sha512)"
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/hash [get]
func TextHashHandler(w http.ResponseWriter, r *http.Request) {
	algorithm := r.URL.Query().Get("algorithm")
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	if algorithm == "" {
		algorithm = "sha256"
	}

	result, err := text.Hash(algorithm, input)
	if err != nil {
		jsonError(w, r, "errors.bad_request", http.StatusBadRequest)
		return
	}

	jsonResponse(w, responseObject(responseField("hash", result), responseField("algorithm", algorithm), responseField("input", input)))
}

// HashAll computes all common hashes
// @Summary Compute all hashes
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/hash/all [get]
func TextHashAllHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result := text.HashAll(input)
	jsonResponse(w, responseObject(responseField("md5", result["md5"]), responseField("sha1", result["sha1"]), responseField("sha256", result["sha256"]), responseField("sha384", result["sha384"]), responseField("sha512", result["sha512"]), responseField("input", input)))
}

// Base64 encode
// @Summary Base64 encode
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/base64/encode [get]
func TextBase64EncodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result := text.Base64Encode(input)

	jsonResponse(w, responseObject(responseField("encoded", result), responseField("input", input)))
}

// Base64 decode
// @Summary Base64 decode
// @Tags Text
// @Produce json
// @Param input query string true "Base64 encoded text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/base64/decode [get]
func TextBase64DecodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result, err := text.Base64Decode(input)
	if err != nil {
		jsonError(w, r, "errors.bad_request", http.StatusBadRequest)
		return
	}

	jsonResponse(w, responseObject(responseField("decoded", result), responseField("input", input)))
}

// Base64URL encode
// @Summary Base64URL encode
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/base64url/encode [get]
func TextBase64URLEncodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result := text.Base64URLEncode(input)

	jsonResponse(w, responseObject(responseField("encoded", result), responseField("input", input)))
}

// Base64URL decode
// @Summary Base64URL decode
// @Tags Text
// @Produce json
// @Param input query string true "Base64URL encoded text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/base64url/decode [get]
func TextBase64URLDecodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result, err := text.Base64URLDecode(input)
	if err != nil {
		jsonError(w, r, "errors.bad_request", http.StatusBadRequest)
		return
	}

	jsonResponse(w, responseObject(responseField("decoded", result), responseField("input", input)))
}

// Base32 encode
// @Summary Base32 encode
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/base32/encode [get]
func TextBase32EncodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result := text.Base32Encode(input)

	jsonResponse(w, responseObject(responseField("encoded", result), responseField("input", input)))
}

// Base32 decode
// @Summary Base32 decode
// @Tags Text
// @Produce json
// @Param input query string true "Base32 encoded text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/base32/decode [get]
func TextBase32DecodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result, err := text.Base32Decode(input)
	if err != nil {
		jsonError(w, r, "errors.bad_request", http.StatusBadRequest)
		return
	}

	jsonResponse(w, responseObject(responseField("decoded", result), responseField("input", input)))
}

// Hex encode
// @Summary Hex encode
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/hex/encode [get]
func TextHexEncodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result := text.HexEncode(input)

	jsonResponse(w, responseObject(responseField("encoded", result), responseField("input", input)))
}

// Hex decode
// @Summary Hex decode
// @Tags Text
// @Produce json
// @Param input query string true "Hex encoded text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/hex/decode [get]
func TextHexDecodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result, err := text.HexDecode(input)
	if err != nil {
		jsonError(w, r, "errors.bad_request", http.StatusBadRequest)
		return
	}

	jsonResponse(w, responseObject(responseField("decoded", result), responseField("input", input)))
}

// URL encode
// @Summary URL encode
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/url/encode [get]
func TextURLEncodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result := text.URLEncode(input)

	jsonResponse(w, responseObject(responseField("encoded", result), responseField("input", input)))
}

// URL decode
// @Summary URL decode
// @Tags Text
// @Produce json
// @Param input query string true "URL encoded text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/url/decode [get]
func TextURLDecodeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}

	result, err := text.URLDecode(input)
	if err != nil {
		jsonError(w, r, "errors.bad_request", http.StatusBadRequest)
		return
	}

	jsonResponse(w, responseObject(responseField("decoded", result), responseField("input", input)))
}

// Case conversion handlers
// @Summary Convert to lowercase
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/case/lower [get]
func TextCaseLowerHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	jsonResponse(w, responseObject(responseField("result", text.ToLower(input)), responseField("input", input)))
}

// @Summary Convert to uppercase
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/case/upper [get]
func TextCaseUpperHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	jsonResponse(w, responseObject(responseField("result", text.ToUpper(input)), responseField("input", input)))
}

// @Summary Convert to title case
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/case/title [get]
func TextCaseTitleHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	jsonResponse(w, responseObject(responseField("result", text.ToTitle(input)), responseField("input", input)))
}

// @Summary Convert to camelCase
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/case/camel [get]
func TextCaseCamelHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	jsonResponse(w, responseObject(responseField("result", text.ToCamelCase(input)), responseField("input", input)))
}

// @Summary Convert to snake_case
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/case/snake [get]
func TextCaseSnakeHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	jsonResponse(w, responseObject(responseField("result", text.ToSnakeCase(input)), responseField("input", input)))
}

// @Summary Convert to kebab-case
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/case/kebab [get]
func TextCaseKebabHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	jsonResponse(w, responseObject(responseField("result", text.ToKebabCase(input)), responseField("input", input)))
}

// @Summary Reverse text
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/reverse [get]
func TextReverseHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	jsonResponse(w, responseObject(responseField("result", text.Reverse(input)), responseField("input", input)))
}

// @Summary Text statistics
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/stats [get]
func TextStatsHandler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	stats := text.Stats(input)
	jsonResponse(w, responseObject(responseField("characters", stats["characters"]), responseField("characters_no_space", stats["characters_no_space"]), responseField("words", stats["words"]), responseField("lines", stats["lines"]), responseField("bytes", stats["bytes"])))
}

// @Summary ROT13 encode/decode
// @Tags Text
// @Produce json
// @Param input query string true "Input text"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/rot13 [get]
func TextROT13Handler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		jsonError(w, r, "errors.required", http.StatusBadRequest)
		return
	}
	jsonResponse(w, responseObject(responseField("result", text.ROT13(input)), responseField("input", input)))
}

// Lorem handlers
// @Summary Generate lorem words
// @Tags Text
// @Produce json
// @Param count query int false "Number of words" default(10)
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/lorem/words [get]
func TextLoremWordsHandler(w http.ResponseWriter, r *http.Request) {
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count == 0 {
		count = 10
	}
	result := text.LoremWords(count)
	jsonResponse(w, responseObject(responseField("words", result), responseField("count", len(result))))
}

// @Summary Generate lorem sentences
// @Tags Text
// @Produce json
// @Param count query int false "Number of sentences" default(5)
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/lorem/sentences [get]
func TextLoremSentencesHandler(w http.ResponseWriter, r *http.Request) {
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count == 0 {
		count = 5
	}
	result := text.LoremSentences(count)
	jsonResponse(w, responseObject(responseField("sentences", result), responseField("count", len(result))))
}

// @Summary Generate lorem paragraphs
// @Tags Text
// @Produce json
// @Param count query int false "Number of paragraphs" default(3)
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/lorem/paragraphs [get]
func TextLoremParagraphsHandler(w http.ResponseWriter, r *http.Request) {
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count == 0 {
		count = 3
	}
	result := text.LoremParagraphs(count)
	jsonResponse(w, responseObject(responseField("paragraphs", result), responseField("count", len(result))))
}

// ID generators
// @Summary Generate ULID
// @Tags Text
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/ulid [get]
func TextULIDHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, responseObject(responseField("ulid", text.ULID())))
}

// @Summary Generate NanoID
// @Tags Text
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/nanoid [get]
func TextNanoIDHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, responseObject(responseField("nanoid", text.NanoID())))
}

// @Summary Generate KSUID
// @Tags Text
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/text/ksuid [get]
func TextKSUIDHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, responseObject(responseField("ksuid", text.KSUID())))
}

// RegisterTextRoutes registers all text service routes
func RegisterTextRoutes(r chi.Router) {
	r.Get("/api/v1/text/uuid", TextUUIDHandler)
	r.Get("/api/v1/text/uuids", TextUUIDsHandler)
	r.Get("/api/v1/text/hash", TextHashHandler)
	r.Get("/api/v1/text/hash/all", TextHashAllHandler)

	// Encoding
	r.Get("/api/v1/text/base64/encode", TextBase64EncodeHandler)
	r.Get("/api/v1/text/base64/decode", TextBase64DecodeHandler)
	r.Get("/api/v1/text/base64url/encode", TextBase64URLEncodeHandler)
	r.Get("/api/v1/text/base64url/decode", TextBase64URLDecodeHandler)
	r.Get("/api/v1/text/base32/encode", TextBase32EncodeHandler)
	r.Get("/api/v1/text/base32/decode", TextBase32DecodeHandler)
	r.Get("/api/v1/text/hex/encode", TextHexEncodeHandler)
	r.Get("/api/v1/text/hex/decode", TextHexDecodeHandler)
	r.Get("/api/v1/text/url/encode", TextURLEncodeHandler)
	r.Get("/api/v1/text/url/decode", TextURLDecodeHandler)

	// Case
	r.Get("/api/v1/text/case/lower", TextCaseLowerHandler)
	r.Get("/api/v1/text/case/upper", TextCaseUpperHandler)
	r.Get("/api/v1/text/case/title", TextCaseTitleHandler)
	r.Get("/api/v1/text/case/camel", TextCaseCamelHandler)
	r.Get("/api/v1/text/case/snake", TextCaseSnakeHandler)
	r.Get("/api/v1/text/case/kebab", TextCaseKebabHandler)

	// Utilities
	r.Get("/api/v1/text/reverse", TextReverseHandler)
	r.Get("/api/v1/text/stats", TextStatsHandler)
	r.Get("/api/v1/text/rot13", TextROT13Handler)

	// Lorem
	r.Get("/api/v1/text/lorem/words", TextLoremWordsHandler)
	r.Get("/api/v1/text/lorem/sentences", TextLoremSentencesHandler)
	r.Get("/api/v1/text/lorem/paragraphs", TextLoremParagraphsHandler)

	// IDs
	r.Get("/api/v1/text/ulid", TextULIDHandler)
	r.Get("/api/v1/text/nanoid", TextNanoIDHandler)
	r.Get("/api/v1/text/ksuid", TextKSUIDHandler)
}
