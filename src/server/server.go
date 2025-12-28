package server

import (
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/api/src/admin"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/graphql"
	"github.com/apimgr/api/src/metrics"
	"github.com/apimgr/api/src/server/handler"
	"github.com/apimgr/api/src/services/crypto"
	"github.com/apimgr/api/src/services/datetime"
	"github.com/apimgr/api/src/services/text"
	"github.com/apimgr/api/src/swagger"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

//go:embed templates/*.tmpl templates/**/*.tmpl
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

var templates *template.Template

// Version information
var (
	Version   = "1.0.0"
	BuildTime = "unknown"
	startTime = time.Now()
)

// New creates a new HTTP server
func New(cfg *config.Config) *http.Server {
	// Initialize page templates
	if err := initTemplates(); err != nil {
		panic(fmt.Sprintf("Failed to parse templates: %v", err))
	}

	r := chi.NewRouter()

	// Core middleware
	r.Use(middleware.RealIP)
	r.Use(requestIDMiddleware)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(securityHeadersMiddleware(cfg))
	r.Use(RateLimitMiddleware(cfg))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.Web.CORS},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization", "X-Request-ID"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Static files
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Web routes
	r.Get("/", homeHandler(cfg))
	r.Get("/text", textPageHandler(cfg))
	r.Get("/crypto", cryptoPageHandler(cfg))
	r.Get("/datetime", datetimePageHandler(cfg))
	r.Get("/api", apiDocsHandler(cfg))
	r.Get("/openapi", openapiHandler(cfg))
	r.Get("/openapi.json", openapiJSONHandler(cfg))
	r.Get("/openapi.yaml", openapiYAMLHandler(cfg))
	r.Get("/swagger", swaggerHandler(cfg))
	r.Get("/graphql", graphqlHandler(cfg))
	r.Post("/graphql", graphqlQueryHandler(cfg))

	// Standard pages (/server/*)
	r.Get("/server/about", aboutPageHandler(cfg))
	r.Get("/server/privacy", privacyPageHandler(cfg))
	r.Get("/server/contact", contactPageHandler(cfg))
	r.Get("/server/help", helpPageHandler(cfg))

	// Admin routes (from admin package)
	admin.SetupRoutes(r, cfg)

	// Health check
	r.Get("/healthz", healthHandler)

	// Metrics endpoint (Prometheus-compatible)
	r.Get("/metrics", metricsPrometheusHandler)
	r.Get("/api/v1/metrics", metricsJSONHandler)

	// Special files
	r.Get("/robots.txt", robotsHandler(cfg))
	r.Get("/security.txt", securityHandler(cfg))
	r.Get("/.well-known/security.txt", securityHandler(cfg))
	r.Get("/manifest.json", manifestHandler(cfg))

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health check and version (JSON)
		r.Get("/healthz", handler.HandleHealthCheck)
		r.Get("/version", handler.HandleVersion)

		// Theme switching
		r.Post("/theme", HandleThemeSwitch)

		// Text utilities
		r.Route("/text", func(r chi.Router) {
			// UUID
			r.Get("/uuid", apiUUIDHandler)
			r.Get("/uuid.txt", apiUUIDTextHandler)
			r.Get("/uuid/{version}", apiUUIDHandler)
			r.Get("/uuid/{version}.txt", apiUUIDTextHandler)
			r.Get("/uuid/{version}/{count}", apiUUIDBatchHandler)

			// Hash
			r.Get("/hash/{algorithm}/{input}", apiHashHandler)
			r.Get("/hash/{algorithm}/{input}.txt", apiHashTextHandler)
			r.Get("/hash/multi/{input}", apiHashMultiHandler)

			// Encode/Decode
			r.Get("/encode/{encoding}/{input}", apiEncodeHandler)
			r.Get("/encode/{encoding}/{input}.txt", apiEncodeTextHandler)
			r.Get("/decode/{encoding}/{input}", apiDecodeHandler)
			r.Get("/decode/{encoding}/{input}.txt", apiDecodeTextHandler)

			// Case conversion
			r.Get("/case/{style}/{input}", apiCaseHandler)
			r.Get("/case/{style}/{input}.txt", apiCaseTextHandler)

			// Lorem ipsum
			r.Get("/lorem", apiLoremHandler)
			r.Get("/lorem/{type}", apiLoremHandler)
			r.Get("/lorem/{type}/{count}", apiLoremHandler)
			r.Get("/lorem/{type}/{count}.txt", apiLoremTextHandler)

			// Text stats
			r.Post("/stats", apiTextStatsHandler)

			// ROT13
			r.Get("/rot13/{input}", apiROT13Handler)
			r.Get("/rot13/{input}.txt", apiROT13TextHandler)

			// Reverse
			r.Get("/reverse/{input}", apiReverseHandler)
			r.Get("/reverse/{input}.txt", apiReverseTextHandler)
		})

		// Crypto utilities
		r.Route("/crypto", func(r chi.Router) {
			// Bcrypt
			r.Get("/bcrypt/{password}", apiBcryptHandler)
			r.Get("/bcrypt/{cost}/{password}", apiBcryptHandler)
			r.Get("/bcrypt/hash/{password}", apiBcryptHandler)
			r.Post("/bcrypt/verify", apiBcryptVerifyHandler)
			r.Get("/bcrypt/verify/{password}/{hash}", apiBcryptVerifyGetHandler)

			// Password generation
			r.Get("/password", apiPasswordHandler)
			r.Get("/password/{length}", apiPasswordHandler)
			r.Get("/password.txt", apiPasswordTextHandler)
			r.Get("/password/{length}.txt", apiPasswordTextHandler)

			// PIN generation
			r.Get("/pin", apiPINHandler)
			r.Get("/pin/{length}", apiPINHandler)
			r.Get("/pin.txt", apiPINTextHandler)
			r.Get("/pin/{length}.txt", apiPINTextHandler)

			// TOTP
			r.Get("/totp/secret", apiTOTPGenerateHandler)
			r.Get("/totp/generate", apiTOTPGenerateHandler)
			r.Get("/totp/code/{secret}", apiTOTPCodeHandler)
			r.Get("/totp/code/{secret}.txt", apiTOTPCodeTextHandler)
			r.Get("/totp/verify/{secret}/{code}", apiTOTPVerifyHandler)

			// Random bytes
			r.Get("/random/bytes/{count}", apiRandomBytesHandler)
			r.Get("/random/hex/{count}", apiRandomHexHandler)

			// Password strength
			r.Get("/password/strength/{password}", apiPasswordStrengthHandler)
			r.Post("/password/strength", apiPasswordStrengthPostHandler)
		})

		// DateTime utilities
		r.Route("/datetime", func(r chi.Router) {
			// Current time
			r.Get("/now", apiDateTimeNowHandler)
			r.Get("/now.txt", apiDateTimeNowTextHandler)
			r.Get("/now/{timezone}", apiDateTimeNowHandler)

			// Timestamp
			r.Get("/timestamp", apiTimestampHandler)
			r.Get("/timestamp.txt", apiTimestampTextHandler)

			// Convert
			r.Get("/convert/{timestamp}", apiConvertTimestampHandler)
			r.Get("/convert/{timestamp}/{timezone}", apiConvertTimestampHandler)
			r.Get("/to-unix/{datetime}", apiToUnixHandler)

			// Add/Subtract
			r.Get("/add/{timestamp}/{duration}", apiAddDurationHandler)
			r.Get("/diff/{timestamp1}/{timestamp2}", apiDiffHandler)

			// Timezones
			r.Get("/timezones", apiTimezonesHandler)
			r.Get("/timezone/{timezone}", apiTimezoneInfoHandler)
			r.Get("/timezone/convert/{timestamp}/{from}/{to}", apiConvertTimezoneHandler)
		})
	})

	return &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Address, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// Template data
