//go:build e2e

// Package e2e is the browser end-to-end suite required by AI.md PART 28
// ("Browser E2E Testing"). Every file in this package sits behind the "e2e"
// build tag so "make test", "go build ./..." and the commit gate never see
// it. The suite is developer-initiated through ./tests/e2e.sh.
package e2e

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/apimgr/api/src/sysservice"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"gopkg.in/yaml.v3"
)

const (
	// Port range mandated by PART 5 for auto-selected listeners.
	portRangeLow  = 64000
	portRangeHigh = 64999
	// Time budget for the server under test to answer its health check.
	serverReadyTimeout = 90 * time.Second
	// Graceful shutdown budget before SIGKILL is used as a last resort.
	shutdownGrace = 15 * time.Second
	// Per-page navigation budget for browser tiers.
	pageTimeout = 45 * time.Second
)

// harness owns every resource shared by the suite: the server process under
// test, the hermeticity sentinel, the artifact directory and the browser
// allocator.
type harness struct {
	binaryPath     string
	port           int
	localURL       string
	browserURL     string
	artifactDir    string
	serverLog      string
	operatorToken  string
	cmd            *exec.Cmd
	logFile        *os.File
	egress         *egressSentinel
	networkFixture *networkFixture
	allocCtx       context.Context
	allocCancel    context.CancelFunc
}

// networkFixture serves the raw TCP dependencies that cannot use the HTTP
// egress sentinel. The E2E container maps example.com and whois.iana.org to
// its own isolated TEST-NET address, so these listeners never reach outside
// the internal Docker network.
type networkFixture struct {
	listeners []net.Listener
	workers   sync.WaitGroup
}

func startNetworkFixture(egress *egressSentinel) (*networkFixture, error) {
	plainHTTP, err := net.Listen("tcp", ":80")
	if err != nil {
		return nil, fmt.Errorf("listen for E2E TCP probe fixture: %w", err)
	}
	whois, err := net.Listen("tcp", ":43")
	if err != nil {
		_ = plainHTTP.Close()
		return nil, fmt.Errorf("listen for E2E WHOIS fixture: %w", err)
	}
	tlsSocket, err := net.Listen("tcp", ":443")
	if err != nil {
		_ = plainHTTP.Close()
		_ = whois.Close()
		return nil, fmt.Errorf("listen for E2E TLS fixture: %w", err)
	}
	certificate, err := egress.hostCertificate("example.com")
	if err != nil {
		_ = plainHTTP.Close()
		_ = whois.Close()
		_ = tlsSocket.Close()
		return nil, fmt.Errorf("create E2E TLS fixture certificate: %w", err)
	}
	secureTLS := tls.NewListener(tlsSocket, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	f := &networkFixture{listeners: []net.Listener{plainHTTP, whois, secureTLS}}
	f.serve(plainHTTP, func(conn net.Conn) {
		_ = conn.Close()
	})
	f.serve(whois, func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
			return
		}
		_, _ = io.WriteString(conn, "domain: example.com\nregistrar: E2E Fixture Registry\n")
	})
	f.serve(secureTLS, func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if secure, ok := conn.(*tls.Conn); ok {
			if err := secure.Handshake(); err != nil {
				return
			}
			_, _ = io.WriteString(secure, "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: 111\r\nConnection: close\r\nServer: E2E Fixture\r\n\r\n<!doctype html><html><head><meta name=\"generator\" content=\"E2E Fixture\"></head><body>Fixture page</body></html>")
		}
	})
	return f, nil
}

func (f *networkFixture) serve(listener net.Listener, handle func(net.Conn)) {
	f.workers.Add(1)
	go func() {
		defer f.workers.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			f.workers.Add(1)
			go func() {
				defer f.workers.Done()
				handle(conn)
			}()
		}
	}()
}

func (f *networkFixture) close() {
	for _, listener := range f.listeners {
		_ = listener.Close()
	}
	f.workers.Wait()
}

// suite is the single harness instance, built in TestMain.
var suite *harness

