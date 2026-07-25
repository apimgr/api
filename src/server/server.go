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

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/graphql"
	"github.com/apimgr/api/src/metrics"
	"github.com/apimgr/api/src/server/handler"
	"github.com/apimgr/api/src/service/crypto"
	"github.com/apimgr/api/src/service/datetime"
	"github.com/apimgr/api/src/service/text"
	"github.com/apimgr/api/src/swagger"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

//go:embed template
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Version information
var (
	Version   = "1.0.0"
	CommitID  = "unknown"
	BuildDate = "unknown"
)

// New creates a new HTTP server
func New(cfg *config.Config) *http.Server {
	// Initialize page templates
	if err := initTemplates(); err != nil {
		panic(fmt.Sprintf("Failed to parse templates: %v", err))
	}

	r := chi.NewRouter()

	// Core middleware
	r.Use(realIPMiddleware(cfg))
	r.Use(requestIDMiddleware)
	r.Use(loggingMiddleware)
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
	r.Get("/network", categoryPageHandler(cfg, "network", "Network Tools", "IP lookup, DNS, WHOIS, SSL, and network utilities"))
	r.Get("/categories", categoriesPageHandler(cfg))
	r.Get("/convert", categoryPageHandler(cfg, "convert", "Unit Conversion", "Length, weight, temperature, currency, and color conversions"))
	r.Get("/dev", categoryPageHandler(cfg, "dev", "Developer Tools", "HTTP echo, formatters, minifiers, and development utilities"))
	r.Get("/docker", categoryPageHandler(cfg, "docker", "Docker Tools", "Docker run/compose conversion, linting, and validation"))
	r.Get("/fun", categoryPageHandler(cfg, "fun", "Fun & Content", "Jokes, quotes, facts, and random entertainment"))
	r.Get("/generate", categoryPageHandler(cfg, "generate", "Generators", "QR codes, barcodes, configs, avatars, and more"))
	r.Get("/geo", categoryPageHandler(cfg, "geo", "Geolocation", "IP lookup, geocoding, distance calculations, and geo encoding"))
	r.Get("/image", categoryPageHandler(cfg, "image", "Images", "Image resize, crop, filters, and manipulation"))
	r.Get("/language", categoryPageHandler(cfg, "language", "Language Tools", "Spell checking, dictionary, thesaurus, and language detection"))
	r.Get("/lorem", categoryPageHandler(cfg, "lorem", "Lorem & Fake Data", "Generate realistic fake data for testing and development"))
	r.Get("/math", categoryPageHandler(cfg, "math", "Math & Numbers", "Calculations, statistics, primes, and matrix operations"))
	r.Get("/osint", categoryPageHandler(cfg, "osint", "OSINT Tools", "Email intelligence, domain research, and username searches"))
	r.Get("/parse", categoryPageHandler(cfg, "parse", "Parsers", "Parsing JSON, YAML, XML, CSV, and format conversions"))
	r.Get("/research", categoryPageHandler(cfg, "research", "Research Tools", "Content extraction, summarization, and citations"))
	r.Get("/system", categoryPageHandler(cfg, "system", "Health & System", "Server health checks, system information, and version details"))
	r.Get("/testing", categoryPageHandler(cfg, "testing", "Testing Tools", "Mocks, fixtures, assertions, and API testing"))
	r.Get("/validate", categoryPageHandler(cfg, "validate", "Validators", "Validating emails, phones, URLs, credit cards, and more"))
	r.Get("/weather", categoryPageHandler(cfg, "weather", "Weather", "Current weather, forecasts, and air quality data"))

	// Per-tool detail pages (see TODO.AI.md for remaining sub-tool wiring)
	for _, tp := range toolPages() {
		r.Get("/"+tp.category+"/"+tp.tool, toolPageHandler(cfg, tp.category, tp.tool, tp.title, tp.description))
	}

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

	// Health check (PART 13: frontend route, optional root alias, API
	// route, and unversioned machine-friendly alias — all mount the same
	// handler so content negotiation behaves identically)
	r.Get("/server/healthz", handler.ServerHealthz(cfg, true))
	if cfg.Server.Healthz.Root.Enabled {
		r.Get("/healthz", handler.ServerHealthz(cfg, true))
	}
	r.Get("/api/v1/server/healthz", handler.ServerHealthz(cfg, false))
	r.Get("/api/healthz", handler.ServerHealthz(cfg, false))

	// Metrics endpoint (Prometheus-compatible, PART 20 — internal only,
	// no JSON alias: the spec defines a single Prometheus text-format
	// endpoint, not a versioned API route)
	r.Get("/metrics", metricsPrometheusHandler)

	// Special files
	r.Get("/robots.txt", robotsHandler(cfg))
	r.Get("/security.txt", securityHandler(cfg))
	r.Get("/.well-known/security.txt", securityHandler(cfg))
	r.Get("/manifest.json", manifestHandler(cfg))

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Version
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

			// Compress/decompress
			r.Post("/compress", apiTextCompressHandler)

			// Diff
			r.Post("/diff", apiTextDiffHandler)

			// Extract
			r.Post("/extract", apiTextExtractHandler)

			// NanoID
			r.Get("/nanoid", apiTextNanoIDHandler)

			// ULID
			r.Get("/ulid", apiTextULIDHandler)

			// Regex
			r.Post("/regex", apiTextRegexHandler)
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

			// Hash (alias of /text/hash for the crypto tool page)
			r.Get("/hash/{algorithm}/{input}", apiHashHandler)
			r.Get("/hash/{algorithm}/{input}.txt", apiHashTextHandler)

			// JWT decode (header/payload only, no signature verification)
			r.Get("/jwt/{token}", apiCryptoJWTDecodeHandler)

			// AES-256-GCM encrypt/decrypt
			r.Post("/encrypt", apiCryptoEncryptHandler)
			r.Post("/decrypt", apiCryptoDecryptHandler)

			// RSA keypair generation / RSA-OAEP encrypt / decrypt
			r.Post("/rsa", apiCryptoRSAHandler)

			// HMAC
			r.Post("/hmac", apiCryptoHMACHandler)
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

		// Network utilities
		r.Route("/network", func(r chi.Router) {
			r.Get("/ip", apiNetworkCallerHandler)
			r.Get("/headers", apiNetworkCallerHandler)
			r.Get("/user-agent", apiNetworkUserAgentHandler)
			r.Get("/mac/{mac}", apiNetworkMACVendorHandler)
			r.Get("/subnet", apiNetworkSubnetHandler)
			r.Get("/ula", apiNetworkULAHandler)
			r.Get("/port", apiNetworkPortHandler)
			r.Get("/dns/{domain}", apiNetworkDNSHandler)
			r.Get("/dns/{domain}/{type}", apiNetworkDNSHandler)
			r.Get("/ping", apiNetworkPingHandler)
			r.Get("/ssl", apiNetworkSSLHandler)
			r.Get("/url", apiNetworkURLHandler)
			r.Get("/whois", apiNetworkWhoisHandler)
			r.Get("/traceroute", apiNetworkTracerouteHandler)
		})

		// Docker utilities
		r.Route("/docker", func(r chi.Router) {
			r.Get("/version", apiDockerVersionHandler)
			r.Get("/port-mapping", apiDockerPortMappingHandler)
			r.Get("/volume-helper", apiDockerVolumeHandler)
			r.Post("/dockerfile-generate", apiDockerfileGenerateHandler)
		})

		// Weather
		r.Route("/weather", func(r chi.Router) {
			r.Get("/current/{location}", apiWeatherCurrentHandler)
			r.Get("/forecast/{location}", apiWeatherForecastHandler)
		})

		// Geolocation
		r.Route("/geo", func(r chi.Router) {
			r.Get("/ip/{ip}", apiGeoIPHandler)
			r.Get("/distance", apiGeoDistanceHandler)
			r.Get("/bearing", apiGeoBearingHandler)
			r.Get("/midpoint", apiGeoMidpointHandler)
		})

		// Math
		r.Route("/math", func(r chi.Router) {
			r.Get("/calculate", apiMathCalculateHandler)
			r.Get("/prime/{n}", apiMathPrimeHandler)
			r.Get("/random/{min}/{max}", apiMathRandomHandler)
			r.Get("/stats", apiMathStatsHandler)
			r.Get("/fibonacci", apiMathFibonacciHandler)
			r.Get("/base", apiMathBaseHandler)
			r.Post("/matrix", apiMathMatrixHandler)
			r.Get("/sequence", apiMathSequenceHandler)
		})

		// Unit Conversion
		r.Route("/convert", func(r chi.Router) {
			r.Get("/length/{value}/{from}/{to}", apiConvertLengthHandler)
			r.Get("/temperature/{value}/{from}/{to}", apiConvertTemperatureHandler)
			r.Get("/weight/{value}/{from}/{to}", apiConvertWeightHandler)
			r.Get("/volume/{value}/{from}/{to}", apiConvertVolumeHandler)
			r.Get("/time/{value}/{from}/{to}", apiConvertTimeHandler)
			r.Get("/area/{value}/{from}/{to}", apiConvertAreaHandler)
			r.Get("/data/{value}/{from}/{to}", apiConvertDataHandler)
			r.Get("/energy/{value}/{from}/{to}", apiConvertEnergyHandler)
			r.Get("/pressure/{value}/{from}/{to}", apiConvertPressureHandler)
			r.Get("/speed/{value}/{from}/{to}", apiConvertSpeedHandler)
			r.Get("/color", apiConvertColorHandler)
			r.Get("/currency", apiConvertCurrencyHandler)
		})

		// Generators
		r.Route("/generate", func(r chi.Router) {
			r.Get("/qr", apiGenerateQRHandler)
		})

		// Validators
		r.Route("/validate", func(r chi.Router) {
			r.Post("/email", apiValidateEmailHandler)
			r.Post("/credit-card", apiValidateCreditCardHandler)
			r.Post("/domain", apiValidateDomainHandler)
			r.Post("/ip", apiValidateIPHandler)
			r.Post("/json", apiValidateJSONHandler)
			r.Post("/mac", apiValidateMACHandler)
			r.Post("/phone", apiValidatePhoneHandler)
			r.Post("/url", apiValidateURLHandler)
			r.Post("/uuid", apiValidateUUIDHandler)
			r.Post("/iban", apiValidateIBANHandler)
			r.Post("/isbn", apiValidateISBNHandler)
			r.Post("/vat", apiValidateVATHandler)
		})

		// Parsers
		r.Route("/parse", func(r chi.Router) {
			r.Post("/json", apiParseJSONHandler)
			r.Post("/xml", apiParseXMLHandler)
			r.Post("/csv", apiParseCSVHandler)
			r.Get("/jwt/{token}", apiParseJWTHandler)
		})

		// Language Tools
		r.Route("/language", func(r chi.Router) {
			r.Post("/detect", apiLanguageDetectHandler)
			r.Get("/phonetic", apiLanguagePhoneticHandler)
			r.Post("/word-count", apiLanguageWordCountHandler)
		})

		// Testing Tools
		r.Route("/test", func(r chi.Router) {
			r.Get("/http", apiTestHTTPHandler)
			r.Post("/assert", apiTestAssertHandler)
			r.Get("/fixture/{type}", apiTestFixtureHandler)
			r.Get("/fake-data", apiTestFakeDataHandler)
		})

		// OSINT Tools
		r.Route("/osint", func(r chi.Router) {
			r.Get("/email/{email}", apiOsintEmailHandler)
			r.Get("/domain/{domain}", apiOsintDomainHandler)
			r.Get("/ip/{ip}", apiOsintIPHandler)
			r.Get("/cert/{domain}", apiOsintCertHandler)
		})

		// Research Tools
		r.Route("/research", func(r chi.Router) {
			r.Post("/citation", apiResearchCitationHandler)
			r.Get("/doi/*", apiResearchDOIHandler)
			r.Post("/extract", apiResearchExtractHandler)
		})

		// Fun & Content
		r.Route("/fun", func(r chi.Router) {
			r.Get("/joke", apiFunJokeHandler)
			r.Get("/fortune", apiFunFortuneHandler)
		})

		// Lorem & Fake Data
		r.Route("/lorem", func(r chi.Router) {
			r.Get("/person", apiLoremPersonHandler)
			r.Get("/address", apiLoremAddressHandler)
			r.Get("/company", apiLoremCompanyHandler)
		})

		// Developer Tools
		r.Route("/dev", func(r chi.Router) {
			r.Post("/format/json", apiDevFormatJSONHandler)
			r.Post("/base64", apiDevBase64Handler)
			r.Post("/url-encode", apiDevURLEncodeHandler)

			// Regex (shared with /text/regex)
			r.Post("/regex", apiTextRegexHandler)
		})

		// Images
		r.Route("/image", func(r chi.Router) {
			r.Get("/placeholder/{width}/{height}", apiImagePlaceholderHandler)
			r.Post("/resize", apiImageResizeHandler)
			r.Post("/crop", apiImageCropHandler)
			r.Post("/metadata", apiImageMetadataHandler)
			r.Post("/convert", apiImageConvertHandler)
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
	CommitID          string
	BuildDate         string
	Mode              string
	SecurityEmail     string
	UpdatedAt         string
	RateLimitRequests int
	RateLimitWindow   int
	Categories        []CategoryInfo
}

// CategoryInfo describes one tool category shown on the /categories index page
type CategoryInfo struct {
	Path        string
	Icon        string
	Name        string
	Description string
	Count       int
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
	publicPages := []string{
		"index", "text", "crypto", "datetime", "network", "openapi", "error", "healthz", "about", "privacy", "contact", "help",
		"categories", "convert", "dev", "docker", "fun", "generate", "geo", "image", "language", "lorem", "math",
		"osint", "parse", "research", "system", "testing", "validate", "weather",
	}

	for _, page := range publicPages {
		tmpl, err := template.ParseFS(templatesFS,
			"template/layout/public.tmpl",
			"template/partial/*.tmpl",
			"template/partial/public/*.tmpl",
			fmt.Sprintf("template/page/%s.tmpl", page),
		)
		if err != nil {
			return fmt.Errorf("failed to parse %s template: %w", page, err)
		}
		pageTemplates[page] = tmpl
	}

	// Per-tool detail pages nested under template/page/tools/{category}/{tool}.tmpl,
	// registered under composite keys like "crypto/hash"
	for _, tp := range toolPages() {
		key := tp.category + "/" + tp.tool
		tmpl, err := template.ParseFS(templatesFS,
			"template/layout/public.tmpl",
			"template/partial/*.tmpl",
			"template/partial/public/*.tmpl",
			fmt.Sprintf("template/page/tools/%s.tmpl", key),
		)
		if err != nil {
			return fmt.Errorf("failed to parse %s tool template: %w", key, err)
		}
		pageTemplates[key] = tmpl
	}

	return nil
}

// toolPage describes one per-tool detail page nested under a category
type toolPage struct {
	category    string
	tool        string
	title       string
	description string
}

// toolPages lists the per-tool detail pages that currently have templates on
// disk under template/page/tools/{category}/{tool}.tmpl (PART 16 frontend
// route mirrors API route rule). Most of the ~240 sub-tool pages linked from
// the 21 category pages have neither a template nor a route yet — see
// TODO.AI.md for the remaining wiring work.
func toolPages() []toolPage {
	return []toolPage{
		{category: "crypto", tool: "hash", title: "Hash Generator", description: "Generate cryptographic hashes using various algorithms (MD5, SHA-1, SHA-256, SHA-512, BLAKE3)"},
		{category: "crypto", tool: "jwt", title: "JWT Decoder", description: "Decode and inspect JSON Web Tokens (JWT). View header, payload, and signature details"},
		{category: "crypto", tool: "totp", title: "TOTP Generator", description: "Generate a time-based one-time password secret, provisioning URI, and current code"},
		{category: "crypto", tool: "random", title: "Random Bytes", description: "Generate cryptographically secure random bytes as raw values and hex encoding"},
		{category: "crypto", tool: "password", title: "Password Generator", description: "Generate secure random passwords with customizable length and character sets"},
		{category: "crypto", tool: "encrypt", title: "AES Encrypt", description: "Encrypt text with AES-256-GCM using a passphrase-derived key"},
		{category: "crypto", tool: "decrypt", title: "AES Decrypt", description: "Decrypt AES-256-GCM ciphertext using the original passphrase"},
		{category: "crypto", tool: "rsa", title: "RSA Encrypt/Decrypt", description: "Generate an RSA keypair, or encrypt/decrypt text with RSA-OAEP (SHA-256)"},
		{category: "crypto", tool: "hmac", title: "HMAC Generator", description: "Compute an HMAC (SHA-1 or SHA-256) of a message using a secret key"},
		{category: "datetime", tool: "now", title: "Current Time", description: "Get the current timestamp in multiple formats including Unix, ISO 8601, and human-readable"},
		{category: "network", tool: "ip", title: "IP Address Lookup", description: "Get detailed information about any IP address including location, ISP, and network details"},
		{category: "network", tool: "headers", title: "Request Headers", description: "Inspect the caller-identifying headers sent with the request"},
		{category: "network", tool: "dns", title: "DNS Lookup", description: "Query DNS records for any domain. Supports A, AAAA, CNAME, MX, TXT, and NS"},
		{category: "text", tool: "uuid", title: "UUID Generator", description: "Generate UUIDs (v1, v3, v4, v5, v6, v7) for use in applications and databases"},
		{category: "text", tool: "hash", title: "Hash Generator", description: "Generate cryptographic hashes of arbitrary text (MD5, SHA-1, SHA-256, SHA-512, BLAKE3)"},
		{category: "crypto", tool: "bcrypt", title: "Bcrypt Hash", description: "Hash a password using bcrypt with a configurable cost factor"},
		{category: "crypto", tool: "pin", title: "PIN Generator", description: "Generate a random numeric PIN of a given length"},
		{category: "crypto", tool: "password-strength", title: "Password Strength Checker", description: "Check the estimated strength of a password"},
		{category: "datetime", tool: "timestamp", title: "Unix Timestamp", description: "Get the current Unix timestamp"},
		{category: "network", tool: "user-agent", title: "User-Agent Lookup", description: "Inspect the User-Agent header sent with the request"},
		{category: "network", tool: "mac", title: "MAC Vendor Lookup", description: "Look up the hardware vendor for a MAC address"},
		{category: "docker", tool: "version", title: "Docker Version", description: "Look up the latest available Docker Engine version"},
		{category: "docker", tool: "port-mapping", title: "Port Mapping Helper", description: "Format or parse a Docker host:container port mapping"},
		{category: "docker", tool: "volume-helper", title: "Volume Mount Helper", description: "Format a Docker host:container volume mount string"},
		{category: "docker", tool: "dockerfile-generate", title: "Dockerfile Generator", description: "Generate a Dockerfile from a structured configuration"},
		{category: "network", tool: "subnet", title: "Subnet Calculator", description: "Calculate network, broadcast, and host range details for a CIDR block"},
		{category: "network", tool: "ula", title: "ULA Generator", description: "Generate an RFC 4193 IPv6 unique-local-address prefix"},
		{category: "network", tool: "port", title: "Random Port", description: "Suggest a random unprivileged TCP/UDP port"},
		{category: "network", tool: "ping", title: "Ping Tool", description: "Measure TCP connect round-trip latency to a host"},
		{category: "network", tool: "ssl", title: "SSL Certificate Info", description: "Check SSL certificate subject, issuer, and validity for a host"},
		{category: "network", tool: "url", title: "URL Parser", description: "Parse and analyze a URL into its component parts"},
		{category: "network", tool: "whois", title: "WHOIS Lookup", description: "Look up domain and IP WHOIS registration information"},
		{category: "weather", tool: "current", title: "Current Weather", description: "Get current weather conditions for a location"},
		{category: "weather", tool: "forecast", title: "Weather Forecast", description: "Get a 1-16 day weather forecast for a location"},
		{category: "geo", tool: "ip", title: "IP Geolocation", description: "Look up geolocation details for a public IP address"},
		{category: "geo", tool: "distance", title: "Distance Calculator", description: "Calculate the great-circle distance between two coordinates"},
		{category: "geo", tool: "bearing", title: "Bearing Calculator", description: "Calculate the initial compass bearing from one coordinate to another"},
		{category: "geo", tool: "midpoint", title: "Midpoint Calculator", description: "Calculate the geographic midpoint between two coordinates"},
		{category: "convert", tool: "length", title: "Length Converter", description: "Convert a length value between feet, meters, inches, centimeters, miles, and kilometers"},
		{category: "convert", tool: "temperature", title: "Temperature Converter", description: "Convert a temperature value between Celsius, Fahrenheit, and Kelvin"},
		{category: "convert", tool: "weight", title: "Weight Converter", description: "Convert a weight value between pounds, kilograms, ounces, and grams"},
		{category: "convert", tool: "volume", title: "Volume Converter", description: "Convert a volume value between gallons and liters"},
		{category: "convert", tool: "time", title: "Time Converter", description: "Convert a time value between seconds, minutes, hours, and days"},
		{category: "convert", tool: "area", title: "Area Converter", description: "Convert an area value between square meters, square feet, acres, and hectares"},
		{category: "convert", tool: "data", title: "Data Size Converter", description: "Convert a data size value between bytes, kilobytes, megabytes, gigabytes, and terabytes"},
		{category: "convert", tool: "energy", title: "Energy Converter", description: "Convert an energy value between joules, calories, and kilowatt-hours"},
		{category: "convert", tool: "pressure", title: "Pressure Converter", description: "Convert a pressure value between pascals, bar, PSI, and atmospheres"},
		{category: "convert", tool: "speed", title: "Speed Converter", description: "Convert a speed value between mph, km/h, m/s, and knots"},
		{category: "convert", tool: "color", title: "Color Converter", description: "Convert a color between hex, RGB, and HSL representations"},
		{category: "convert", tool: "currency", title: "Currency Converter", description: "Convert an amount between currencies using live ECB reference rates"},
		{category: "math", tool: "calculate", title: "Calculator", description: "Run add/subtract/multiply/divide and other math operations"},
		{category: "math", tool: "gcd", title: "GCD Calculator", description: "Find the greatest common divisor of two integers"},
		{category: "math", tool: "percentage", title: "Percentage Calculator", description: "Calculate a percentage of a value or the percentage change between two values"},
		{category: "math", tool: "logarithm", title: "Logarithm Calculator", description: "Calculate natural, base-10, or base-2 logarithms"},
		{category: "math", tool: "trigonometry", title: "Trigonometry Calculator", description: "Calculate sine, cosine, and tangent of an angle in radians"},
		{category: "math", tool: "prime", title: "Prime Checker", description: "Check whether a number is prime"},
		{category: "math", tool: "random", title: "Random Number Generator", description: "Generate a random integer within a range"},
		{category: "math", tool: "stats", title: "Statistics Calculator", description: "Calculate min, max, sum, average, and median of a list of numbers"},
		{category: "math", tool: "fibonacci", title: "Fibonacci Generator", description: "Generate the first N numbers of the Fibonacci sequence"},
		{category: "math", tool: "base", title: "Base Converter", description: "Convert a number between bases 2-36"},
		{category: "math", tool: "matrix", title: "Matrix Calculator", description: "Add, multiply, or find the determinant of matrices"},
		{category: "math", tool: "sequence", title: "Sequence Generator", description: "Generate arithmetic or geometric number sequences"},
		{category: "parse", tool: "json", title: "JSON Parser", description: "Parse a raw JSON document and view the decoded structure"},
		{category: "parse", tool: "xml", title: "XML Parser", description: "Parse a raw XML document and view the decoded structure"},
		{category: "parse", tool: "csv", title: "CSV Parser", description: "Parse a CSV document (first row as headers) into structured rows"},
		{category: "parse", tool: "jwt", title: "JWT Parser", description: "Decode and inspect the header, payload, and signature of a JSON Web Token"},
		{category: "research", tool: "citation", title: "Citation Formatter", description: "Format a reference into an APA, MLA, or Chicago style citation"},
		{category: "research", tool: "doi", title: "DOI Validator", description: "Validate a DOI and get its canonical https://doi.org resolver URL"},
		{category: "fun", tool: "joke", title: "Random Joke", description: "Get a random joke type paired with a fortune-cookie style saying"},
		{category: "fun", tool: "fortune", title: "Random Fortune", description: "Get a single random fortune-cookie style saying"},
		{category: "lorem", tool: "person", title: "Fake Person", description: "Generate a fake person with a name, email, and phone number"},
		{category: "lorem", tool: "address", title: "Fake Address", description: "Generate a fake street address"},
		{category: "lorem", tool: "company", title: "Fake Company", description: "Generate a fake company name and catchphrase"},
		{category: "testing", tool: "http", title: "Mock HTTP Response", description: "Generate a mock API response fixture and measure execution time"},
		{category: "testing", tool: "assertions", title: "Assertion Runner", description: "Run an equal, not_equal, contains, true, or false assertion and get a pass/fail result"},
		{category: "testing", tool: "fixtures", title: "Test Fixture Generator", description: "Generate a named test fixture (user, api_response, or custom type)"},
		{category: "testing", tool: "fake-data", title: "Fake Data Generator", description: "Generate a fake test email, username, or mock user"},
		{category: "osint", tool: "email", title: "Email Intelligence", description: "Validate an email address and check for MX records"},
		{category: "osint", tool: "domain", title: "WHOIS Lookup", description: "Look up registrar, creation/expiry dates, and nameservers for a domain"},
		{category: "osint", tool: "ip", title: "IP Intelligence", description: "Look up geolocation and ISP information for a public IP address"},
		{category: "osint", tool: "cert", title: "TLS Certificate Lookup", description: "Inspect a domain's TLS certificate details"},
		{category: "dev", tool: "format-json", title: "Format JSON", description: "Pretty-print and re-indent a raw JSON document"},
		{category: "dev", tool: "base64", title: "Base64 Encode/Decode", description: "Encode or decode text using standard or URL-safe base64"},
		{category: "dev", tool: "url-encode", title: "URL Encode/Decode", description: "Encode or decode text for safe use in URLs"},
		{category: "validate", tool: "email", title: "Validate Email", description: "Check whether an email address is correctly formatted"},
		{category: "validate", tool: "credit-card", title: "Validate Credit Card", description: "Check a credit card number against the Luhn algorithm"},
		{category: "validate", tool: "domain", title: "Validate Domain", description: "Check whether a domain name is correctly formatted"},
		{category: "validate", tool: "ip", title: "Validate IP Address", description: "Check whether a string is a valid IPv4 or IPv6 address"},
		{category: "validate", tool: "json", title: "Validate JSON", description: "Check whether a request body is well-formed JSON"},
		{category: "validate", tool: "mac", title: "Validate MAC Address", description: "Check whether a string is a correctly formatted MAC address"},
		{category: "validate", tool: "phone", title: "Validate Phone Number", description: "Check whether a string is a correctly formatted phone number"},
		{category: "validate", tool: "url", title: "Validate URL", description: "Check whether a string is a correctly formatted URL"},
		{category: "validate", tool: "uuid", title: "Validate UUID", description: "Check whether a string is a correctly formatted UUID"},
		{category: "validate", tool: "iban", title: "Validate IBAN", description: "Check an IBAN against the ISO 13616 mod-97 checksum"},
		{category: "validate", tool: "isbn", title: "Validate ISBN", description: "Check an ISBN-10 or ISBN-13 book number's check digit"},
		{category: "validate", tool: "vat", title: "Validate VAT Number", description: "Check an EU/UK/CH/NO VAT number's structural format"},
		{category: "image", tool: "placeholder", title: "Placeholder Image", description: "Generate a placeholder image of any size, format, and background color"},
		{category: "image", tool: "resize", title: "Resize Image", description: "Upload an image and resize it to a new width and height"},
		{category: "image", tool: "crop", title: "Crop Image", description: "Upload an image and crop a region out of it"},
		{category: "image", tool: "metadata", title: "Image Metadata", description: "Upload an image and inspect its dimensions, format, and size"},
		{category: "image", tool: "convert", title: "Convert Image Format", description: "Upload an image and convert it to PNG, JPEG, or GIF"},
		{category: "language", tool: "phonetic", title: "Phonetic Encoding", description: "Generate the Soundex and Metaphone phonetic codes for a word"},
		{category: "language", tool: "word-count", title: "Word Count", description: "Count words, characters, lines, and sentences in text"},
		{category: "text", tool: "encode", title: "Encode", description: "Encode text using base64, base32, hex, URL, or HTML encoding"},
		{category: "text", tool: "decode", title: "Decode", description: "Decode text encoded with base64, base32, hex, URL, or HTML encoding"},
		{category: "text", tool: "case", title: "Case Converter", description: "Convert text between upper, lower, title, camel, snake, kebab, and other case styles"},
		{category: "text", tool: "lorem", title: "Lorem Ipsum", description: "Generate placeholder Lorem Ipsum text by word, sentence, or paragraph"},
		{category: "datetime", tool: "convert", title: "Convert Timestamp", description: "Convert a Unix timestamp to a human-readable date/time in any timezone"},
		{category: "datetime", tool: "unix", title: "To Unix Timestamp", description: "Convert a human-readable date/time string to a Unix timestamp"},
		{category: "datetime", tool: "add", title: "Add Duration", description: "Add a duration to a Unix timestamp"},
		{category: "datetime", tool: "diff", title: "Timestamp Diff", description: "Compute the difference between two Unix timestamps"},
		{category: "text", tool: "compress", title: "Compress/Decompress", description: "Compress or decompress text using gzip, zlib, or flate/deflate"},
		{category: "text", tool: "diff", title: "Text Diff", description: "Compare two blocks of text and show the differences"},
		{category: "text", tool: "extract", title: "Extract", description: "Extract emails, URLs, IP addresses, or phone numbers from text"},
		{category: "text", tool: "nanoid", title: "NanoID Generator", description: "Generate a compact, URL-friendly unique ID"},
		{category: "text", tool: "ulid", title: "ULID Generator", description: "Generate a sortable, timestamp-based unique ID"},
		{category: "text", tool: "regex", title: "Regex Tester", description: "Test a regular expression against text: match, replace, or explain"},
		{category: "dev", tool: "regex", title: "Regex Tester", description: "Test a regular expression against text: match, replace, or explain"},
	}
}

// toolPageHandler renders a per-tool detail page under a category
func toolPageHandler(cfg *config.Config, category, tool, title, description string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, category)
		data.PageTitle = title
		data.PageDescription = description
		renderPage(w, category+"/"+tool, data)
	}
}

