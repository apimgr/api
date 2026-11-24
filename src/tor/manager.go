package tor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Manager owns the lifecycle of the dedicated Tor Service - start, restart,
// config updates, key rotation, and shutdown - and is safe for concurrent
// use. There is exactly one Manager per running server process.
type Manager struct {
	mu sync.Mutex

	svc         *Service
	cfg         Config
	enabled     bool
	starting    bool
	status      string
	startCancel context.CancelFunc
	startDone   chan struct{}

	configDir         string
	dataDir           string
	monitorCancel     context.CancelFunc
	onBackendListener func(net.Listener)

	// lastVanity holds the most recently found (but not yet applied)
	// vanity address candidate, so a later "tor vanity apply" CLI
	// invocation (a separate process) can apply it via ApplyVanity
	// without re-transmitting the private key blob over the loopback
	// control channel a second time.
	lastVanity *VanityResult
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

// NewManager creates a Manager bound to the resolved config and data roots.
func NewManager(cfg Config, configDir, dataDir string) *Manager {
	return &Manager{
		cfg:       cfg,
		configDir: configDir,
		dataDir:   dataDir,
		enabled:   true,
		status:    "starting",
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
	if m.svc != nil || m.starting {
		m.mu.Unlock()
		return fmt.Errorf("tor already started")
	}
	m.starting = true
	m.status = "starting"
	m.enabled = true
	cfg, configDir, dataDir := m.cfg, m.configDir, m.dataDir
	startCtx, cancelStart := context.WithCancel(ctx)
	done := make(chan struct{})
	m.startCancel = cancelStart
	m.startDone = done
	m.mu.Unlock()

	svc, err := start(startCtx, cfg, configDir, dataDir)
	if err == nil && startCtx.Err() != nil {
		err = startCtx.Err()
	}
	if err != nil && svc != nil {
		_ = svc.Close()
		svc = nil
	}
	cancelStart()
	m.mu.Lock()
	m.starting = false
	m.startCancel = nil
	m.startDone = nil
	close(done)
	if err != nil {
		m.enabled = !errors.Is(err, ErrBinaryNotFound)
		if m.enabled {
			m.status = "unavailable"
		} else {
			m.status = "disabled"
		}
		m.mu.Unlock()
		recordStartFailed(err)
		return err
	}

	m.svc = svc
	m.status = "healthy"
	recordStarted()
	if m.monitorCancel == nil {
		monitorCtx, cancel := context.WithCancel(context.Background())
		m.monitorCancel = cancel
		go m.monitor(monitorCtx)
	}
	backendListener, onBackendListener := svc.backendListener, m.onBackendListener
	m.mu.Unlock()
	if onBackendListener != nil {
		onBackendListener(backendListener)
	}
	return nil
}

// SetBackendListenerHandler registers the HTTP server callback for every
// private PROXY-protocol listener created during Tor start or restart.
func (m *Manager) SetBackendListenerHandler(handler func(net.Listener)) {
	m.mu.Lock()
	m.onBackendListener = handler
	listener := net.Listener(nil)
	if m.svc != nil {
		listener = m.svc.backendListener
	}
	m.mu.Unlock()
	if handler != nil && listener != nil {
		handler(listener)
	}
}

func (m *Manager) monitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Ping(); err != nil {
				log.Printf("Tor: control connection lost, restarting: %v", err)
				restartCtx, cancel := context.WithTimeout(ctx, time.Duration(m.ConfigSnapshot().BootstrapTimeout)*time.Second)
				if restartErr := m.Restart(restartCtx); restartErr != nil {
					log.Printf("Tor: restart failed: %v", restartErr)
				}
				cancel()
			}
		}
	}
}