type PageData struct {
	SiteTitle         string
	SiteIcon          string
	BaseURL           string
	Theme             string
	ActivePage        string
	PageTitle         string
	PageDescription   string
	Tagline           string
	Version           string
	BuildTime         string
	Mode              string
	AdminEmail        string
	SecurityEmail     string
	UpdatedAt         string
	RateLimitRequests int
	RateLimitWindow   int
}

func newPageData(cfg *config.Config, activePage string) PageData {
	baseURL := fmt.Sprintf("http://%s:%s", cfg.Server.FQDN, cfg.Server.Port)
	if cfg.Server.FQDN == "" || cfg.Server.FQDN == "localhost" {
		baseURL = fmt.Sprintf("http://localhost:%s", cfg.Server.Port)
	}
	return PageData{
		SiteTitle:  cfg.Server.Branding.Title,
		SiteIcon:   "🛠️",
		BaseURL:    baseURL,
		Theme:      cfg.Web.UI.Theme,
		ActivePage: activePage,
	}
}

// pageTemplates holds pre-parsed templates for each page
var pageTemplates map[string]*template.Template

// initTemplates initializes all page templates with their dependencies
func initTemplates() error {
	pageTemplates = make(map[string]*template.Template)

	// Public pages use base layout
	publicPages := []string{"index", "text", "crypto", "datetime", "openapi", "error", "healthz", "about", "privacy", "contact", "help"}

	for _, page := range publicPages {
		tmpl, err := template.ParseFS(templatesFS,
			"templates/layouts/base.tmpl",
			"templates/partials/*.tmpl",
			"templates/components/*.tmpl",
			fmt.Sprintf("templates/pages/%s.tmpl", page),
		)
		if err != nil {
			return fmt.Errorf("failed to parse %s template: %w", page, err)
		}
		pageTemplates[page] = tmpl
	}

	// Admin pages use admin layout
	adminPages := []string{"dashboard", "settings"}

	for _, page := range adminPages {
		tmpl, err := template.ParseFS(templatesFS,
			"templates/layouts/admin.tmpl",
			"templates/partials/*.tmpl",
			"templates/components/*.tmpl",
			fmt.Sprintf("templates/admin/%s.tmpl", page),
		)
		if err != nil {
			return fmt.Errorf("failed to parse admin/%s template: %w", page, err)
		}
		pageTemplates["admin-"+page] = tmpl
	}

	return nil
}

