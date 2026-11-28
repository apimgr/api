package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/health"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/server/handler"
)

// newTestConfig points the paths package at fresh per-test temp
// directories and loads a real default config through it, so New() and
// the handlers under test see production-shaped config values instead of
// a hand-rolled struct that could drift from defaultConfig().
func newTestConfig(t *testing.T) *config.ServerConfig {
	t.Helper()
	configDir := t.TempDir()
	dataDir := t.TempDir()
	logDir := t.TempDir()
	paths.SetApplicationPathOverrides(configDir, dataDir, logDir)

	cfg, err := config.Load(nil)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	return cfg
}

// reqWithParams builds a request carrying chi URL params, mimicking what
// the chi router injects before invoking a leaf handler, so handlers can
// be unit tested directly without standing up the full router tree.
func reqWithParams(method, target string, params map[string]string, body *bytes.Buffer) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, body)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}

	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// decodeJSONResponseBody decodes a recorder body without changing its envelope.
func decodeJSONResponseBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	return out
}

// decodeJSON unwraps a successful PART 14 envelope for handler data assertions.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	body := decodeJSONResponseBody(t, rec)
	if body["ok"] == true {
		if data, ok := body["data"].(map[string]interface{}); ok {
			return data
		}
	}
	return body
}

// newTestServer builds the real router via New(). initTemplates() parses
// the spec-mandated layout/public.tmpl plus the mandatory partial set
// (partial/*.tmpl, partial/public/*.tmpl) against every publicPages entry,
// all of which now have a corresponding template/page/*.tmpl file, so
// New() no longer panics; the recover here guards against a future
// regression reintroducing a template-wiring gap.
func newTestServer(t *testing.T, cfg *config.ServerConfig) *http.Server {
	t.Helper()
	if db := database.GetServerDB(); db != nil {
		handler.SetDatabase(db)
	}
	handler.SetSchedulerProbe(func() error { return nil })
	t.Cleanup(func() {
		handler.SetDatabase(nil)
		handler.SetSchedulerProbe(nil)
	})
	var srv *http.Server
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("New(cfg) panicked: %v", r)
			}
		}()
		srv = NewHTTPServer(cfg)
	}()
	return srv
}