// Restart stops and restarts the dedicated Tor process using the current
// configuration.
func (m *Manager) Restart(ctx context.Context) error {
	m.mu.Lock()
	if m.starting {
		cancelStart, done := m.startCancel, m.startDone
		m.mu.Unlock()
		if cancelStart != nil {
			cancelStart()
		}
		if done != nil {
			<-done
		}
		return m.Restart(ctx)
	}
	svc := m.svc
	m.svc = nil
	m.status = "starting"
	m.mu.Unlock()

	if svc != nil {
		if err := svc.Close(); err != nil {
			log.Printf("Tor: warning, error stopping previous process: %v", err)
		}
	}

	// The process is down for the whole restart window, so the gauges must
	// report that until the following Start succeeds.
	recordStopped()

	return m.Start(ctx)
}

// UpdateConfig applies new settings and restarts Tor to pick them up. The
// torrc on disk is regenerated to match the new configuration.
func (m *Manager) UpdateConfig(ctx context.Context, cfg Config) error {
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
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

	// The process is down for the whole restart window, so the gauges must
	// report that until the following Start succeeds.
	recordStopped()

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

	// The process is down for the whole restart window, so the gauges must
	// report that until the following Start succeeds.
	recordStopped()

	if err := ensureTorFile(onionKeyPath(dataDir), keyBlob); err != nil {
		return "", fmt.Errorf("failed to save imported onion key: %w", err)
	}

	if err := m.Start(ctx); err != nil {
		return "", err
	}

	return m.OnionAddress(), nil
}

// ConfigSnapshot returns a copy of the Manager's current Config, for
// read-only inspection (e.g. the internal /server/tor/status and
// /server/tor/validate handlers).
func (m *Manager) ConfigSnapshot() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// SetLastVanity records a found-but-not-yet-applied vanity address
// candidate so a subsequent ApplyLastVanity call can apply it.
func (m *Manager) SetLastVanity(v *VanityResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastVanity = v
}

// LastVanity returns the most recently found vanity address candidate, or
// nil if none is pending.
func (m *Manager) LastVanity() *VanityResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastVanity
}

// ApplyLastVanity applies the pending vanity address candidate recorded by
// SetLastVanity, clearing it afterward regardless of outcome. It returns an
// error if no candidate is pending.
func (m *Manager) ApplyLastVanity(ctx context.Context) (string, error) {
	m.mu.Lock()
	v := m.lastVanity
	m.lastVanity = nil
	m.mu.Unlock()

	if v == nil {
		return "", fmt.Errorf("no vanity address candidate is pending; run 'tor vanity start' first")
	}
	return m.ApplyKeys(ctx, v.Blob)
}

// OnionAddress returns the current hidden service's .onion address, or ""
// if Tor is not running.
func (m *Manager) OnionAddress() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.svc == nil || m.status != "healthy" {
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

	err := svc.Ping()
	m.mu.Lock()
	if err != nil {
		m.status = "unavailable"
	} else if m.svc == svc {
		m.status = "healthy"
	}
	m.mu.Unlock()
	recordHealth(err)
	return err
}

// Running reports whether the dedicated Tor process is currently active.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.svc != nil && m.status == "healthy"
}

// StatusSnapshot returns Tor's fixed health state without waiting for bootstrap.
func (m *Manager) StatusSnapshot() (enabled, running bool, status, hostname string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.svc != nil && m.status == "healthy" {
		hostname = m.svc.OnionAddress()
	}
	return m.enabled, m.svc != nil && m.status == "healthy", m.status, hostname
}

// BackendListener returns the private Tor-only listener after successful start.
func (m *Manager) BackendListener() net.Listener {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.svc == nil {
		return nil
	}
	return m.svc.backendListener
}

// Close stops the dedicated Tor process, if running.
func (m *Manager) Close() error {
	m.mu.Lock()
	svc := m.svc
	m.svc = nil
	m.status = "unavailable"
	startCancel, startDone := m.startCancel, m.startDone
	cancel := m.monitorCancel
	m.monitorCancel = nil
	m.mu.Unlock()
	if startCancel != nil {
		startCancel()
	}
	if startDone != nil {
		<-startDone
		m.mu.Lock()
		svc = m.svc
		m.svc = nil
		m.mu.Unlock()
	}
	if cancel != nil {
		cancel()
	}

	recordStopped()

	if svc == nil {
		return nil
	}
	return svc.Close()
}