// renderPage renders a page using the base layout
func renderPage(w http.ResponseWriter, page string, data PageData) {
	tmpl, ok := pageTemplates[page]
	if !ok {
		http.Error(w, "Template not found: "+page, http.StatusInternalServerError)
		return
	}

	err := tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// Web handlers
func homeHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "home")
		data.PageTitle = ""
		data.PageDescription = "Universal API Toolkit with text, crypto, datetime, and network utilities"
		renderPage(w, "index", data)
	}
}

func textPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "text")
		data.PageTitle = "Text Utilities"
		data.PageDescription = "UUID generation, hashing, encoding, and text manipulation"
		renderPage(w, "text", data)
	}
}

func cryptoPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "crypto")
		data.PageTitle = "Cryptography Tools"
		data.PageDescription = "Password hashing, TOTP generation, and secure passwords"
		renderPage(w, "crypto", data)
	}
}

func datetimePageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "datetime")
		data.PageTitle = "DateTime Tools"
		data.PageDescription = "Timestamp conversion, timezone handling, and date calculations"
		renderPage(w, "datetime", data)
	}
}

func aboutPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "about")
		data.PageTitle = "About"
		data.PageDescription = "About " + cfg.Server.Branding.Title
		data.Tagline = cfg.Server.Branding.Tagline
		data.Version = Version
		data.BuildTime = BuildTime
		data.Mode = cfg.Server.Mode
		renderPage(w, "about", data)
	}
}

func privacyPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "privacy")
		data.PageTitle = "Privacy Policy"
		data.PageDescription = "Privacy policy for " + cfg.Server.Branding.Title
		data.UpdatedAt = time.Now().Format("January 2006")
		renderPage(w, "privacy", data)
	}
}

func contactPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "contact")
		data.PageTitle = "Contact"
		data.PageDescription = "Contact information"
		data.AdminEmail = cfg.Server.Admin.Email
		data.SecurityEmail = cfg.Web.Security.Contact
		renderPage(w, "contact", data)
	}
}

func helpPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "help")
		data.PageTitle = "Help"
		data.PageDescription = "Getting started with " + cfg.Server.Branding.Title
		data.RateLimitRequests = cfg.Server.RateLimit.Requests
		data.RateLimitWindow = cfg.Server.RateLimit.Window
		renderPage(w, "help", data)
	}
}

func apiDocsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "api")
		data.PageTitle = "API Documentation"
		data.PageDescription = "REST API documentation for CasTools - Universal API Toolkit"
		renderPage(w, "openapi", data)
	}
}

func swaggerHandler(cfg *config.Config) http.HandlerFunc {
	// Use new swagger package for Swagger UI with theme support
	baseURL := getBaseURL(cfg)
	return swagger.ServeUI(baseURL + "/openapi.json")
}