// TestNew_RouterServesCoreEndpoints builds the real router via New() with
// a production-shaped config and drives representative routes through it
// end-to-end (via httptest.NewServer), covering static/special files,
// health/version, and a nested API route registration all at once.
func TestNew_RouterServesCoreEndpoints(t *testing.T) {
	cfg := newTestConfig(t)
	require.NoError(t, database.InitDatabase(cfg.Server.Database, paths.DataDir()))
	defer func() { _ = database.Close() }()
	require.NoError(t, database.EnsureAppSecrets(t.Context()))
	require.NoError(t, database.EnsureCaptchaSchema(t.Context()))
	require.NoError(t, database.EnsureRateLimitSchema(t.Context()))
	// This test exercises route registration/wiring across every tool page,
	// not rate-limiting behavior (that is covered by ratelimit_test.go), so
	// raise the read-class ceiling well above the table size to avoid a
	// false 429 as more tool pages are added over time.
	cfg.Server.RateLimit.Read.Requests = 10000
	cfg.Server.RateLimit.GlobalBurst = 10000
	srv := newTestServer(t, cfg)
	require.NotNil(t, srv)
	require.NotNil(t, srv.Handler)

	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"home page", http.MethodGet, "/", http.StatusOK},
		{"about page", http.MethodGet, "/server/about", http.StatusOK},
		{"privacy page", http.MethodGet, "/server/privacy", http.StatusOK},
		{"DPO page", http.MethodGet, "/server/dpo", http.StatusOK},
		{"DPO API mirror", http.MethodGet, "/api/v1/server/dpo", http.StatusOK},
		{"contact page", http.MethodGet, "/server/contact", http.StatusOK},
		{"contact API descriptor", http.MethodGet, "/api/v1/server/contact", http.StatusOK},
		{"help page", http.MethodGet, "/server/help", http.StatusOK},
		{"MCP help page", http.MethodGet, "/server/help/mcp", http.StatusOK},
		{"help API mirror", http.MethodGet, "/api/v1/server/help", http.StatusOK},
		{"MCP help API mirror", http.MethodGet, "/api/v1/server/help/mcp", http.StatusOK},
		{"terms page", http.MethodGet, "/server/terms", http.StatusOK},
		{"security page", http.MethodGet, "/server/security", http.StatusOK},
		{"security policy page", http.MethodGet, "/server/security/policy", http.StatusOK},
		{"security acknowledgments page", http.MethodGet, "/server/security/thanks", http.StatusOK},
		{"LLM discovery file", http.MethodGet, "/llms.txt", http.StatusOK},
		{"robots.txt", http.MethodGet, "/robots.txt", http.StatusOK},
		{"noncanonical security.txt path", http.MethodGet, "/security.txt", http.StatusNotFound},
		{"well-known security.txt", http.MethodGet, "/.well-known/security.txt", http.StatusOK},
		{"unconfigured PGP public key", http.MethodGet, "/.well-known/pgp-key.asc", http.StatusNotFound},
		{"manifest.json", http.MethodGet, "/manifest.json", http.StatusOK},
		{"static stylesheet", http.MethodGet, "/static/css/common.css", http.StatusOK},
		{"static application script", http.MethodGet, "/static/js/app.js", http.StatusOK},
		{"healthz frontend", http.MethodGet, "/server/healthz", http.StatusOK},
		{"healthz api", http.MethodGet, "/api/v1/server/healthz", http.StatusOK},
		{"healthz unversioned alias", http.MethodGet, "/api/healthz", http.StatusOK},
		// Per-service bearer tokens are mandatory (PART 20); the test config
		// leaves them empty, which disables the service and answers 403.
		{"metrics without token", http.MethodGet, "/metrics", http.StatusUnauthorized},
		{"unknown cli release artifact", http.MethodGet, "/api/v1/cli/binaries/api-cli-linux-amd64", http.StatusNotFound},
		{"unversioned autodiscover", http.MethodGet, "/api/autodiscover", http.StatusOK},
		{"versioned API root", http.MethodGet, "/api/v1", http.StatusOK},
		{"api v1 uuid", http.MethodGet, "/api/v1/text/uuid", http.StatusOK},
		{"api terms page", http.MethodGet, "/api/v1/server/terms", http.StatusOK},
		{"api security policy page", http.MethodGet, "/api/v1/server/security/policy", http.StatusOK},
		{"api security page", http.MethodGet, "/api/v1/server/security", http.StatusOK},
		{"api security acknowledgments", http.MethodGet, "/api/v1/server/security/thanks", http.StatusOK},
		{"api v1 docker version missing image", http.MethodGet, "/api/v1/docker/version", http.StatusBadRequest},
		{"unknown route 404", http.MethodGet, "/this-route-does-not-exist", http.StatusNotFound},
		{"network category page", http.MethodGet, "/network", http.StatusOK},
		{"categories index page", http.MethodGet, "/categories", http.StatusOK},
		{"convert category page", http.MethodGet, "/convert", http.StatusOK},
		{"dev category page", http.MethodGet, "/dev", http.StatusOK},
		{"docker category page", http.MethodGet, "/docker", http.StatusOK},
		{"fun category page", http.MethodGet, "/fun", http.StatusOK},
		{"generate category page", http.MethodGet, "/generate", http.StatusOK},
		{"generate barcode tool page", http.MethodGet, "/generate/barcode", http.StatusOK},
		{"generate avatar tool page", http.MethodGet, "/generate/avatar", http.StatusOK},
		{"generate config tool page", http.MethodGet, "/generate/config", http.StatusOK},
		{"generate sql tool page", http.MethodGet, "/generate/sql", http.StatusOK},
		{"generate api-docs tool page", http.MethodGet, "/generate/api-docs", http.StatusOK},
		{"generate license tool page", http.MethodGet, "/generate/license", http.StatusOK},
		{"generate gitignore tool page", http.MethodGet, "/generate/gitignore", http.StatusOK},
		{"generate dockerfile tool page", http.MethodGet, "/generate/dockerfile", http.StatusOK},
		{"generate ssh-key tool page", http.MethodGet, "/generate/ssh-key", http.StatusOK},
		{"generate identicon tool page", http.MethodGet, "/generate/identicon", http.StatusOK},
		{"generate placeholder tool page", http.MethodGet, "/generate/placeholder", http.StatusOK},
		{"geo category page", http.MethodGet, "/geo", http.StatusOK},
		{"image category page", http.MethodGet, "/image", http.StatusOK},
		{"language category page", http.MethodGet, "/language", http.StatusOK},
		{"language phonetic tool page", http.MethodGet, "/language/phonetic", http.StatusOK},
		{"language word-count tool page", http.MethodGet, "/language/word-count", http.StatusOK},
		{"language keywords tool page", http.MethodGet, "/language/keywords", http.StatusOK},
		{"language readability tool page", http.MethodGet, "/language/readability", http.StatusOK},
		{"language reading-time tool page", http.MethodGet, "/language/reading-time", http.StatusOK},
		{"language sentiment tool page", http.MethodGet, "/language/sentiment", http.StatusOK},
		{"lorem category page", http.MethodGet, "/lorem", http.StatusOK},
		{"math category page", http.MethodGet, "/math", http.StatusOK},
		{"osint category page", http.MethodGet, "/osint", http.StatusOK},
		{"parse category page", http.MethodGet, "/parse", http.StatusOK},
		{"research category page", http.MethodGet, "/research", http.StatusOK},
		{"system category page", http.MethodGet, "/system", http.StatusOK},
		{"testing category page", http.MethodGet, "/testing", http.StatusOK},
		{"validate category page", http.MethodGet, "/validate", http.StatusOK},
		{"weather category page", http.MethodGet, "/weather", http.StatusOK},
		{"crypto hash tool page", http.MethodGet, "/crypto/hash", http.StatusOK},
		{"crypto jwt tool page", http.MethodGet, "/crypto/jwt", http.StatusOK},
		{"crypto totp tool page", http.MethodGet, "/crypto/totp", http.StatusOK},
		{"crypto random tool page", http.MethodGet, "/crypto/random", http.StatusOK},
		{"crypto password tool page", http.MethodGet, "/crypto/password", http.StatusOK},
		{"datetime now tool page", http.MethodGet, "/datetime/now", http.StatusOK},
		{"network ip tool page", http.MethodGet, "/network/ip", http.StatusOK},
		{"network headers tool page", http.MethodGet, "/network/headers", http.StatusOK},
		{"network dns tool page", http.MethodGet, "/network/dns", http.StatusOK},
		{"text uuid tool page", http.MethodGet, "/text/uuid", http.StatusOK},
		{"text hash tool page", http.MethodGet, "/text/hash", http.StatusOK},
		{"crypto bcrypt tool page", http.MethodGet, "/crypto/bcrypt", http.StatusOK},
		{"crypto pin tool page", http.MethodGet, "/crypto/pin", http.StatusOK},
		{"crypto password-strength tool page", http.MethodGet, "/crypto/password-strength", http.StatusOK},
		{"crypto encrypt tool page", http.MethodGet, "/crypto/encrypt", http.StatusOK},
		{"crypto decrypt tool page", http.MethodGet, "/crypto/decrypt", http.StatusOK},
		{"crypto rsa tool page", http.MethodGet, "/crypto/rsa", http.StatusOK},
		{"crypto hmac tool page", http.MethodGet, "/crypto/hmac", http.StatusOK},
		{"crypto certificate tool page", http.MethodGet, "/crypto/certificate", http.StatusOK},
		{"crypto ed25519 tool page", http.MethodGet, "/crypto/ed25519", http.StatusOK},
		{"crypto pgp tool page", http.MethodGet, "/crypto/pgp", http.StatusOK},
		{"datetime timestamp tool page", http.MethodGet, "/datetime/timestamp", http.StatusOK},
		{"network user-agent tool page", http.MethodGet, "/network/user-agent", http.StatusOK},
		{"network mac tool page", http.MethodGet, "/network/mac", http.StatusOK},
		{"docker version tool page", http.MethodGet, "/docker/version", http.StatusOK},
		{"docker port-mapping tool page", http.MethodGet, "/docker/port-mapping", http.StatusOK},
		{"docker volume-helper tool page", http.MethodGet, "/docker/volume-helper", http.StatusOK},
		{"docker dockerfile-generate tool page", http.MethodGet, "/docker/dockerfile-generate", http.StatusOK},
		{"docker dockerfile-lint tool page", http.MethodGet, "/docker/dockerfile-lint", http.StatusOK},
		{"docker best-practices tool page", http.MethodGet, "/docker/best-practices", http.StatusOK},
		{"docker compose-validate tool page", http.MethodGet, "/docker/compose-validate", http.StatusOK},
		{"docker compose-to-run tool page", http.MethodGet, "/docker/compose-to-run", http.StatusOK},
		{"docker run-to-compose tool page", http.MethodGet, "/docker/run-to-compose", http.StatusOK},
		{"docker env-parser tool page", http.MethodGet, "/docker/env-parser", http.StatusOK},
		{"docker network-helper tool page", http.MethodGet, "/docker/network-helper", http.StatusOK},
		{"docker security-scan tool page", http.MethodGet, "/docker/security-scan", http.StatusOK},
		{"docker size-optimizer tool page", http.MethodGet, "/docker/size-optimizer", http.StatusOK},
		{"network subnet tool page", http.MethodGet, "/network/subnet", http.StatusOK},
		{"network ula tool page", http.MethodGet, "/network/ula", http.StatusOK},
		{"network port tool page", http.MethodGet, "/network/port", http.StatusOK},
		{"network ping tool page", http.MethodGet, "/network/ping", http.StatusOK},
		{"network ssl tool page", http.MethodGet, "/network/ssl", http.StatusOK},
		{"network url tool page", http.MethodGet, "/network/url", http.StatusOK},
		{"network whois tool page", http.MethodGet, "/network/whois", http.StatusOK},
		{"weather current tool page", http.MethodGet, "/weather/current", http.StatusOK},
		{"weather forecast tool page", http.MethodGet, "/weather/forecast", http.StatusOK},
		{"geo ip tool page", http.MethodGet, "/geo/ip", http.StatusOK},
		{"geo distance tool page", http.MethodGet, "/geo/distance", http.StatusOK},
		{"geo bearing tool page", http.MethodGet, "/geo/bearing", http.StatusOK},
		{"geo midpoint tool page", http.MethodGet, "/geo/midpoint", http.StatusOK},
		{"geo geocode tool page", http.MethodGet, "/geo/geocode", http.StatusOK},
		{"geo reverse tool page", http.MethodGet, "/geo/reverse", http.StatusOK},
		{"geo timezone tool page", http.MethodGet, "/geo/timezone", http.StatusOK},
		{"geo country tool page", http.MethodGet, "/geo/country", http.StatusOK},
		{"geo geohash tool page", http.MethodGet, "/geo/geohash", http.StatusOK},
		{"geo h3 tool page", http.MethodGet, "/geo/h3", http.StatusOK},
		{"geo pluscode tool page", http.MethodGet, "/geo/pluscode", http.StatusOK},
		{"geo bbox tool page", http.MethodGet, "/geo/bbox", http.StatusOK},
		{"convert length tool page", http.MethodGet, "/convert/length", http.StatusOK},
		{"convert temperature tool page", http.MethodGet, "/convert/temperature", http.StatusOK},
		{"convert weight tool page", http.MethodGet, "/convert/weight", http.StatusOK},
		{"convert volume tool page", http.MethodGet, "/convert/volume", http.StatusOK},
		{"convert time tool page", http.MethodGet, "/convert/time", http.StatusOK},
		{"convert area tool page", http.MethodGet, "/convert/area", http.StatusOK},
		{"convert data tool page", http.MethodGet, "/convert/data", http.StatusOK},
		{"convert energy tool page", http.MethodGet, "/convert/energy", http.StatusOK},
		{"convert pressure tool page", http.MethodGet, "/convert/pressure", http.StatusOK},
		{"convert speed tool page", http.MethodGet, "/convert/speed", http.StatusOK},
		{"convert color tool page", http.MethodGet, "/convert/color", http.StatusOK},
		{"convert currency tool page", http.MethodGet, "/convert/currency", http.StatusOK},
		{"math calculate tool page", http.MethodGet, "/math/calculate", http.StatusOK},
		{"math gcd tool page", http.MethodGet, "/math/gcd", http.StatusOK},
		{"math percentage tool page", http.MethodGet, "/math/percentage", http.StatusOK},
		{"math logarithm tool page", http.MethodGet, "/math/logarithm", http.StatusOK},
		{"math trigonometry tool page", http.MethodGet, "/math/trigonometry", http.StatusOK},
		{"math prime tool page", http.MethodGet, "/math/prime", http.StatusOK},
		{"math random tool page", http.MethodGet, "/math/random", http.StatusOK},
		{"math stats tool page", http.MethodGet, "/math/stats", http.StatusOK},
		{"math fibonacci tool page", http.MethodGet, "/math/fibonacci", http.StatusOK},
		{"math base tool page", http.MethodGet, "/math/base", http.StatusOK},
		{"math matrix tool page", http.MethodGet, "/math/matrix", http.StatusOK},
		{"math sequence tool page", http.MethodGet, "/math/sequence", http.StatusOK},
		{"parse json tool page", http.MethodGet, "/parse/json", http.StatusOK},
		{"parse xml tool page", http.MethodGet, "/parse/xml", http.StatusOK},
		{"parse csv tool page", http.MethodGet, "/parse/csv", http.StatusOK},
		{"parse jwt tool page", http.MethodGet, "/parse/jwt", http.StatusOK},
		{"parse env tool page", http.MethodGet, "/parse/env", http.StatusOK},
		{"parse html tool page", http.MethodGet, "/parse/html", http.StatusOK},
		{"parse ini tool page", http.MethodGet, "/parse/ini", http.StatusOK},
		{"parse log tool page", http.MethodGet, "/parse/log", http.StatusOK},
		{"parse markdown tool page", http.MethodGet, "/parse/markdown", http.StatusOK},
		{"parse sql tool page", http.MethodGet, "/parse/sql", http.StatusOK},
		{"parse toml tool page", http.MethodGet, "/parse/toml", http.StatusOK},
		{"parse yaml tool page", http.MethodGet, "/parse/yaml", http.StatusOK},
		{"research citation tool page", http.MethodGet, "/research/citation", http.StatusOK},
		{"research doi tool page", http.MethodGet, "/research/doi", http.StatusOK},
		{"fun joke tool page", http.MethodGet, "/fun/joke", http.StatusOK},
		{"fun fortune tool page", http.MethodGet, "/fun/fortune", http.StatusOK},
		{"fun dad-joke tool page", http.MethodGet, "/fun/dad-joke", http.StatusOK},
		{"fun programming-joke tool page", http.MethodGet, "/fun/programming-joke", http.StatusOK},
		{"fun quote tool page", http.MethodGet, "/fun/quote", http.StatusOK},
		{"fun fact tool page", http.MethodGet, "/fun/fact", http.StatusOK},
		{"fun riddle tool page", http.MethodGet, "/fun/riddle", http.StatusOK},
		{"fun trivia tool page", http.MethodGet, "/fun/trivia", http.StatusOK},
		{"fun motivational tool page", http.MethodGet, "/fun/motivational", http.StatusOK},
		{"fun insult tool page", http.MethodGet, "/fun/insult", http.StatusOK},
		{"fun compliment tool page", http.MethodGet, "/fun/compliment", http.StatusOK},
		{"fun meme tool page", http.MethodGet, "/fun/meme", http.StatusOK},
		{"lorem person tool page", http.MethodGet, "/lorem/person", http.StatusOK},
		{"lorem address tool page", http.MethodGet, "/lorem/address", http.StatusOK},
		{"lorem company tool page", http.MethodGet, "/lorem/company", http.StatusOK},
		{"testing http tool page", http.MethodGet, "/testing/http", http.StatusOK},
		{"testing assertions tool page", http.MethodGet, "/testing/assertions", http.StatusOK},
		{"testing fixtures tool page", http.MethodGet, "/testing/fixtures", http.StatusOK},
		{"testing fake-data tool page", http.MethodGet, "/testing/fake-data", http.StatusOK},
		{"osint email tool page", http.MethodGet, "/osint/email", http.StatusOK},
		{"osint domain tool page", http.MethodGet, "/osint/domain", http.StatusOK},
		{"osint ip tool page", http.MethodGet, "/osint/ip", http.StatusOK},
		{"osint cert tool page", http.MethodGet, "/osint/cert", http.StatusOK},
		{"osint subdomain tool page", http.MethodGet, "/osint/subdomain", http.StatusOK},
		{"osint tech-stack tool page", http.MethodGet, "/osint/tech-stack", http.StatusOK},
		{"dev format json tool page", http.MethodGet, "/dev/format-json", http.StatusOK},
		{"dev base64 tool page", http.MethodGet, "/dev/base64", http.StatusOK},
		{"dev url-encode tool page", http.MethodGet, "/dev/url-encode", http.StatusOK},
		{"validate email tool page", http.MethodGet, "/validate/email", http.StatusOK},
		{"validate credit-card tool page", http.MethodGet, "/validate/credit-card", http.StatusOK},
		{"validate domain tool page", http.MethodGet, "/validate/domain", http.StatusOK},
		{"validate ip tool page", http.MethodGet, "/validate/ip", http.StatusOK},
		{"validate json tool page", http.MethodGet, "/validate/json", http.StatusOK},
		{"validate mac tool page", http.MethodGet, "/validate/mac", http.StatusOK},
		{"validate phone tool page", http.MethodGet, "/validate/phone", http.StatusOK},
		{"validate url tool page", http.MethodGet, "/validate/url", http.StatusOK},
		{"validate uuid tool page", http.MethodGet, "/validate/uuid", http.StatusOK},
		{"validate iban tool page", http.MethodGet, "/validate/iban", http.StatusOK},
		{"validate isbn tool page", http.MethodGet, "/validate/isbn", http.StatusOK},
		{"validate vat tool page", http.MethodGet, "/validate/vat", http.StatusOK},
		{"image placeholder tool page", http.MethodGet, "/image/placeholder", http.StatusOK},
		{"image resize tool page", http.MethodGet, "/image/resize", http.StatusOK},
		{"image crop tool page", http.MethodGet, "/image/crop", http.StatusOK},
		{"image metadata tool page", http.MethodGet, "/image/metadata", http.StatusOK},
		{"image convert tool page", http.MethodGet, "/image/convert", http.StatusOK},
		{"image avatar tool page", http.MethodGet, "/image/avatar", http.StatusOK},
		{"image barcode tool page", http.MethodGet, "/image/barcode", http.StatusOK},
		{"image identicon tool page", http.MethodGet, "/image/identicon", http.StatusOK},
		{"image filter tool page", http.MethodGet, "/image/filter", http.StatusOK},
		{"image optimize tool page", http.MethodGet, "/image/optimize", http.StatusOK},
		{"image watermark tool page", http.MethodGet, "/image/watermark", http.StatusOK},
		{"text encode tool page", http.MethodGet, "/text/encode", http.StatusOK},
		{"text decode tool page", http.MethodGet, "/text/decode", http.StatusOK},
		{"text case tool page", http.MethodGet, "/text/case", http.StatusOK},
		{"text lorem tool page", http.MethodGet, "/text/lorem", http.StatusOK},
		{"datetime convert tool page", http.MethodGet, "/datetime/convert", http.StatusOK},
		{"datetime unix tool page", http.MethodGet, "/datetime/unix", http.StatusOK},
		{"datetime add tool page", http.MethodGet, "/datetime/add", http.StatusOK},
		{"datetime diff tool page", http.MethodGet, "/datetime/diff", http.StatusOK},
		{"datetime format tool page", http.MethodGet, "/datetime/format", http.StatusOK},
		{"datetime parse tool page", http.MethodGet, "/datetime/parse", http.StatusOK},
		{"datetime cron tool page", http.MethodGet, "/datetime/cron", http.StatusOK},
		{"datetime calendar tool page", http.MethodGet, "/datetime/calendar", http.StatusOK},
		{"datetime workdays tool page", http.MethodGet, "/datetime/workdays", http.StatusOK},
		{"datetime sunrise tool page", http.MethodGet, "/datetime/sunrise", http.StatusOK},
		{"datetime moon tool page", http.MethodGet, "/datetime/moon", http.StatusOK},
		{"text compress tool page", http.MethodGet, "/text/compress", http.StatusOK},
		{"text diff tool page", http.MethodGet, "/text/diff", http.StatusOK},
		{"text extract tool page", http.MethodGet, "/text/extract", http.StatusOK},
		{"text nanoid tool page", http.MethodGet, "/text/nanoid", http.StatusOK},
		{"text ulid tool page", http.MethodGet, "/text/ulid", http.StatusOK},
		{"text regex tool page", http.MethodGet, "/text/regex", http.StatusOK},
		{"dev regex tool page", http.MethodGet, "/dev/regex", http.StatusOK},
		{"dev cron tool page", http.MethodGet, "/dev/cron", http.StatusOK},
		{"dev jwt tool page", http.MethodGet, "/dev/jwt", http.StatusOK},
		{"dev echo tool page", http.MethodGet, "/dev/echo", http.StatusOK},
		{"dev css-format tool page", http.MethodGet, "/dev/css-format", http.StatusOK},
		{"dev html-format tool page", http.MethodGet, "/dev/html-format", http.StatusOK},
		{"dev js-format tool page", http.MethodGet, "/dev/js-format", http.StatusOK},
		{"dev sql-format tool page", http.MethodGet, "/dev/sql-format", http.StatusOK},
		{"dev xml-format tool page", http.MethodGet, "/dev/xml-format", http.StatusOK},
		{"api v1 text nanoid", http.MethodGet, "/api/v1/text/nanoid", http.StatusOK},
		{"api v1 text ulid", http.MethodGet, "/api/v1/text/ulid", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, ts.URL+tt.path, nil)
			require.NoError(t, err)
			resp, err := ts.Client().Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			assert.Equal(t, tt.wantStatus, resp.StatusCode, "path %s", tt.path)
		})
	}
}

