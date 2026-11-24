// Package httputil contains shared HTTP client detection and response helpers.
package httputil

import (
	"net/http"
	"strings"
)

// IsOurCLI reports whether a request comes from this project's interactive CLI.
func IsOurCLI(r *http.Request, projectName string) bool {
	if r == nil || projectName == "" {
		return false
	}
	return strings.HasPrefix(r.Header.Get("User-Agent"), projectName+"-cli/")
}

// IsTextBrowser reports whether a request comes from a terminal browser that
// can navigate and submit forms but cannot execute JavaScript.
func IsTextBrowser(r *http.Request) bool {
	if r == nil {
		return false
	}
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	for _, browser := range []string{"lynx/", "w3m/", "links ", "links/", "elinks/", "browsh/", "carbonyl/", "netsurf"} {
		if strings.Contains(ua, browser) {
			return true
		}
	}
	return false
}

// IsHTTPTool reports whether a request comes from a non-interactive HTTP client.
func IsHTTPTool(r *http.Request) bool {
	if r == nil {
		return true
	}
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	if ua == "" {
		return true
	}
	for _, tool := range []string{
		"curl/", "wget/", "httpie/", "xh/", "hurl/", "libcurl/",
		"python-requests/", "python-urllib/", "python-httpx/", "aiohttp/", "urllib3/",
		"go-http-client/", "go-resty/", "fasthttp", "reqwest/", "ureq/", "hyper/",
		"axios/", "node-fetch/", "undici", "superagent/", "deno/", "bun/",
		"okhttp/", "apache-httpclient/", "java/", "guzzlehttp/", "libwww-perl/", "lwp-trivial/", "faraday", "dart/", "windowspowershell/",
		"postmanruntime/", "insomnia/", "bruno-runtime/", "k6/",
	} {
		if strings.Contains(ua, tool) {
			return true
		}
	}
	return false
}

// IsNonInteractiveClient identifies HTTP tools that need a readable text dump.
// The project's CLI and text browsers are interactive and render their own UI.
func IsNonInteractiveClient(r *http.Request, projectName string) bool {
	if IsOurCLI(r, projectName) || IsTextBrowser(r) {
		return false
	}
	return IsHTTPTool(r)
}
