// Package sanitize implements the credential-redaction half of the Output
// Sanitization Pipeline (AI.md PART 11) as a shared, mode-independent
// helper. Per AI.md PART 6 "Debug Mode", keys, tokens, passwords, and
// secrets are ALWAYS redacted — no mode, not even `debug`, is allowed to
// emit them, so this package deliberately has no mode awareness at all.
package sanitize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RedactedPlaceholder replaces every credential value this package strips.
const RedactedPlaceholder = "REDACTED"

// credentialKeys are the field/parameter names whose values are always
// redacted, matching the sensitive-parameter list in AI.md PART 11's
// Output Sanitization Pipeline.
var credentialKeys = []string{
	"access_token",
	"api_key",
	"apikey",
	"auth",
	"authorization",
	"client_secret",
	"code",
	"credential",
	"encryption_key",
	"key",
	"passphrase",
	"password",
	"private_key",
	"refresh_token",
	"secret",
	"session",
	"token",
}

// credentialAssignment matches `name=value`, `name: value`, and
// `name="value"` style assignments for any credential key, capturing the
// key plus its separator so only the value is replaced. The separator
// group also absorbs an HTTP auth scheme (`Bearer `/`Basic `) so a header
// line keeps its scheme and loses only the credential itself.
var credentialAssignment = regexp.MustCompile(
	`(?i)\b(` + strings.Join(credentialKeys, "|") + `)(\s*[:=]\s*(?:bearer\s+|basic\s+)?)("[^"]*"|'[^']*'|[^\s,;&)"']+)`)

// bearerToken matches an HTTP bearer/basic credential in free-form text.
var bearerToken = regexp.MustCompile(`(?i)\b(bearer|basic)\s+([A-Za-z0-9\-._~+/=]+)`)

// urlCredentials matches the `user:password@` userinfo section of a URL so
// database and cache connection strings never leak their password.
var urlCredentials = regexp.MustCompile(`(?i)([a-z][a-z0-9+.\-]*://)([^/\s:@]+):([^/\s@]+)@`)

var internalAddress = regexp.MustCompile(`(?i)(?:\b10\.(?:\d{1,3}\.){2}\d{1,3}\b|\b172\.(?:1[6-9]|2\d|3[01])\.(?:\d{1,3}\.)\d{1,3}\b|\b192\.168\.(?:\d{1,3}\.)\d{1,3}\b|\b127\.(?:\d{1,3}\.){2}\d{1,3}\b|\b169\.254\.(?:\d{1,3}\.)\d{1,3}\b)`)
var filesystemPath = regexp.MustCompile(`((?:^|[\s"'=(]))(?:/(?:root|home|tmp|var|etc|opt|srv|mnt|workspace|app|usr|proc|run|data|private|Library|Users)/[^\s"'<>]*)|(?:\b[A-Za-z]:\\[^\s"'<>]*)|((?:^|[\s"'=(]))(?:\.{1,2}/)[^\s"'<>]*`)
var sensitiveQueryNames = map[string]struct{}{
	"token": {}, "session": {}, "code": {}, "key": {}, "password": {}, "secret": {},
	"auth": {}, "pwd": {}, "api_key": {}, "apikey": {}, "access_token": {}, "refresh_token": {},
}