func TestHomePageToolCountMatchesRegisteredToolPages(t *testing.T) {
	cfg := newTestConfig(t)
	srv := newTestServer(t, cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), i18n.TranslateFormat("en", "categories.summary", map[string]string{
		"tools":      strconv.Itoa(len(toolPages())),
		"categories": strconv.Itoa(len(allCategories())),
	}))
	assert.NotContains(t, rec.Body.String(), "1,418")
}

func TestInteractiveDocsUseSharedShellAndLocalAssets(t *testing.T) {
	srv := newTestServer(t, newTestConfig(t))
	for _, tt := range []struct {
		path       string
		mountID    string
		localAsset string
		endpoint   string
	}{
		{"/server/docs/swagger", "swagger-ui", "/static/vendor/swagger-ui/swagger-ui-bundle.js", "/api/v1/server/swagger"},
		{"/server/docs/graphql", "graphiql", "/static/vendor/graphiql/graphiql.min.js", "/api/v1/server/graphql"},
	} {
		t.Run(tt.mountID, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("Accept", "text/html")
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			body := rec.Body.String()
			assert.Contains(t, body, `class="header"`)
			assert.Contains(t, body, `class="footer"`)
			assert.Contains(t, body, `class="page-title"`)
			assert.Contains(t, body, tt.mountID)
			assert.Contains(t, body, tt.localAsset)
			assert.Contains(t, body, tt.endpoint)
			assert.Contains(t, body, "<noscript>")
			assert.NotContains(t, body, "https://cdn.")
			assert.NotContains(t, body, "<script>")
		})
	}
}

