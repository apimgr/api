package server

import (
	"context"
	"mime"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

const (
	// formBodyField is the form field a plain HTML form uses to carry the
	// payload that JSON/CLI clients send as the raw request body.
	formBodyField = "body"
	// formValueLimit bounds the total size of a submitted form, matching the
	// 1MB cap readRequestBody applies to raw bodies.
	formValueLimit = 1 << 20
	// formMultipartMemory bounds the in-memory portion of a multipart form.
	formMultipartMemory = 1 << 20
	// formMaxFields bounds how many submitted fields are promoted into the
	// query string so a hostile form cannot grow the parameter set without limit.
	formMaxFields = 64
	// formMaxValueLength bounds the length of any single promoted field value.
	formMaxValueLength = 1 << 20
)

// formBodyContextKeyType is the unexported context key type used to carry a
// submitted form body field to handlers that read the raw request body.
type formBodyContextKeyType struct{}

// formBodyContextKey is the context key holding the submitted body field.
var formBodyContextKey = formBodyContextKeyType{}

// formInputMiddleware lets a plain HTML form drive the same API endpoints that
// JSON, query-string, and CLI clients already use. For urlencoded/multipart
// submissions under /api/ it promotes the submitted fields into a copy of the
// request query string and stashes the "body" field for readRequestBody.
// Existing shapes are untouched: any other content type (JSON, raw text) is
// passed through unread, and an explicit query parameter always wins over a
// form field of the same name.
func formInputMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isFormSubmission(r) {
			next.ServeHTTP(w, r)
			return
		}
		if err := parseSubmittedForm(w, r); err != nil {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, requestWithFormInput(r))
	})
}

// isFormSubmission reports whether the request is a body-carrying API request
// whose content type is one of the two encodings an HTML form can produce.
func isFormSubmission(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return false
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mediaType == "application/x-www-form-urlencoded" || mediaType == "multipart/form-data"
}

// parseSubmittedForm parses the submitted form with a bounded reader so a
// large or malformed body cannot exhaust memory.
func parseSubmittedForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, formValueLimit)
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return err
	}
	if mediaType == "multipart/form-data" {
		return r.ParseMultipartForm(formMultipartMemory)
	}
	return r.ParseForm()
}

// requestWithFormInput returns a shallow copy of r whose URL carries the
// submitted form fields as query parameters and whose context carries the
// submitted body field. The original request is left untouched so middleware
// registered earlier in the chain (access logging) never observes the merged
// values.
func requestWithFormInput(r *http.Request) *http.Request {
	ctx := r.Context()
	if values, ok := r.PostForm[formBodyField]; ok && len(values) > 0 && len(values[0]) <= formMaxValueLength {
		ctx = context.WithValue(ctx, formBodyContextKey, values[0])
	}
	query := r.URL.Query()
	names := make([]string, 0, len(r.PostForm))
	for name := range r.PostForm {
		names = append(names, name)
	}
	sort.Strings(names)
	promoted := 0
	for _, name := range names {
		if promoted >= formMaxFields {
			break
		}
		if _, exists := query[name]; exists {
			continue
		}
		value := r.PostForm.Get(name)
		if len(value) > formMaxValueLength {
			continue
		}
		query.Set(name, value)
		promoted++
	}
	copied := r.WithContext(ctx)
	merged := *r.URL
	merged.RawQuery = query.Encode()
	copied.URL = &merged
	return copied
}

// paramValue resolves a named input from the chi URL parameter first, then
// from the query string (which formInputMiddleware has already merged any
// submitted form fields into). Path-parameter callers are unaffected; a plain
// HTML form reaches the same handler through the fallback route registered by
// registerFormFallbacks.
func paramValue(r *http.Request, name string) string {
	if value := chi.URLParam(r, name); value != "" {
		return value
	}
	return r.URL.Query().Get(name)
}

// formFallbackPattern returns the parameterless prefix of a parameterised
// route pattern, which is the path a plain HTML form submits to. It returns an
// empty string when the pattern takes no path parameters.
func formFallbackPattern(pattern string) string {
	index := strings.IndexAny(pattern, "{*")
	if index < 0 {
		return ""
	}
	prefix := strings.TrimSuffix(pattern[:index], "/")
	if prefix == "" {
		return ""
	}
	if strings.HasSuffix(pattern, ".txt") {
		return prefix + ".txt"
	}
	return prefix
}

// handlerIdentity returns a comparable identity for a registered handler so
// two routes sharing one handler can be recognised. Handlers that are neither
// functions nor pointers (a file server value, for example) have no comparable
// identity and are reported as unusable.
func handlerIdentity(handler http.Handler) (uintptr, bool) {
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Func, reflect.Ptr, reflect.UnsafePointer:
		return value.Pointer(), true
	default:
		return 0, false
	}
}

// registerFormFallbacks walks every route already registered on the router and
// registers a parameterless variant of each parameterised route, so a plain
// HTML form can submit ?name=value to the same handler. A fallback that would
// collide with an explicitly registered route, or with another fallback
// resolving to a different handler, is skipped so existing behaviour is never
// replaced. chi.Walk only ever fails by propagating an error from the walk
// function, and neither walk function here returns one, so the walks cannot
// fail.
func registerFormFallbacks(router *chi.Mux) {
	type fallback struct {
		method  string
		pattern string
		handler http.Handler
	}
	explicit := map[string]bool{}
	claimed := map[string]uintptr{}
	skipped := map[string]bool{}
	pending := []fallback{}
	_ = chi.Walk(router, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		explicit[method+" "+route] = true
		return nil
	})
	_ = chi.Walk(router, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/") {
			return nil
		}
		pattern := formFallbackPattern(route)
		if pattern == "" {
			return nil
		}
		identity, ok := handlerIdentity(handler)
		if !ok {
			return nil
		}
		key := method + " " + pattern
		if explicit[key] || skipped[key] {
			return nil
		}
		if existing, ok := claimed[key]; ok {
			if existing != identity {
				skipped[key] = true
			}
			return nil
		}
		claimed[key] = identity
		pending = append(pending, fallback{method: method, pattern: pattern, handler: handler})
		return nil
	})
	for _, route := range pending {
		if skipped[route.method+" "+route.pattern] {
			continue
		}
		router.Method(route.method, route.pattern, route.handler)
	}
}
