package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	commonhttp "github.com/apimgr/api/src/common/httputil"
	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/swagger"
)

// apiTextMiddleware applies the PART 14 API serializer to API JSON responses
// when the request explicitly asks for text or comes from a non-interactive
// HTTP client. MCP endpoints retain their JSON-RPC transport contract.
func apiTextMiddleware(projectName string, routeSets ...*[]swagger.Route) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") || isMCPPath(r.URL.Path) || !apiRequestWantsText(r, projectName) {
				next.ServeHTTP(w, r)
				return
			}

			buffered := &bufferedResponse{header: make(http.Header)}
			for key, values := range w.Header() {
				buffered.header[key] = append([]string(nil), values...)
			}
			request := r
			if strings.HasSuffix(r.URL.Path, ".txt") {
				request = r.Clone(r.Context())
				urlCopy := new(url.URL)
				*urlCopy = *r.URL
				urlCopy.Path = strings.TrimSuffix(urlCopy.Path, ".txt")
				urlCopy.RawPath = strings.TrimSuffix(urlCopy.RawPath, ".txt")
				request.URL = urlCopy
			}
			next.ServeHTTP(buffered, request)
			status := buffered.status
			if status == 0 {
				status = http.StatusOK
			}
			contentType := buffered.header.Get("Content-Type")
			if !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
				copyBufferedResponse(w, buffered, buffered.body.Bytes(), status)
				return
			}
			primary, message := apiTextMetadata(r, routeSets)
			textBody, err := apiJSONToText(buffered.body.Bytes(), r.Method, r.URL.Path, primary, message, i18n.LangFromRequest(r))
			if err != nil {
				textBody = i18n.Translate(i18n.LangFromRequest(r), "api.text.serialization_failed") + "\n"
				status = http.StatusInternalServerError
			}
			buffered.header.Set("Content-Type", "text/plain; charset=utf-8")
			buffered.header.Add("Vary", "Accept, User-Agent")
			deleteBufferedEntityHeaders(buffered.header)
			copyBufferedResponse(w, buffered, []byte(textBody), status)
		})
	}
}

func isMCPPath(path string) bool {
	path = strings.TrimSuffix(path, ".txt")
	return path == "/mcp" || path == "/api/mcp" || strings.HasSuffix(path, "/server/mcp")
}

func apiTextMetadata(r *http.Request, routeSets []*[]swagger.Route) (string, string) {
	path := strings.TrimSuffix(r.URL.Path, ".txt")
	for _, routeSet := range routeSets {
		if routeSet == nil {
			continue
		}
		for _, route := range *routeSet {
			if strings.EqualFold(route.Method, r.Method) && apiRoutePatternMatches(route.Path, path) {
				return route.Primary, ""
			}
		}
	}
	definition := apiRouteDefinition(r.Method, path)
	return definition.Primary, definition.TextMessage
}

func apiRequestWantsText(r *http.Request, projectName string) bool {
	if strings.HasSuffix(r.URL.Path, ".txt") {
		return true
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "application/json") {
		return false
	}
	if strings.Contains(accept, "text/plain") {
		return true
	}
	return commonhttp.IsNonInteractiveClient(r, projectName)
}

type orderedJSON struct {
	value any
}

type orderedObject []orderedPair
type orderedPair struct {
	key   string
	value orderedJSON
}
type orderedArray []orderedJSON

func readOrderedJSON(dec *json.Decoder) (orderedJSON, error) {
	token, err := dec.Token()
	if err != nil {
		return orderedJSON{}, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := make(orderedObject, 0)
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return orderedJSON{}, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return orderedJSON{}, fmt.Errorf("invalid object key")
				}
				child, err := readOrderedJSON(dec)
				if err != nil {
					return orderedJSON{}, err
				}
				object = append(object, orderedPair{key: key, value: child})
			}
			_, err := dec.Token()
			return orderedJSON{value: object}, err
		case '[':
			array := make(orderedArray, 0)
			for dec.More() {
				child, err := readOrderedJSON(dec)
				if err != nil {
					return orderedJSON{}, err
				}
				array = append(array, child)
			}
			_, err := dec.Token()
			return orderedJSON{value: array}, err
		}
	}
	return orderedJSON{value: token}, nil
}

