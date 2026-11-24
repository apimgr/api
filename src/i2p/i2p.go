// Package i2p manages an I2P eepsite through SAMv3, using a private i2pd
// process when available and an operator-provided SAM bridge otherwise.
package i2p

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
)

// State is the operator-safe lifecycle state used by status and health output.
type State string

const (
	StateDisabled   State = "disabled"
	StateNoProvider State = "no_provider"
	StateStarting   State = "starting"
	StateBuilding   State = "building"
	StateRunning    State = "running"
	StateError      State = "error"
)

// Provider identifies the SAM provider selected for the eepsite.
type Provider string

const (
	ProviderNone Provider = "none"
	ProviderI2PD Provider = "i2pd"
	ProviderSAM  Provider = "sam"
)

// Snapshot contains public-safe I2P lifecycle data.
type Snapshot struct {
	Enabled  bool
	Running  bool
	State    State
	Provider Provider
	Address  string
}

// Manager owns the I2P provider and backend listener for one server process.
type Manager struct {
	mu            sync.RWMutex
	cfg           config.I2PConfig
	state         State
	provider      Provider
	address       string
	service       *service
	listener      net.Listener
	server        *http.Server
	backend       http.Handler
	cancel        context.CancelFunc
	attemptCancel context.CancelFunc
	wake          chan struct{}
	runDone       chan struct{}
	started       bool
}

type service struct {
	provider Provider
	address  string
	control  net.Conn
	forward  net.Conn
	command  *exec.Cmd
	exited   <-chan struct{}
	lost     chan struct{}
	lostOnce sync.Once
}

var samBase64 = base64.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-~")
var errDuplicatedDestination = errors.New("SAM destination is already active")
var errInvalidDestination = errors.New("SAM rejected the persisted destination key")

var globalManager struct {
	sync.RWMutex
	manager *Manager
}

// Set registers the active process manager for health and URL resolution.
func Set(manager *Manager) {
	globalManager.Lock()
	globalManager.manager = manager
	globalManager.Unlock()
}

// Get returns the active process manager, if one was created.
func Get() *Manager {
	globalManager.RLock()
	defer globalManager.RUnlock()
	return globalManager.manager
}

// NewManager creates an I2P manager. Start never delays the clearnet listener.
func NewManager(cfg config.I2PConfig, backend http.Handler) *Manager {
	return &Manager{cfg: cfg, state: StateDisabled, provider: ProviderNone, backend: backend, wake: make(chan struct{}, 1)}
}

// Start starts provider discovery and tunnel construction asynchronously.
func (m *Manager) Start(parent context.Context) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	ctx, cancel := context.WithCancel(parent)
	m.cancel = cancel
	m.runDone = make(chan struct{})
	done := m.runDone
	m.mu.Unlock()
	go func() { defer close(done); m.run(ctx) }()
}

func (m *Manager) run(ctx context.Context) {
	var listener net.Listener
	backoff := 5 * time.Second
	providerWarned := false
	for ctx.Err() == nil {
		m.mu.RLock()
		cfg := m.cfg
		m.mu.RUnlock()
		if !cfg.Enabled {
			m.setState(StateDisabled, ProviderNone, "")
			m.stopBackend()
			listener = nil
			if !m.waitChange(ctx, 0) {
				return
			}
			continue
		}
		provider, binary, err := resolveProvider(&cfg)
		if err != nil {
			m.setState(StateNoProvider, ProviderNone, "")
			if !providerWarned {
				log.Printf("WARN: I2P provider unavailable; eepsite disabled")
				providerWarned = true
			}
			if !m.waitChange(ctx, 30*time.Second) {
				return
			}
			continue
		}
		providerWarned = false
		if listener == nil {
			if err := ensurePrivateDirs(); err != nil {
				log.Printf("WARN: I2P private directories unavailable: %v", err)
				m.setState(StateError, provider, "")
				if !m.waitChange(ctx, backoff) {
					return
				}
				continue
			}
			listener, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				log.Printf("WARN: I2P backend listener unavailable: %v", err)
				m.setState(StateError, provider, "")
				if !m.waitChange(ctx, backoff) {
					return
				}
				continue
			}
			m.mu.Lock()
			server := &http.Server{Handler: m.backend, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
			m.listener, m.server = listener, server
			m.mu.Unlock()
			go func() { _ = server.Serve(listener) }()
		}
		m.setState(StateStarting, provider, "")
		attemptCtx, attemptCancel := context.WithCancel(ctx)
		m.mu.Lock()
		m.attemptCancel = attemptCancel
		m.mu.Unlock()
		svc, err := startService(attemptCtx, &cfg, provider, binary, listener.Addr().(*net.TCPAddr).Port, func() { m.setState(StateBuilding, provider, "") })
		attemptCancel()
		m.mu.Lock()
		m.attemptCancel = nil
		m.mu.Unlock()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if ctx.Err() == nil {
				log.Printf("WARN: I2P start failed: %v", err)
			}
			m.setState(StateError, provider, "")
			if errors.Is(err, errDuplicatedDestination) {
				backoff = 60 * time.Second
			}
			if errors.Is(err, errInvalidDestination) {
				if !m.waitChange(ctx, 0) {
					return
				}
				continue
			}
			if !m.waitChange(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 5*time.Minute)
			continue
		}
		backoff = 5 * time.Second
		m.mu.Lock()
		m.service, m.address, m.state, m.provider = svc, svc.address, StateRunning, provider
		m.mu.Unlock()
		watchService(svc, func() { m.setState(StateError, provider, "") })
		log.Printf("I2P eepsite running (%s): %s", provider, svc.address)
		if m.monitor(ctx, svc, provider) {
			closeService(svc)
			return
		}
		closeService(svc)
		m.mu.Lock()
		m.service, m.address = nil, ""
		m.mu.Unlock()
	}
}

