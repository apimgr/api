//go:build e2e

// Package e2e is the browser end-to-end suite required by AI.md PART 28
// ("Browser E2E Testing"). Every file in this package sits behind the "e2e"
// build tag so "make test", "go build ./..." and the commit gate never see
// it. The suite is developer-initiated through ./tests/e2e.sh.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
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

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
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
	binaryPath  string
	port        int
	localURL    string
	browserURL  string
	artifactDir string
	serverLog   string
	cmd         *exec.Cmd
	logFile     *os.File
	egress      *egressSentinel
	allocCtx    context.Context
	allocCancel context.CancelFunc
}

// suite is the single harness instance, built in TestMain.
var suite *harness

// egressSentinel stands in for every outbound network destination. The server
// under test is started with HTTP_PROXY/HTTPS_PROXY pointing here, so any
// attempt to reach the public internet (GeoIP downloads, update checks,
// webhooks) is captured instead of leaving the machine. AI.md PART 28
// requires the suite to pass offline.
type egressSentinel struct {
	server *httptest.Server
	mu     sync.Mutex
	hits   []string
}

func startEgressSentinel() *egressSentinel {
	s := &egressSentinel{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits = append(s.hits, r.Method+" "+r.Host+r.URL.RequestURI())
		s.mu.Unlock()
		http.Error(w, "external network is blocked in the e2e suite", http.StatusBadGateway)
	}))
	return s
}

func (s *egressSentinel) attempts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.hits))
	copy(out, s.hits)
	return out
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

	if err := h.stopServer(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: server teardown: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	if attempts := h.egress.attempts(); len(attempts) > 0 {
		fmt.Fprintf(os.Stderr, "e2e: %d outbound network attempt(s) recorded (suite must be hermetic):\n", len(attempts))
		for _, a := range attempts {
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

	port, err := freePort()
	if err != nil {
		return nil, err
	}

	h := &harness{
		binaryPath:  binary,
		port:        port,
		artifactDir: artifactDir,
		serverLog:   filepath.Join(artifactDir, "server.log"),
		localURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		egress:      startEgressSentinel(),
	}

	// The browser runs in a sidecar container, so it reaches the server under
	// test by container name rather than over loopback.
	browserHost := os.Getenv("E2E_SERVER_HOST")
	if browserHost == "" {
		browserHost = "127.0.0.1"
	}
	h.browserURL = fmt.Sprintf("http://%s:%d", browserHost, port)

	if err := h.startServer(); err != nil {
		return nil, err
	}
	if err := h.startBrowser(); err != nil {
		_ = h.stopServer()
		return nil, err
	}
	return h, nil
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

// fixtureConfig is the per-run server.yml. It disables the scheduler (the only
// component that reaches the public internet) and rate limiting (the crawl
// tier would otherwise trip the limiter), and enables the root /healthz alias
// so the suite can assert both health routes.
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
  rate_limit:
    enabled: false
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
		"--log", logDir,
		"--cache", cacheDir,
		"--address", "0.0.0.0",
		"--port", strconv.Itoa(h.port),
		"--mode", "production",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		"HOME="+h.artifactDir,
		"TZ=UTC",
		"HTTP_PROXY="+h.egress.server.URL,
		"HTTPS_PROXY="+h.egress.server.URL,
		"http_proxy="+h.egress.server.URL,
		"https_proxy="+h.egress.server.URL,
		"NO_PROXY=127.0.0.1,localhost,::1",
		"no_proxy=127.0.0.1,localhost,::1",
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
	mu       sync.Mutex
	console  []string
	failed   []string
	requests []string
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

// newTab opens a browser tab with network instrumentation enabled. When
// scriptsDisabled is true the tab runs the Tier 2 (no-JavaScript) profile.
func newTab(t *testing.T, scriptsDisabled bool) (context.Context, *pageRecorder) {
	t.Helper()
	if suite.allocCtx == nil {
		t.Fatal("browser allocator is not initialised")
	}

	ctx, cancel := chromedp.NewContext(suite.allocCtx)
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
			}
		case *network.EventLoadingFailed:
			if !e.Canceled {
				rec.addFailed(fmt.Sprintf("%s load failed: %s", e.Type, e.ErrorText))
			}
		case *network.EventResponseReceived:
			if e.Response != nil && e.Response.Status >= 400 {
				rec.addFailed(fmt.Sprintf("%s returned HTTP %d", e.Response.URL, e.Response.Status))
			}
		}
	})

	actions := []chromedp.Action{network.Enable()}
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