func apiJSONToText(source []byte, method, path string, metadata ...string) (string, error) {
	primary := ""
	if len(metadata) > 0 {
		primary = metadata[0]
	}
	lang := i18n.DefaultLanguage
	if len(metadata) > 2 && metadata[2] != "" {
		lang = metadata[2]
	}
	dec := json.NewDecoder(bytes.NewReader(source))
	dec.UseNumber()
	root, err := readOrderedJSON(dec)
	if err != nil {
		return "", err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("unexpected trailing JSON value")
		}
		return "", err
	}

	if object, ok := root.value.(orderedObject); ok {
		if errorValue, exists := object.get("error"); exists {
			code := scalarString(errorValue)
			message := i18n.Translate(lang, "api.text.request_failed")
			if nested, ok := errorValue.value.(orderedObject); ok {
				if nestedCode, found := nested.get("code"); found {
					code = scalarString(nestedCode)
				}
				if nestedMessage, found := nested.get("message"); found {
					message = scalarString(nestedMessage)
				}
			}
			if value, found := object.get("message"); found {
				message = scalarString(value)
			}
			if code == "" {
				code = "REQUEST_FAILED"
			}
			return oneTrailingNewline(i18n.TranslateFormat(lang, "api.text.error_prefix", map[string]string{"code": code, "message": message})), nil
		}
		message := ""
		if messageValue, found := object.get("message"); found {
			message = scalarString(messageValue)
		}
		data, hasData := object.get("data")
		if hasData {
			if primary != "" {
				if dataObject, ok := data.value.(orderedObject); ok {
					if primaryValue, found := dataObject.get(primary); found {
						return oneTrailingNewline(scalarString(primaryValue)), nil
					}
				}
			}
			if list, ok := data.value.(orderedArray); ok {
				return serializeTextList(list, lang), nil
			}
			if nested, ok := data.value.(orderedObject); ok {
				var lines []string
				flattenTextObject(&lines, nested, "", lang)
				if message == "" {
					message = apiRouteMessage(method, "", lang)
				}
				return oneTrailingNewline(i18n.TranslateFormat(lang, "api.text.success_with_lines", map[string]string{"message": message, "lines": strings.Join(lines, "\n")})), nil
			}
			if data.value != nil {
				// Primary values are opt-in route metadata. This repository's
				// registered route table does not yet carry that metadata, so a
				// scalar response is rendered as a one-field object.
				return oneTrailingNewline(i18n.TranslateFormat(lang, "api.text.success_with_data", map[string]string{"message": apiRouteMessage(method, "", lang), "data": scalarString(data)})), nil
			}
		}
		if message == "" {
			message = apiRouteMessage(method, "", lang)
		}
		return oneTrailingNewline(i18n.TranslateFormat(lang, "api.text.success", map[string]string{"message": message})), nil
	}
	if list, ok := root.value.(orderedArray); ok {
		return serializeTextList(list, lang), nil
	}
	if root.value == nil {
		return i18n.Translate(lang, "api.text.ok") + "\n", nil
	}
	return oneTrailingNewline(scalarString(root)), nil
}

func (o orderedObject) get(key string) (orderedJSON, bool) {
	for _, pair := range o {
		if pair.key == key {
			return pair.value, true
		}
	}
	return orderedJSON{}, false
}

func flattenTextObject(lines *[]string, object orderedObject, prefix, lang string) {
	for _, pair := range object {
		key := pair.key
		if prefix != "" {
			key = prefix + "." + key
		}
		switch value := pair.value.value.(type) {
		case orderedObject:
			flattenTextObject(lines, value, key, lang)
		case orderedArray:
			if allTextScalars(value) {
				parts := make([]string, 0, len(value))
				for _, item := range value {
					parts = append(parts, scalarString(item))
				}
				*lines = append(*lines, key+": "+strings.Join(parts, ", "))
			} else {
				*lines = append(*lines, key+": "+serializeTextList(value, lang))
			}
		default:
			*lines = append(*lines, key+": "+scalarString(pair.value))
		}
	}
}

func serializeTextList(list orderedArray, lang string) string {
	if allTextScalars(list) {
		lines := []string{i18n.TranslateFormat(lang, "api.text.items_found", map[string]string{"count": fmt.Sprint(len(list))})}
		for _, item := range list {
			lines = append(lines, scalarString(item))
		}
		return oneTrailingNewline(strings.Join(lines, "\n"))
	}
	lines := []string{i18n.TranslateFormat(lang, "api.text.items_found", map[string]string{"count": fmt.Sprint(len(list))})}
	for _, item := range list {
		if object, ok := item.value.(orderedObject); ok {
			var fields []string
			flattenTextObject(&fields, object, "", lang)
			lines = append(lines, strings.Join(fields, "\n"))
		} else {
			lines = append(lines, scalarString(item))
		}
	}
	return oneTrailingNewline(lines[0] + "\n" + strings.Join(lines[1:], "\n\n"))
}

func allTextScalars(list orderedArray) bool {
	for _, item := range list {
		switch item.value.(type) {
		case orderedObject, orderedArray:
			return false
		}
	}
	return true
}

func scalarString(value orderedJSON) string {
	if value.value == nil {
		return ""
	}
	if text, ok := value.value.(string); ok {
		return text
	}
	return fmt.Sprint(value.value)
}

func oneTrailingNewline(text string) string {
	return strings.TrimRight(text, "\n") + "\n"
}