func TestGraphQLGETAliasesServeReadOnlyQueries(t *testing.T) {
	srv := newTestServer(t, newTestConfig(t))
	query := url.QueryEscape("{ health { status } }")
	for _, endpoint := range []string{"/api/graphql", "/api/v1/server/graphql"} {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, endpoint+"?query="+query, nil)
			req.Header.Set("Accept", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
			assert.Contains(t, rec.Body.String(), `"data"`)
		})
	}
}

func TestBrowserNotFoundUsesThemedSharedLayout(t *testing.T) {
	srv := newTestServer(t, newTestConfig(t))
	req := httptest.NewRequest(http.MethodGet, "/unknown-browser-page", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), `class="header"`)
	assert.Contains(t, rec.Body.String(), `class="footer"`)
	assert.Contains(t, rec.Body.String(), `class="error-title page-title"`)
	assert.Contains(t, rec.Body.String(), "/static/css/common.css")
}

func TestStaticStylesheetHasBrowserStylesheetContentType(t *testing.T) {
	srv := newTestServer(t, newTestConfig(t))
	req := httptest.NewRequest(http.MethodGet, "/static/css/common.css", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/css")
	assert.Contains(t, rec.Body.String(), "body {")
}

func TestNew_ConfiguredAPIVersionRoutesAndOpenAPI(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.APIVersion = "v2"
	cfg.Server.RateLimit.Read.Requests = 10000
	srv := newTestServer(t, cfg)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/text/uuid")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp, err = http.Get(ts.URL + "/api/v1/text/uuid")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	request, err := http.NewRequest(http.MethodGet, ts.URL+"/api/swagger", nil)
	require.NoError(t, err)
	request.Header.Set("Accept", "application/json")
	resp, err = http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var spec map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&spec))
	paths, ok := spec["paths"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, paths, "/api/v2/text/uuid")
	assert.NotContains(t, paths, "/api/v1/text/uuid")
}

// TestNew_SecurityHeadersApplied confirms the security headers middleware
// registered in New() actually fires on a real request through the
// assembled router (not just in isolation).
func TestNew_SecurityHeadersApplied(t *testing.T) {
	cfg := newTestConfig(t)
	srv := newTestServer(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
}

// TestNew_HealthzRootAlias covers the config-gated optional "/healthz"
// root alias: present when enabled, absent (404) when not.
func TestNew_HealthzRootAlias(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		cfg := newTestConfig(t)
		require.NoError(t, database.InitDatabase(cfg.Server.Database, paths.DataDir()))
		t.Cleanup(func() { _ = database.Close() })
		cfg.Server.Healthz.Root.Enabled = true
		cfg.Server.RateLimit.Health.Requests = 10000
		cfg.Server.RateLimit.GlobalBurst = 10000
		srv := newTestServer(t, cfg)

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := newTestConfig(t)
		cfg.Server.Healthz.Root.Enabled = false
		srv := newTestServer(t, cfg)

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

// TestNewPageData covers request-aware public origin resolution for page
// templates.
func TestNewPageData(t *testing.T) {
	t.Setenv("DOMAIN", "")
	cfg := &config.ServerConfig{}
	cfg.Server.FQDN = "configured.example"
	cfg.Server.Port = "8080"
	cfg.Server.Branding.Title = "Test Title"
	cfg.Web.UI.Theme = "dark"
	req := httptest.NewRequest(http.MethodGet, "http://visitor.example/", nil)
	data := newPageData(cfg, req, "home")

	assert.Equal(t, "http://visitor.example:8080", data.BaseURL)
	assert.Equal(t, "Test Title", data.SiteTitle)
	assert.Equal(t, "dark", data.Theme)
	assert.Equal(t, "theme-dark", data.ThemeClass)
	assert.Equal(t, "home", data.ActivePage)
	assert.Equal(t, "🛠️", data.SiteIcon)
	assert.Equal(t, len(toolPages()), data.TotalTools)
	assert.Equal(t, len(allCategories()), data.TotalCategories)
}

// TestBuildURLUsesConfiguredServerPort verifies the request-aware origin
// falls back to the configured listen port when the public host has none.
func TestBuildURLUsesConfiguredServerPort(t *testing.T) {
	t.Setenv("DOMAIN", "")
	cfg := &config.ServerConfig{}
	cfg.Server.Port = "8080"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "api.example.com"
	req = requestWithServerConfig(req, cfg)

	assert.Equal(t, "http://api.example.com:8080", BuildURL(req, ""))
}

// TestJSONResponse verifies JSON responses use the PART 14 success envelope.
func TestJSONResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonResponse(rec, map[string]interface{}{"foo": "bar", "n": 42})

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	got := decodeJSONResponseBody(t, rec)
	assert.Equal(t, true, got["ok"])
	data, ok := got["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "bar", data["foo"])
	assert.Equal(t, float64(42), data["n"])
}

// TestTextResponse verifies Content-Type and body are set exactly as
// given, with no transformation.
func TestTextResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	textResponse(rec, "hello world")

	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "hello world", rec.Body.String())
}