// SanitizeResponseJSON applies the value-level stages of the PART 11 response
// pipeline to a JSON response body. Struct callers should first use
// FilterResponseFields so serialization can enforce explicit json tags.
func SanitizeResponseJSON(body []byte, production bool) ([]byte, error) {
	value, err := decodeJSONValue(body)
	if err != nil {
		return nil, err
	}
	value = sanitizeResponseValue(value, "", production)
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

type orderedJSONField struct {
	name  string
	value interface{}
}

type orderedJSONObject []orderedJSONField

// TaggedField is one explicitly declared JSON object field.
type TaggedField struct {
	Name  string
	Value interface{}
}

// TaggedObject contains only fields explicitly declared by its producer.
type TaggedObject []TaggedField

// OpaqueUserJSON marks caller-supplied parsed JSON that is intentionally
// returned as data. Its arbitrary member names remain confined to this
// explicitly tagged response field, and sensitive member values are still
// sanitized recursively.
type OpaqueUserJSON struct {
	Value interface{}
}

// PublicResponseValue explicitly marks a value as safe for one allowlisted
// response field whose name otherwise matches a credential key.
type PublicResponseValue struct {
	Value interface{}
}

// TaggedResponseFields is implemented only by response types whose
// serialized fields include a documented dynamic portion.
type TaggedResponseFields interface {
	TaggedResponseFields() TaggedObject
}

func (object orderedJSONObject) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, field := range object {
		if index > 0 {
			buffer.WriteByte(',')
		}
		name, err := json.Marshal(field.name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(field.value)
		if err != nil {
			return nil, err
		}
		buffer.Write(name)
		buffer.WriteByte(':')
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func decodeJSONValue(data []byte) (interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decodeOrderedJSONValue(decoder)
}

func decodeOrderedJSONValue(decoder *json.Decoder) (interface{}, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(orderedJSONObject, 0)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("JSON object key is %T, want string", keyToken)
			}
			value, err := decodeOrderedJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object = append(object, orderedJSONField{name: key, value: value})
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		array := make([]interface{}, 0)
		for decoder.More() {
			value, err := decodeOrderedJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

// MarshalTaggedResponse applies explicit struct-tag filtering before JSON
// serialization and then applies the response value sanitizer.
func MarshalTaggedResponse(value interface{}, production bool) ([]byte, error) {
	filtered := FilterResponseFields(value, production)
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	return SanitizeResponseJSON(encoded, production)
}

// FormatResponseJSON applies the public API's two-space indentation and
// exactly one trailing newline to an already sanitized JSON response.
func FormatResponseJSON(encoded []byte) ([]byte, error) {
	var indented bytes.Buffer
	if err := json.Indent(&indented, encoded, "", "  "); err != nil {
		return nil, err
	}
	indented.WriteByte('\n')
	return indented.Bytes(), nil
}

// WriteTaggedJSON marshals a typed response through the field allow-list and
// writes its complete JSON representation to the destination.
func WriteTaggedJSON(destination io.Writer, value interface{}, production bool) error {
	body, err := MarshalTaggedResponse(value, production)
	if err != nil {
		return err
	}
	body, err = FormatResponseJSON(body)
	if err != nil {
		return err
	}
	_, err = destination.Write(body)
	return err
}

// FilterResponseFields copies a response value while retaining only exported
// struct fields with explicit json tags. It also drops dev_only fields in
// production before the value reaches encoding/json.
func FilterResponseFields(value interface{}, production bool) interface{} {
	return filterTaggedValue(reflect.ValueOf(value), production)
}

func filterTaggedValue(value reflect.Value, production bool) interface{} {
	if !value.IsValid() {
		return nil
	}
	if value.CanInterface() {
		if response, ok := value.Interface().(TaggedResponseFields); ok {
			return filterTaggedValue(reflect.ValueOf(response.TaggedResponseFields()), production)
		}
		if opaque, ok := value.Interface().(OpaqueUserJSON); ok {
			return filterOpaqueUserJSON(reflect.ValueOf(opaque.Value), "", production)
		}
		if public, ok := value.Interface().(PublicResponseValue); ok {
			return filterTaggedValue(reflect.ValueOf(public.Value), production)
		}
		if scalar, ok := taggedScalar(value.Interface(), production); ok {
			return scalar
		}
		if object, ok := value.Interface().(TaggedObject); ok {
			result := make(orderedJSONObject, 0, len(object))
			seen := make(map[string]struct{}, len(object))
			for _, field := range object {
				if field.Name == "" || field.Name == "-" {
					continue
				}
				publicValue, explicitlyPublic := field.Value.(PublicResponseValue)
				if IsCredentialKey(field.Name) && !explicitlyPublic {
					continue
				}

				if _, exists := seen[field.Name]; exists {
					continue
				}
				seen[field.Name] = struct{}{}
				fieldValue := field.Value
				if explicitlyPublic {
					fieldValue = publicValue.Value
				}
				result = append(result, orderedJSONField{name: field.Name, value: filterTaggedValue(reflect.ValueOf(fieldValue), production)})
			}
			return result
		}
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		result := make(orderedJSONObject, 0, value.NumField())
		typeOf := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := typeOf.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag, tagged := field.Tag.Lookup("json")
			if !tagged {
				continue
			}
			options := strings.Split(tag, ",")
			name := options[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			if IsCredentialKey(name) {
				continue
			}
			if production && field.Tag.Get("dev_only") == "true" {
				continue
			}
			fieldValue := value.Field(i)
			if len(options) > 1 && options[1] == "omitempty" && fieldValue.IsZero() {
				continue
			}
			filtered := filterTaggedValue(fieldValue, production)
			if len(options) > 1 && options[1] == "string" {
				encoded, err := json.Marshal(filtered)
				if err == nil {
					filtered = string(encoded)
				}
			}
			result = append(result, orderedJSONField{name: name, value: filtered})
		}
		return result
	case reflect.Map:
		return nil
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			if value.CanInterface() {
				return value.Interface()
			}
			return nil
		}
		result := make([]interface{}, value.Len())
		for i := 0; i < value.Len(); i++ {
			result[i] = filterTaggedValue(value.Index(i), production)
		}
		return result
	case reflect.String:
		return sanitizeResponseString(value.String(), "")
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return nil
	}
}

func filterOpaqueUserJSON(value reflect.Value, key string, production bool) interface{} {
	if !value.IsValid() {
		return nil
	}
	if value.CanInterface() {
		if scalar, ok := taggedScalar(value.Interface(), production); ok {
			return scalar
		}
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return nil
		}
		keys := value.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		result := make(orderedJSONObject, 0, len(keys))
		for _, key := range keys {
			name := key.String()
			if IsCredentialKey(name) || production && (name == "_debug" || name == "_internal_id") {
				continue
			}
			result = append(result, orderedJSONField{name: name, value: filterOpaqueUserJSON(value.MapIndex(key), name, production)})
		}
		return result
	case reflect.Slice, reflect.Array:
		result := make([]interface{}, value.Len())
		for i := 0; i < value.Len(); i++ {
			result[i] = filterOpaqueUserJSON(value.Index(i), key, production)
		}
		return result
	case reflect.String:
		return sanitizeResponseString(value.String(), key)
	default:
		return filterTaggedValue(value, production)
	}
}

func taggedScalar(value interface{}, production bool) (interface{}, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed.Format(time.RFC3339Nano), true
	case *time.Time:
		if typed == nil {
			return nil, true
		}
		return typed.Format(time.RFC3339Nano), true
	case net.IP:
		return typed.String(), true
	case net.IPNet:
		return typed.String(), true
	case url.URL:
		return typed.String(), true
	case *url.URL:
		if typed == nil {
			return nil, true
		}
		return typed.String(), true
	case json.Number:
		return typed, true
	case json.RawMessage:
		decoded, err := decodeJSONValue(typed)
		if err != nil {
			return nil, true
		}
		switch decoded.(type) {
		case orderedJSONObject, []interface{}:
			return nil, true
		default:
			return sanitizeResponseValue(decoded, "", production), true
		}
	default:
		return nil, false
	}
}

func sanitizeResponseValue(value interface{}, key string, production bool) interface{} {
	switch typed := value.(type) {
	case orderedJSONObject:
		result := make(orderedJSONObject, 0, len(typed))
		for _, field := range typed {
			if production && (field.name == "_debug" || field.name == "_internal_id") {
				continue
			}
			result = append(result, orderedJSONField{name: field.name, value: sanitizeResponseValue(field.value, field.name, production)})
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{}, len(typed))
		for name, item := range typed {
			if production && (name == "_debug" || name == "_internal_id") {
				continue
			}
			result[name] = sanitizeResponseValue(item, name, production)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(typed))
		for i, item := range typed {
			result[i] = sanitizeResponseValue(item, key, production)
		}
		return result
	case string:
		return sanitizeResponseString(typed, key)
	default:
		return value
	}
}

func sanitizeResponseString(value, key string) string {
	lower := strings.ToLower(key)
	isURLField := strings.Contains(lower, "url") || lower == "uri" || lower == "href" || lower == "src" || lower == "action"
	if parsed, err := url.Parse(value); err == nil && parsed.RawQuery != "" && (isURLField || parsed.IsAbs()) {
		query := parsed.Query()
		for name := range query {
			if _, sensitive := sensitiveQueryNames[strings.ToLower(name)]; sensitive {
				query.Del(name)
			}
		}
		parsed.RawQuery = query.Encode()
		parsed.ForceQuery = false
		value = parsed.String()
	}
	value = internalAddress.ReplaceAllString(value, "[redacted]")
	value = filesystemPath.ReplaceAllString(value, "${1}[redacted]${2}")
	limit := -1
	if isURLField {
		limit = 256
	} else if strings.Contains(lower, "message") || strings.Contains(lower, "sample") {
		limit = 200
	} else if strings.Contains(lower, "stack") {
		limit = 2048
	}
	if limit > 0 {
		characters := []rune(value)
		if len(characters) > limit {
			value = string(characters[:limit])
		}
	}
	return value
}

// SanitizeResponseText applies query, address, path, and category-specific
// truncation rules to one response string.
func SanitizeResponseText(value, field string) string {
	return sanitizeResponseString(value, field)
}

// IsCredentialKey reports whether a field or parameter name identifies a
// credential whose field must never be emitted in a response.
func IsCredentialKey(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "public_key" || strings.HasSuffix(normalized, "_public_key") {
		return false
	}
	for _, key := range credentialKeys {
		if normalized == key || strings.HasSuffix(normalized, "_"+key) {
			return true
		}
	}
	return false
}

// RedactCredentials replaces credential values found in free-form text
// (error strings, log lines, debug dumps) with RedactedPlaceholder. It is
// applied in every mode, including debug, and never removes non-credential
// detail so diagnostics stay useful.
func RedactCredentials(s string) string {
	if s == "" {
		return s
	}

	redacted := urlCredentials.ReplaceAllString(s, "${1}${2}:"+RedactedPlaceholder+"@")
	redacted = credentialAssignment.ReplaceAllString(redacted, "${1}${2}"+RedactedPlaceholder)
	redacted = bearerToken.ReplaceAllString(redacted, "${1} "+RedactedPlaceholder)

	return redacted
}

// SanitizeLogText removes credentials, terminal escape sequences, controls,
// and non-ASCII characters so a value cannot forge or decorate a log line.
func SanitizeLogText(value string) string {
	value = RedactCredentials(value)
	var out strings.Builder
	for i := 0; i < len(value); {
		if value[i] == 0x1b {
			i++
			if i >= len(value) {
				continue
			}
			switch value[i] {
			case '[':
				i++
				for i < len(value) {
					b := value[i]
					i++
					if b >= 0x40 && b <= 0x7e {
						break
					}
				}
			case ']':
				i++
				for i < len(value) {
					if value[i] == 0x07 {
						i++
						break
					}
					if value[i] == 0x1b && i+1 < len(value) && value[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			default:
				i++
			}
			continue
		}
		if value[i] < 0x20 || value[i] > 0x7e {
			out.WriteByte(' ')
			i++
			continue
		}
		out.WriteByte(value[i])
		i++
	}
	return out.String()
}

// SanitizeLogFields recursively redacts credential-named fields and
// sanitizes text values before structured logs are serialized.
func SanitizeLogFields(fields map[string]interface{}) map[string]interface{} {
	if fields == nil {
		return nil
	}
	sanitized := make(map[string]interface{}, len(fields))
	for key, value := range fields {
		safeKey := SanitizeLogText(key)
		if IsCredentialKey(key) {
			sanitized[safeKey] = RedactedPlaceholder
			continue
		}
		sanitized[safeKey] = sanitizeLogValue(value)
	}
	return sanitized
}

func sanitizeLogValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return value
	case string:
		return SanitizeLogText(typed)
	case error:
		return SanitizeLogText(typed.Error())
	case map[string]interface{}:
		return SanitizeLogFields(typed)
	case map[string]string:
		fields := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			fields[key] = item
		}
		return SanitizeLogFields(fields)
	case []interface{}:
		items := make([]interface{}, len(typed))
		for i, item := range typed {
			items[i] = sanitizeLogValue(item)
		}
		return items
	case []string:
		items := make([]string, len(typed))
		for i, item := range typed {
			items[i] = SanitizeLogText(item)
		}
		return items
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return SanitizeLogText(fmt.Sprint(value))
		}
		var decoded interface{}
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return SanitizeLogText(fmt.Sprint(value))
		}
		return sanitizeLogValue(decoded)
	}
}
