package tor

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Manager owns the lifecycle of the dedicated Tor Service - start, restart,
// config updates, key rotation, and shutdown - and is safe for concurrent
// use. There is exactly one Manager per running server process.
type Manager struct {
	mu sync.Mutex

	svc *Service
	cfg Config

	serverPort int
	configDir  string
	dataDir    string
}

// global holds the process-wide Manager instance so packages that cannot
// import a dependency cycle back to the composition root (e.g. the
// scheduler's tor_health task) can still reach the running Tor service.
var (
	globalMu sync.RWMutex
	global   *Manager
)

// Set registers m as the process-wide Manager instance. Called once from
// main.go's composition root after the Manager is created.
func Set(m *Manager) {
	globalMu.Lock()
	defer globalMu.Unlock()
	global = m
}

// Get returns the process-wide Manager instance, or nil if Tor was never
// started (e.g. no Tor binary found, or Tor is disabled).
func Get() *Manager {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return global
}

// NewManager creates a Manager bound to a specific server port and
// {config_dir}/{data_dir} roots. Start must be called separately.
func NewManager(serverPort int, cfg Config, configDir, dataDir string) *Manager {
	return &Manager{
		cfg:        cfg,
		serverPort: serverPort,
		configDir:  configDir,
		dataDir:    dataDir,
	}
}

// onionKeyPath returns the path to the persistent v3 hidden-service key
// under the given {data_dir}.
func onionKeyPath(dataDir string) string {
	return filepath.Join(dataDir, "tor", "site", "hs_ed25519_secret_key")
}

// removeOnionKey deletes the hidden-service key directory so a fresh v3
// address is generated on the next start. Missing files are not an error.
func removeOnionKey(keyPath string) error {
	siteDir := filepath.Dir(keyPath)
	if err := os.RemoveAll(siteDir); err != nil {
		return err
	}
	return os.MkdirAll(siteDir, 0700)
}

// Start starts the dedicated Tor process and hidden service. A missing Tor
// binary or any Tor failure is returned as an error for the caller to log
// as non-fatal - the server must continue running without Tor.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.svc != nil {
		return fmt.Errorf("tor already started")
	}

	svc, err := start(ctx, m.serverPort, m.cfg, m.configDir, m.dataDir)
	if err != nil {
		return err
	}

	m.svc = svc
	return nil
}

// Restart stops and restarts the dedicated Tor process using the current
// configuration.
func (m *Manager) Restart(ctx context.Context) error {
	m.mu.Lock()
	svc := m.svc
	m.svc = nil
	m.mu.Unlock()

	if svc != nil {
		if err := svc.Close(); err != nil {
			log.Printf("Tor: warning, error stopping previous process: %v", err)
		}
	}

	return m.Start(ctx)
}

// UpdateConfig applies new settings and restarts Tor to pick them up. The
// torrc on disk is regenerated to match the new configuration.
func (m *Manager) UpdateConfig(ctx context.Context, cfg Config) error {
	m.mu.Lock()
	m.cfg = cfg
	torrcPath := filepath.Join(m.configDir, "tor", "torrc")
	m.mu.Unlock()

	if err := updateTorrc(torrcPath, []byte(generateTorrc(cfg))); err != nil {
		return fmt.Errorf("failed to update torrc: %w", err)
	}
	log.Println("Tor: updated torrc with new settings")

	return m.Restart(ctx)
}

// RegenerateAddress discards the current hidden-service key, forcing a new
// v3 onion address to be generated on the next restart, and returns it.
func (m *Manager) RegenerateAddress(ctx context.Context) (string, error) {
	m.mu.Lock()
	svc := m.svc
	m.svc = nil
	dataDir := m.dataDir
	m.mu.Unlock()

	if svc != nil {
		if err := svc.Close(); err != nil {
			log.Printf("Tor: warning, error stopping previous process: %v", err)
		}
	}

	if err := removeOnionKey(onionKeyPath(dataDir)); err != nil {
		return "", fmt.Errorf("failed to remove existing onion key: %w", err)
	}

	if err := m.Start(ctx); err != nil {
		return "", err
	}

	return m.OnionAddress(), nil
}

// ApplyKeys imports an operator-supplied ed25519 private key blob so the
// hidden service takes on a specific (e.g. vanity) .onion address, and
// restarts Tor to apply it.
func (m *Manager) ApplyKeys(ctx context.Context, keyBlob []byte) (string, error) {
	m.mu.Lock()
	svc := m.svc
	m.svc = nil
	dataDir := m.dataDir
	m.mu.Unlock()

	if svc != nil {
		if err := svc.Close(); err != nil {
			log.Printf("Tor: warning, error stopping previous process: %v", err)
		}
	}

	if err := ensureTorFile(onionKeyPath(dataDir), keyBlob); err != nil {
		return "", fmt.Errorf("failed to save imported onion key: %w", err)
	}

	if err := m.Start(ctx); err != nil {
		return "", err
	}

	return m.OnionAddress(), nil
}

// OnionAddress returns the current hidden service's .onion address, or ""
// if Tor is not running.
func (m *Manager) OnionAddress() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.svc == nil {
		return ""
	}
	return m.svc.OnionAddress()
}

// GetHTTPClient returns an *http.Client, optionally routed through Tor. See
// Service.GetHTTPClient.
func (m *Manager) GetHTTPClient(useTor bool) *http.Client {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.svc == nil {
		return &http.Client{}
	}
	return m.svc.GetHTTPClient(useTor)
}

// Ping checks the running Tor control connection is still responsive. It
// returns an error (never panics) when Tor was never started.
func (m *Manager) Ping() error {
	m.mu.Lock()
	svc := m.svc
	m.mu.Unlock()

	if svc == nil {
		return fmt.Errorf("tor not running")
	}
	return svc.Ping()
}

// Running reports whether the dedicated Tor process is currently active.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.svc != nil
}

// Close stops the dedicated Tor process, if running.
func (m *Manager) Close() error {
	m.mu.Lock()
	svc := m.svc
	m.svc = nil
	m.mu.Unlock()

	if svc == nil {
		return nil
	}
	return svc.Close()
}