// TestErrorResponse covers that the status code, Content-Type, and JSON
// error envelope are all set correctly for a range of status codes.
func TestErrorResponse(t *testing.T) {
	tests := []struct {
		name    string
		message string
		status  int
	}{
		{"bad request", "invalid input", http.StatusBadRequest},
		{"internal error", "boom", http.StatusInternalServerError},
		{"empty message", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
			errorResponse(rec, req, tt.message, tt.status)

			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			got := decodeJSONResponseBody(t, rec)
			assert.Equal(t, false, got["ok"])
			assert.NotEmpty(t, got["error"])
			assert.NotEmpty(t, got["message"])
			if tt.message != "" {
				assert.NotContains(t, got["message"], tt.message)
			}
		})
	}
}

func TestErrorResponseUsesRequestLanguage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test?lang=es", nil)
	rec := httptest.NewRecorder()
	errorResponse(rec, req, "internal detail must not escape", http.StatusInternalServerError)
	got := decodeJSONResponseBody(t, rec)
	assert.Equal(t, "Error interno del servidor", got["message"])
	assert.NotContains(t, rec.Body.String(), "internal detail must not escape")
}

// TestApiUUIDHandler covers default (v4) and explicit version params, and
// that the JSON envelope contains a non-empty uuid plus the echoed
// version.
func TestApiUUIDHandler(t *testing.T) {
	tests := []struct {
		name        string
		versionParm string
		wantVersion float64
	}{
		{"no version param defaults to 4", "", 4},
		{"explicit v4", "4", 4},
		{"explicit v1", "1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]string{}
			if tt.versionParm != "" {
				params["version"] = tt.versionParm
			}
			req := reqWithParams(http.MethodGet, "/api/v1/text/uuid", params, nil)
			rec := httptest.NewRecorder()

			apiUUIDHandler(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			got := decodeJSONResponseBody(t, rec)
			assert.Equal(t, true, got["ok"])
			data, ok := got["data"].(map[string]interface{})
			require.True(t, ok)
			assert.NotEmpty(t, data["uuid"])
			assert.Equal(t, tt.wantVersion, data["version"])
		})
	}
}

// TestApiUUIDHandler_UnrecognizedVersionFallsBackToV4 covers that an
// unrecognized version number is not an error — text.UUID's default case
// silently falls back to a v4 UUID rather than rejecting the request.
func TestApiUUIDHandler_UnrecognizedVersionFallsBackToV4(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/uuid/99", map[string]string{"version": "99"}, nil)
	rec := httptest.NewRecorder()

	apiUUIDHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	got := decodeJSONResponseBody(t, rec)
	assert.Equal(t, true, got["ok"])
	data, ok := got["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["uuid"])
	assert.Equal(t, float64(99), data["version"])
}

// TestApiUUIDTextHandler mirrors the JSON case but for the plain-text
// variant.
func TestApiUUIDTextHandler(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/uuid.txt", nil, nil)
		rec := httptest.NewRecorder()
		apiUUIDTextHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotEmpty(t, rec.Body.String())
	})

	t.Run("unrecognized version still succeeds via v4 fallback", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/uuid/99.txt", map[string]string{"version": "99"}, nil)
		rec := httptest.NewRecorder()
		apiUUIDTextHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.False(t, strings.HasPrefix(rec.Body.String(), "Error:"))
	})
}

// TestApiUUIDBatchHandler covers default count/version, an explicit
// count, and the boundary case of a zero count, which text.UUIDs clamps
// up to 1 rather than returning an empty batch.
func TestApiUUIDBatchHandler(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		count     string
		wantCount float64
	}{
		{"defaults", "", "", 10},
		{"explicit count 3", "4", "3", 3},
		{"zero count clamped to 1", "4", "0", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]string{}
			if tt.version != "" {
				params["version"] = tt.version
			}
			if tt.count != "" {
				params["count"] = tt.count
			}
			req := reqWithParams(http.MethodGet, "/api/v1/text/uuid/4/3", params, nil)
			rec := httptest.NewRecorder()

			apiUUIDBatchHandler(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			got := decodeJSONResponseBody(t, rec)
			assert.Equal(t, true, got["ok"])
			data, ok := got["data"].(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, tt.wantCount, data["count"])
			uuids, ok := data["uuids"].([]interface{})
			require.True(t, ok)
			assert.Len(t, uuids, int(tt.wantCount))
		})
	}
}

// TestApiHashHandler covers a supported algorithm, an unsupported one
// (must 400 with a JSON error, never 500), and an empty input.
func TestApiHashHandler(t *testing.T) {
	tests := []struct {
		name       string
		algorithm  string
		input      string
		wantStatus int
	}{
		{"sha256 of hello", "sha256", "hello", http.StatusOK},
		{"md5 of empty string", "md5", "", http.StatusOK},
		{"unsupported algorithm", "not-a-real-algo", "hello", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithParams(http.MethodGet, "/api/v1/text/hash/x/y", map[string]string{
				"algorithm": tt.algorithm,
				"input":     tt.input,
			}, nil)
			rec := httptest.NewRecorder()

			apiHashHandler(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			got := decodeJSON(t, rec)
			if tt.wantStatus == http.StatusOK {
				assert.NotEmpty(t, got["hash"])
			} else {
				assert.NotEmpty(t, got["error"])
			}
		})
	}
}

// TestApiHashMultiHandler covers that every requested algorithm shows up
// in the map, including for an empty input string.
func TestApiHashMultiHandler(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/hash/multi/hello", map[string]string{"input": "hello"}, nil)
	rec := httptest.NewRecorder()

	apiHashMultiHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	got := decodeJSON(t, rec)
	hashes, ok := got["hashes"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, hashes)
}

// TestApiEncodeDecodeHandler_RoundTrip covers every supported encoding
// round-tripping correctly, plus the unsupported-encoding error path for
// both encode and decode.
func TestApiEncodeDecodeHandler_RoundTrip(t *testing.T) {
	encodings := []string{"base64", "base64url", "base32", "hex", "base16", "url"}
	const input = "Hello, World! / test+data="

	for _, enc := range encodings {
		t.Run(enc, func(t *testing.T) {
			encReq := reqWithParams(http.MethodGet, "/api/v1/text/encode/x/y", map[string]string{
				"encoding": enc,
				"input":    input,
			}, nil)
			encRec := httptest.NewRecorder()
			apiEncodeHandler(encRec, encReq)
			require.Equal(t, http.StatusOK, encRec.Code)
			encGot := decodeJSON(t, encRec)
			encoded, _ := encGot["output"].(string)
			require.NotEmpty(t, encoded)

			decReq := reqWithParams(http.MethodGet, "/api/v1/text/decode/x/y", map[string]string{
				"encoding": enc,
				"input":    encoded,
			}, nil)
			decRec := httptest.NewRecorder()
			apiDecodeHandler(decRec, decReq)
			require.Equal(t, http.StatusOK, decRec.Code)
			decGot := decodeJSON(t, decRec)
			assert.Equal(t, input, decGot["output"])
		})
	}

	t.Run("unsupported encoding on encode", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/encode/x/y", map[string]string{
			"encoding": "rot47",
			"input":    "abc",
		}, nil)
		rec := httptest.NewRecorder()
		apiEncodeHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unsupported encoding on decode", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/decode/x/y", map[string]string{
			"encoding": "rot47",
			"input":    "abc",
		}, nil)
		rec := httptest.NewRecorder()
		apiDecodeHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("malformed base64 fails to decode", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/decode/x/y", map[string]string{
			"encoding": "base64",
			"input":    "%%%not-base64%%%",
		}, nil)
		rec := httptest.NewRecorder()
		apiDecodeHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiEncodeTextHandler_UnsupportedEncoding verifies the handler's
// canonical API error envelope for an invalid encoding.
func TestApiEncodeTextHandler_UnsupportedEncoding(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/encode/x/y.txt", map[string]string{
		"encoding": "nope",
		"input":    "abc",
	}, nil)
	rec := httptest.NewRecorder()
	apiEncodeTextHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_ENCODING", decodeJSON(t, rec)["error"])
}

// TestApiDecodeTextHandler_DecodeError verifies the canonical API error
// envelope for invalid encoded input.
func TestApiDecodeTextHandler_DecodeError(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/decode/x/y.txt", map[string]string{
		"encoding": "base64",
		"input":    "%%%",
	}, nil)
	rec := httptest.NewRecorder()
	apiDecodeTextHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_INPUT", decodeJSON(t, rec)["error"])
}

