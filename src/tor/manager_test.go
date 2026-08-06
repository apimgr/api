package tor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestNewManager verifies NewManager stores its constructor arguments and
// starts with no running Service.
func TestNewManager(t *testing.T) {
	cfg := DefaultConfig()
	m := NewManager(8080, cfg, "/cfgdir", "/datadir")

	if m.serverPort != 8080 {
		t.Errorf("serverPort = %d, want 8080", m.serverPort)
	}
	if m.configDir != "/cfgdir" {
		t.Errorf("configDir = %q, want /cfgdir", m.configDir)
	}
	if m.dataDir != "/datadir" {
		t.Errorf("dataDir = %q, want /datadir", m.dataDir)
	}
	if m.cfg != cfg {
		t.Errorf("cfg = %+v, want %+v", m.cfg, cfg)
	}
	if m.svc != nil {
		t.Error("expected svc to be nil before Start")
	}
	if m.Running() {
		t.Error("expected Running() to be false before Start")
	}
}

// TestSetGet verifies the process-wide global accessor round-trips and
// defaults to nil when nothing was ever registered.
func TestSetGet(t *testing.T) {
	// Save/restore the package-global so this test does not leak state into
	// other tests in this package.
	prev := Get()
	t.Cleanup(func() { Set(prev) })

	Set(nil)
	if got := Get(); got != nil {
		t.Errorf("Get() after Set(nil) = %v, want nil", got)
	}

	m := NewManager(1, DefaultConfig(), "a", "b")
	Set(m)
	if got := Get(); got != m {
		t.Errorf("Get() = %v, want %v", got, m)
	}
}

// TestOnionKeyPath verifies the persistent v3 key path is built under
// {data_dir}/tor/site/.
func TestOnionKeyPath(t *testing.T) {
	got := onionKeyPath("/data")
	want := filepath.Join("/data", "tor", "site", "hs_ed25519_secret_key")
	if got != want {
		t.Errorf("onionKeyPath() = %q, want %q", got, want)
	}
}

// TestRemoveOnionKey verifies the hidden-service key directory is removed
// and recreated empty, and that a missing directory is not an error.
func TestRemoveOnionKey(t *testing.T) {
	dataDir := t.TempDir()
	siteDir := filepath.Join(dataDir, "tor", "site")
	keyPath := filepath.Join(siteDir, "hs_ed25519_secret_key")

	if err := os.MkdirAll(siteDir, 0700); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("fake-key"), 0600); err != nil {
		t.Fatalf("setup WriteFile failed: %v", err)
	}

	if err := removeOnionKey(keyPath); err != nil {
		t.Fatalf("removeOnionKey failed: %v", err)
	}

	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Errorf("expected key file to be removed, stat err=%v", err)
	}
	info, err := os.Stat(siteDir)
	if err != nil {
		t.Fatalf("expected site dir to be recreated: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected site dir to be a directory")
	}
}