func (m *Manager) waitChange(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		case <-m.wake:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-m.wake:
		return true
	case <-timer.C:
		return true
	}
}

func (m *Manager) stopBackend() {
	m.mu.Lock()
	listener, server := m.listener, m.server
	m.listener, m.server = nil, nil
	m.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
	if listener != nil {
		_ = listener.Close()
	}
}

// UpdateConfig applies a configuration reload and restarts I2P.
func (m *Manager) UpdateConfig(cfg config.I2PConfig) {
	m.mu.Lock()
	m.cfg = cfg
	if m.attemptCancel != nil {
		m.attemptCancel()
	}
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func watchService(svc *service, lost func()) {
	for _, conn := range []net.Conn{svc.control, svc.forward} {
		go func(conn net.Conn) {
			var one [1]byte
			_, err := conn.Read(one[:])
			if err == nil || !errors.Is(err, net.ErrClosed) {
				svc.lostOnce.Do(func() { close(svc.lost) })
				lost()
			}
		}(conn)
	}
}

func (m *Manager) monitor(ctx context.Context, svc *service, provider Provider) bool {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return true
		case <-m.wake:
			return false
		case <-svc.exited:
			m.setState(StateError, provider, "")
			return false
		case <-svc.lost:
			m.setState(StateError, provider, "")
			return false
		case <-ticker.C:
			if provider == ProviderSAM {
				if probeSAM(m.cfg.SAMAddress) {
					failures = 0
				} else {
					failures++
				}
				if failures >= 3 {
					m.setState(StateError, provider, "")
					return false
				}
			}
			if svc.control == nil || svc.forward == nil {
				m.setState(StateError, provider, "")
				return false
			}
		}
	}
}

func (m *Manager) setState(state State, provider Provider, address string) {
	m.mu.Lock()
	m.state, m.provider, m.address = state, provider, address
	m.mu.Unlock()
}

// Snapshot returns current public-safe lifecycle state.
func (m *Manager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Snapshot{Enabled: m.cfg.Enabled && m.provider != ProviderNone, Running: m.state == StateRunning, State: m.state, Provider: m.provider, Address: m.address}
}

// Address returns the active .b32.i2p destination, if available.
func (m *Manager) Address() string { return m.Snapshot().Address }

// HealthCheck validates the active provider without sending commands to the
// SAM session-owning control connection. A failed check signals the lifecycle
// monitor, which performs the bounded restart with backoff.
func (m *Manager) HealthCheck() error {
	m.mu.RLock()
	svc, state, provider := m.service, m.state, m.provider
	samAddress := m.cfg.SAMAddress
	m.mu.RUnlock()
	if state == StateDisabled || state == StateNoProvider {
		return nil
	}
	if svc == nil {
		if state == StateError {
			return errors.New("I2P provider unavailable")
		}
		return nil
	}
	if provider == ProviderSAM && !probeSAM(samAddress) {
		m.setState(StateError, provider, "")
		select {
		case m.wake <- struct{}{}:
		default:
		}
		return errors.New("I2P SAM bridge unavailable")
	}
	select {
	case <-svc.exited:
		m.setState(StateError, provider, "")
		return errors.New("managed i2pd process exited")
	case <-svc.lost:
		m.setState(StateError, provider, "")
		return errors.New("I2P SAM session closed")
	default:
		return nil
	}
}