func openapiHandler(cfg *config.Config) http.HandlerFunc {
	// Redirect /openapi to /swagger for consistency
	return swaggerHandler(cfg)
}

func openapiJSONHandler(cfg *config.Config) http.HandlerFunc {
	// Use new swagger package to generate OpenAPI spec
	baseURL := getBaseURL(cfg)
	return swagger.ServeSpec(Version, baseURL)
}

func openapiYAMLHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Note: Per AI.md PART 20, OpenAPI spec uses JSON format only (NO YAML)
		// Redirect to JSON endpoint as per specification
		http.Redirect(w, r, "/openapi.json", http.StatusFound)
	}
}

// getBaseURL returns the base URL for the server
func getBaseURL(cfg *config.Config) string {
	baseURL := fmt.Sprintf("http://%s:%s", cfg.Server.FQDN, cfg.Server.Port)
	if cfg.Server.FQDN == "" || cfg.Server.FQDN == "localhost" {
		baseURL = fmt.Sprintf("http://localhost:%s", cfg.Server.Port)
	}
	if cfg.Server.SSL.Enabled {
		baseURL = fmt.Sprintf("https://%s:%s", cfg.Server.FQDN, cfg.Server.Port)
	}
	return baseURL
}


func graphqlHandler(cfg *config.Config) http.HandlerFunc {
	// Use new graphql package for GraphiQL UI with theme support
	baseURL := getBaseURL(cfg)
	return graphql.ServeUI(baseURL + "/graphql")
}

func graphqlQueryHandler(cfg *config.Config) http.HandlerFunc {
	// Use new graphql package to handle queries
	return graphql.HandleQuery
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"service":   "CasTools",
		"version":   "1.0.0",
	})
}

func apiHealthHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"service":   "CasTools",
		"version":   "1.0.0",
	})
}

// metricsPrometheusHandler serves metrics in Prometheus format
func metricsPrometheusHandler(w http.ResponseWriter, r *http.Request) {
	metrics.Get().ServePrometheus(w, r)
}

// metricsJSONHandler serves metrics in JSON format
func metricsJSONHandler(w http.ResponseWriter, r *http.Request) {
	metrics.Get().ServeJSON(w, r)
}

func robotsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "User-agent: *")
		for _, path := range cfg.Web.Robots.Allow {
			fmt.Fprintf(w, "Allow: %s\n", path)
		}
		for _, path := range cfg.Web.Robots.Deny {
			fmt.Fprintf(w, "Disallow: %s\n", path)
		}
		// Add sitemap reference
		baseURL := fmt.Sprintf("http://%s:%s", cfg.Server.FQDN, cfg.Server.Port)
		fmt.Fprintf(w, "Sitemap: %s/sitemap.xml\n", baseURL)
	}
}

func securityHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// RFC 9116 compliant security.txt
		fmt.Fprintf(w, "Contact: mailto:%s\n", cfg.Web.Security.Contact)
		fmt.Fprintf(w, "Expires: %s\n", cfg.Web.Security.Expires.Format(time.RFC3339))
		fmt.Fprintln(w, "Preferred-Languages: en")
	}
}

func manifestHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":             "CasTools",
			"short_name":       "CasTools",
			"description":      "Universal API Toolkit",
			"start_url":        "/",
			"display":          "standalone",
			"background_color": "#1e1e2e",
			"theme_color":      "#6366f1",
		})
	}
}

// Response helpers
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func textResponse(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(text))
}

func errorResponse(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// Text API handlers
func apiUUIDHandler(w http.ResponseWriter, r *http.Request) {
	version := 4
	if v := chi.URLParam(r, "version"); v != "" {
		version, _ = strconv.Atoi(v)
	}

	uuid, err := text.UUID(version)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"uuid":    uuid,
		"version": version,
	})
}

func apiUUIDTextHandler(w http.ResponseWriter, r *http.Request) {
	version := 4
	if v := chi.URLParam(r, "version"); v != "" {
		version, _ = strconv.Atoi(v)
	}

	uuid, err := text.UUID(version)
	if err != nil {
		textResponse(w, "Error: "+err.Error())
		return
	}

	textResponse(w, uuid)
}

