package tor

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// Service wraps a running dedicated Tor process and its v3 hidden service,
// per AI.md PART 31. The server binary fully owns the process lifecycle.
type Service struct {
	process *torProcess

	// onionAddress is the full address read from Tor's HiddenServiceDir.
	onionAddress string

	backendListener net.Listener

	// dialer routes outbound connections through Tor; nil when
	// TorConfig.UseNetwork is false or the dialer could not be created.
	dialer proxy.ContextDialer
}

// start launches the dedicated Tor process and waits for its hidden service.
//
// configDir/dataDir are the resolved {config_dir} and {data_dir} roots
// (paths.ConfigDir()/paths.DataDir() in the caller) - this package never
// resolves OS paths itself so it stays decoupled from src/paths.
func start(ctx context.Context, cfg TorConfig, configDir, dataDir string) (*Service, error) {
	binary, err := findBinary(cfg.Binary)
	if err != nil {
		return nil, err
	}

	if err := ensureTorDirs(configDir, dataDir); err != nil {
		return nil, fmt.Errorf("failed to create tor directories: %w", err)
	}

	torrcPath := filepath.Join(configDir, "tor", "torrc")
	torDataDir := filepath.Join(dataDir, "tor")
	hsDir := filepath.Join(dataDir, "tor", "site")
	backendListener, backendPort, err := listenBackend()
	if err != nil {
		return nil, fmt.Errorf("failed to bind dedicated Tor backend: %w", err)
	}
	keepBackend := false
	defer func() {
		if !keepBackend {
			_ = backendListener.Close()
		}
	}()

	torrcContent := generateTorrc(cfg, torDataDir, hsDir, backendPort)

	if err := updateTorrc(torrcPath, []byte(torrcContent)); err != nil {
		return nil, fmt.Errorf("failed to ensure torrc: %w", err)
	}
	log.Printf("Tor: generated dedicated torrc at %s", torrcPath)

	log.Println("Starting Tor hidden service...")
	controlFile := controlPortPath(torDataDir)
	if err := os.Remove(controlFile); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove stale Tor control port file: %w", err)
	}
	process, err := spawnTor(binary, torrcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to start dedicated tor: %w", err)
	}
	keepProcess := false
	defer func() {
		if !keepProcess {
			_ = process.close()
		}
	}()
	startupTimeout := 60 * time.Second
	if cfg.StartupTimeout > 0 {
		startupTimeout = time.Duration(cfg.StartupTimeout) * time.Second
	}
	controlAddress, err := process.waitControlPort(ctx, controlFile, startupTimeout)
	if err != nil {
		return nil, fmt.Errorf("tor control port: %w", err)
	}
	ctrl, controlConn, err := dialControl(ctx, controlAddress)
	if err != nil {
		return nil, fmt.Errorf("tor control authentication: %w", err)
	}
	process.control = ctrl
	process.controlConn = controlConn

	bootstrapTimeout := time.Duration(cfg.BootstrapTimeout) * time.Second
	dialCtx, cancel := context.WithTimeout(ctx, bootstrapTimeout)
	defer cancel()

	slow := make(chan struct{})
	slowTimer := time.AfterFunc(30*time.Second, func() { close(slow) })
	defer slowTimer.Stop()
	go func() {
		select {
		case <-slow:
			log.Println("Tor: connecting...")
		case <-dialCtx.Done():
		}
	}()

	events, err := bootstrapTor(dialCtx, ctrl, process)
	if err != nil {
		return nil, fmt.Errorf("tor bootstrap timeout, hidden service unavailable: %w", err)
	}

	address, err := waitHostname(ctx, filepath.Join(hsDir, "hostname"), bootstrapTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed waiting for Tor hidden-service hostname: %w", err)
	}
	if !waitDescriptor(ctx, events, process, address) {
		log.Printf("Tor: hidden-service descriptor upload is still pending")
	}

	svc := &Service{
		process:         process,
		onionAddress:    address,
		backendListener: backendListener,
	}
	keepBackend = true
	keepProcess = true

	if cfg.UseNetwork {
		if values, infoErr := ctrl.GetInfo("net/listeners/socks"); infoErr == nil && len(values) > 0 {
			address := strings.Trim(strings.TrimSpace(values[0].Val), "\"")
			if dialer, dialErr := proxy.SOCKS5("tcp", address, nil, proxy.Direct); dialErr == nil {
				if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
					svc.dialer = contextDialer
				}
			}
		}
		if svc.dialer == nil {
			log.Printf("Tor: warning, failed to create outbound dialer")
		} else {
			log.Println("Tor: outbound connections enabled")
		}
	}

	log.Printf("Tor: %s", svc.OnionAddress())
	return svc, nil
}

func listenBackend() (net.Listener, int, error) {
	for attempt := 0; attempt < 1000; attempt++ {
		port := 64000 + rand.Intn(1000)
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			return listener, port, nil
		}
	}
	return nil, 0, fmt.Errorf("no available Tor backend port in 64000-64999")
}

// OnionAddress returns the full ".onion" address for this hidden service.
func (s *Service) OnionAddress() string {
	return s.onionAddress
}

// OutboundEnabled reports whether outbound-via-Tor networking is available.
func (s *Service) OutboundEnabled() bool {
	return s.dialer != nil
}

// GetHTTPClient returns an *http.Client. When useTor is true and an
// outbound dialer is available, requests are routed through Tor; otherwise
// a direct client is returned.
func (s *Service) GetHTTPClient(useTor bool) *http.Client {
	if !useTor || s.dialer == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}

	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			DialContext: s.dialer.DialContext,
		},
	}
}

// Ping checks the Tor control connection is still responsive.
func (s *Service) Ping() error {
	if s.process == nil || s.process.control == nil {
		return fmt.Errorf("tor control connection not available")
	}
	s.process.setControlDeadline(10 * time.Second)
	_, err := s.process.control.GetInfo("version")
	s.process.clearControlDeadline()
	return err
}

// Close terminates the dedicated Tor process.
func (s *Service) Close() error {
	if s.backendListener != nil {
		_ = s.backendListener.Close()
	}
	if s.process != nil {
		return s.process.close()
	}
	return nil
}