// Close stops the provider and loopback backend.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	cancel, done := m.cancel, m.runDone
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.stopBackend()
	return nil
}

func resolveProvider(cfg *config.I2PConfig) (Provider, string, error) {
	if binary, err := resolveI2PD(cfg.Binary); err == nil {
		return ProviderI2PD, binary, nil
	}
	if samHandshake(cfg.SAMAddress) == nil {
		return ProviderSAM, "", nil
	}
	return ProviderNone, "", errors.New("no i2pd binary or reachable SAM bridge")
}

func resolveI2PD(configured string) (string, error) {
	if configured != "" {
		st, err := os.Stat(configured)
		if err != nil || st.IsDir() {
			return "", errors.New("configured i2pd binary not found")
		}
		return configured, nil
	}
	for _, path := range []string{"/usr/bin/i2pd", "/usr/sbin/i2pd", "/usr/local/bin/i2pd", "/opt/homebrew/bin/i2pd"} {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path, nil
		}
	}
	return exec.LookPath("i2pd")
}

func startService(ctx context.Context, cfg *config.I2PConfig, provider Provider, binary string, backendPort int, building func()) (*service, error) {
	svc := &service{provider: provider, lost: make(chan struct{})}
	samAddress := cfg.SAMAddress
	if provider == ProviderI2PD {
		var err error
		samAddress, err = launchI2PD(ctx, binary, &svc.command, &svc.exited)
		if err != nil {
			return nil, err
		}
	}
	keys := filepath.Join(paths.DataDir(), "i2p", "site", "site-keys.dat")
	address, control, forward, err := createEepsite(ctx, cfg, samAddress, keys, backendPort, building)
	if err != nil {
		stopChild(svc.command, svc.exited)
		return nil, err
	}
	svc.address, svc.control, svc.forward = address, control, forward
	return svc, nil
}

func createEepsite(ctx context.Context, cfg *config.I2PConfig, samAddress, keyPath string, backendPort int, building func()) (string, net.Conn, net.Conn, error) {
	control, reader, err := samDial(ctx, samAddress)
	if err != nil {
		return "", nil, nil, fmt.Errorf("SAM control connection: %w", err)
	}
	priv, err := loadDestination(control, reader, keyPath, cfg.SignatureType)
	if err != nil {
		closeSAMConnection(control)
		return "", nil, nil, err
	}
	destination, err := destinationBytes(priv)
	if err != nil {
		closeSAMConnection(control)
		return "", nil, nil, err
	}
	address := b32Address(destination)
	building()
	var id string
	var reply string
	for attempt := 0; attempt < 2; attempt++ {
		id = fmt.Sprintf("site-%d-%d", os.Getpid(), attempt)
		if err := samWrite(control, "SESSION CREATE STYLE=STREAM ID=%s DESTINATION=%s i2cp.leaseSetEncType=4 inbound.length=%d outbound.length=%d inbound.quantity=%d outbound.quantity=%d", id, priv, cfg.InboundLength, cfg.OutboundLength, cfg.InboundQuantity, cfg.OutboundQuantity); err != nil {
			closeSAMConnection(control)
			return "", nil, nil, errors.New("SESSION CREATE write failed")
		}
		_ = control.SetDeadline(time.Now().Add(time.Duration(cfg.BootstrapTimeout) * time.Second))
		reply, err = reader.ReadString('\n')
		_ = control.SetDeadline(time.Time{})
		if err != nil {
			closeSAMConnection(control)
			return "", nil, nil, errors.New("SESSION CREATE failed or timed out")
		}
		if strings.Contains(reply, "RESULT=OK") {
			break
		}
		if attempt == 0 && strings.Contains(reply, "RESULT=DUPLICATED_ID") {
			continue
		}
		closeSAMConnection(control)
		if strings.Contains(reply, "RESULT=DUPLICATED_DEST") {
			return "", nil, nil, errDuplicatedDestination
		}
		if strings.Contains(reply, "RESULT=INVALID_KEY") {
			return "", nil, nil, errInvalidDestination
		}
		return "", nil, nil, errors.New("SESSION CREATE failed")
	}
	forward, freader, err := samDial(ctx, samAddress)
	if err != nil {
		closeSAMConnection(control)
		return "", nil, nil, err
	}
	if err := samWrite(forward, "STREAM FORWARD ID=%s PORT=%d HOST=127.0.0.1 SILENT=true", id, backendPort); err != nil {
		closeSAMConnection(control)
		closeSAMConnection(forward)
		return "", nil, nil, errors.New("STREAM FORWARD write failed")
	}
	_ = forward.SetDeadline(time.Now().Add(10 * time.Second))
	reply, err = freader.ReadString('\n')
	_ = forward.SetDeadline(time.Time{})
	if err != nil || !strings.Contains(reply, "RESULT=OK") {
		closeSAMConnection(control)
		closeSAMConnection(forward)
		return "", nil, nil, errors.New("STREAM FORWARD failed")
	}
	return address, control, forward, nil
}