func apiUUIDBatchHandler(w http.ResponseWriter, r *http.Request) {
	version := 4
	count := 10
	if v := chi.URLParam(r, "version"); v != "" {
		version, _ = strconv.Atoi(v)
	}
	if c := chi.URLParam(r, "count"); c != "" {
		count, _ = strconv.Atoi(c)
	}

	uuids, err := text.UUIDs(version, count)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"uuids":   uuids,
		"version": version,
		"count":   len(uuids),
	})
}

func apiHashHandler(w http.ResponseWriter, r *http.Request) {
	algorithm := chi.URLParam(r, "algorithm")
	input := chi.URLParam(r, "input")

	hash, err := text.Hash(algorithm, input)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"algorithm": algorithm,
		"input":     input,
		"hash":      hash,
	})
}

func apiHashTextHandler(w http.ResponseWriter, r *http.Request) {
	algorithm := chi.URLParam(r, "algorithm")
	input := chi.URLParam(r, "input")

	hash, err := text.Hash(algorithm, input)
	if err != nil {
		textResponse(w, "Error: "+err.Error())
		return
	}

	textResponse(w, hash)
}

func apiHashMultiHandler(w http.ResponseWriter, r *http.Request) {
	input := chi.URLParam(r, "input")
	hashes := text.HashAll(input)

	jsonResponse(w, map[string]interface{}{
		"input":  input,
		"hashes": hashes,
	})
}

func apiEncodeHandler(w http.ResponseWriter, r *http.Request) {
	encoding := strings.ToLower(chi.URLParam(r, "encoding"))
	input := chi.URLParam(r, "input")

	var output string
	var err error

	switch encoding {
	case "base64":
		output = text.Base64Encode(input)
	case "base64url":
		output = text.Base64URLEncode(input)
	case "base32":
		output = text.Base32Encode(input)
	case "hex", "base16":
		output = text.HexEncode(input)
	case "url":
		output = text.URLEncode(input)
	default:
		errorResponse(w, "unsupported encoding: "+encoding, http.StatusBadRequest)
		return
	}

	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"encoding": encoding,
		"input":    input,
		"output":   output,
	})
}

func apiEncodeTextHandler(w http.ResponseWriter, r *http.Request) {
	encoding := strings.ToLower(chi.URLParam(r, "encoding"))
	input := chi.URLParam(r, "input")

	var output string

	switch encoding {
	case "base64":
		output = text.Base64Encode(input)
	case "base64url":
		output = text.Base64URLEncode(input)
	case "base32":
		output = text.Base32Encode(input)
	case "hex", "base16":
		output = text.HexEncode(input)
	case "url":
		output = text.URLEncode(input)
	default:
		textResponse(w, "Error: unsupported encoding")
		return
	}

	textResponse(w, output)
}

func apiDecodeHandler(w http.ResponseWriter, r *http.Request) {
	encoding := strings.ToLower(chi.URLParam(r, "encoding"))
	input := chi.URLParam(r, "input")

	var output string
	var err error

	switch encoding {
	case "base64":
		output, err = text.Base64Decode(input)
	case "base64url":
		output, err = text.Base64URLDecode(input)
	case "base32":
		output, err = text.Base32Decode(input)
	case "hex", "base16":
		output, err = text.HexDecode(input)
	case "url":
		output, err = text.URLDecode(input)
	default:
		errorResponse(w, "unsupported encoding: "+encoding, http.StatusBadRequest)
		return
	}

	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"encoding": encoding,
		"input":    input,
		"output":   output,
	})
}

func apiDecodeTextHandler(w http.ResponseWriter, r *http.Request) {
	encoding := strings.ToLower(chi.URLParam(r, "encoding"))
	input := chi.URLParam(r, "input")

	var output string
	var err error

	switch encoding {
	case "base64":
		output, err = text.Base64Decode(input)
	case "base64url":
		output, err = text.Base64URLDecode(input)
	case "base32":
		output, err = text.Base32Decode(input)
	case "hex", "base16":
		output, err = text.HexDecode(input)
	case "url":
		output, err = text.URLDecode(input)
	default:
		textResponse(w, "Error: unsupported encoding")
		return
	}

	if err != nil {
		textResponse(w, "Error: "+err.Error())
		return
	}

	textResponse(w, output)
}