// TestApiCaseHandler covers every supported style plus the unsupported
// style error path.
func TestApiCaseHandler(t *testing.T) {
	tests := []struct {
		name       string
		style      string
		input      string
		wantOutput string
		wantStatus int
	}{
		{"lower", "lower", "HeLLo", "hello", http.StatusOK},
		{"upper", "upper", "HeLLo", "HELLO", http.StatusOK},
		{"snake", "snake", "Hello World", "hello_world", http.StatusOK},
		{"unsupported style", "sarcasm", "Hello", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithParams(http.MethodGet, "/api/v1/text/case/x/y", map[string]string{
				"style": tt.style,
				"input": tt.input,
			}, nil)
			rec := httptest.NewRecorder()
			apiCaseHandler(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusOK {
				got := decodeJSON(t, rec)
				assert.Equal(t, tt.wantOutput, got["output"])
			}
		})
	}
}

// TestApiLoremHandler_Defaults covers the default type ("paragraphs") and
// default count (5) when neither URL param is present.
func TestApiLoremHandler_Defaults(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/lorem", nil, nil)
	rec := httptest.NewRecorder()
	apiLoremHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	got := decodeJSON(t, rec)
	assert.Equal(t, "paragraphs", got["type"])
	assert.Equal(t, float64(5), got["count"])
}

// TestApiLoremHandler_Words covers the words branch with an explicit
// count.
func TestApiLoremHandler_Words(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/lorem/words/3", map[string]string{
		"type":  "words",
		"count": "3",
	}, nil)
	rec := httptest.NewRecorder()
	apiLoremHandler(rec, req)

	got := decodeJSON(t, rec)
	words, ok := got["text"].([]interface{})
	require.True(t, ok)
	assert.Len(t, words, 3)
}