func closeSAMConnection(conn net.Conn) {
	if err := conn.Close(); err != nil {
		log.Printf("I2P: failed to close SAM connection: %v", err)
	}
}

func samDial(ctx context.Context, address string) (net.Conn, *bufio.Reader, error) {
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, err
	}
	reader := bufio.NewReader(conn)
	if err := samWrite(conn, "HELLO VERSION MIN=3.1 MAX=3.3"); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	reply, err := reader.ReadString('\n')
	_ = conn.SetDeadline(time.Time{})
	if err != nil || !strings.Contains(reply, "RESULT=OK") {
		_ = conn.Close()
		return nil, nil, errors.New("SAM HELLO failed")
	}
	return conn, reader, nil
}

func samHandshake(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, reader, err := samDial(ctx, address)
	if err != nil {
		return err
	}
	_ = samWrite(conn, "QUIT")
	_, _ = reader.ReadString('\n')
	return conn.Close()
}

func probeSAM(address string) bool { return samHandshake(address) == nil }

func samWrite(conn net.Conn, format string, args ...any) error {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := fmt.Fprintf(conn, format+"\n", args...)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func loadDestination(conn net.Conn, reader *bufio.Reader, path string, signatureType int) (string, error) {
	if err := ensurePrivateFile(path); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err == nil {
		priv := strings.TrimSpace(string(b))
		if _, err := destinationBytes(priv); err != nil {
			return "", fmt.Errorf("corrupt I2P destination key %s", path)
		}
		return priv, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := samWrite(conn, "DEST GENERATE SIGNATURE_TYPE=%d", signatureType); err != nil {
		return "", errors.New("DEST GENERATE failed")
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	reply, err := reader.ReadString('\n')
	_ = conn.SetDeadline(time.Time{})
	if err != nil {
		return "", errors.New("DEST GENERATE failed")
	}
	priv := samValue(reply, "PRIV")
	if _, err := destinationBytes(priv); err != nil {
		return "", errors.New("SAM generated an invalid destination")
	}
	if err := writePrivateAtomic(path, []byte(priv+"\n")); err != nil {
		return "", err
	}
	return priv, nil
}

func samValue(line, key string) string {
	for _, field := range strings.Fields(line) {
		if k, v, ok := strings.Cut(field, "="); ok && k == key {
			return v
		}
	}
	return ""
}

func destinationBytes(priv string) ([]byte, error) {
	raw, err := samBase64.DecodeString(priv)
	if err != nil || len(raw) < 387 {
		return nil, errors.New("invalid I2P destination key")
	}
	n := 387 + int(binary.BigEndian.Uint16(raw[385:387]))
	if len(raw) < n {
		return nil, errors.New("truncated I2P destination key")
	}
	return raw[:n], nil
}

func b32Address(destination []byte) string {
	sum := sha256.Sum256(destination)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])) + ".b32.i2p"
}

