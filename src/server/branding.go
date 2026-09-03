package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/apimgr/api/src/config"
)

// Routes the branding handlers are mounted on. A configured local file is
// served through these paths so the operator's filesystem layout never leaks
// into a public URL.
const (
	brandingFaviconRoute = "/branding/favicon"
	brandingLogoRoute    = "/branding/logo"
)

// Embedded defaults used whenever the operator has not configured a custom
// image (AI.md PART 16 "Image Sources": empty = built-in fallback).
const (
	brandingDefaultFavicon = "/static/images/favicon.ico"
	brandingDefaultLogo    = ""
)

// brandingAssetURL resolves a configured branding image to the URL a template
// should emit. Three sources are supported per AI.md PART 16 "Image Sources":
// an empty value falls back to the embedded default, an https:// URL is used
// as-is, and anything else is treated as a local file path served through
// route.
func brandingAssetURL(configured, route, embeddedDefault string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		if embeddedDefault == "" {
			return ""
		}
		return assetURL(embeddedDefault)
	}
	if strings.HasPrefix(configured, "https://") || strings.HasPrefix(configured, "http://") {
		return configured
	}
	return route
}

// brandingLocalFile returns the absolute path of a configured branding image
// when it names a readable local regular file, and false otherwise. Remote
// URLs and unset values are never served from disk.
func brandingLocalFile(configured string) (string, bool) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return "", false
	}
	if strings.HasPrefix(configured, "https://") || strings.HasPrefix(configured, "http://") {
		return "", false
	}
	if !filepath.IsAbs(configured) {
		return "", false
	}
	clean := filepath.Clean(configured)
	info, err := os.Stat(clean)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return clean, true
}

// brandingImageHandler serves a configured local branding image. It returns
// 404 when nothing is configured, when the value is a remote URL (the template
// links to it directly in that case), or when the file is missing.
func brandingImageHandler(cfg *config.Config, pick func(*config.Config) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path, ok := brandingLocalFile(pick(cfg))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, path)
	}
}

// faviconHandler backs the root /favicon.ico that browsers request without
// being told to. A configured custom favicon wins; otherwise the embedded
// default is redirected to so it keeps the immutable asset caching.
func faviconHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if path, ok := brandingLocalFile(cfg.Server.Branding.Favicon); ok {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.ServeFile(w, r, path)
			return
		}
		configured := strings.TrimSpace(cfg.Server.Branding.Favicon)
		if strings.HasPrefix(configured, "https://") || strings.HasPrefix(configured, "http://") {
			http.Redirect(w, r, configured, http.StatusFound)
			return
		}
		http.Redirect(w, r, assetURL(brandingDefaultFavicon), http.StatusFound)
	}
}