// TestApiTextStatsHandler covers a valid JSON body and the invalid-body
// error path.
func TestApiTextStatsHandler(t *testing.T) {
	t.Run("valid body", func(t *testing.T) {
		body := bytes.NewBufferString(`{"text":"hello world"}`)
		req := reqWithParams(http.MethodPost, "/api/v1/text/stats", nil, body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		apiTextStatsHandler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid json body", func(t *testing.T) {
		body := bytes.NewBufferString(`not json`)
		req := reqWithParams(http.MethodPost, "/api/v1/text/stats", nil, body)
		rec := httptest.NewRecorder()

		apiTextStatsHandler(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("empty body", func(t *testing.T) {
		req := reqWithParams(http.MethodPost, "/api/v1/text/stats", nil, bytes.NewBuffer(nil))
		rec := httptest.NewRecorder()

		apiTextStatsHandler(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiROT13Handler_Idempotent verifies ROT13 applied twice returns the
// original input (a genuine algorithmic property, not just a fixed
// example).
func TestApiROT13Handler_Idempotent(t *testing.T) {
	const input = "Hello, World!"

	req1 := reqWithParams(http.MethodGet, "/api/v1/text/rot13/x", map[string]string{"input": input}, nil)
	rec1 := httptest.NewRecorder()
	apiROT13Handler(rec1, req1)
	got1 := decodeJSON(t, rec1)
	once, _ := got1["output"].(string)
	require.NotEqual(t, input, once)

	req2 := reqWithParams(http.MethodGet, "/api/v1/text/rot13/x", map[string]string{"input": once}, nil)
	rec2 := httptest.NewRecorder()
	apiROT13Handler(rec2, req2)
	got2 := decodeJSON(t, rec2)
	assert.Equal(t, input, got2["output"])
}

// TestApiReverseHandler covers a normal string, an empty string, and a
// palindrome (output equals input).
func TestApiReverseHandler(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantOutput string
	}{
		{"normal string", "abc", "cba"},
		{"empty string", "", ""},
		{"palindrome", "level", "level"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithParams(http.MethodGet, "/api/v1/text/reverse/x", map[string]string{"input": tt.input}, nil)
			rec := httptest.NewRecorder()
			apiReverseHandler(rec, req)

			got := decodeJSON(t, rec)
			assert.Equal(t, tt.wantOutput, got["output"])
		})
	}
}

// bcryptHashForTest generates a bcrypt hash via the same handler-level
// entry point used elsewhere in this file, at a low cost to keep tests
// fast.
func bcryptHashForTest(t *testing.T, password string) string {
	t.Helper()
	req := reqWithParams(http.MethodGet, "/api/v1/crypto/bcrypt/x", map[string]string{
		"password": password,
		"cost":     "4",
	}, nil)
	rec := httptest.NewRecorder()
	apiBcryptHandler(rec, req)
	got := decodeJSON(t, rec)
	hash, _ := got["hash"].(string)
	require.NotEmpty(t, hash)
	return hash
}

// TestApiBcryptHandler_AndVerify covers hash generation at an explicit
// cost plus round-tripping through the GET-param verify handler, both
// for the correct password (valid=true) and a wrong one (valid=false).
func TestApiBcryptHandler_AndVerify(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/crypto/bcrypt/x", map[string]string{
		"password": "correct horse",
		"cost":     "4",
	}, nil)
	rec := httptest.NewRecorder()
	apiBcryptHandler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeJSON(t, rec)
	hash, _ := got["hash"].(string)
	require.NotEmpty(t, hash)
	assert.Equal(t, float64(4), got["cost"])

	t.Run("correct password verifies true", func(t *testing.T) {
		vReq := reqWithParams(http.MethodGet, "/api/v1/crypto/bcrypt/verify/x/y", map[string]string{
			"password": "correct horse",
			"hash":     hash,
		}, nil)
		vRec := httptest.NewRecorder()
		apiBcryptVerifyGetHandler(vRec, vReq)
		vGot := decodeJSON(t, vRec)
		assert.Equal(t, true, vGot["valid"])
	})

	t.Run("wrong password verifies false", func(t *testing.T) {
		vReq := reqWithParams(http.MethodGet, "/api/v1/crypto/bcrypt/verify/x/y", map[string]string{
			"password": "wrong password",
			"hash":     hash,
		}, nil)
		vRec := httptest.NewRecorder()
		apiBcryptVerifyGetHandler(vRec, vReq)
		vGot := decodeJSON(t, vRec)
		assert.Equal(t, false, vGot["valid"])
	})
}

// TestApiBcryptVerifyHandler_PostBody covers the JSON-body verify
// endpoint including the invalid-body error path.
func TestApiBcryptVerifyHandler_PostBody(t *testing.T) {
	hash := bcryptHashForTest(t, "swordfish")

	t.Run("valid body", func(t *testing.T) {
		body := bytes.NewBufferString(`{"password":"swordfish","hash":"` + hash + `"}`)
		req := reqWithParams(http.MethodPost, "/api/v1/crypto/bcrypt/verify", nil, body)
		rec := httptest.NewRecorder()
		apiBcryptVerifyHandler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		got := decodeJSON(t, rec)
		assert.Equal(t, true, got["valid"])
	})

	t.Run("invalid body", func(t *testing.T) {
		body := bytes.NewBufferString(`not json`)
		req := reqWithParams(http.MethodPost, "/api/v1/crypto/bcrypt/verify", nil, body)
		rec := httptest.NewRecorder()
		apiBcryptVerifyHandler(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiPasswordHandler covers the default length and an explicit
// length, and that the generated password's length matches the request.
func TestApiPasswordHandler(t *testing.T) {
	tests := []struct {
		name       string
		lengthParm string
		wantLen    float64
	}{
		{"default length", "", 16},
		{"explicit length 24", "24", 24},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]string{}
			if tt.lengthParm != "" {
				params["length"] = tt.lengthParm
			}
			req := reqWithParams(http.MethodGet, "/api/v1/crypto/password", params, nil)
			rec := httptest.NewRecorder()
			apiPasswordHandler(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			got := decodeJSON(t, rec)
			pw, _ := got["password"].(string)
			assert.Len(t, pw, int(tt.wantLen))
		})
	}
}

// TestApiPINHandler covers an explicit PIN length, and that the PIN is
// digits-only.
func TestApiPINHandler(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/crypto/pin/6", map[string]string{"length": "6"}, nil)
	rec := httptest.NewRecorder()
	apiPINHandler(rec, req)

	got := decodeJSON(t, rec)
	pin, _ := got["pin"].(string)
	assert.Len(t, pin, 6)
	for _, c := range pin {
		assert.True(t, c >= '0' && c <= '9', "PIN must be digits only, got %q", pin)
	}
}

// TestApiTOTPGenerateAndVerify exercises the full TOTP lifecycle: generate
// a secret, derive the current code from it, then verify that code — and
// confirm an obviously-wrong code fails verification.
func TestApiTOTPGenerateAndVerify(t *testing.T) {
	genReq := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/secret", nil, nil)
	genRec := httptest.NewRecorder()
	apiTOTPGenerateHandler(genRec, genReq)
	require.Equal(t, http.StatusOK, genRec.Code)
	genGot := decodeJSON(t, genRec)
	secret, _ := genGot["secret"].(string)
	require.NotEmpty(t, secret)

	codeReq := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/code/x", map[string]string{"secret": secret}, nil)
	codeRec := httptest.NewRecorder()
	apiTOTPCodeHandler(codeRec, codeReq)
	require.Equal(t, http.StatusOK, codeRec.Code)
	codeGot := decodeJSON(t, codeRec)
	code, _ := codeGot["code"].(string)
	require.NotEmpty(t, code)

	t.Run("correct code verifies true", func(t *testing.T) {
		vReq := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/verify/x/y", map[string]string{
			"secret": secret,
			"code":   code,
		}, nil)
		vRec := httptest.NewRecorder()
		apiTOTPVerifyHandler(vRec, vReq)
		vGot := decodeJSON(t, vRec)
		assert.Equal(t, true, vGot["valid"])
	})

	t.Run("wrong code verifies false", func(t *testing.T) {
		vReq := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/verify/x/y", map[string]string{
			"secret": secret,
			"code":   "000000",
		}, nil)
		vRec := httptest.NewRecorder()
		apiTOTPVerifyHandler(vRec, vReq)
		vGot := decodeJSON(t, vRec)
		assert.Equal(t, false, vGot["valid"])
	})
}

// TestApiTOTPCodeHandler_InvalidSecret covers a malformed base32 secret
// producing a 400 rather than a panic.
func TestApiTOTPCodeHandler_InvalidSecret(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/code/x", map[string]string{"secret": "not valid base32!!"}, nil)
	rec := httptest.NewRecorder()
	apiTOTPCodeHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestApiRandomBytesHandler_And_Hex cover that the requested byte count
// is honored (including the zero-count boundary) and that the hex
// variant is exactly twice as long as the requested byte count.
func TestApiRandomBytesHandler_And_Hex(t *testing.T) {
	tests := []struct {
		name  string
		count string
	}{
		{"default count", ""},
		{"explicit count 8", "8"},
		{"zero count", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]string{}
			if tt.count != "" {
				params["count"] = tt.count
			}

			req := reqWithParams(http.MethodGet, "/api/v1/crypto/random/bytes/x", params, nil)
			rec := httptest.NewRecorder()
			apiRandomBytesHandler(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			got := decodeJSON(t, rec)

			hexReq := reqWithParams(http.MethodGet, "/api/v1/crypto/random/hex/x", params, nil)
			hexRec := httptest.NewRecorder()
			apiRandomHexHandler(hexRec, hexReq)
			require.Equal(t, http.StatusOK, hexRec.Code)

			wantLen := got["length"].(float64)
			assert.Len(t, hexRec.Body.String(), int(wantLen)*2)
		})
	}
}

// TestApiPasswordStrengthHandler_And_Post cover both the GET URL-param
// and POST JSON-body variants of password-strength, including the
// malformed JSON body error path for the POST variant.
func TestApiPasswordStrengthHandler_And_Post(t *testing.T) {
	weakReq := reqWithParams(http.MethodGet, "/api/v1/crypto/password/strength/x", map[string]string{"password": "1234"}, nil)
	weakRec := httptest.NewRecorder()
	apiPasswordStrengthHandler(weakRec, weakReq)
	assert.Equal(t, http.StatusOK, weakRec.Code)

	body := bytes.NewBufferString(`{"password":"Tr0ub4dor&3xtraLong!"}`)
	strongReq := reqWithParams(http.MethodPost, "/api/v1/crypto/password/strength", nil, body)
	strongRec := httptest.NewRecorder()
	apiPasswordStrengthPostHandler(strongRec, strongReq)
	assert.Equal(t, http.StatusOK, strongRec.Code)

	t.Run("invalid body", func(t *testing.T) {
		badBody := bytes.NewBufferString(`not json`)
		req := reqWithParams(http.MethodPost, "/api/v1/crypto/password/strength", nil, badBody)
		rec := httptest.NewRecorder()
		apiPasswordStrengthPostHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiDateTimeNowHandler covers no-timezone, URL-param timezone, and
// an invalid timezone error path.
func TestApiDateTimeNowHandler(t *testing.T) {
	t.Run("no timezone", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/now", nil, nil)
		rec := httptest.NewRecorder()
		apiDateTimeNowHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("valid timezone param", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/now/x", map[string]string{"timezone": "UTC"}, nil)
		rec := httptest.NewRecorder()
		apiDateTimeNowHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timezone", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/now/x", map[string]string{"timezone": "Not/A_Real_Zone"}, nil)
		rec := httptest.NewRecorder()
		apiDateTimeNowHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiTimestampHandler covers all three unix representations are
// present and internally consistent (ms derived from the same instant as
// the seconds value, within a generous tolerance).
func TestApiTimestampHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datetime/timestamp", nil)
	rec := httptest.NewRecorder()
	apiTimestampHandler(rec, req)

	got := decodeJSON(t, rec)
	unix, _ := got["unix"].(float64)
	unixMS, _ := got["unix_ms"].(float64)
	assert.Greater(t, unix, float64(0))
	assert.InDelta(t, unix*1000, unixMS, 2000)
}

// TestApiConvertTimestampHandler covers a valid timestamp and an invalid
// (non-numeric) timestamp.
func TestApiConvertTimestampHandler(t *testing.T) {
	t.Run("valid timestamp", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/convert/x", map[string]string{"timestamp": "1700000000"}, nil)
		rec := httptest.NewRecorder()
		apiConvertTimestampHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/convert/x", map[string]string{"timestamp": "not-a-number"}, nil)
		rec := httptest.NewRecorder()
		apiConvertTimestampHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiToUnixHandler covers a well-formed datetime string and the
// invalid-input error path.
func TestApiToUnixHandler(t *testing.T) {
	t.Run("valid RFC3339", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/to-unix/x", map[string]string{"datetime": "2024-01-01T00:00:00Z"}, nil)
		rec := httptest.NewRecorder()
		apiToUnixHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid datetime", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/to-unix/x", map[string]string{"datetime": "not-a-date"}, nil)
		rec := httptest.NewRecorder()
		apiToUnixHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiAddDurationHandler covers a valid timestamp+duration pair and
// the invalid-timestamp error path.
func TestApiAddDurationHandler(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/add/x/y", map[string]string{
			"timestamp": "1700000000",
			"duration":  "1h",
		}, nil)
		rec := httptest.NewRecorder()
		apiAddDurationHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/add/x/y", map[string]string{
			"timestamp": "nope",
			"duration":  "1h",
		}, nil)
		rec := httptest.NewRecorder()
		apiAddDurationHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiDiffHandler covers both timestamps valid, and each one being
// individually invalid (two distinct error branches in the handler).
func TestApiDiffHandler(t *testing.T) {
	t.Run("both valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/diff/x/y", map[string]string{
			"timestamp1": "1700000000",
			"timestamp2": "1700003600",
		}, nil)
		rec := httptest.NewRecorder()
		apiDiffHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timestamp1", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/diff/x/y", map[string]string{
			"timestamp1": "nope",
			"timestamp2": "1700003600",
		}, nil)
		rec := httptest.NewRecorder()
		apiDiffHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid timestamp2", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/diff/x/y", map[string]string{
			"timestamp1": "1700000000",
			"timestamp2": "nope",
		}, nil)
		rec := httptest.NewRecorder()
		apiDiffHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiTimezonesHandler covers the list is non-empty.
func TestApiTimezonesHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datetime/timezones", nil)
	rec := httptest.NewRecorder()
	apiTimezonesHandler(rec, req)

	got := decodeJSON(t, rec)
	zones, ok := got["timezones"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, zones)
}

// TestApiTimezoneInfoHandler covers a known timezone and an invalid one.
func TestApiTimezoneInfoHandler(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/timezone/x", map[string]string{"timezone": "UTC"}, nil)
		rec := httptest.NewRecorder()
		apiTimezoneInfoHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/timezone/x", map[string]string{"timezone": "Not/Real"}, nil)
		rec := httptest.NewRecorder()
		apiTimezoneInfoHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiConvertTimezoneHandler covers a valid conversion and the
// invalid-timestamp error path.
func TestApiConvertTimezoneHandler(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/timezone/convert/x/y/z", map[string]string{
			"timestamp": "1700000000",
			"from":      "UTC",
			"to":        "America/New_York",
		}, nil)
		rec := httptest.NewRecorder()
		apiConvertTimezoneHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/timezone/convert/x/y/z", map[string]string{
			"timestamp": "nope",
			"from":      "UTC",
			"to":        "America/New_York",
		}, nil)
		rec := httptest.NewRecorder()
		apiConvertTimezoneHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestRobotsHandler covers that configured Allow/Deny paths are emitted
// and the sitemap line is present.
func TestRobotsHandler(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.FQDN = "example.com"
	cfg.Server.Port = "8080"
	cfg.Web.Robots.Allow = []string{"/"}
	cfg.Web.Robots.Deny = []string{"/private"}

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	robotsHandler(cfg)(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "Allow: /")
	assert.Contains(t, body, "Disallow: /private")
	assert.Contains(t, body, "Sitemap: http://example.com:8080/sitemap.xml")
}

// TestApiSwaggerSpecHandler_ServesJSON covers PART 14's canonical
// /api/swagger and /api/v1/server/swagger routes: OpenAPI is JSON-only,
// there is no YAML route or redirect.
func TestApiSwaggerSpecHandler_ServesJSON(t *testing.T) {
	cfg := &config.ServerConfig{}
	req := httptest.NewRequest(http.MethodGet, "/api/swagger", nil)
	rec := httptest.NewRecorder()
	apiSwaggerSpecHandler(cfg)(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
}

func TestAllCategoriesCountsRegisteredTools(t *testing.T) {
	counts := make(map[string]int)
	for _, page := range toolPages() {
		counts[page.category]++
	}
	for _, category := range allCategories() {
		assert.Equal(t, counts[strings.TrimPrefix(category.Path, "/")], category.Count, category.Path)
	}
}

func TestMCPAliasesUseOneJSONHandler(t *testing.T) {
	cfg := newTestConfig(t)
	srv := newTestServer(t, cfg)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	for _, path := range []string{"/mcp", "/api/mcp", APIBasePath(cfg) + "/server/mcp"} {
		t.Run(path, func(t *testing.T) {
			get, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
			require.NoError(t, err)
			getResponse, err := ts.Client().Do(get)
			require.NoError(t, err)
			defer func() { _ = getResponse.Body.Close() }()
			assert.Equal(t, http.StatusMethodNotAllowed, getResponse.StatusCode)
			assert.Contains(t, getResponse.Header.Get("Content-Type"), "application/json")

			post, err := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			require.NoError(t, err)
			post.Header.Set("Content-Type", "application/json")
			postResponse, err := ts.Client().Do(post)
			require.NoError(t, err)
			defer func() { _ = postResponse.Body.Close() }()
			assert.Equal(t, http.StatusUnauthorized, postResponse.StatusCode)
			assert.Contains(t, postResponse.Header.Get("Content-Type"), "application/json")
		})
	}
}

func TestHelpAPIMirrorsExposeRegisteredPagesAndMCPData(t *testing.T) {
	t.Setenv("DOMAIN", "example.com")
	cfg := newTestConfig(t)
	srv := newTestServer(t, cfg)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	request, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/server/help", nil)
	require.NoError(t, err)
	request.Header.Set("Accept", "application/json")
	response, err := ts.Client().Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var help struct {
		OK   bool `json:"ok"`
		Data struct {
			Pages []map[string]string `json:"pages"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&help))
	require.True(t, help.OK)
	require.Len(t, help.Data.Pages, 1)
	assert.Equal(t, "/server/help/mcp", help.Data.Pages[0]["path"])
	assert.NotEmpty(t, help.Data.Pages[0]["title"])
	assert.Len(t, help.Data.Pages[0], 2)

	mcpRequest, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/server/help/mcp", nil)
	require.NoError(t, err)
	mcpRequest.Header.Set("Accept", "application/json")
	mcpResponse, err := ts.Client().Do(mcpRequest)
	require.NoError(t, err)
	defer func() { _ = mcpResponse.Body.Close() }()
	require.Equal(t, http.StatusOK, mcpResponse.StatusCode)
	var mcp struct {
		OK   bool `json:"ok"`
		Data struct {
			Enabled bool     `json:"enabled"`
			URL     string   `json:"url"`
			Aliases []string `json:"aliases"`
			Auth    struct {
				Scheme string `json:"scheme"`
			} `json:"auth"`
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
			Project string `json:"project"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(mcpResponse.Body).Decode(&mcp))
	require.True(t, mcp.OK)
	assert.True(t, mcp.Data.Enabled)
	assert.Contains(t, mcp.Data.URL, "/api/v1/server/mcp")
	assert.Equal(t, "Bearer", mcp.Data.Auth.Scheme)
	assert.NotEmpty(t, mcp.Data.Project)
	assert.NotEmpty(t, mcp.Data.Tools)
}

func TestHealthPageUsesSharedPublicShellAndTranslatedSections(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.Branding.Title = "Health Test"
	cfg.Server.Branding.Tagline = "Status overview"
	srv := NewHTTPServer(cfg)
	req := httptest.NewRequest(http.MethodGet, "/server/healthz?lang=es", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, `<title>api - Estado del servidor</title>`)
	assert.Contains(t, body, `id="main-content"`)
	assert.Regexp(t, `data-base-url="http://example\.com:\d+"`, body)
	assert.Contains(t, body, `class="theme-dark"`)
	assert.Contains(t, body, `<h1 class="page-title">📦 Health Test</h1>`)
	assert.Contains(t, body, `<h2>Versión y compilación</h2>`)
	assert.Contains(t, body, `<h2>Tiempo de ejecución</h2>`)
	assert.Contains(t, body, `<h2>Funciones</h2>`)
	assert.Contains(t, body, `<h2>Estado de componentes</h2>`)
	assert.Contains(t, body, `<h2>Estadísticas del servidor</h2>`)

	positions := []int{
		strings.Index(body, `<h1 class="page-title">📦 Health Test</h1>`),
		strings.Index(body, `class="status-banner`),
		strings.Index(body, `<h2>Versión y compilación</h2>`),
		strings.Index(body, `<h2>Tiempo de ejecución</h2>`),
		strings.Index(body, `<h2>Funciones</h2>`),
		strings.Index(body, `<h2>Estado de componentes</h2>`),
		strings.Index(body, `<h2>Estadísticas del servidor</h2>`),
	}
	for i := 1; i < len(positions); i++ {
		assert.Greater(t, positions[i], positions[i-1], "health sections follow PART 13 order")
	}
}

func TestHealthPageDatasetLabelsAndDates(t *testing.T) {
	cfg := newTestConfig(t)
	req := httptest.NewRequest(http.MethodGet, "/server/healthz?lang=fr", nil)
	localTime := time.Date(2025, time.February, 3, 12, 34, 56, 0, time.FixedZone("test", -7*60*60))
	response := handler.HealthResponse{
		Timestamp: localTime,
		Build:     handler.BuildInfo{Date: localTime.Format(time.RFC3339)},
		Checks: handler.ChecksInfo{Datasets: []health.Check{
			{Key: "cve_nvd", Status: "ok"},
			{Key: "trivy_db", Status: "ok"},
			{Key: "blocklist_firehol_level1", Status: "ok"},
			{Key: "geoip_country", Status: "ok"},
		}},
	}
	data := newHealthPageData(cfg, req, response)

	assert.Equal(t, "February 03, 2025 at 19:34:56 UTC", data.TimestampText)
	assert.Equal(t, "2025-02-03T19:34:56Z", data.TimestampISO)
	assert.Equal(t, "February 03, 2025 at 19:34:56 UTC", data.BuildDateText)
	assert.Equal(t, "2025-02-03T19:34:56Z", data.BuildDateISO)
	require.Len(t, data.Checks, 4)
	assert.Equal(t, "Données de vulnérabilités NVD", data.Checks[0].Label)
	assert.Equal(t, "Base de données de vulnérabilités Trivy", data.Checks[1].Label)
	assert.Equal(t, "Source de liste de blocage : firehol_level1", data.Checks[2].Label)
	assert.Equal(t, "Vérification de santé : geoip_country", data.Checks[3].Label)
}