// egressSentinel stands in for every outbound network destination. The server
// under test is started with HTTP_PROXY/HTTPS_PROXY pointing here, so approved
// provider calls receive deterministic local fixtures and every unmapped
// destination is blocked. AI.md PART 28 requires the suite to pass offline.
type egressSentinel struct {
	server    *httptest.Server
	ca        *x509.Certificate
	caKey     *ecdsa.PrivateKey
	mu        sync.Mutex
	hits      []string
	blocked   []string
	certCache map[string]tls.Certificate
}

func startEgressSentinel(caPath string) (*egressSentinel, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate E2E fixture CA key: %w", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "apimgr E2E fixture CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(cryptorand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create E2E fixture CA certificate: %w", err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("parse E2E fixture CA certificate: %w", err)
	}
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644); err != nil {
		return nil, fmt.Errorf("write E2E fixture CA certificate: %w", err)
	}
	s := &egressSentinel{ca: ca, caKey: caKey, certCache: make(map[string]tls.Certificate)}
	s.server = httptest.NewServer(http.HandlerFunc(s.serveProxy))
	return s, nil
}

func (s *egressSentinel) attempts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.blocked))
	copy(out, s.blocked)
	return out
}

func (s *egressSentinel) recordBlocked(address string) {
	s.mu.Lock()
	s.blocked = append(s.blocked, address)
	s.mu.Unlock()
}

func (s *egressSentinel) recordHit(address string) {
	s.mu.Lock()
	s.hits = append(s.hits, address)
	s.mu.Unlock()
}

func (s *egressSentinel) hitsFor(host string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var matches []string
	for _, hit := range s.hits {
		if strings.Contains(strings.ToLower(hit), strings.ToLower(host)) {
			matches = append(matches, hit)
		}
	}
	return matches
}

func (s *egressSentinel) serveProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		address := r.URL.Host + r.URL.RequestURI()
		s.recordHit(r.Method + " " + address)
		if !s.serveFixture(w, r) {
			s.recordBlocked(r.Method + " " + address)
			http.Error(w, "external network is blocked in the e2e suite", http.StatusBadGateway)
		}
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "HTTPS fixture proxy cannot hijack the connection", http.StatusInternalServerError)
		return
	}
	conn, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	if _, err := fmt.Fprint(buffered, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := buffered.Flush(); err != nil {
		return
	}

	host := r.Host
	name, _, err := net.SplitHostPort(host)
	if err == nil {
		host = name
	}
	s.recordHit("CONNECT " + host)

	certificate, err := s.hostCertificate(host)
	if err != nil {
		s.recordBlocked("CONNECT " + host + ": " + err.Error())
		return
	}
	tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}})
	if err := tlsConn.Handshake(); err != nil {
		s.recordBlocked("CONNECT " + host + ": TLS handshake failed")
		return
	}
	reader := bufio.NewReader(tlsConn)
	for {
		request, err := http.ReadRequest(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.recordBlocked("CONNECT " + host + ": tunneled request read failed: " + err.Error())
			}
			return
		}
		request.URL.Scheme = "https"
		request.URL.Host = host
		request.Host = host
		address := request.Method + " " + host + request.URL.RequestURI()
		recorder := httptest.NewRecorder()
		if !s.serveFixture(recorder, request) {
			s.recordBlocked(address)
			http.Error(recorder, "external network is blocked in the e2e suite", http.StatusBadGateway)
		}
		response := recorder.Result()
		response.Close = request.Close
		if err := response.Write(tlsConn); err != nil {
			_ = response.Body.Close()
			return
		}
		_ = response.Body.Close()
		_ = request.Body.Close()
		if request.Close {
			return
		}
	}
}

