package server

import (
	"bytes"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"

	commonhttp "github.com/apimgr/api/src/common/httputil"
	"github.com/apimgr/api/src/config"
)

// frontendJSONMiddleware maps explicit JSON requests to the paired API route.
// The downstream API handler owns the response contract, so frontend
// negotiation cannot drift from the documented endpoint.
func frontendJSONMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			accept := strings.ToLower(r.Header.Get("Accept"))
			if !strings.Contains(accept, "application/json") || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/locales/") || strings.HasPrefix(r.URL.Path, "/mcp") || strings.HasPrefix(r.URL.Path, "/metrics/") || (r.URL.Path == "/metrics" && cfg.Server.Metrics.Root.Enabled) || (r.URL.Path == "/healthz" && cfg.Server.Healthz.Root.Enabled) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Add("Vary", "Accept")
			apiRequest := r.Clone(r.Context())
			urlCopy := *r.URL
			apiRequest.URL = &urlCopy
			apiRequest.URL.Path = path.Join(APIBasePath(cfg), r.URL.Path)
			next.ServeHTTP(w, apiRequest)
		})
	}
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *bufferedResponse) Header() http.Header { return w.header }

func (w *bufferedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

// frontendTextMiddleware converts rendered HTML for explicit plain-text
// requests and non-interactive HTTP tools. Text browsers receive server
// rendered HTML with script execution removed.
func frontendTextMiddleware(projectName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/mcp") || strings.HasPrefix(r.URL.Path, "/static/") {
				next.ServeHTTP(w, r)
				return
			}
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
			}
			accept := strings.ToLower(r.Header.Get("Accept"))
			wantsText := strings.Contains(accept, "text/plain") && !strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
			if !strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html") && !strings.Contains(accept, "text/plain") {
				wantsText = commonhttp.IsNonInteractiveClient(r, projectName)
			}
			wantsNoJS := commonhttp.IsTextBrowser(r) && !strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/plain")
			if !wantsText && !wantsNoJS {
				next.ServeHTTP(w, r)
				return
			}

			buffered := &bufferedResponse{header: make(http.Header)}
			for key, values := range w.Header() {
				buffered.header[key] = append([]string(nil), values...)
			}
			next.ServeHTTP(buffered, r)
			status := buffered.status
			if status == 0 {
				status = http.StatusOK
			}
			body := buffered.body.Bytes()
			contentType := buffered.header.Get("Content-Type")
			if contentType == "" {
				contentType = http.DetectContentType(body)
			}
			mediaType, _, _ := mime.ParseMediaType(contentType)
			if mediaType != "text/html" {
				copyBufferedResponse(w, buffered, body, status)
				return
			}

			vary := buffered.header.Values("Vary")
			if !headerHasToken(vary, "Accept") {
				buffered.header.Add("Vary", "Accept")
			}
			if !headerHasToken(buffered.header.Values("Vary"), "User-Agent") {
				buffered.header.Add("Vary", "User-Agent")
			}
			if wantsText {
				buffered.header.Set("Content-Type", "text/plain; charset=utf-8")
				deleteBufferedEntityHeaders(buffered.header)
				copyBufferedResponse(w, buffered, []byte(commonhttp.HTML2TextConverter(string(body), 80)), status)
				return
			}

			buffered.header.Set("Content-Type", "text/html; charset=utf-8")
			deleteBufferedEntityHeaders(buffered.header)
			copyBufferedResponse(w, buffered, []byte(commonhttp.RenderNoJSHTML(string(body))), status)
		})
	}
}

func copyBufferedResponse(w http.ResponseWriter, buffered *bufferedResponse, body []byte, status int) {
	for key := range w.Header() {
		delete(w.Header(), key)
	}
	for key, values := range buffered.header {
		w.Header()[key] = append([]string(nil), values...)
	}
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		log.Printf("frontend response write failed: %v", err)
	}
}

func deleteBufferedEntityHeaders(header http.Header) {
	header.Del("Content-Length")
	header.Del("Content-Encoding")
}

func headerHasToken(values []string, token string) bool {
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(item), token) {
				return true
			}
		}
	}
	return false
}
