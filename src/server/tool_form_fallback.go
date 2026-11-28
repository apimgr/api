package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/go-chi/chi/v5"
)

const (
	toolFormMemoryLimit = 1 << 20
)

var (
	toolFormTagPattern  = regexp.MustCompile(`(?s)<form\b[^>]*\bclass="[^"]*\btool-form\b[^"]*"[^>]*>`)
	toolFormAttrPattern = regexp.MustCompile(`\b([\w-]+)="([^"]*)"`)
	toolFormPlaceholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

type toolFormEndpoint struct {
	Endpoint      string
	EmptyEndpoint string
	BatchEndpoint string
	Kind          string
	Method        string
}

func toolFormFallbackHandler(cfg *config.ServerConfig, page toolPage, apiRouter chi.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.method_not_allowed"), http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, cfg.Server.Limits.MaxBodySizeBytes())
		if err := r.ParseMultipartForm(toolFormMemoryLimit); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				renderToolFormFailure(w, cfg, page, r, http.StatusRequestEntityTooLarge, "The submitted form is too large.")
				return
			}
			if !errors.Is(err, http.ErrNotMultipart) {
				renderToolFormFailure(w, cfg, page, r, http.StatusBadRequest, "Unable to read the submitted form.")
				return
			}
			if err := r.ParseForm(); err != nil {
				if errors.As(err, &maxBytesErr) {
					renderToolFormFailure(w, cfg, page, r, http.StatusRequestEntityTooLarge, "The submitted form is too large.")
					return
				}
				renderToolFormFailure(w, cfg, page, r, http.StatusBadRequest, "Unable to read the submitted form.")
				return
			}
		}
		if r.MultipartForm != nil {
			defer func() {
				if err := r.MultipartForm.RemoveAll(); err != nil {
					slog.Warn("tool form temporary upload cleanup failed")
				}
			}()
		}
		if !normalizeInputValues(r.PostForm) || !normalizeInputValues(r.Form) {
			renderToolFormFailure(w, cfg, page, r, http.StatusBadRequest, "Please check the submitted values.")
			return
		}
		spec, err := loadToolFormEndpoint(page)
		if err != nil {
			renderToolFormFailure(w, cfg, page, r, http.StatusNotImplemented, "This form is not available without JavaScript yet.")
			return
		}
		endpoint := selectToolEndpoint(spec, r.PostForm)
		apiRequest, err := buildToolAPIRequest(r, cfg, spec, endpoint)
		if err != nil {
			renderToolFormFailure(w, cfg, page, r, http.StatusBadRequest, "Please check the submitted values.")
			return
		}
		if apiRouter == nil {
			renderToolFormFailure(w, cfg, page, r, http.StatusServiceUnavailable, "The tool is temporarily unavailable.")
			return
		}
		response := httptest.NewRecorder()
		apiRouter.ServeHTTP(response, apiRequest)
		if response.Code >= http.StatusBadRequest {
			renderToolFormFailure(w, cfg, page, r, response.Code, toolAPIErrorMessage(response.Body.Bytes()))
			return
		}
		data := toolFormPageData(cfg, page, r)
		contentType := response.Header().Get("Content-Type")
		if mediaType, ok := safeToolImageType(contentType); ok {
			data.ToolImageURL = template.URL("data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(response.Body.Bytes()))
		} else {
			data.ToolResult = formatToolAPIResult(response.Body.Bytes(), contentType)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(http.StatusOK)
		renderPage(w, page.category+"/"+page.tool, data)
	}
}

func selectToolEndpoint(spec toolFormEndpoint, values url.Values) string {
	endpoint := spec.Endpoint
	if spec.EmptyEndpoint != "" && values.Get("ip") == "" {
		endpoint = spec.EmptyEndpoint
	}
	if spec.BatchEndpoint != "" && formInt(values.Get("count"), 1) > 1 {
		endpoint = spec.BatchEndpoint
	}
	return endpoint
}

