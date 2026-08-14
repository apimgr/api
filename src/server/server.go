package server

import (
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/theme"
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
)

//go:embed template
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Version information
var (
	Version      = "1.0.0"
	CommitID     = "unknown"
	BuildDate    = "unknown"
	OfficialSite = ""
)

// New creates a new HTTP server
func New(cfg *config.Config) *http.Server {
	// Initialize page templates
	if err := initTemplates(); err != nil {
		panic(fmt.Sprintf("Failed to parse templates: %v", err))
	}

	r := chi.NewRouter()

	// Core middleware
	r.Use(serverTimingMiddleware(cfg))
	r.Use(realIPMiddleware(cfg))
	r.Use(requestIDMiddleware)
	r.Use(loggingMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(securityHeadersMiddleware(cfg))
	r.Use(secFetchValidationMiddleware(cfg))
	r.Use(RateLimitMiddleware(cfg))
	r.Use(corsMiddleware(cfg))

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
		r.Get("/"+tp.category+"/"+tp.tool, toolPageHandler(cfg, tp.category, tp.tool, tp.title, tp.description, tp.reason))
	}

	r.Get("/api", apiDocsHandler(cfg))

	// Swagger/GraphQL docs UIs and API routes (PART 14 canonical paths).
	// The old root paths (/openapi, /openapi.json, /openapi.yaml, /swagger,
	// /graphql) are no longer served — do not implement redirects from
	// them. The unversioned /api/swagger and /api/graphql aliases mount
	// the SAME handler as their /api/v1/server/* counterparts (no
	// redirect), per PART 14's "Unversioned API aliases" rules.
	r.Get("/server/docs/swagger", swaggerUIHandler(cfg))
	r.Get("/server/docs/graphql", graphqlUIHandler(cfg))
	r.Get("/api/swagger", apiSwaggerSpecHandler(cfg))
	r.Post("/api/graphql", graphql.HandleQuery)
	r.Get("/api/v1/server/swagger", apiSwaggerSpecHandler(cfg))
	r.Post("/api/v1/server/graphql", graphql.HandleQuery)

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
	// endpoint, not a versioned API route). Endpoint path and optional
	// bearer-token auth are both configurable via server.metrics.*.
	if cfg.Server.Metrics.Enabled {
		metricsPath := cfg.Server.Metrics.Endpoint
		if metricsPath == "" {
			metricsPath = "/metrics"
		}
		r.Get(metricsPath, metricsPrometheusHandler(cfg))
	}

	// Special files
	r.Get("/robots.txt", robotsHandler(cfg))
	r.Get("/security.txt", securityHandler(cfg))
	r.Get("/.well-known/security.txt", securityHandler(cfg))
	r.Get("/manifest.json", manifestHandler(cfg))
	r.Get("/sw.js", serviceWorkerHandler())
	r.Get("/offline.html", offlinePageHandler())

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Version
		r.Get("/version", handler.HandleVersion)

		// Theme switching
		r.Post("/theme", HandleThemeSwitch)

		// Browser-emitted reports (CSP violations, NEL, deprecation,
		// intervention, crash, and the generic "default" group) — see
		// AI.md PART 11 "Reporting API (Modern + Legacy)". One handler
		// serves every {name} since all report endpoints share the same
		// scope, shape, rate limits, and sanitization rules.
		r.Route("/server/reports", func(r chi.Router) {
			r.Post("/{name}", reportsHandler())
		})

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

			// X.509 certificate generation (self-signed) / parse
			r.Post("/certificate", apiCryptoCertificateHandler)

			// Ed25519 keypair generation / sign / verify
			r.Post("/ed25519", apiCryptoEd25519Handler)

			// PGP keypair generation / encrypt / decrypt
			r.Post("/pgp", apiCryptoPGPHandler)
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

			// Format/Parse
			r.Get("/format/{timestamp}/{format}", apiDatetimeFormatHandler)
			r.Get("/parse/{value}", apiDatetimeParseHandler)

			// Cron
			r.Get("/cron", apiDatetimeCronHandler)

			// Calendar
			r.Get("/calendar/{year}/{month}", apiDatetimeCalendarHandler)

			// Workdays
			r.Get("/workdays/{start}/{end}", apiDatetimeWorkdaysHandler)

			// Sun/Moon
			r.Get("/sunrise/{lat}/{lon}", apiDatetimeSunriseHandler)
			r.Get("/sunrise/{lat}/{lon}/{date}", apiDatetimeSunriseHandler)
			r.Get("/moon", apiDatetimeMoonHandler)
			r.Get("/moon/{date}", apiDatetimeMoonHandler)
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
			r.Post("/dockerfile-lint", apiDockerLintHandler)
			r.Get("/best-practices", apiDockerBestPracticesHandler)
			r.Post("/compose-validate", apiDockerComposeValidateHandler)
			r.Post("/compose-to-run", apiDockerComposeToRunHandler)
			r.Post("/run-to-compose", apiDockerRunToComposeHandler)
			r.Post("/env-parser", apiDockerEnvParserHandler)
			r.Get("/network-helper", apiDockerNetworkHelperHandler)
			r.Post("/security-scan", apiDockerSecurityScanHandler)
			r.Post("/size-optimizer", apiDockerSizeOptimizerHandler)
		})

		// Weather
		r.Route("/weather", func(r chi.Router) {
			r.Get("/current/{location}", apiWeatherCurrentHandler)
			r.Get("/forecast/{location}", apiWeatherForecastHandler)
			r.Get("/air-quality/{location}", apiWeatherAirQualityHandler)
			r.Get("/alerts/{location}", apiWeatherAlertsHandler)
			r.Get("/astronomy/{location}", apiWeatherAstronomyHandler)
			r.Get("/historical/{location}", apiWeatherHistoricalHandler)
			r.Get("/hourly/{location}", apiWeatherHourlyHandler)
			r.Get("/maps/{location}", apiWeatherMapsHandler)
			r.Get("/marine/{location}", apiWeatherMarineHandler)
			r.Get("/pollen/{location}", apiWeatherPollenHandler)
			r.Get("/radar/{location}", apiWeatherRadarHandler)
			r.Get("/uv/{location}", apiWeatherUVHandler)
		})

		// Geolocation
		r.Route("/geo", func(r chi.Router) {
			r.Get("/ip/{ip}", apiGeoIPHandler)
			r.Get("/distance", apiGeoDistanceHandler)
			r.Get("/bearing", apiGeoBearingHandler)
			r.Get("/midpoint", apiGeoMidpointHandler)
			r.Get("/geocode", apiGeoGeocodeHandler)
			r.Get("/reverse", apiGeoReverseHandler)
			r.Get("/timezone", apiGeoTimezoneHandler)
			r.Get("/country", apiGeoCountryHandler)
			r.Get("/geohash", apiGeoGeohashHandler)
			r.Get("/h3", apiGeoH3Handler)
			r.Get("/pluscode", apiGeoPlusCodeHandler)
			r.Get("/bbox", apiGeoBBoxHandler)
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
			r.Get("/barcode", apiGenerateBarcodeHandler)
			r.Get("/avatar", apiGenerateAvatarHandler)
			r.Get("/identicon", apiGenerateIdenticonHandler)
			r.Get("/dockerfile", apiGenerateDockerfileHandler)
			r.Get("/gitignore", apiGenerateGitignoreHandler)
			r.Get("/license", apiGenerateLicenseHandler)
			r.Get("/config", apiGenerateConfigHandler)
			r.Post("/sql", apiGenerateSQLHandler)
			r.Get("/ssh-key", apiGenerateSSHKeyHandler)
			r.Get("/api-docs", apiGenerateAPIDocsHandler)
			r.Get("/placeholder/{width}/{height}", apiGeneratePlaceholderHandler)
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
			r.Post("/env", apiParseEnvHandler)
			r.Post("/html", apiParseHTMLHandler)
			r.Post("/ini", apiParseINIHandler)
			r.Post("/log", apiParseLogHandler)
			r.Post("/markdown", apiParseMarkdownHandler)
			r.Post("/sql", apiParseSQLHandler)
			r.Post("/toml", apiParseTOMLHandler)
			r.Post("/yaml", apiParseYAMLHandler)
		})

		// Language Tools
		r.Route("/language", func(r chi.Router) {
			r.Post("/detect", apiLanguageDetectHandler)
			r.Get("/phonetic", apiLanguagePhoneticHandler)
			r.Post("/word-count", apiLanguageWordCountHandler)
			r.Post("/keywords", apiLanguageKeywordsHandler)
			r.Post("/readability", apiLanguageReadabilityHandler)
			r.Post("/reading-time", apiLanguageReadingTimeHandler)
			r.Post("/sentiment", apiLanguageSentimentHandler)
			r.Post("/dictionary", apiLanguageDictionaryHandler)
			r.Post("/thesaurus", apiLanguageThesaurusHandler)
			r.Post("/spell-check", apiLanguageSpellCheckHandler)
			r.Post("/grammar", apiLanguageGrammarHandler)
			r.Post("/translate", apiLanguageTranslateHandler)
		})

		// Testing Tools
		r.Route("/test", func(r chi.Router) {
			r.Get("/http", apiTestHTTPHandler)
			r.Post("/assert", apiTestAssertHandler)
			r.Get("/fixture/{type}", apiTestFixtureHandler)
			r.Get("/fake-data", apiTestFakeDataHandler)
			r.Post("/api-client", apiTestAPIClientHandler)
			r.Post("/curl-generator", apiTestCurlGeneratorHandler)
			r.Post("/postman", apiTestPostmanHandler)
			r.Get("/request-inspector", apiTestRequestInspectorHandler)
			r.Post("/request-inspector", apiTestRequestInspectorHandler)
			r.Get("/status-codes", apiTestStatusCodesHandler)
			r.Get("/status-codes/{code}", apiTestStatusCodesHandler)
			r.Get("/response-generator", apiTestResponseGeneratorHandler)
			r.Post("/webhook", apiTestWebhookHandler)
			r.Get("/load-test", apiTestLoadTestHandler)
			r.Post("/load-test", apiTestLoadTestHandler)
			r.Get("/mock-server", apiTestMockServerHandler)
			r.Post("/mock-server", apiTestMockServerHandler)
		})

		// OSINT Tools
		r.Route("/osint", func(r chi.Router) {
			r.Get("/email/{email}", apiOsintEmailHandler)
			r.Get("/domain/{domain}", apiOsintDomainHandler)
			r.Get("/ip/{ip}", apiOsintIPHandler)
			r.Get("/cert/{domain}", apiOsintCertHandler)
			r.Get("/subdomain/{domain}", apiOsintSubdomainHandler)
			r.Get("/tech-stack", apiOsintTechStackHandler)
			r.Get("/breach/{email}", apiOsintBreachHandler)
			r.Get("/company/{name}", apiOsintCompanyHandler)
			r.Get("/metadata/{target}", apiOsintMetadataHandler)
			r.Get("/phone/{number}", apiOsintPhoneHandler)
			r.Get("/social/{username}", apiOsintSocialHandler)
			r.Get("/username/{username}", apiOsintUsernameHandler)
		})

		// Research Tools
		r.Route("/research", func(r chi.Router) {
			r.Post("/citation", apiResearchCitationHandler)
			r.Get("/doi/*", apiResearchDOIHandler)
			r.Post("/extract", apiResearchExtractHandler)
			r.Post("/arxiv", apiResearchArxivHandler)
			r.Post("/bibtex", apiResearchBibtexHandler)
			r.Post("/footnotes", apiResearchFootnotesHandler)
			r.Post("/isbn", apiResearchIsbnHandler)
			r.Post("/metadata", apiResearchMetadataHandler)
			r.Post("/outline", apiResearchOutlineHandler)
			r.Post("/pdf-extract", apiResearchPdfExtractHandler)
			r.Post("/readability", apiResearchReadabilityHandler)
			r.Post("/scraper", apiResearchScraperHandler)
			r.Post("/summarize", apiResearchSummarizeHandler)
		})

		// Fun & Content
		r.Route("/fun", func(r chi.Router) {
			r.Get("/joke", apiFunJokeHandler)
			r.Get("/fortune", apiFunFortuneHandler)
			r.Get("/dad-joke", apiFunDadJokeHandler)
			r.Get("/programming-joke", apiFunProgrammingJokeHandler)
			r.Get("/quote", apiFunQuoteHandler)
			r.Get("/fact", apiFunFactHandler)
			r.Get("/riddle", apiFunRiddleHandler)
			r.Get("/trivia", apiFunTriviaHandler)
			r.Get("/motivational", apiFunMotivationalHandler)
			r.Get("/insult", apiFunInsultHandler)
			r.Get("/compliment", apiFunComplimentHandler)
			r.Get("/meme", apiFunMemeHandler)
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
			r.Post("/format/css", apiDevFormatCSSHandler)
			r.Post("/format/html", apiDevFormatHTMLHandler)
			r.Post("/format/js", apiDevFormatJSHandler)
			r.Post("/format/sql", apiDevFormatSQLHandler)
			r.Post("/format/xml", apiDevFormatXMLHandler)
			r.Post("/base64", apiDevBase64Handler)
			r.Post("/url-encode", apiDevURLEncodeHandler)
			r.Get("/cron", apiDevCronHandler)
			r.Get("/jwt/{token}", apiDevJWTHandler)
			r.Get("/echo", apiDevEchoHandler)

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
			r.Get("/avatar", apiImageAvatarHandler)
			r.Get("/barcode", apiImageBarcodeHandler)
			r.Get("/identicon", apiImageIdenticonHandler)
			r.Get("/qr", apiImageQRHandler)
			r.Post("/filter", apiImageFilterHandler)
			r.Post("/optimize", apiImageOptimizeHandler)
			r.Post("/watermark", apiImageWatermarkHandler)
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
	SiteTitle          string
	SiteIcon           string
	BaseURL            string
	Theme              string
	ThemeClass         string
	Layout             string
	ActivePage         string
	PageTitle          string
	PageDescription    string
	NotSupportedReason string
	Tagline            string
	Version            string
	CommitID           string
	BuildDate          string
	Mode               string
	SecurityEmail      string
	UpdatedAt          string
	RateLimitRequests  int
	RateLimitWindow    int
	Categories         []CategoryInfo
}

// CategoryInfo describes one tool category shown on the /categories index page
type CategoryInfo struct {
	Path        string
	Icon        string
	Name        string
	Description string
	Count       int
}

// newPageData builds the common template data for a page render. Theme is
// resolved per-request from the "theme" cookie (GetTheme/ThemeClass in
// theme.go) so the server-rendered class="theme-dark|theme-light|theme-auto"
// on <html> reflects the visitor's actual saved preference, never a global
// config default.
func newPageData(cfg *config.Config, r *http.Request, activePage string) PageData {
	baseURL := fmt.Sprintf("http://%s:%s", cfg.Server.FQDN, cfg.Server.Port)
	if cfg.Server.FQDN == "" || cfg.Server.FQDN == "localhost" {
		baseURL = fmt.Sprintf("http://localhost:%s", cfg.Server.Port)
	}
	theme := GetTheme(r)
	return PageData{
		SiteTitle:  cfg.Server.Branding.Title,
		SiteIcon:   "🛠️",
		BaseURL:    baseURL,
		Theme:      string(theme),
		ThemeClass: ThemeClass(theme),
		Layout:     "public",
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
	// registered under composite keys like "crypto/hash". Entries with a
	// non-empty reason (the 28 permanent API gaps) instead share
	// template/page/tools/unsupported.tmpl — see the loop below.
	for _, tp := range toolPages() {
		key := tp.category + "/" + tp.tool
		contentTemplate := fmt.Sprintf("template/page/tools/%s.tmpl", key)
		if tp.reason != "" {
			// Permanent API gap (PART 16 route mirroring): reuse the one
			// shared "not supported" template instead of a per-tool file.
			contentTemplate = "template/page/tools/unsupported.tmpl"
		}
		tmpl, err := template.ParseFS(templatesFS,
			"template/layout/public.tmpl",
			"template/partial/*.tmpl",
			"template/partial/public/*.tmpl",
			contentTemplate,
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
	// reason is set only for the 28 permanent API gaps (PART 16 route-
	// mirroring: every wired API route, including a 501 NOT_SUPPORTED one,
	// gets a matching frontend page). When non-empty, initTemplates()
	// renders this entry from the shared template/page/tools/unsupported.tmpl
	// instead of a per-tool template, and the handler surfaces reason as
	// PageData.NotSupportedReason instead of a working form.
	reason string
}

// toolPages lists the per-tool detail pages that currently have templates on
// disk under template/page/tools/{category}/{tool}.tmpl (PART 16 frontend
// route mirrors API route rule). This includes the 28 permanent API gaps
// (entries with a non-empty reason), which render the shared
// template/page/tools/unsupported.tmpl instead of a working form so their
// wired-but-501 API route still has a matching frontend page. Most of the
// remaining ~240 sub-tool pages linked from the 21 category pages have
// neither a template nor a route yet — see TODO.AI.md for the remaining
// wiring work.
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
		{category: "crypto", tool: "certificate", title: "X.509 Certificate", description: "Generate a self-signed X.509 certificate, or parse an existing PEM certificate's details"},
		{category: "crypto", tool: "ed25519", title: "Ed25519 Sign/Verify", description: "Generate an Ed25519 keypair, sign a message, or verify a signature"},
		{category: "crypto", tool: "pgp", title: "PGP Encrypt/Decrypt", description: "Generate a PGP keypair, or encrypt/decrypt a message with PGP"},
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
		{category: "docker", tool: "run-to-compose", title: "Run to Compose", description: "Convert docker run to docker-compose"},
		{category: "docker", tool: "compose-to-run", title: "Compose to Run", description: "Convert docker-compose to docker run"},
		{category: "docker", tool: "dockerfile-lint", title: "Dockerfile Linter", description: "Lint and validate Dockerfiles"},
		{category: "docker", tool: "compose-validate", title: "Compose Validator", description: "Validate docker-compose files"},
		{category: "docker", tool: "env-parser", title: "ENV Parser", description: "Parse environment variables"},
		{category: "docker", tool: "network-helper", title: "Network Helper", description: "Generate network configurations"},
		{category: "docker", tool: "best-practices", title: "Best Practices", description: "Docker best practices guide"},
		{category: "docker", tool: "security-scan", title: "Security Scanner", description: "Scan for security issues"},
		{category: "docker", tool: "size-optimizer", title: "Size Optimizer", description: "Optimize image size"},
		{category: "network", tool: "subnet", title: "Subnet Calculator", description: "Calculate network, broadcast, and host range details for a CIDR block"},
		{category: "network", tool: "ula", title: "ULA Generator", description: "Generate an RFC 4193 IPv6 unique-local-address prefix"},
		{category: "network", tool: "port", title: "Random Port", description: "Suggest a random unprivileged TCP/UDP port"},
		{category: "network", tool: "ping", title: "Ping Tool", description: "Measure TCP connect round-trip latency to a host"},
		{category: "network", tool: "ssl", title: "SSL Certificate Info", description: "Check SSL certificate subject, issuer, and validity for a host"},
		{category: "network", tool: "url", title: "URL Parser", description: "Parse and analyze a URL into its component parts"},
		{category: "network", tool: "whois", title: "WHOIS Lookup", description: "Look up domain and IP WHOIS registration information"},
		{category: "weather", tool: "current", title: "Current Weather", description: "Get current weather conditions for a location"},
		{category: "weather", tool: "forecast", title: "Weather Forecast", description: "Get a 1-16 day weather forecast for a location"},
		{category: "weather", tool: "air-quality", title: "Air Quality", description: "Get current air quality index and pollutant levels for a location"},
		{category: "weather", tool: "alerts", title: "Weather Alerts", description: "Get active government weather alerts for a location (NWS, Environment Canada, MeteoAlarm)"},
		{category: "weather", tool: "astronomy", title: "Astronomy", description: "Get sunrise, sunset, and daylight data for a location"},
		{category: "weather", tool: "historical", title: "Historical Weather", description: "Get historical daily weather for a location over a date range"},
		{category: "weather", tool: "hourly", title: "Hourly Forecast", description: "Get an hourly weather forecast for a location"},
		{category: "weather", tool: "marine", title: "Marine Conditions", description: "Get current marine and ocean conditions for a coastal location"},
		{category: "weather", tool: "pollen", title: "Pollen Count", description: "Get current pollen counts for a location"},
		{category: "weather", tool: "uv", title: "UV Index", description: "Get the current UV index for a location"},
		{category: "geo", tool: "ip", title: "IP Geolocation", description: "Look up geolocation details for a public IP address"},
		{category: "geo", tool: "distance", title: "Distance Calculator", description: "Calculate the great-circle distance between two coordinates"},
		{category: "geo", tool: "bearing", title: "Bearing Calculator", description: "Calculate the initial compass bearing from one coordinate to another"},
		{category: "geo", tool: "midpoint", title: "Midpoint Calculator", description: "Calculate the geographic midpoint between two coordinates"},
		{category: "geo", tool: "geocode", title: "Geocode Address", description: "Convert address to coordinates"},
		{category: "geo", tool: "reverse", title: "Reverse Geocode", description: "Get address from coordinates"},
		{category: "geo", tool: "timezone", title: "Timezone Lookup", description: "Get timezone from coordinates"},
		{category: "geo", tool: "country", title: "Country Info", description: "Get country information and codes"},
		{category: "geo", tool: "geohash", title: "Geohash Encoder", description: "Encode coordinates to geohash"},
		{category: "geo", tool: "h3", title: "H3 Encoder", description: "Uber H3 hexagonal indexing"},
		{category: "geo", tool: "pluscode", title: "Plus Codes", description: "Google Plus Codes encoding"},
		{category: "geo", tool: "bbox", title: "Bounding Box", description: "Calculate bounding boxes"},
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
		{category: "parse", tool: "env", title: "Env Parser", description: "Parse .env-style KEY=VALUE documents into a key/value map"},
		{category: "parse", tool: "html", title: "HTML Parser", description: "Parse an HTML document into a structural summary (title, meta, headings, links, images, forms)"},
		{category: "parse", tool: "ini", title: "INI Parser", description: "Parse an INI document into sections of key/value pairs"},
		{category: "parse", tool: "log", title: "Log Parser", description: "Best-effort parse of log lines into timestamp, level, and message"},
		{category: "parse", tool: "markdown", title: "Markdown Structure Parser", description: "Extract headings, links, and code blocks from a Markdown document"},
		{category: "parse", tool: "sql", title: "SQL Structure Parser", description: "Best-effort extraction of statement type, tables, and columns from a SQL statement"},
		{category: "parse", tool: "toml", title: "TOML Parser", description: "Parse a TOML document into a structured map"},
		{category: "parse", tool: "yaml", title: "YAML Parser", description: "Parse a YAML document into a structured map"},
		{category: "research", tool: "citation", title: "Citation Formatter", description: "Format a reference into an APA, MLA, or Chicago style citation"},
		{category: "research", tool: "doi", title: "DOI Validator", description: "Validate a DOI and get its canonical https://doi.org resolver URL"},
		{category: "research", tool: "arxiv", title: "arXiv Lookup", description: "Look up an arXiv paper by ID using the free, keyless arXiv API"},
		{category: "research", tool: "isbn", title: "ISBN Lookup", description: "Look up book metadata by ISBN using the free, keyless Open Library API"},
		{category: "fun", tool: "joke", title: "Random Joke", description: "Get a random joke type paired with a fortune-cookie style saying"},
		{category: "fun", tool: "fortune", title: "Random Fortune", description: "Get a single random fortune-cookie style saying"},
		{category: "fun", tool: "dad-joke", title: "Dad Jokes", description: "Classic dad jokes"},
		{category: "fun", tool: "programming-joke", title: "Programming Jokes", description: "Jokes for developers"},
		{category: "fun", tool: "quote", title: "Random Quote", description: "Inspirational quotes"},
		{category: "fun", tool: "fact", title: "Random Fact", description: "Interesting facts"},
		{category: "fun", tool: "riddle", title: "Riddles", description: "Brain teasers and riddles"},
		{category: "fun", tool: "trivia", title: "Trivia Questions", description: "Random trivia"},
		{category: "fun", tool: "motivational", title: "Motivational Quote", description: "Get motivated"},
		{category: "fun", tool: "insult", title: "Insult Generator", description: "Playful, harmless roasts and mock insults"},
		{category: "fun", tool: "compliment", title: "Compliment Generator", description: "Random compliments"},
		{category: "fun", tool: "meme", title: "Meme Generator", description: "Classic meme caption text"},
		{category: "lorem", tool: "person", title: "Fake Person", description: "Generate a fake person with a name, email, and phone number"},
		{category: "lorem", tool: "address", title: "Fake Address", description: "Generate a fake street address"},
		{category: "lorem", tool: "company", title: "Fake Company", description: "Generate a fake company name and catchphrase"},
		{category: "testing", tool: "http", title: "Mock HTTP Response", description: "Generate a mock API response fixture and measure execution time"},
		{category: "testing", tool: "assertions", title: "Assertion Runner", description: "Run an equal, not_equal, contains, true, or false assertion and get a pass/fail result"},
		{category: "testing", tool: "fixtures", title: "Test Fixture Generator", description: "Generate a named test fixture (user, api_response, or custom type)"},
		{category: "testing", tool: "fake-data", title: "Fake Data Generator", description: "Generate a fake test email, username, or mock user"},
		{category: "testing", tool: "api-client", title: "API Client Code Generator", description: "Generate curl, JavaScript, Python, and Go client code for an HTTP request"},
		{category: "testing", tool: "curl-generator", title: "curl Command Generator", description: "Generate a single curl command from a method, URL, headers, and body"},
		{category: "testing", tool: "postman", title: "Postman Collection Generator", description: "Generate a minimal Postman Collection v2.1 JSON for an HTTP request"},
		{category: "testing", tool: "request-inspector", title: "Request Inspector", description: "Inspect the method, headers, query parameters, and body of the current request"},
		{category: "testing", tool: "status-codes", title: "HTTP Status Code Reference", description: "Look up an HTTP status code's reason phrase and description, or browse the full table"},
		{category: "testing", tool: "response-generator", title: "Mock Response Generator", description: "Generate a mock API response fixture"},
		{category: "testing", tool: "webhook", title: "Webhook Inspector", description: "POST a payload and get back an inspection of its headers and body"},
		{category: "osint", tool: "email", title: "Email Intelligence", description: "Validate an email address and check for MX records"},
		{category: "osint", tool: "domain", title: "WHOIS Lookup", description: "Look up registrar, creation/expiry dates, and nameservers for a domain"},
		{category: "osint", tool: "ip", title: "IP Intelligence", description: "Look up geolocation and ISP information for a public IP address"},
		{category: "osint", tool: "cert", title: "TLS Certificate Lookup", description: "Inspect a domain's TLS certificate details"},
		{category: "osint", tool: "subdomain", title: "Subdomain Discovery", description: "Discover subdomains of a domain by resolving common subdomain labels"},
		{category: "osint", tool: "tech-stack", title: "Tech Stack Detection", description: "Detect web server, framework, and CMS signatures from a site's HTTP response"},
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
		{category: "image", tool: "avatar", title: "Generate Avatar", description: "Generate an initials-based avatar image"},
		{category: "image", tool: "barcode", title: "Generate Barcode", description: "Generate a 1D barcode image from text data"},
		{category: "image", tool: "qr", title: "Generate QR Code", description: "Generate a QR code image from text data or a Wi-Fi join payload"},
		{category: "image", tool: "identicon", title: "Generate Identicon", description: "Generate a deterministic identicon image from a seed"},
		{category: "image", tool: "filter", title: "Apply Image Filter", description: "Upload an image and apply a grayscale, sepia, invert, blur, brighten, or darken filter"},
		{category: "image", tool: "optimize", title: "Optimize Image", description: "Upload an image and re-encode it to reduce file size"},
		{category: "image", tool: "watermark", title: "Watermark Image", description: "Upload an image and tile a text watermark across it"},
		{category: "language", tool: "phonetic", title: "Phonetic Encoding", description: "Generate the Soundex and Metaphone phonetic codes for a word"},
		{category: "language", tool: "word-count", title: "Word Count", description: "Count words, characters, lines, and sentences in text"},
		{category: "language", tool: "keywords", title: "Keyword Extraction", description: "Extract the most frequent non-stopword keywords from text"},
		{category: "language", tool: "readability", title: "Readability Scores", description: "Compute Flesch Reading Ease, Flesch-Kincaid Grade, and Gunning Fog scores for text"},
		{category: "language", tool: "reading-time", title: "Reading Time", description: "Estimate reading time for text at a given words-per-minute rate"},
		{category: "language", tool: "sentiment", title: "Sentiment Analysis", description: "Score text as positive, negative, or neutral using a lexicon-based heuristic"},
		{category: "language", tool: "dictionary", title: "Dictionary Lookup", description: "Look up word definitions using the free, keyless Dictionary API"},
		{category: "language", tool: "thesaurus", title: "Thesaurus", description: "Look up word synonyms and antonyms using the free, keyless Datamuse API"},
		{category: "text", tool: "encode", title: "Encode", description: "Encode text using base64, base32, hex, URL, or HTML encoding"},
		{category: "text", tool: "decode", title: "Decode", description: "Decode text encoded with base64, base32, hex, URL, or HTML encoding"},
		{category: "text", tool: "case", title: "Case Converter", description: "Convert text between upper, lower, title, camel, snake, kebab, and other case styles"},
		{category: "text", tool: "lorem", title: "Lorem Ipsum", description: "Generate placeholder Lorem Ipsum text by word, sentence, or paragraph"},
		{category: "datetime", tool: "convert", title: "Convert Timestamp", description: "Convert a Unix timestamp to a human-readable date/time in any timezone"},
		{category: "datetime", tool: "unix", title: "To Unix Timestamp", description: "Convert a human-readable date/time string to a Unix timestamp"},
		{category: "datetime", tool: "add", title: "Add Duration", description: "Add a duration to a Unix timestamp"},
		{category: "datetime", tool: "diff", title: "Timestamp Diff", description: "Compute the difference between two Unix timestamps"},
		{category: "datetime", tool: "format", title: "Date Formatter", description: "Format dates in various styles"},
		{category: "datetime", tool: "parse", title: "Date Parser", description: "Parse date strings to timestamps"},
		{category: "datetime", tool: "cron", title: "Cron Parser", description: "Parse and explain cron expressions"},
		{category: "datetime", tool: "calendar", title: "Calendar", description: "View calendar for any month/year"},
		{category: "datetime", tool: "workdays", title: "Business Days", description: "Calculate working days between dates"},
		{category: "datetime", tool: "sunrise", title: "Sunrise/Sunset", description: "Calculate sunrise and sunset times"},
		{category: "datetime", tool: "moon", title: "Moon Phase", description: "Calculate current moon phase"},
		{category: "text", tool: "compress", title: "Compress/Decompress", description: "Compress or decompress text using gzip, zlib, or flate/deflate"},
		{category: "text", tool: "diff", title: "Text Diff", description: "Compare two blocks of text and show the differences"},
		{category: "text", tool: "extract", title: "Extract", description: "Extract emails, URLs, IP addresses, or phone numbers from text"},
		{category: "text", tool: "nanoid", title: "NanoID Generator", description: "Generate a compact, URL-friendly unique ID"},
		{category: "text", tool: "ulid", title: "ULID Generator", description: "Generate a sortable, timestamp-based unique ID"},
		{category: "text", tool: "regex", title: "Regex Tester", description: "Test a regular expression against text: match, replace, or explain"},
		{category: "dev", tool: "regex", title: "Regex Tester", description: "Test a regular expression against text: match, replace, or explain"},
		{category: "dev", tool: "echo", title: "HTTP Echo", description: "Echo back request details"},
		{category: "dev", tool: "xml-format", title: "XML Formatter", description: "Format/minify XML"},
		{category: "dev", tool: "html-format", title: "HTML Formatter", description: "Format/minify HTML"},
		{category: "dev", tool: "css-format", title: "CSS Formatter", description: "Format/minify CSS"},
		{category: "dev", tool: "js-format", title: "JavaScript Formatter", description: "Format/minify JavaScript"},
		{category: "dev", tool: "sql-format", title: "SQL Formatter", description: "Format SQL queries"},
		{category: "dev", tool: "cron", title: "Cron Tester", description: "Test cron expressions"},
		{category: "dev", tool: "jwt", title: "JWT Debugger", description: "Debug JSON Web Tokens"},
		{category: "generate", tool: "barcode", title: "Barcode", description: "EAN, UPC, Code128, Code39"},
		{category: "generate", tool: "qr", title: "QR Code", description: "QR code from text data or a Wi-Fi join payload"},
		{category: "generate", tool: "avatar", title: "Avatar", description: "Generate avatars from initials"},
		{category: "generate", tool: "config", title: "Config Files", description: "Generate configuration templates"},
		{category: "generate", tool: "sql", title: "SQL Schema", description: "Generate SQL database schemas"},
		{category: "generate", tool: "api-docs", title: "API Documentation", description: "Generate API documentation"},
		{category: "generate", tool: "license", title: "License File", description: "Generate license files"},
		{category: "generate", tool: "gitignore", title: ".gitignore", description: "Generate .gitignore files"},
		{category: "generate", tool: "dockerfile", title: "Dockerfile", description: "Generate Dockerfile templates"},
		{category: "generate", tool: "ssh-key", title: "SSH Key", description: "Generate SSH key pairs"},
		{category: "generate", tool: "identicon", title: "Identicon", description: "Generate identicons from hashes"},
		{category: "generate", tool: "placeholder", title: "Placeholder Image", description: "Generate placeholder images"},

		// The 28 permanent API gaps (see TODO.AI.md "Known permanent API
		// gaps"): each has a real, wired /api/{version}/... route that
		// honestly returns 501 NOT_SUPPORTED rather than inventing
		// behavior IDEA.md's declared scope/non-goals/trust-boundary
		// excludes. PART 16 requires a matching frontend route for every
		// wired API route, so each gets a page here too — rendered via
		// the shared unsupported.tmpl rather than a working form.
		{category: "language", tool: "detect", title: "Language Detection", description: "Detect the language of a piece of text", reason: "Language auto-detection is a declared non-goal in IDEA.md; only language code/name lookup is supported."},
		{category: "language", tool: "translate", title: "Translate", description: "Translate text between languages", reason: "Machine translation is a declared non-goal in IDEA.md and commercial translation APIs are outside the declared free/keyless trust boundary."},
		{category: "language", tool: "grammar", title: "Grammar Check", description: "Check text for grammar issues", reason: "Grammar checking is not named in IDEA.md's declared Language scope of code/name lookup and listing."},
		{category: "language", tool: "spell-check", title: "Spell Check", description: "Check text for spelling issues", reason: "Spell checking is not named in IDEA.md's declared Language scope of code/name lookup and listing."},
		{category: "research", tool: "extract", title: "Citation Extraction", description: "Extract citations from unstructured text", reason: "Citation extraction from free-form text is unimplemented; IDEA.md's Research scope covers citation formatting, bibliography generation, DOI validation, and the arXiv/ISBN lookups only."},
		{category: "research", tool: "bibtex", title: "BibTeX Export", description: "Export a citation as BibTeX", reason: "BibTeX export is not named in IDEA.md's declared Research scope of citation formatting/bibliography/DOI/arXiv/ISBN."},
		{category: "research", tool: "footnotes", title: "Footnote Formatter", description: "Format footnotes for a document", reason: "Footnote formatting is not named in IDEA.md's declared Research scope of citation formatting/bibliography/DOI/arXiv/ISBN."},
		{category: "research", tool: "metadata", title: "Page Metadata Extraction", description: "Extract metadata from a web page", reason: "Web page metadata extraction is not named in IDEA.md's declared Research scope of citation formatting/bibliography/DOI/arXiv/ISBN."},
		{category: "research", tool: "outline", title: "Document Outline", description: "Generate a document outline", reason: "Document outlining is not named in IDEA.md's declared Research scope of citation formatting/bibliography/DOI/arXiv/ISBN."},
		{category: "research", tool: "pdf-extract", title: "PDF Text Extraction", description: "Extract text from a PDF", reason: "PDF text extraction needs a new third-party dependency to parse untrusted binaries; outside IDEA.md's declared scope."},
		{category: "research", tool: "readability", title: "Readability Score", description: "Score the readability of a page or text", reason: "Readability scoring of remote pages is not named in IDEA.md's declared Research scope of citation formatting/bibliography/DOI/arXiv/ISBN."},
		{category: "research", tool: "scraper", title: "Web Scraper", description: "Scrape content from a web page", reason: "Web scraping is not named in IDEA.md's declared Research scope of citation formatting/bibliography/DOI/arXiv/ISBN."},
		{category: "research", tool: "summarize", title: "Text Summarizer", description: "Summarize a piece of text", reason: "A genuine summarizer needs an external/keyed NLP or LLM service, excluded by IDEA.md's free/keyless integration policy."},
		{category: "osint", tool: "breach", title: "Breach Check", description: "Check whether an email or username appears in a known breach", reason: "Breach-database checking requires a commercial keyed third-party API, outside IDEA.md's declared free/keyless OSINT trust boundary."},
		{category: "osint", tool: "company", title: "Company Lookup", description: "Look up company registration details", reason: "Company lookup requires a commercial keyed third-party API, outside IDEA.md's declared free/keyless OSINT trust boundary."},
		{category: "osint", tool: "metadata", title: "File Metadata Extraction", description: "Extract metadata from an uploaded file", reason: "Generic file-metadata extraction duplicates the existing image/metadata tool and is outside OSINT's declared 4-mechanism scope of IP geolocation/WHOIS/DNS/TLS certificate."},
		{category: "osint", tool: "phone", title: "Phone Intelligence", description: "Look up carrier and risk details for a phone number", reason: "Phone-number intelligence requires a commercial keyed API; validate/phone already covers format validation within IDEA.md's declared scope."},
		{category: "osint", tool: "social", title: "Social Profile Discovery", description: "Discover social media profiles for a name or handle", reason: "Cross-platform profile discovery would require probing dozens of third-party platforms, outside IDEA.md's declared free/keyless OSINT trust boundary."},
		{category: "osint", tool: "username", title: "Username Search", description: "Search for a username across platforms", reason: "Cross-platform username discovery would require probing dozens of third-party platforms, outside IDEA.md's declared free/keyless OSINT trust boundary."},
		{category: "testing", tool: "load-test", title: "Load Test", description: "Run a load test against a target URL", reason: "Load testing would require firing outbound HTTP traffic at a caller-supplied target, outside IDEA.md's declared outbound-call boundary."},
		{category: "testing", tool: "mock-server", title: "Mock Server", description: "Spin up a temporary mock HTTP server", reason: "A mock server needs either a second runtime-managed listening socket (forbidden by the no-runtime-port-change config rule) or persisting caller-defined rules (forbidden by IDEA.md's no-persistent-storage non-goal)."},
		{category: "weather", tool: "maps", title: "Weather Map Tiles", description: "Get weather map tile imagery for a region", reason: "Keyless weather tile imagery has no free provider within IDEA.md's declared outbound-call boundary; a real implementation needs a keyed provider."},
		{category: "weather", tool: "radar", title: "Radar Imagery", description: "Get radar imagery for a location", reason: "Keyless radar imagery has no free provider within IDEA.md's declared outbound-call boundary; a real implementation needs a keyed provider."},
		{category: "network", tool: "traceroute", title: "Traceroute", description: "Trace the network path to a host", reason: "A real traceroute needs TTL-limited probes and a raw ICMP socket (CAP_NET_RAW or root), which this unprivileged self-contained binary cannot assume it has."},
	}
}

// toolPageHandler renders a per-tool detail page under a category
func toolPageHandler(cfg *config.Config, category, tool, title, description, reason string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, category)
		data.PageTitle = title
		data.PageDescription = description
		data.NotSupportedReason = reason
		renderPage(w, category+"/"+tool, data)
	}
}