func ensurePrivateDirs() error {
	for _, dir := range []string{filepath.Join(paths.ConfigDir(), "i2p"), filepath.Join(paths.DataDir(), "i2p"), filepath.Join(paths.DataDir(), "i2p", "site")} {
		if err := ensurePrivateDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateDir(dir string) error {
	if info, err := os.Lstat(dir); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return errors.New("I2P directory path is not a real directory")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("I2P directory path is not a real directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Chown(dir, os.Getuid(), os.Getgid())
	}
	return nil
}

func preparePrivateFile(path string) error {
	if err := ensurePrivateFile(path); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	closeErr := file.Close()
	if closeErr != nil {
		return closeErr
	}
	return ensurePrivateFile(path)
}

func ensurePrivateFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("I2P key path is not a regular file")
	}
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Chown(path, os.Getuid(), os.Getgid())
	}
	return nil
}

func writePrivateAtomic(path string, contents []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".i2p-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if _, err = f.Write(contents); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = atomicReplace(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chown(path, os.Getuid(), os.Getgid()); err != nil {
			return err
		}
	}
	return nil
}

func launchI2PD(ctx context.Context, binary string, command **exec.Cmd, exited *<-chan struct{}) (string, error) {
	confDir, dataDir := filepath.Join(paths.ConfigDir(), "i2p"), filepath.Join(paths.DataDir(), "i2p")
	samPort, err := unusedPort()
	if err != nil {
		return "", err
	}
	confPath := filepath.Join(confDir, "i2pd.conf")
	logPath := filepath.Join(paths.LogDir(), "i2pd.log")
	if err := preparePrivateFile(logPath); err != nil {
		return "", err
	}
	conf := fmt.Sprintf("daemon = false\nipv4 = true\nipv6 = false\nnotransit = true\nfloodfill = false\nlog = file\nlogfile = %s\nloglevel = warn\n\n[http]\nenabled = false\n[httpproxy]\nenabled = false\n[socksproxy]\nenabled = false\n[sam]\nenabled = true\naddress = 127.0.0.1\nport = %d\n[bob]\nenabled = false\n[i2cp]\nenabled = false\n[i2pcontrol]\nenabled = false\n[upnp]\nenabled = false\n", logPath, samPort)
	if err := writePrivateAtomic(confPath, []byte(conf)); err != nil {
		return "", err
	}
	if err := ensurePrivateFile(logPath); err != nil {
		return "", err
	}
	tunconf, tundir := filepath.Join(confDir, "tunnels.conf"), filepath.Join(confDir, "tunnels.d")
	if err := ensurePrivateFile(tunconf); err != nil {
		return "", err
	}
	if _, err := os.Stat(tunconf); errors.Is(err, fs.ErrNotExist) {
		if err := writePrivateAtomic(tunconf, nil); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if err := ensurePrivateDir(tundir); err != nil {
		return "", err
	}
	pidPath := filepath.Join(dataDir, "i2pd.pid")
	reapStaleI2PD(pidPath, binary)
	cmd := exec.Command(binary, "--conf", confPath, "--tunconf", tunconf, "--tunnelsdir", tundir, "--datadir", dataDir, "--pidfile", pidPath)
	setParentDeathSignal(cmd)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan struct{})
	*command, *exited = cmd, done
	go func() { _ = cmd.Wait(); close(done) }()
	address := fmt.Sprintf("127.0.0.1:%d", samPort)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			stopChild(cmd, done)
			return "", ctx.Err()
		}
		if conn, _, err := samDial(ctx, address); err == nil {
			_ = conn.Close()
			return address, nil
		}
		select {
		case <-done:
			return "", errors.New("i2pd exited before SAM became ready")
		case <-time.After(500 * time.Millisecond):
		}
	}
	stopChild(cmd, done)
	return "", errors.New("i2pd SAM did not become ready within 60 seconds")
}

func unusedPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("close temporary port listener: %w", err)
	}
	return port, nil
}

func stopChild(cmd *exec.Cmd, exited <-chan struct{}) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	terminateProcess(cmd)
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
	}
}

func closeService(svc *service) {
	if svc == nil {
		return
	}
	if svc.forward != nil {
		_ = svc.forward.Close()
	}
	if svc.control != nil {
		_ = svc.control.Close()
	}
	stopChild(svc.command, svc.exited)
}

func reapStaleI2PD(pidPath, binary string) {
	b, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid); err != nil || pid <= 1 || pid == os.Getpid() {
		return
	}
	if processMatches(pid, binary, filepath.Join(paths.ConfigDir(), "i2p", "i2pd.conf")) {
		terminatePID(pid)
	}
}