func (s *egressSentinel) hostCertificate(host string) (tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if certificate, ok := s.certCache[host]; ok {
		return certificate, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate fixture host key: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, template, s.ca, &key.PublicKey, s.caKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create fixture host certificate: %w", err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{der, s.ca.Raw}, PrivateKey: key}
	s.certCache[host] = certificate
	return certificate, nil
}

func (s *egressSentinel) serveFixture(w http.ResponseWriter, r *http.Request) bool {
	host := strings.ToLower(r.URL.Hostname())
	if host == "" {
		host = strings.ToLower(r.Host)
	}
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case host == "api.frankfurter.dev":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"amount":1,"base":"USD","date":"2024-01-01","rates":{"EUR":0.92,"USD":1}}`)
	case host == "nominatim.openstreetmap.org" && r.URL.Path == "/search":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"display_name":"E2E Fixture Location, Test City, United States","lat":"38.8977","lon":"-77.0365","type":"city","class":"place"}]`)
	case host == "nominatim.openstreetmap.org" && r.URL.Path == "/reverse":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"display_name":"E2E Fixture Location, Test City, United States","address":{"road":"Fixture Road","city":"Test City","county":"Test County","state":"Test State","postcode":"20001","country":"United States","country_code":"us"}}`)
	case host == "geocoding-api.open-meteo.com":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"name":"Test City","latitude":38.8977,"longitude":-77.0365,"country":"United States","country_code":"US","timezone":"America/New_York"}]}`)
	case strings.HasSuffix(host, ".open-meteo.com") || host == "api.open-meteo.com":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"latitude":38.8977,"longitude":-77.0365,"timezone":"America/New_York","timezone_abbreviation":"GMT-5","utc_offset_seconds":-18000,"current_weather":{"time":"2024-01-01T12:00","temperature":21.5,"windspeed":12.4,"winddirection":180,"weathercode":2},"current":{"time":"2024-01-01T12:00","temperature_2m":21.5,"apparent_temperature":20.1,"relative_humidity_2m":55,"pressure_msl":1012.3,"wind_speed_10m":12.4,"wind_direction_10m":180,"cloud_cover":40,"weather_code":2,"us_aqi":35,"european_aqi":28,"pm10":8,"pm2_5":5,"carbon_monoxide":200,"nitrogen_dioxide":10,"sulphur_dioxide":2,"ozone":60,"uv_index":1,"uv_index_clear_sky":1,"alder_pollen":0,"birch_pollen":0,"grass_pollen":1,"mugwort_pollen":0,"olive_pollen":0,"ragweed_pollen":0,"wave_height":1,"wave_direction":180,"wave_period":8,"swell_wave_height":0.5,"wind_wave_height":0.5,"ocean_current_velocity":0.2,"sea_surface_temperature":18},"daily":{"time":["2024-01-01","2024-01-02"],"weather_code":[2,1],"temperature_2m_max":[22,23],"temperature_2m_min":[12,13],"rain_sum":[0,0],"snowfall_sum":[0,0],"precipitation_sum":[0,0],"windspeed_10m_max":[10,11],"sunrise":["2024-01-01T07:00","2024-01-02T07:00"],"sunset":["2024-01-01T17:00","2024-01-02T17:00"],"daylight_duration":[36000,36000],"sunshine_duration":[30000,30000],"uv_index_max":[2,2]},"hourly":{"time":["2024-01-01T12:00","2024-01-01T13:00"],"temperature_2m":[21,22],"precipitation_probability":[0,0],"weather_code":[2,1],"wind_speed_10m":[10,11]}}`)
	case host == "api.weather.gov":
		w.Header().Set("Content-Type", "application/geo+json")
		_, _ = io.WriteString(w, `{"type":"FeatureCollection","features":[]}`)
	case host == "api.dictionaryapi.dev":
		word := strings.TrimPrefix(r.URL.Path, "/api/v2/entries/en/")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `[{"word":%q,"phonetics":[{"text":"/%s/"}],"meanings":[{"partOfSpeech":"noun","definitions":[{"definition":"An E2E fixture definition.","example":"%s"}]}]}]`, word, word, word)
	case host == "api.datamuse.com":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"word":"fixture-synonym"}]`)
	case host == "export.arxiv.org":
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><id>http://arxiv.org/abs/2101.00001v1</id><title>E2E Fixture Paper</title><summary>A deterministic fixture paper.</summary><published>2024-01-01T00:00:00Z</published><updated>2024-01-01T00:00:00Z</updated><author><name>E2E Fixture Author</name></author><link href="https://arxiv.org/abs/2101.00001v1" rel="alternate"/></entry></feed>`)
	case host == "openlibrary.org":
		query := r.URL.Query()
		key := query.Get("bibkeys")
		if key == "" {
			key = "ISBN:9780140328721"
		}
		body, _ := json.Marshal(map[string]any{key: map[string]any{"title": "E2E Fixture Book", "publish_date": "2024", "number_of_pages": 100, "url": "https://openlibrary.org/books/E2E", "authors": []map[string]string{{"name": "E2E Fixture Author"}}, "publishers": []map[string]string{{"name": "E2E Fixture Press"}}}})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	case host == "example.com" || host == "1.1.1.1":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>E2E Fixture</title></head><body><main>Deterministic E2E fixture</main></body></html>`)
	default:
		return false
	}
	return true
}

func (s *egressSentinel) close() {
	s.server.Close()
}

// TestMain builds the shared harness, runs the suite and tears everything
// down: the server process is stopped, the artifact directory is reported and
// any outbound network attempt fails the run.
func TestMain(m *testing.M) {
	h, err := newHarness()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: harness startup failed: %v\n", err)
		os.Exit(1)
	}
	suite = h

	code := m.Run()
	if code != 0 {
		for _, hit := range h.egress.hitsFor("example.com") {
			fmt.Fprintf(os.Stderr, "e2e: observed fixture proxy request: %s\n", hit)
		}
	}

	if err := h.stopServer(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: server teardown: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	h.networkFixture.close()
	if blocked := h.egress.attempts(); len(blocked) > 0 {
		fmt.Fprintf(os.Stderr, "e2e: %d unmocked outbound request(s) blocked (suite must be hermetic):\n", len(blocked))
		for _, a := range blocked {
			fmt.Fprintf(os.Stderr, "  %s\n", a)
		}
		code = 1
	}
	h.egress.close()
	if h.allocCancel != nil {
		h.allocCancel()
	}
	fmt.Fprintf(os.Stderr, "e2e: artifacts and server log: %s\n", h.artifactDir)
	os.Exit(code)
}

func newHarness() (*harness, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}

	binary := os.Getenv("E2E_BINARY")
	if binary == "" {
		binary = filepath.Join(root, "binaries", "api")
	}
	if _, err := os.Stat(binary); err != nil {
		return nil, fmt.Errorf("server binary %q not found - run ./tests/e2e.sh: %w", binary, err)
	}

	artifactDir, err := newArtifactDir()
	if err != nil {
		return nil, err
	}
	if err := prepareArtifactPermissions(artifactDir); err != nil {
		return nil, err
	}
	if err := addNetworkFixtureHosts(); err != nil {
		return nil, err
	}

	port, err := freePort()
	if err != nil {
		return nil, err
	}
	egress, err := startEgressSentinel(filepath.Join(artifactDir, "e2e-fixture-ca.pem"))
	if err != nil {
		return nil, err
	}
	networkFixture, err := startNetworkFixture(egress)
	if err != nil {
		egress.close()
		return nil, err
	}

	h := &harness{
		binaryPath:     binary,
		port:           port,
		artifactDir:    artifactDir,
		serverLog:      filepath.Join(artifactDir, "server.log"),
		localURL:       fmt.Sprintf("http://127.0.0.1:%d", port),
		egress:         egress,
		networkFixture: networkFixture,
	}

	// The browser runs in a sidecar container, so it reaches the server under
	// test by container name rather than over loopback.
	browserHost := os.Getenv("E2E_SERVER_HOST")
	if browserHost == "" {
		browserHost = "127.0.0.1"
	}
	h.browserURL = fmt.Sprintf("http://%s:%d", browserHost, port)

	if err := h.startServer(); err != nil {
		h.networkFixture.close()
		h.egress.close()
		return nil, err
	}
	if err := h.readOperatorToken(); err != nil {
		_ = h.stopServer()
		h.networkFixture.close()
		h.egress.close()
		return nil, err
	}
	if err := h.startBrowser(); err != nil {
		_ = h.stopServer()
		h.networkFixture.close()
		h.egress.close()
		return nil, err
	}
	return h, nil
}

func addNetworkFixtureHosts() error {
	ip := net.ParseIP(strings.TrimSpace(os.Getenv("E2E_FIXTURE_IP")))
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("E2E_FIXTURE_IP must be a valid IPv4 address")
	}
	const hostsPath = "/etc/hosts"
	contents, err := os.ReadFile(hostsPath)
	if err != nil {
		return fmt.Errorf("read E2E fixture hosts file: %w", err)
	}
	missing := make([]string, 0, 2)
	for _, hostname := range []string{"example.com", "whois.iana.org"} {
		found := false
		for _, line := range strings.Split(string(contents), "\n") {
			fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
			if len(fields) < 2 {
				continue
			}
			for _, alias := range fields[1:] {
				if strings.EqualFold(alias, hostname) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			missing = append(missing, hostname)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	file, err := os.OpenFile(hostsPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("open E2E fixture hosts file: %w", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := fmt.Fprintf(file, "\n%s %s\n", ip, strings.Join(missing, " ")); err != nil {
		return fmt.Errorf("install E2E fixture host aliases: %w", err)
	}
	return nil
}

// prepareArtifactPermissions lets the server's dedicated account traverse
// the private mounted artifact root after startup drops root privileges.
func prepareArtifactPermissions(artifactDir string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	dataDir := filepath.Join(artifactDir, "data")
	if err := sysservice.CreateServiceUser(dataDir); err != nil {
		return fmt.Errorf("create service account for E2E server: %w", err)
	}
	uid, gid, err := sysservice.ServiceAccountIDs()
	if err != nil {
		return fmt.Errorf("resolve E2E server service account: %w", err)
	}
	paths := []string{artifactDir}
	if artifactRoot := os.Getenv("E2E_ARTIFACT_ROOT"); artifactRoot != "" {
		paths = append(paths, artifactRoot)
	}
	for _, path := range paths {
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("assign E2E artifact path %s to service account: %w", path, err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("restrict E2E artifact path %s: %w", path, err)
		}
	}
	return nil
}

// newArtifactDir creates the run directory under the mandated tempdir
// structure ${TMPDIR:-/tmp}/apimgr/api-XXXXXX - never inside the project tree.
func newArtifactDir() (string, error) {
	root := os.Getenv("E2E_ARTIFACT_ROOT")
	if root == "" {
		root = filepath.Join(os.TempDir(), "apimgr")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create artifact root: %w", err)
	}
	dir, err := os.MkdirTemp(root, "api-")
	if err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	return dir, nil
}

// repoRoot walks up from the working directory until it finds go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above the working directory")
		}
		dir = parent
	}
}

// freePort picks an unused port inside the PART 5 range 64000-64999.
func freePort() (int, error) {
	for attempt := 0; attempt < 256; attempt++ {
		port := portRangeLow + rand.Intn(portRangeHigh-portRangeLow+1)
		ln, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
		if err != nil {
			continue
		}
		if err := ln.Close(); err != nil {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("no free port in %d-%d", portRangeLow, portRangeHigh)
}

// fixtureConfig is the per-run server.yml. It disables scheduled work and
// GeoIP downloads, raises rate limits for the internal-link crawl, and enables
// the root /healthz alias so the suite can assert both health routes. Rate
// limiting has no disable toggle.
const fixtureConfig = `# Generated per e2e run by tests/e2e - never committed, never reused.
server:
  address: 0.0.0.0
  port: "%d"
  mode: production
  branding:
    title: CasTools
    tagline: Universal API Toolkit
  schedule:
    enabled: false
  geoip:
    enabled: false
  rate_limit:
    read:
      requests: 100000
      window: 60
    write:
      requests: 100000
      window: 60
    health:
      requests: 100000
      window: 60
    strict:
      requests: 100000
      window: 900
    auth_failures:
      requests: 100000
      window: 900
    global_burst: 100000
  healthz:
    root:
      enabled: true
  metrics:
    enabled: false
  update:
    auto_install: false
  database:
    driver: sqlite