// renderPage renders a page using the base layout.
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
		data := newPageData(cfg, r, "home")
		data.PageTitle = ""
		data.PageDescription = "Universal API Toolkit with text, crypto, datetime, and network utilities"
		renderPage(w, "index", data)
	}
}

func textPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "text")
		data.PageTitle = "Text Utilities"
		data.PageDescription = "UUID generation, hashing, encoding, and text manipulation"
		renderPage(w, "text", data)
	}
}

func cryptoPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "crypto")
		data.PageTitle = "Cryptography Tools"
		data.PageDescription = "Password hashing, TOTP generation, and secure passwords"
		renderPage(w, "crypto", data)
	}
}

func categoryPageHandler(cfg *config.Config, category, title, description string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, category)
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
		{Path: "/testing", Icon: "🧪", Name: "Testing Tools", Description: "Mocks, fixtures, assertions, and API testing", Count: 45},
		{Path: "/validate", Icon: "✅", Name: "Validators", Description: "Validating emails, phones, URLs, credit cards, and more", Count: 68},
		{Path: "/weather", Icon: "⛅", Name: "Weather", Description: "Current weather, forecasts, and air quality data", Count: 27},
	}
}

func categoriesPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "categories")
		data.PageTitle = "Browse All Categories"
		data.PageDescription = "All tool categories available in " + cfg.Server.Branding.Title
		data.Categories = allCategories()
		renderPage(w, "categories", data)
	}
}

func datetimePageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "datetime")
		data.PageTitle = "DateTime Tools"
		data.PageDescription = "Timestamp conversion, timezone handling, and date calculations"
		renderPage(w, "datetime", data)
	}
}

func aboutPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "about")
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
		data := newPageData(cfg, r, "privacy")
		data.PageTitle = "Privacy Policy"
		data.PageDescription = "Privacy policy for " + cfg.Server.Branding.Title
		data.UpdatedAt = time.Now().Format("January 2006")
		renderPage(w, "privacy", data)
	}
}

func contactPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "contact")
		data.PageTitle = "Contact"
		data.PageDescription = "Contact information"
		data.SecurityEmail = cfg.Web.Security.Contact
		renderPage(w, "contact", data)
	}
}

func helpPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "help")
		data.PageTitle = "Help"
		data.PageDescription = "Getting started with " + cfg.Server.Branding.Title
		data.RateLimitRequests = cfg.Server.RateLimit.Read.Requests
		data.RateLimitWindow = cfg.Server.RateLimit.Read.Window
		renderPage(w, "help", data)
	}
}

func apiDocsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "api")
		data.PageTitle = "API Documentation"
		data.PageDescription = "REST API documentation for CasTools - Universal API Toolkit"
		renderPage(w, "openapi", data)
	}
}