// TestRemoveOnionKeyMissingDir verifies removing a key under a directory
// that never existed is not an error - it is simply (re)created.
func TestRemoveOnionKeyMissingDir(t *testing.T) {
	dataDir := t.TempDir()
	keyPath := filepath.Join(dataDir, "tor", "site", "hs_ed25519_secret_key")

	if err := removeOnionKey(keyPath); err != nil {
		t.Fatalf("removeOnionKey on missing dir failed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(keyPath)); err != nil {
		t.Errorf("expected site dir to exist after removeOnionKey: %v", err)
	}
}

// TestManagerStartMissingBinary verifies Start surfaces findBinary's error
// (Tor is optional - a missing binary must never panic) rather than
// requiring a real Tor process, which is unavailable in this sandbox.
func TestManagerStartMissingBinary(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Binary = filepath.Join(t.TempDir(), "does-not-exist-tor")

	m := NewManager(0, cfg, t.TempDir(), t.TempDir())
	err := m.Start(context.Background())
	if err == nil {
		t.Fatal("expected an error when the configured Tor binary is missing")
	}
	if m.Running() {
		t.Error("expected Running() to remain false after a failed Start")
	}
}

// TestManagerStartAlreadyStarted verifies the "already started" guard fires
// before any real Tor process is touched. A fake, zero-value Service is
// assigned directly (same-package access) to simulate an already-running
// Manager without needing a live Tor binary.
func TestManagerStartAlreadyStarted(t *testing.T) {
	m := NewManager(0, DefaultConfig(), t.TempDir(), t.TempDir())
	m.svc = &Service{}

	err := m.Start(context.Background())
	if err == nil {
		t.Fatal("expected an error when Tor is already started")
	}
}

// TestManagerRestartWithMissingBinary verifies Restart stops any existing
// (fake) Service and then surfaces the same missing-binary error as Start.
func TestManagerRestartWithMissingBinary(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Binary = filepath.Join(t.TempDir(), "does-not-exist-tor")

	m := NewManager(0, cfg, t.TempDir(), t.TempDir())
	m.svc = &Service{}

	err := m.Restart(context.Background())
	if err == nil {
		t.Fatal("expected an error from Restart when the Tor binary is missing")
	}
	if m.Running() {
		t.Error("expected Running() to be false after a failed Restart")
	}
}

// TestManagerUpdateConfigWritesTorrc verifies UpdateConfig regenerates and
// overwrites torrc on disk even though the subsequent Restart cannot
// actually start Tor in this sandbox (no Tor binary available).
func TestManagerUpdateConfigWritesTorrc(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Binary = filepath.Join(t.TempDir(), "does-not-exist-tor")

	m := NewManager(0, cfg, configDir, dataDir)

	newCfg := cfg
	newCfg.BandwidthRate = "9 MB"

	// Restart will fail (no Tor binary) - that is expected and asserted
	// separately; what this test verifies is that torrc was regenerated
	// before that failure.
	_ = m.UpdateConfig(context.Background(), newCfg)

	torrcPath := filepath.Join(configDir, "tor", "torrc")
	data, err := os.ReadFile(torrcPath)
	if err != nil {
		t.Fatalf("expected torrc to exist: %v", err)
	}
	if got := string(data); got == "" {
		t.Error("expected non-empty torrc content")
	}
	if m.cfg.BandwidthRate != "9 MB" {
		t.Errorf("expected manager cfg to be updated, got %q", m.cfg.BandwidthRate)
	}
}

// TestManagerRegenerateAddress verifies RegenerateAddress removes the
// existing onion key and surfaces the (expected, sandbox-only) start
// failure rather than panicking.
func TestManagerRegenerateAddress(t *testing.T) {
	dataDir := t.TempDir()
	keyPath := onionKeyPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("old-key"), 0600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Binary = filepath.Join(t.TempDir(), "does-not-exist-tor")

	m := NewManager(0, cfg, t.TempDir(), dataDir)
	m.svc = &Service{}

	addr, err := m.RegenerateAddress(context.Background())
	if err == nil {
		t.Fatal("expected an error since Tor cannot actually start in this sandbox")
	}
	if addr != "" {
		t.Errorf("expected empty address on failure, got %q", addr)
	}
	if _, statErr := os.Stat(keyPath); !os.IsNotExist(statErr) {
		t.Errorf("expected old key to be removed, stat err=%v", statErr)
	}
}

// TestManagerApplyKeys verifies ApplyKeys persists the supplied key blob to
// disk before attempting (and, in this sandbox, failing) to start Tor.
func TestManagerApplyKeys(t *testing.T) {
	dataDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Binary = filepath.Join(t.TempDir(), "does-not-exist-tor")

	m := NewManager(0, cfg, t.TempDir(), dataDir)
	m.svc = &Service{}

	blob := []byte("imported-ed25519-key-blob")
	addr, err := m.ApplyKeys(context.Background(), blob)
	if err == nil {
		t.Fatal("expected an error since Tor cannot actually start in this sandbox")
	}
	if addr != "" {
		t.Errorf("expected empty address on failure, got %q", addr)
	}

	got, readErr := os.ReadFile(onionKeyPath(dataDir))
	if readErr != nil {
		t.Fatalf("expected imported key to be written: %v", readErr)
	}
	if string(got) != string(blob) {
		t.Errorf("imported key content = %q, want %q", got, blob)
	}
}

// TestManagerOnionAddress verifies OnionAddress returns "" with no Service
// and delegates to the Service otherwise.
func TestManagerOnionAddress(t *testing.T) {
	m := NewManager(0, DefaultConfig(), t.TempDir(), t.TempDir())
	if got := m.OnionAddress(); got != "" {
		t.Errorf("OnionAddress() with no service = %q, want empty", got)
	}

	m.svc = &Service{onionID: "abcdefghijklmnop"}
	if got := m.OnionAddress(); got != "abcdefghijklmnop.onion" {
		t.Errorf("OnionAddress() = %q, want abcdefghijklmnop.onion", got)
	}
}

// TestManagerGetHTTPClient verifies a basic client is returned with no
// Service running, and that the call is delegated once one exists.
func TestManagerGetHTTPClient(t *testing.T) {
	m := NewManager(0, DefaultConfig(), t.TempDir(), t.TempDir())
	client := m.GetHTTPClient(true)
	if client == nil {
		t.Fatal("expected a non-nil client with no service running")
	}

	m.svc = &Service{}
	client = m.GetHTTPClient(false)
	if client == nil {
		t.Fatal("expected a non-nil client once a service is set")
	}
}

// TestManagerPing verifies Ping errors cleanly with no Service, and
// delegates once one exists (still erroring, since the fake Service has no
// real Tor control connection).
func TestManagerPing(t *testing.T) {
	m := NewManager(0, DefaultConfig(), t.TempDir(), t.TempDir())
	if err := m.Ping(); err == nil {
		t.Error("expected an error when Tor was never started")
	}

	m.svc = &Service{}
	if err := m.Ping(); err == nil {
		t.Error("expected an error from a fake service with no real control connection")
	}
}

// TestManagerRunning verifies Running reflects whether a Service is set.
func TestManagerRunning(t *testing.T) {
	m := NewManager(0, DefaultConfig(), t.TempDir(), t.TempDir())
	if m.Running() {
		t.Error("expected Running() to be false initially")
	}

	m.svc = &Service{}
	if !m.Running() {
		t.Error("expected Running() to be true once svc is set")
	}
}

// TestManagerClose verifies Close is a no-op with no Service, and clears an
// existing one.
func TestManagerClose(t *testing.T) {
	m := NewManager(0, DefaultConfig(), t.TempDir(), t.TempDir())
	if err := m.Close(); err != nil {
		t.Errorf("Close() with no service = %v, want nil", err)
	}

	m.svc = &Service{}
	if err := m.Close(); err != nil {
		t.Errorf("Close() with a fake (tor-nil) service = %v, want nil", err)
	}
	if m.Running() {
		t.Error("expected Running() to be false after Close")
	}
}