`

// startServer writes a fresh fixture config and database directory, starts the
// binary once for the whole suite and waits for its health endpoint.
func (h *harness) startServer() error {
	configDir := filepath.Join(h.artifactDir, "config")
	dataDir := filepath.Join(h.artifactDir, "data")
	logDir := filepath.Join(h.artifactDir, "log")
	cacheDir := filepath.Join(h.artifactDir, "cache")
	for _, dir := range []string{configDir, dataDir, logDir, cacheDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	cfgPath := filepath.Join(configDir, "server.yml")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf(fixtureConfig, h.port)), 0o600); err != nil {
		return fmt.Errorf("write fixture config: %w", err)
	}

	logFile, err := os.Create(h.serverLog)
	if err != nil {
		return fmt.Errorf("create server log: %w", err)
	}
	h.logFile = logFile

	cmd := exec.Command(h.binaryPath,
		"--config", configDir,
		"--data", dataDir,
		"--backup", filepath.Join(dataDir, "backups", "api"),
		"--log", logDir,
		"--cache", cacheDir,
		"--address", "0.0.0.0",
		"--port", strconv.Itoa(h.port),
		"--mode", "production",
		"--debug",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	serverEnv := make([]string, 0, len(os.Environ())+10)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(name) {
		case "DOMAIN", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "SSL_CERT_FILE":
			continue
		}
		serverEnv = append(serverEnv, value)
	}
	cmd.Env = append(serverEnv,
		"HOME="+h.artifactDir,
		"DATABASE_DIR="+filepath.Join(dataDir, "db", "sqlite"),
		"DOMAIN="+os.Getenv("E2E_SERVER_HOST"),
		"TZ=UTC",
		"HTTP_PROXY="+h.egress.server.URL,
		"HTTPS_PROXY="+h.egress.server.URL,
		"http_proxy="+h.egress.server.URL,
		"https_proxy="+h.egress.server.URL,
		"SSL_CERT_FILE="+filepath.Join(h.artifactDir, "e2e-fixture-ca.pem"),
		"NO_PROXY=127.0.0.1,localhost,::1,example.com,whois.iana.org",
		"no_proxy=127.0.0.1,localhost,::1,example.com,whois.iana.org",
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	h.cmd = cmd

	if err := h.waitReady(); err != nil {
		_ = h.stopServer()
		return err
	}
	return nil
}

func (h *harness) readOperatorToken() error {
	path := filepath.Join(h.artifactDir, "config", "secrets.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read E2E operator token from generated secrets file: %w", err)
	}
	var secrets struct {
		Server struct {
			Token string `yaml:"token"`
		} `yaml:"server"`
	}
	if err := yaml.Unmarshal(data, &secrets); err != nil {
		return fmt.Errorf("decode E2E operator token from generated secrets file: %w", err)
	}
	if secrets.Server.Token == "" {
		return errors.New("generated E2E secrets file has no server.token")
	}
	h.operatorToken = secrets.Server.Token
	return nil
}

// waitReady polls the health endpoint until the server answers or the budget
// expires; on failure the server log tail is included in the error.
func (h *harness) waitReady() error {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(serverReadyTimeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(h.localURL + "/server/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if h.cmd.ProcessState != nil && h.cmd.ProcessState.Exited() {
			return fmt.Errorf("server exited before becoming ready\n%s", h.logTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("server not ready within %s\n%s", serverReadyTimeout, h.logTail())
}

// logTail returns the last 4 KiB of the server log for failure messages.
func (h *harness) logTail() string {
	data, err := os.ReadFile(h.serverLog)
	if err != nil {
		return "(server log unavailable: " + err.Error() + ")"
	}
	const max = 4096
	if len(data) > max {
		data = data[len(data)-max:]
	}
	return "--- server log tail ---\n" + string(data)
}

// stopServer signals the captured PID directly - SIGTERM first, SIGKILL only
// if the process ignores the graceful shutdown budget.
func (h *harness) stopServer() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	proc := h.cmd.Process
	h.cmd = nil

	done := make(chan error, 1)
	go func() {
		_, err := proc.Wait()
		done <- err
	}()

	if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("signal server pid %d: %w", proc.Pid, err)
	}
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill server pid %d: %w", proc.Pid, err)
		}
		<-done
	}
	if h.logFile != nil {
		_ = h.logFile.Close()
		h.logFile = nil
	}
	return nil
}

// startBrowser attaches to the headless-shell sidecar. The allocator lives for
// the whole suite; each test opens its own tab from it.
func (h *harness) startBrowser() error {
	browser := os.Getenv("E2E_BROWSER_URL")
	if browser == "" {
		return errors.New("E2E_BROWSER_URL is unset - the headless-shell sidecar is required (run ./tests/e2e.sh)")
	}
	ctx, cancel := chromedp.NewRemoteAllocator(context.Background(), browser)
	h.allocCtx = ctx
	h.allocCancel = cancel
	return nil
}

// pageRecorder collects the console errors, failed requests and request
// origins of a single tab, backing the Tier 3 assertions.
type pageRecorder struct {
	mu        sync.Mutex
	console   []string
	failed    []string
	requests  []string
	posts     []string
	responses []string
	loads     uint64
}

func (p *pageRecorder) addConsole(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.console = append(p.console, msg)
}

func (p *pageRecorder) addFailed(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failed = append(p.failed, msg)
}

func (p *pageRecorder) addRequest(rawURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, rawURL)
}

func (p *pageRecorder) addPost(rawURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.posts = append(p.posts, rawURL)
}

func (p *pageRecorder) addResponse(rawURL string, status int, mimeType string, headers map[string]any) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	disposition, contentLength := "", ""
	for name, value := range headers {
		switch strings.ToLower(name) {
		case "content-disposition":
			disposition = fmt.Sprint(value)
		case "content-length":
			contentLength = fmt.Sprint(value)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.responses = append(p.responses, fmt.Sprintf("%s %d %s length=%s disposition=%s", parsed.Path, status, mimeType, contentLength, disposition))
}

func (p *pageRecorder) consoleErrors() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.console...)
}

func (p *pageRecorder) failedRequests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.failed...)
}

func (p *pageRecorder) requestURLs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.requests...)
}

func (p *pageRecorder) postPaths() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	paths := make([]string, 0, len(p.posts))
	for _, rawURL := range p.posts {
		if parsed, err := url.Parse(rawURL); err == nil {
			paths = append(paths, parsed.Path)
		}
	}
	return paths
}

func (p *pageRecorder) responseSummaries(path string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var matches []string
	for _, response := range p.responses {
		if strings.HasPrefix(response, path+" ") {
			matches = append(matches, response)
		}
	}
	return matches
}

func (p *pageRecorder) loadCount() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loads
}

func (p *pageRecorder) waitForLoadAfter(ctx context.Context, previous uint64) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if p.loadCount() > previous {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// newTab opens a browser tab with network instrumentation enabled. When
// scriptsDisabled is true the tab runs the Tier 2 (no-JavaScript) profile.
func newTab(t *testing.T, scriptsDisabled bool) (context.Context, *pageRecorder) {
	t.Helper()
	if suite.allocCtx == nil {
		t.Fatal("browser allocator is not initialised")
	}

	ctx, cancel := chromedp.NewContext(suite.allocCtx, chromedp.WithNewBrowserContext())
	t.Cleanup(cancel)
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, pageTimeout)
	t.Cleanup(timeoutCancel)

	rec := &pageRecorder{}
	chromedp.ListenTarget(timeoutCtx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			if e.Type == runtime.APITypeError {
				rec.addConsole(consoleText(e))
			}
		case *runtime.EventExceptionThrown:
			if e.ExceptionDetails != nil {
				rec.addConsole(e.ExceptionDetails.Error())
			}
		case *network.EventRequestWillBeSent:
			if e.Request != nil {
				rec.addRequest(e.Request.URL)
				if e.Request.Method == http.MethodPost {
					rec.addPost(e.Request.URL)
				}
			}
		case *network.EventLoadingFailed:
			// GraphiQL's published stylesheet includes data: font faces, which
			// the project's required font-src policy intentionally blocks. The
			// UI has system-font fallbacks; external origins are checked by the
			// hermetic-origin tier, so these policy-blocked font faces are not
			// treated as failed page assets.
			if !e.Canceled && !(e.Type == network.ResourceTypeFont && e.BlockedReason == network.BlockedReasonCsp) {
				rec.addFailed(fmt.Sprintf("%s load failed: %s", e.Type, e.ErrorText))
			}
		case *network.EventResponseReceived:
			if e.Response != nil && e.Response.Status >= 400 {
				rec.addFailed(fmt.Sprintf("%s returned HTTP %d", e.Response.URL, e.Response.Status))
			}
			if e.Response != nil {
				rec.addResponse(e.Response.URL, int(e.Response.Status), e.Response.MimeType, e.Response.Headers)
			}
		case *page.EventLoadEventFired:
			rec.mu.Lock()
			rec.loads++
			rec.mu.Unlock()
		}
	})

	actions := []chromedp.Action{network.Enable(), page.Enable()}
	if scriptsDisabled {
		// Tier 2: verify every flow without any client-side JavaScript.
		actions = append(actions, emulation.SetScriptExecutionDisabled(true))
	}
	if err := chromedp.Run(timeoutCtx, actions...); err != nil {
		t.Fatalf("initialise browser tab: %v", err)
	}
	return timeoutCtx, rec
}

// consoleText flattens a console.error call into a readable line.
func consoleText(e *runtime.EventConsoleAPICalled) string {
	parts := make([]string, 0, len(e.Args))
	for _, arg := range e.Args {
		if arg == nil {
			continue
		}
		if len(arg.Value) > 0 {
			parts = append(parts, strings.Trim(string(arg.Value), `"`))
			continue
		}
		parts = append(parts, arg.Description)
	}
	return strings.Join(parts, " ")
}