// swaggerUIHandler serves the Swagger UI at the PART 14 canonical
// /server/docs/swagger path; it fetches its spec from /api/swagger.
func swaggerUIHandler(cfg *config.Config) http.HandlerFunc {
	baseURL := getBaseURL(cfg)
	return swagger.ServeUI(baseURL + "/api/swagger")
}

// apiSwaggerSpecHandler serves the OpenAPI JSON spec. Mounted at both
// /api/swagger (unversioned alias) and /api/v1/server/swagger (versioned
// canonical) — same handler, no redirect, per PART 14. JSON only, no YAML.
func apiSwaggerSpecHandler(cfg *config.Config) http.HandlerFunc {
	baseURL := getBaseURL(cfg)
	return swagger.ServeSpec(Version, baseURL)
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

// graphqlUIHandler serves the GraphiQL UI at the PART 14 canonical
// /server/docs/graphql path; it POSTs to /api/graphql.
func graphqlUIHandler(cfg *config.Config) http.HandlerFunc {
	baseURL := getBaseURL(cfg)
	return graphql.ServeUI(baseURL + "/api/graphql")
}

// metricsPrometheusHandler serves metrics in Prometheus format. When
// server.metrics.token is set, requests must present a matching
// "Authorization: Bearer <token>" header (PART 20 optional bearer-token
// auth) - compared in constant time, never with ==. An empty token means
// no auth check: the endpoint relies on firewall/proxy/NetworkPolicy
// restriction alone (PART 20 Access Control).
func metricsPrometheusHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token := cfg.Server.Metrics.Token; token != "" {
			const prefix = "Bearer "
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, prefix) ||
				subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, prefix)), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		metrics.Get().ServePrometheus(w, r)
	}
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
		// /manifest.json is never long-cached - a stale cached manifest
		// (icons/theme colors) delays PWA update visibility.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", buildETag())
		// Manifest colors trace back to theme.ThemePaletteDark (the default
		// theme) in src/common/theme/colors.go, rather than unrelated
		// hardcoded hex values, so installed-PWA chrome matches the site.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":             "CasTools",
			"short_name":       "CasTools",
			"description":      "Universal API Toolkit",
			"start_url":        "/",
			"scope":            "/",
			"display":          "standalone",
			"orientation":      "any",
			"background_color": theme.ThemePaletteDark.Background,
			"theme_color":      theme.ThemePaletteDark.Primary,
			"categories":       []string{"utilities"},
			"icons": []map[string]string{
				{"src": "/static/images/icons/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"},
				{"src": "/static/images/icons/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
				{"src": "/static/images/icons/icon-192-maskable.png", "sizes": "192x192", "type": "image/png", "purpose": "maskable"},
				{"src": "/static/images/icons/icon-512-maskable.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"},
			},
		})
	}
}