func loadToolFormEndpoint(page toolPage) (toolFormEndpoint, error) {
	path := "template/page/tools/" + page.category + "/" + page.tool + ".tmpl"
	contents, err := templatesFS.ReadFile(path)
	if err != nil {
		return toolFormEndpoint{}, fmt.Errorf("read tool template %s: %w", path, err)
	}
	tag := toolFormTagPattern.Find(contents)
	if len(tag) == 0 {
		return toolFormEndpoint{}, fmt.Errorf("tool form metadata not found")
	}
	attrs := make(map[string]string)
	for _, match := range toolFormAttrPattern.FindAllSubmatch(tag, -1) {
		attrs[string(match[1])] = string(match[2])
	}
	spec := toolFormEndpoint{Method: http.MethodGet}
	switch {
	case attrs["data-body-endpoint"] != "":
		spec.Endpoint, spec.Kind, spec.Method = attrs["data-body-endpoint"], "raw-body", http.MethodPost
	case attrs["data-query-post-endpoint"] != "":
		spec.Endpoint, spec.Kind, spec.Method = attrs["data-query-post-endpoint"], "query", http.MethodPost
	case attrs["data-image-template"] != "":
		spec.Endpoint, spec.Kind = attrs["data-image-template"], "template-query"
	case attrs["data-template"] != "":
		spec.Endpoint, spec.Kind = attrs["data-template"], "template"
	case attrs["data-endpoint"] != "":
		spec.Endpoint, spec.Kind = attrs["data-endpoint"], "query"
	case attrs["data-api-endpoint"] != "":
		spec.Endpoint, spec.Kind, spec.Method = attrs["data-api-endpoint"], "multipart", http.MethodPost
	case strings.HasPrefix(attrs["action"], "{{.APIBasePath}}"):
		spec.Endpoint, spec.Kind = attrs["action"], "action"
		if strings.EqualFold(attrs["method"], http.MethodPost) {
			spec.Method = http.MethodPost
		}
	default:
		return toolFormEndpoint{}, fmt.Errorf("tool form endpoint metadata is missing")
	}
	spec.BatchEndpoint = attrs["data-batch-template"]
	spec.EmptyEndpoint = attrs["data-empty-template"]
	return spec, nil
}

func buildToolAPIRequest(r *http.Request, cfg *config.ServerConfig, spec toolFormEndpoint, endpointTemplate string) (*http.Request, error) {
	endpoint := strings.ReplaceAll(endpointTemplate, "{{.APIBasePath}}", APIBasePath(cfg))
	endpoint = strings.ReplaceAll(endpoint, "{{ .APIBasePath }}", APIBasePath(cfg))
	endpoint = fillToolEndpoint(endpoint, r.PostForm)
	u, err := url.Parse(endpoint)
	if err != nil || u.IsAbs() || u.Host != "" {
		return nil, fmt.Errorf("invalid API endpoint metadata")
	}
	basePath := APIBasePath(cfg)
	if !strings.HasPrefix(u.Path, basePath+"/") {
		return nil, fmt.Errorf("API endpoint is outside the configured version")
	}
	query := u.Query()
	method := spec.Method
	var body io.Reader
	contentType := ""
	switch spec.Kind {
	case "query":
		mergeFormQuery(query, r.PostForm, nil)
	case "template-query":
		mergeFormQuery(query, r.PostForm, toolEndpointFields(endpointTemplate))
	case "raw-body":
		bodyText := r.PostForm.Get("body")
		body = strings.NewReader(bodyText)
		contentType = "text/plain; charset=utf-8"
		mergeFormQuery(query, r.PostForm, map[string]bool{"body": true})
	case "multipart":
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		if r.MultipartForm == nil {
			return nil, fmt.Errorf("multipart form is required")
		}
		if err := copyToolMultipartForm(writer, r.MultipartForm); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		body = &buffer
		contentType = writer.FormDataContentType()
	default:
		if method == http.MethodPost {
			body = strings.NewReader(r.PostForm.Encode())
			contentType = "application/x-www-form-urlencoded"
		}
	}
	requestURL := &url.URL{Path: u.Path, RawPath: u.RawPath, RawQuery: query.Encode()}
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, chi.NewRouteContext())
	apiRequest := r.Clone(ctx)
	apiRequest.URL = requestURL
	apiRequest.RequestURI = requestURL.RequestURI()
	apiRequest.Method = method
	if body == nil {
		apiRequest.Body = http.NoBody
	} else {
		apiRequest.Body = io.NopCloser(body)
	}
	apiRequest.ContentLength = -1
	apiRequest.Header = r.Header.Clone()
	apiRequest.Header.Set("Accept", "text/plain, */*;q=0.9")
	if contentType != "" {
		apiRequest.Header.Set("Content-Type", contentType)
	}
	return apiRequest, nil
}

