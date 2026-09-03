package tor

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cretz/bine/control"
	"github.com/cretz/bine/tor"
)

// Service wraps a running dedicated Tor process and its v3 hidden service,
// per AI.md PART 31. The server binary fully owns the process lifecycle.
type Service struct {
	// tor is the underlying bine process/control wrapper.
	tor *tor.Tor

	// onionID is the .onion service ID (without the ".onion" suffix).
	onionID string

	// serverPort is the server's own HTTP port the hidden service forwards
	// to on 127.0.0.1.
	serverPort int

	// dialer routes outbound connections through Tor; nil when
	// Config.UseNetwork is false or the dialer could not be created.
	dialer *tor.Dialer
}

// start starts a dedicated Tor process, waits for bootstrap, and creates a
// persistent v3 hidden service that forwards .onion:{VirtualPort} to
// 127.0.0.1:serverPort. serverPort must already be accepting connections -
// Tor forwards to an existing listener, it does not open a new one.
//
// configDir/dataDir are the resolved {config_dir} and {data_dir} roots
// (paths.ConfigDir()/paths.DataDir() in the caller) - this package never
// resolves OS paths itself so it stays decoupled from src/paths.
func start(ctx context.Context, serverPort int, cfg Config, configDir, dataDir string) (*Service, error) {
	binary, err := findBinary(cfg.Binary)
	if err != nil {
		return nil, err
	}

	if err := ensureTorDirs(configDir, dataDir); err != nil {
		return nil, fmt.Errorf("failed to create tor directories: %w", err)
	}

	torrcPath := filepath.Join(configDir, "tor", "torrc")
	torDataDir := filepath.Join(dataDir, "tor")
	keyPath := filepath.Join(dataDir, "tor", "site", "hs_ed25519_secret_key")

	torrcContent := generateTorrc(cfg)

	created, err := ensureTorrc(torrcPath, []byte(torrcContent))
	if err != nil {
		return nil, fmt.Errorf("failed to ensure torrc: %w", err)
	}
	if created {
		log.Printf("Tor: created new torrc at %s", torrcPath)
	} else {
		log.Printf("Tor: using existing torrc at %s", torrcPath)
	}

	conf := &tor.StartConf{
		ExePath:         binary,
		TorrcFile:       torrcPath,
		DataDir:         torDataDir,
		NoAutoSocksPort: true,
	}

	log.Println("Starting Tor hidden service...")
	t, err := tor.Start(ctx, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to start dedicated tor: %w", err)
	}

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

	if err := t.EnableNetwork(dialCtx, true); err != nil {
		t.Close()
		return nil, fmt.Errorf("tor bootstrap timeout, hidden service unavailable: %w", err)
	}

	// Load the existing v3 key blob (if any) for a persistent address.
	var existingKey control.Key
	if keyData, err := os.ReadFile(keyPath); err == nil && len(keyData) > 0 {
		existingKey, err = control.ED25519KeyFromBlob(string(keyData))
		if err != nil {
			t.Close()
			return nil, fmt.Errorf("failed to parse existing onion key: %w", err)
		}
	}

	addOnionReq := &control.AddOnionRequest{
		Ports: []*control.KeyVal{
			control.NewKeyVal(fmt.Sprintf("%d", cfg.VirtualPort), fmt.Sprintf("127.0.0.1:%d", serverPort)),
		},
	}
	if existingKey != nil {
		addOnionReq.Key = existingKey
	} else {
		addOnionReq.Key = control.GenKey(control.KeyAlgoED25519V3)
	}

	resp, err := t.Control.AddOnion(addOnionReq)
	if err != nil {
		t.Close()
		return nil, fmt.Errorf("failed to create onion service: %w", err)
	}

	if existingKey == nil && resp.Key != nil {
		if err := ensureTorFile(keyPath, []byte(resp.Key.Blob())); err != nil {
			log.Printf("Tor: warning, failed to save onion key: %v", err)
		}
	}

	svc := &Service{
		tor:        t,
		onionID:    resp.ServiceID,
		serverPort: serverPort,
	}

	if cfg.UseNetwork {
		dialer, err := t.Dialer(ctx, nil)
		if err != nil {
			log.Printf("Tor: warning, failed to create outbound dialer: %v", err)
		} else {
			svc.dialer = dialer
			log.Println("Tor: outbound connections enabled")
		}
	}

	log.Printf("Tor: %s", svc.OnionAddress())
	return svc, nil
}

// OnionAddress returns the full ".onion" address for this hidden service.
func (s *Service) OnionAddress() string {
	return s.onionID + ".onion"
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
	if s.tor == nil || s.tor.Control == nil {
		return fmt.Errorf("tor control connection not available")
	}
	_, err := s.tor.Control.GetInfo("version")
	return err
}

// Close terminates the dedicated Tor process.
func (s *Service) Close() error {
	if s.tor != nil {
		return s.tor.Close()
	}
	return nil
}