// buildETag stamps a weak ETag from the running build so /sw.js and
// /manifest.json responses (see PART 16 "Offline Behavior" caching table)
// change identity on every new build without needing per-file hashing.
func buildETag() string {
	return fmt.Sprintf(`W/"%s-%s"`, Version, CommitID)
}

// serviceWorkerHandler serves the embedded /sw.js at the required root
// scope with no-cache so browsers always see a new service worker promptly
// after a build changes it.
func serviceWorkerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := staticFS.ReadFile("static/sw.js")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", buildETag())
		w.Write(data)
	}
}

// offlinePageHandler serves the embedded /offline.html fallback page that
// the service worker returns when a navigation request fails offline and
// no cached copy of the requested page exists.
func offlinePageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := staticFS.ReadFile("static/offline.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
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

type encodeParams struct {
	Encoding string `validate:"required,oneof=base64 base64url base32 hex base16 url"`
}

func apiEncodeHandler(w http.ResponseWriter, r *http.Request) {
	encoding := strings.ToLower(chi.URLParam(r, "encoding"))
	input := chi.URLParam(r, "input")

	if !validateStruct(w, encodeParams{Encoding: encoding}) {
		return
	}

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

type decodeParams struct {
	Encoding string `validate:"required,oneof=base64 base64url base32 hex base16 url"`
}

func apiDecodeHandler(w http.ResponseWriter, r *http.Request) {
	encoding := strings.ToLower(chi.URLParam(r, "encoding"))
	input := chi.URLParam(r, "input")

	if !validateStruct(w, decodeParams{Encoding: encoding}) {
		return
	}

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

type caseParams struct {
	Style string `validate:"required,oneof=lower upper title camel snake kebab"`
}

func apiCaseHandler(w http.ResponseWriter, r *http.Request) {
	style := strings.ToLower(chi.URLParam(r, "style"))
	input := chi.URLParam(r, "input")

	if !validateStruct(w, caseParams{Style: style}) {
		return
	}

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
	if err := decodeJSONBody(r, &input); err != nil {
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
	if err := decodeJSONBody(r, &input); err != nil {
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
	if err := decodeJSONBody(r, &input); err != nil {
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