func apiCaseHandler(w http.ResponseWriter, r *http.Request) {
	style := strings.ToLower(chi.URLParam(r, "style"))
	input := chi.URLParam(r, "input")

	var output string

	switch style {
	case "lower":
		output = text.ToLower(input)
	case "upper":
		output = text.ToUpper(input)
	case "title":
		output = text.ToTitle(input)
	case "camel":
		output = text.ToCamelCase(input)
	case "snake":
		output = text.ToSnakeCase(input)
	case "kebab":
		output = text.ToKebabCase(input)
	default:
		errorResponse(w, "unsupported style: "+style, http.StatusBadRequest)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"style":  style,
		"input":  input,
		"output": output,
	})
}

func apiCaseTextHandler(w http.ResponseWriter, r *http.Request) {
	style := strings.ToLower(chi.URLParam(r, "style"))
	input := chi.URLParam(r, "input")

	var output string

	switch style {
	case "lower":
		output = text.ToLower(input)
	case "upper":
		output = text.ToUpper(input)
	case "title":
		output = text.ToTitle(input)
	case "camel":
		output = text.ToCamelCase(input)
	case "snake":
		output = text.ToSnakeCase(input)
	case "kebab":
		output = text.ToKebabCase(input)
	default:
		textResponse(w, "Error: unsupported style")
		return
	}

	textResponse(w, output)
}

func apiLoremHandler(w http.ResponseWriter, r *http.Request) {
	loremType := chi.URLParam(r, "type")
	if loremType == "" {
		loremType = "paragraphs"
	}

	count := 5
	if c := chi.URLParam(r, "count"); c != "" {
		count, _ = strconv.Atoi(c)
	}

	var result interface{}

	switch loremType {
	case "words":
		result = text.LoremWords(count)
	case "sentences":
		result = text.LoremSentences(count)
	case "paragraphs":
		result = text.LoremParagraphs(count)
	default:
		result = text.LoremParagraphs(count)
	}

	jsonResponse(w, map[string]interface{}{
		"type":  loremType,
		"count": count,
		"text":  result,
	})
}

func apiLoremTextHandler(w http.ResponseWriter, r *http.Request) {
	loremType := chi.URLParam(r, "type")
	if loremType == "" {
		loremType = "paragraphs"
	}

	count := 5
	if c := chi.URLParam(r, "count"); c != "" {
		count, _ = strconv.Atoi(c)
	}

	var result []string

	switch loremType {
	case "words":
		result = text.LoremWords(count)
	case "sentences":
		result = text.LoremSentences(count)
	case "paragraphs":
		result = text.LoremParagraphs(count)
	default:
		result = text.LoremParagraphs(count)
	}

	textResponse(w, strings.Join(result, "\n\n"))
}

func apiTextStatsHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	jsonResponse(w, text.Stats(input.Text))
}

func apiROT13Handler(w http.ResponseWriter, r *http.Request) {
	input := chi.URLParam(r, "input")
	output := text.ROT13(input)

	jsonResponse(w, map[string]interface{}{
		"input":  input,
		"output": output,
	})
}

func apiROT13TextHandler(w http.ResponseWriter, r *http.Request) {
	input := chi.URLParam(r, "input")
	textResponse(w, text.ROT13(input))
}

func apiReverseHandler(w http.ResponseWriter, r *http.Request) {
	input := chi.URLParam(r, "input")
	output := text.Reverse(input)

	jsonResponse(w, map[string]interface{}{
		"input":  input,
		"output": output,
	})
}

func apiReverseTextHandler(w http.ResponseWriter, r *http.Request) {
	input := chi.URLParam(r, "input")
	textResponse(w, text.Reverse(input))
}

// Crypto API handlers
func apiBcryptHandler(w http.ResponseWriter, r *http.Request) {
	password := chi.URLParam(r, "password")
	cost := 12
	// Check URL param first, then query param
	if c := chi.URLParam(r, "cost"); c != "" {
		cost, _ = strconv.Atoi(c)
	} else if c := r.URL.Query().Get("cost"); c != "" {
		cost, _ = strconv.Atoi(c)
	}

	hash, err := crypto.BcryptHash(password, cost)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"algorithm": "bcrypt",
		"cost":      cost,
		"hash":      hash,
	})
}

func apiBcryptVerifyGetHandler(w http.ResponseWriter, r *http.Request) {
	password := chi.URLParam(r, "password")
	hash := chi.URLParam(r, "hash")

	valid := crypto.BcryptVerify(password, hash)
	jsonResponse(w, map[string]interface{}{
		"valid": valid,
	})
}

func apiBcryptVerifyHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
		Hash     string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	valid := crypto.BcryptVerify(input.Password, input.Hash)

	jsonResponse(w, map[string]interface{}{
		"valid": valid,
		"cost":  crypto.BcryptCost(input.Hash),
	})
}

func apiPasswordHandler(w http.ResponseWriter, r *http.Request) {
	length := 16
	if l := chi.URLParam(r, "length"); l != "" {
		length, _ = strconv.Atoi(l)
	}

	opts := crypto.DefaultPasswordOptions()

	if r.URL.Query().Get("uppercase") == "false" {
		opts.Uppercase = false
	}
	if r.URL.Query().Get("lowercase") == "false" {
		opts.Lowercase = false
	}
	if r.URL.Query().Get("numbers") == "false" {
		opts.Numbers = false
	}
	if r.URL.Query().Get("symbols") == "false" {
		opts.Symbols = false
	}
	if r.URL.Query().Get("exclude_similar") == "true" {
		opts.ExcludeSimilar = true
	}

	password, err := crypto.GeneratePassword(length, opts)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"password": password,
		"length":   length,
	})
}

func apiPasswordTextHandler(w http.ResponseWriter, r *http.Request) {
	length := 16
	if l := chi.URLParam(r, "length"); l != "" {
		length, _ = strconv.Atoi(l)
	}

	password, err := crypto.GeneratePassword(length, crypto.DefaultPasswordOptions())
	if err != nil {
		textResponse(w, "Error: "+err.Error())
		return
	}

	textResponse(w, password)
}

func apiPINHandler(w http.ResponseWriter, r *http.Request) {
	length := 4
	if l := chi.URLParam(r, "length"); l != "" {
		length, _ = strconv.Atoi(l)
	}

	pin, err := crypto.GeneratePIN(length)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"pin":    pin,
		"length": length,
	})
}

func apiPINTextHandler(w http.ResponseWriter, r *http.Request) {
	length := 4
	if l := chi.URLParam(r, "length"); l != "" {
		length, _ = strconv.Atoi(l)
	}

	pin, err := crypto.GeneratePIN(length)
	if err != nil {
		textResponse(w, "Error: "+err.Error())
		return
	}

	textResponse(w, pin)
}

func apiTOTPGenerateHandler(w http.ResponseWriter, r *http.Request) {
	issuer := r.URL.Query().Get("issuer")
	if issuer == "" {
		issuer = "CasTools"
	}
	account := r.URL.Query().Get("account")
	if account == "" {
		account = "user@example.com"
	}

	secret, err := crypto.GenerateTOTPSecret(20)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	code, _ := crypto.GenerateTOTP(secret, 6, 30)
	uri := crypto.GenerateTOTPURI(secret, issuer, account)

	jsonResponse(w, map[string]interface{}{
		"secret":       secret,
		"uri":          uri,
		"current_code": code,
		"issuer":       issuer,
		"account":      account,
		"algorithm":    "SHA1",
		"digits":       6,
		"period":       30,
	})
}

func apiTOTPCodeHandler(w http.ResponseWriter, r *http.Request) {
	secret := chi.URLParam(r, "secret")

	code, err := crypto.GenerateTOTP(secret, 6, 30)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	remaining := 30 - (time.Now().Unix() % 30)

	jsonResponse(w, map[string]interface{}{
		"code":              code,
		"remaining_seconds": remaining,
		"period":            30,
	})
}

func apiTOTPCodeTextHandler(w http.ResponseWriter, r *http.Request) {
	secret := chi.URLParam(r, "secret")

	code, err := crypto.GenerateTOTP(secret, 6, 30)
	if err != nil {
		textResponse(w, "Error: "+err.Error())
		return
	}

	textResponse(w, code)
}

func apiTOTPVerifyHandler(w http.ResponseWriter, r *http.Request) {
	secret := chi.URLParam(r, "secret")
	code := chi.URLParam(r, "code")

	valid := crypto.VerifyTOTP(secret, code, 6, 30, 1)

	jsonResponse(w, map[string]interface{}{
		"valid": valid,
	})
}

func apiRandomBytesHandler(w http.ResponseWriter, r *http.Request) {
	count := 32
	if c := chi.URLParam(r, "count"); c != "" {
		count, _ = strconv.Atoi(c)
	}

	bytes, err := crypto.RandomBytes(count)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"bytes":  bytes,
		"hex":    hex.EncodeToString(bytes),
		"length": len(bytes),
	})
}