func fillToolEndpoint(endpoint string, values url.Values) string {
	queryAt := strings.IndexByte(endpoint, '?')
	return toolFormPlaceholder.ReplaceAllStringFunc(endpoint, func(token string) string {
		field := strings.Trim(token, "{}")
		value := values.Get(field)
		if queryAt >= 0 && strings.Index(endpoint, token) > queryAt {
			return url.QueryEscape(value)
		}
		if value == "" {
			return ""
		}
		return url.PathEscape(value)
	})
}

func toolEndpointFields(endpoint string) map[string]bool {
	fields := make(map[string]bool)
	for _, match := range toolFormPlaceholder.FindAllStringSubmatch(endpoint, -1) {
		fields[match[1]] = true
	}
	return fields
}

func mergeFormQuery(query url.Values, form url.Values, skip map[string]bool) {
	for name, values := range form {
		if name == "_csrf" || name == "csrf_token" || name == "_method" || (skip != nil && skip[name]) || len(values) == 0 {
			continue
		}
		query[name] = append([]string(nil), values...)
	}
}

func copyToolMultipartForm(writer *multipart.Writer, form *multipart.Form) error {
	for name, values := range form.Value {
		if name == "_csrf" || name == "csrf_token" || name == "_method" {
			continue
		}
		for _, value := range values {
			if err := writer.WriteField(name, value); err != nil {
				return err
			}
		}
	}
	for name, files := range form.File {
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				return err
			}
			part, err := writer.CreateFormFile(name, fileHeader.Filename)
			if err != nil {
				closeErr := file.Close()
				return errors.Join(err, closeErr)
			}
			_, copyErr := io.Copy(part, file)
			closeErr := file.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return err
			}
		}
	}
	return nil
}

func safeToolImageType(contentType string) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", false
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return mediaType, true
	default:
		return "", false
	}
}

func formatToolAPIResult(body []byte, contentType string) string {
	if strings.Contains(contentType, "json") {
		var value any
		if json.Unmarshal(body, &value) == nil {
			formatted, err := json.MarshalIndent(value, "", "  ")
			if err == nil {
				return string(formatted)
			}
		}
	}
	return strings.TrimSpace(string(body))
}

func toolAPIErrorMessage(body []byte) string {
	var response struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &response) == nil && response.Message != "" {
		return response.Message
	}
	return "The tool request failed. Please review the submitted values and try again."
}

func renderToolFormFailure(w http.ResponseWriter, cfg *config.ServerConfig, page toolPage, r *http.Request, status int, message string) {
	data := toolFormPageData(cfg, page, r)
	data.ToolError = message
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	renderPage(w, page.category+"/"+page.tool, data)
}

func toolFormPageData(cfg *config.ServerConfig, page toolPage, r *http.Request) PageData {
	data := newPageData(cfg, r, page.category)
	data.PageTitle = page.title
	data.PageDescription = page.description
	if titleKey, descriptionKey := toolMetadataKeys(page.category, page.tool); titleKey != "" && descriptionKey != "" {
		data.PageTitle = i18n.Translate(data.Lang, titleKey)
		data.PageDescription = i18n.Translate(data.Lang, descriptionKey)
	}
	return data
}

func formInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