// setThemeCookie stores the PART 16 theme cookie for the server under test so
// the next navigation is rendered server-side in that theme.
func setThemeCookie(ctx context.Context, theme string) error {
	u, err := url.Parse(suite.browserURL)
	if err != nil {
		return err
	}
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetCookie("theme", theme).
			WithDomain(u.Hostname()).
			WithPath("/").
			Do(ctx)
	}))
}

// httpClient returns a Tier 1 client that never follows redirects, so the
// suite can assert redirect status codes and Location headers directly.
func httpClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// fetch performs a Tier 1 request against the server under test and returns
// the response together with its fully read body.
func fetch(t *testing.T, path string, headers map[string]string, cookies map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, suite.localURL+path, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s: %v", path, err)
	}
	return resp, string(body)
}

// getHTML fetches a page as a browser would, for Tier 1 SSR assertions.
func getHTML(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	return fetch(t, path, map[string]string{
		"Accept":     "text/html,application/xhtml+xml",
		"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) e2e-suite",
	}, nil)
}

// requireStatus fails the test when a response carries an unexpected status.
func requireStatus(t *testing.T, resp *http.Response, path string, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("%s: got HTTP %d, want %d", path, resp.StatusCode, want)
	}
}

// requireContains fails the test when the rendered output is missing content
// that must be present in the initial response.
func requireContains(t *testing.T, subject, want, context string) {
	t.Helper()
	if !strings.Contains(subject, want) {
		t.Errorf("%s: missing %q in response", context, want)
	}
}

// saveArtifacts writes the current page HTML and a screenshot into the run's
// artifact directory. It is called by browser tiers on failure only.
func saveArtifacts(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	safe := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(name)
	var html string
	var shot []byte
	if err := chromedp.Run(ctx, chromedp.OuterHTML("html", &html), chromedp.CaptureScreenshot(&shot)); err != nil {
		t.Logf("artifact capture for %s failed: %v", name, err)
		return
	}
	htmlPath := filepath.Join(suite.artifactDir, safe+".html")
	shotPath := filepath.Join(suite.artifactDir, safe+".png")
	if err := os.WriteFile(htmlPath, []byte(html), 0o600); err != nil {
		t.Logf("write %s: %v", htmlPath, err)
	}
	if err := os.WriteFile(shotPath, shot, 0o600); err != nil {
		t.Logf("write %s: %v", shotPath, err)
	}
	t.Logf("artifacts written: %s %s", htmlPath, shotPath)
}