// renderPage renders a page using the base layout
func renderPage(w http.ResponseWriter, page string, data PageData) {
	tmpl, ok := pageTemplates[page]
	if !ok {
		http.Error(w, "Template not found: "+page, http.StatusInternalServerError)
		return
	}

	err := tmpl.ExecuteTemplate(w, "public.tmpl", data)
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

func categoryPageHandler(cfg *config.Config, category, title, description string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, category)
		data.PageTitle = title
		data.PageDescription = description
		renderPage(w, category, data)
	}
}

// allCategories lists the 21 tool categories shown on the /categories index page
func allCategories() []CategoryInfo {
	return []CategoryInfo{
		{Path: "/text", Icon: "📝", Name: "Text Utilities", Description: "UUID generation, hashing, encoding, and text manipulation", Count: 89},
		{Path: "/crypto", Icon: "🔐", Name: "Cryptography", Description: "Password hashing, TOTP generation, and secure passwords", Count: 147},
		{Path: "/datetime", Icon: "🕐", Name: "Date & Time", Description: "Timestamp conversion, timezone handling, and date calculations", Count: 67},
		{Path: "/network", Icon: "🌐", Name: "Network Tools", Description: "IP lookup, DNS, WHOIS, SSL, and network utilities", Count: 98},
		{Path: "/convert", Icon: "🔄", Name: "Unit Conversion", Description: "Length, weight, temperature, currency, and color conversions", Count: 42},
		{Path: "/dev", Icon: "🛠️", Name: "Developer Tools", Description: "HTTP echo, formatters, minifiers, and development utilities", Count: 94},
		{Path: "/docker", Icon: "🐋", Name: "Docker Tools", Description: "Docker run/compose conversion, linting, and validation", Count: 24},
		{Path: "/fun", Icon: "🎉", Name: "Fun & Content", Description: "Jokes, quotes, facts, and random entertainment", Count: 71},
		{Path: "/generate", Icon: "✨", Name: "Generators", Description: "QR codes, barcodes, configs, avatars, and more", Count: 76},
		{Path: "/geo", Icon: "🌍", Name: "Geolocation", Description: "IP lookup, geocoding, distance calculations, and geo encoding", Count: 52},
		{Path: "/image", Icon: "🖼️", Name: "Images", Description: "Image resize, crop, filters, and manipulation", Count: 68},
		{Path: "/language", Icon: "📖", Name: "Language Tools", Description: "Spell checking, dictionary, thesaurus, and language detection", Count: 48},
		{Path: "/lorem", Icon: "🎭", Name: "Lorem & Fake Data", Description: "Generate realistic fake data for testing and development", Count: 3},
		{Path: "/math", Icon: "🔢", Name: "Math & Numbers", Description: "Calculations, statistics, primes, and matrix operations", Count: 84},
		{Path: "/osint", Icon: "🕵️", Name: "OSINT Tools", Description: "Email intelligence, domain research, and username searches", Count: 42},
		{Path: "/parse", Icon: "🔍", Name: "Parsers", Description: "Parsing JSON, YAML, XML, CSV, and format conversions", Count: 72},
		{Path: "/research", Icon: "📚", Name: "Research Tools", Description: "Content extraction, summarization, and citations", Count: 28},
		{Path: "/system", Icon: "🩺", Name: "Health & System", Description: "Server health checks, system information, and version details", Count: 3},
		{Path: "/testing", Icon: "🧪", Name: "Testing Tools", Description: "Mocks, fixtures, assertions, and API testing", Count: 36},
		{Path: "/validate", Icon: "✅", Name: "Validators", Description: "Validating emails, phones, URLs, credit cards, and more", Count: 68},
		{Path: "/weather", Icon: "⛅", Name: "Weather", Description: "Current weather, forecasts, and air quality data", Count: 15},
	}
}

func categoriesPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "categories")
		data.PageTitle = "Browse All Categories"
		data.PageDescription = "All tool categories available in " + cfg.Server.Branding.Title
		data.Categories = allCategories()
		renderPage(w, "categories", data)
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
		data.CommitID = CommitID
		data.BuildDate = BuildDate
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
		data.SecurityEmail = cfg.Web.Security.Contact
		renderPage(w, "contact", data)
	}
}

func helpPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, "help")
		data.PageTitle = "Help"
		data.PageDescription = "Getting started with " + cfg.Server.Branding.Title
		data.RateLimitRequests = cfg.Server.RateLimit.Read.Requests
		data.RateLimitWindow = cfg.Server.RateLimit.Read.Window
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

// metricsPrometheusHandler serves metrics in Prometheus format
func metricsPrometheusHandler(w http.ResponseWriter, r *http.Request) {
	metrics.Get().ServePrometheus(w, r)
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