func apiRandomHexHandler(w http.ResponseWriter, r *http.Request) {
	count := 32
	if c := chi.URLParam(r, "count"); c != "" {
		count, _ = strconv.Atoi(c)
	}

	bytes, err := crypto.RandomBytes(count)
	if err != nil {
		textResponse(w, "Error: "+err.Error())
		return
	}

	textResponse(w, hex.EncodeToString(bytes))
}

func apiPasswordStrengthHandler(w http.ResponseWriter, r *http.Request) {
	password := chi.URLParam(r, "password")
	jsonResponse(w, crypto.PasswordStrength(password))
}

func apiPasswordStrengthPostHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	jsonResponse(w, crypto.PasswordStrength(input.Password))
}

// DateTime API handlers
func apiDateTimeNowHandler(w http.ResponseWriter, r *http.Request) {
	timezone := chi.URLParam(r, "timezone")
	if timezone == "" {
		timezone = r.URL.Query().Get("timezone")
	}

	result, err := datetime.Now(timezone)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, result)
}

func apiDateTimeNowTextHandler(w http.ResponseWriter, r *http.Request) {
	textResponse(w, strconv.FormatInt(time.Now().Unix(), 10))
}

func apiTimestampHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"unix":    time.Now().Unix(),
		"unix_ms": time.Now().UnixMilli(),
		"unix_ns": time.Now().UnixNano(),
	})
}

func apiTimestampTextHandler(w http.ResponseWriter, r *http.Request) {
	textResponse(w, strconv.FormatInt(time.Now().Unix(), 10))
}

func apiConvertTimestampHandler(w http.ResponseWriter, r *http.Request) {
	timestamp, err := strconv.ParseInt(chi.URLParam(r, "timestamp"), 10, 64)
	if err != nil {
		errorResponse(w, "invalid timestamp", http.StatusBadRequest)
		return
	}

	timezone := chi.URLParam(r, "timezone")

	result, err := datetime.FromUnix(timestamp, timezone)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, result)
}

func apiToUnixHandler(w http.ResponseWriter, r *http.Request) {
	dt := chi.URLParam(r, "datetime")

	timestamp, err := datetime.ToUnix(dt)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"datetime": dt,
		"unix":     timestamp,
	})
}

func apiAddDurationHandler(w http.ResponseWriter, r *http.Request) {
	timestamp, err := strconv.ParseInt(chi.URLParam(r, "timestamp"), 10, 64)
	if err != nil {
		errorResponse(w, "invalid timestamp", http.StatusBadRequest)
		return
	}

	duration := chi.URLParam(r, "duration")

	result, err := datetime.AddDuration(timestamp, duration)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, result)
}

func apiDiffHandler(w http.ResponseWriter, r *http.Request) {
	timestamp1, err := strconv.ParseInt(chi.URLParam(r, "timestamp1"), 10, 64)
	if err != nil {
		errorResponse(w, "invalid timestamp1", http.StatusBadRequest)
		return
	}

	timestamp2, err := strconv.ParseInt(chi.URLParam(r, "timestamp2"), 10, 64)
	if err != nil {
		errorResponse(w, "invalid timestamp2", http.StatusBadRequest)
		return
	}

	jsonResponse(w, datetime.Diff(timestamp1, timestamp2))
}

func apiTimezonesHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"timezones": datetime.Timezones(),
	})
}

func apiTimezoneInfoHandler(w http.ResponseWriter, r *http.Request) {
	timezone := chi.URLParam(r, "timezone")

	result, err := datetime.TimezoneInfo(timezone)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, result)
}

func apiConvertTimezoneHandler(w http.ResponseWriter, r *http.Request) {
	timestamp, err := strconv.ParseInt(chi.URLParam(r, "timestamp"), 10, 64)
	if err != nil {
		errorResponse(w, "invalid timestamp", http.StatusBadRequest)
		return
	}

	from := chi.URLParam(r, "from")
	to := chi.URLParam(r, "to")

	result, err := datetime.ConvertTimezone(timestamp, from, to)
	if err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, result)
}

// Middleware functions

// requestIDMiddleware adds a unique request ID to each request
func getUptime() string {
	d := time.Since(startTime)

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
