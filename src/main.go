package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/email"
	"github.com/apimgr/api/src/geoip"
	"github.com/apimgr/api/src/metrics"
	appmode "github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/pidfile"
	"github.com/apimgr/api/src/scheduler"
	"github.com/apimgr/api/src/server"
	"github.com/apimgr/api/src/server/handler"
	"github.com/apimgr/api/src/ssl"
	"github.com/apimgr/api/src/sysservice"
	"github.com/apimgr/api/src/tor"
	"github.com/apimgr/api/src/update"
)

var (
	Version  = "1.0.0"
	CommitID = "unknown"
	// BuildDate is derived from BuildEpoch in init(); stays "unknown" when
	// BuildEpoch is unset (dev builds that skip -ldflags).
	BuildDate = "unknown"
	// BuildEpoch is the Unix build timestamp (seconds, UTC) embedded via
	// -ldflags at build time; "0" when unset. See AI.md "Embedded Build
	// Info" - BuildDate is derived from this, never embedded directly.
	BuildEpoch   = "0"
	OfficialSite = ""
)

// buildEpoch parses the embedded BuildEpoch ldflag; 0 when unset or invalid.
func buildEpoch() int64 {
	n, err := strconv.ParseInt(BuildEpoch, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// init derives BuildDate (RFC 3339 UTC) from the embedded BuildEpoch, per
// AI.md "Embedded Build Info" - BuildDate is never itself an ldflag.
func init() {
	if n := buildEpoch(); n > 0 {
		BuildDate = time.Unix(n, 0).UTC().Format("2006-01-02T15:04:05Z")
	}
}

// Sysexits-style exit codes (BSD sysexits.h); the stdlib does not export
// these, so they are defined locally per go_conventions.md.
const (
	exUsage       = 64 // Invalid flag or argument value
	exUnavailable = 69 // Required service or resource unavailable
	exOSErr       = 71 // OS-level error (fork/daemonize failed, etc.)
	exCantCreat   = 73 // Output file cannot be created (PID file)
	exConfig      = 78 // Configuration error
)

func main() {
	// Get actual binary name for user-facing messages
	binaryName := filepath.Base(os.Args[0])

	// CLI flags (only -h and -v have short forms per spec)
	showHelp := flag.Bool("help", false, "Show help")
	flag.BoolVar(showHelp, "h", false, "Show help (short)")
	showVersion := flag.Bool("version", false, "Show version")
	flag.BoolVar(showVersion, "v", false, "Show version (short)")

	// Server configuration
	mode := flag.String("mode", "", "Application mode: {production|development|debug}")
	configDir := flag.String("config", "", "Configuration directory")
	dataDir := flag.String("data", "", "Data directory")
	logDir := flag.String("log", "", "Log directory")
	cacheDir := flag.String("cache", "", "Cache directory")
	backupDir := flag.String("backup", "", "Backup directory")
	pidFile := flag.String("pid", "", "PID file path")
	address := flag.String("address", "", "Listen address")
	port := flag.String("port", "", "Listen port")
	baseURL := flag.String("baseurl", "", "URL path prefix (default: /)")
	daemon := flag.Bool("daemon", false, "Daemonize (detach from terminal)")
	debug := flag.Bool("debug", false, "Enable debug mode")
	colorFlag := flag.String("color", "auto", "Color output: auto, yes, no")
	langFlag := flag.String("lang", "", "Interface language code")
	shellCmd := flag.String("shell", "", "Shell integration: completions, init, help")

	// Status check
	showStatus := flag.Bool("status", false, "Show service status")

	// Service management
	serviceCmd := flag.String("service", "", "Service command: start, restart, stop, reload, --install, --uninstall, --disable, --help")

	// Maintenance commands
	maintenanceCmd := flag.String("maintenance", "", "Maintenance command: backup, restore, update, mode, setup")

	// Update command
	updateCmd := flag.String("update", "", "Update command: check, yes, or branch {stable|beta|daily}")

	// Tor management commands
	torCmd := flag.String("tor", "", "Tor command: status, validate, restart, regenerate, vanity, import-keys")

	flag.Parse()

	// Handle help
	if *showHelp {
		printHelp(binaryName)
		os.Exit(0)
	}

	// Handle version
	if *showVersion {
		cprintf("%s v%s\n", binaryName, Version)
		cprintf("Built: %s\n", BuildDate)
		cprintf("Go: %s\n", runtime.Version())
		cprintf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Resolve directory/network flags against their environment variable
	// fallbacks (CLI flag > env var > default), per PART 8.
	resolvedConfigDir := envOrFlag(*configDir, "CONFIG_DIR")
	resolvedDataDir := envOrFlag(*dataDir, "DATA_DIR")
	resolvedLogDir := envOrFlag(*logDir, "LOG_DIR")
	resolvedCacheDir := envOrFlag(*cacheDir, "CACHE_DIR")
	resolvedBackupDir := envOrFlag(*backupDir, "BACKUP_DIR")
	resolvedPIDFile := envOrFlag(*pidFile, "PID_FILE")
	resolvedAddress := envOrFlag(*address, "LISTEN")
	resolvedPort := envOrFlag(*port, "PORT")

	// Initialize paths early for commands that need them
	paths.Init(resolvedConfigDir, resolvedDataDir, resolvedLogDir)
	paths.InitCache(resolvedCacheDir)
	paths.InitBackup(resolvedBackupDir)

	// Resolve --color/NO_COLOR and --lang, applying them process-wide.
	// Config is not loaded yet, so this only applies the CLI-flag/NO_COLOR/
	// auto-detect tiers; it is re-resolved with the config-file tier below
	// once config.Load() succeeds, per AI.md PART 8's priority order.
	applyColorMode(*colorFlag, nil)
	if *langFlag != "" {
		appmode.SetLang(*langFlag)
	} else if envLang := os.Getenv("LANG"); envLang != "" {
		appmode.SetLang(envLang)
	}

	// Handle --shell before anything else needs a config/database
	if *shellCmd != "" {
		args := flag.Args()
		shellArg := ""
		if len(args) > 0 {
			shellArg = args[0]
		}
		handleShellCommand(*shellCmd, shellArg, binaryName)
		os.Exit(0)
	}

	// Handle status check
	if *showStatus {
		checkStatus()
		os.Exit(0)
	}

	// Handle service commands
	if *serviceCmd != "" {
		handleServiceCommand(*serviceCmd, binaryName)
		os.Exit(0)
	}

	// Handle maintenance commands
	if *maintenanceCmd != "" {
		// Get optional argument (file path or setting value)
		args := flag.Args()
		optionalArg := ""
		if len(args) > 0 {
			optionalArg = args[0]
		}
		handleMaintenanceCommand(*maintenanceCmd, optionalArg, args, binaryName)
		os.Exit(0)
	}

	// Handle update commands
	if *updateCmd != "" {
		// Get optional argument for branch command
		args := flag.Args()
		optionalArg := ""
		if len(args) > 0 {
			optionalArg = args[0]
		}
		handleUpdateCommand(*updateCmd, optionalArg, binaryName)
		os.Exit(0)
	}

	// Handle tor commands
	if *torCmd != "" {
		args := flag.Args()
		handleTorCommand(*torCmd, args, binaryName)
		os.Exit(0)
	}

	// Load configuration first so the database driver/URL/token it
	// specifies (server.cache.* sibling, server.database.*) are known
	// before opening the database connection.
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Failed to load configuration: %v", err)
		os.Exit(exConfig)
	}

	// Initialize database
	if err := database.Init(cfg.Server.Database, paths.DataDir()); err != nil {
		log.Printf("Failed to initialize database: %v", err)
		os.Exit(exUnavailable)
	}
	defer database.Close()

	// Generate the project-level cryptographic secrets on first start and
	// apply their automatic rotation schedule (AI.md PART 11 "Cryptographic
	// Keys"). A failure here is fatal: cookie signing and CSRF both depend on
	// this material.
	if err := database.EnsureAppSecrets(context.Background()); err != nil {
		log.Printf("Failed to initialize project secrets: %v", err)
		os.Exit(exUnavailable)
	}

	// Set database for health checks
	handler.SetDatabase(database.GetServerDB())

	// Re-resolve --color/NO_COLOR now that config is available, applying the
	// output.color/output.emoji config-file tier per AI.md PART 8's
	// priority order (CLI flag > config file > NO_COLOR env var > auto-detect).
	applyColorMode(*colorFlag, cfg.Output.Color)
	applyEmojiOverride(cfg.Output.Emoji)

	// Initialize logging system
	if err := server.InitLogger(&cfg.Server.Logs); err != nil {
		log.Printf("Warning: Failed to initialize logging system: %v", err)
	}

	// Record any first-run IDEA.md → Header Tightening Auto-Map changes to
	// the setup audit log now that the logger is available (config.Load()
	// runs before InitLogger, so it can't log directly — see
	// config.LastAutoTightenChanges).
	if logger := server.GetLogger(); logger != nil {
		for _, change := range config.LastAutoTightenChanges() {
			logger.LogAudit("header_auto_tighten", map[string]interface{}{
				"field":   change.Field,
				"old":     change.OldValue,
				"new":     change.NewValue,
				"trigger": change.Trigger,
			})
		}
	}

	// Initialize GeoIP database (load if exists, or will download on first use)
	if err := geoip.Get().LoadFromConfig(cfg.Server.GeoIP, paths.DataDir()); err != nil {
		log.Printf("Warning: Failed to load GeoIP database: %v (will auto-download on first request)", err)
	}

	// Initialize outbound email (PART 17): auto-detect a local SMTP relay
	// if none is configured, or connection-test the configured host every
	// startup; never blocks startup either way.
	initSMTP(cfg)

	// Override config with CLI flags (flags have highest priority)
	if resolvedAddress != "" {
		cfg.Server.Address = resolvedAddress
	}
	if resolvedPort != "" {
		cfg.Server.Port = resolvedPort
	}
	if *baseURL != "" {
		cfg.Server.BaseURL = *baseURL
	}

	// Determine whether --debug was explicitly passed (as opposed to
	// defaulting to false), so it can win over DEBUG env / debug mode
	// per the priority order in mode.Initialize.
	debugFlagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "debug" {
			debugFlagSet = true
		}
	})

	// Set application mode + debug flag (CLI > env > debug mode's default > default)
	if err := appmode.Initialize(*mode, *debug, debugFlagSet); err != nil {
		log.Printf("Invalid --mode value: %v", err)
		os.Exit(exUsage)
	}
	cfg.Server.Mode = appmode.GetCurrentMode().String()

	// Daemonize (detach from terminal) before writing the PID file, so the
	// PID file reflects the actual detached process, not the parent. Per
	// AI.md PART 8, --daemon is a manual-start-only concern: --service
	// start decides for itself via shouldDaemonize/detectServiceManager.
	if *daemon {
		if err := daemonize(); err != nil {
			log.Printf("Failed to daemonize: %v", err)
			os.Exit(exOSErr)
		}
	}

	// Write PID file (skipped entirely in containers per PART 8 - the
	// container runtime supervises the process and PIDs are namespace-local)
	pidPath := resolvedPIDFile
	if pidPath == "" {
		pidPath = paths.DefaultPIDPath()
	}
	if err := pidfile.WritePIDFile(pidPath); err != nil {
		log.Printf("Failed to write PID file: %v", err)
		os.Exit(exCantCreat)
	}
	defer pidfile.RemovePIDFile(pidPath)

	// Pass build version info to the health handler
	handler.Version = Version
	handler.CommitID = CommitID
	handler.BuildDate = BuildDate

	// Pass build version info to the web frontend
	server.Version = Version
	server.CommitID = CommitID
	server.BuildDate = BuildDate
	server.OfficialSite = OfficialSite

	// Fail fast if any embedded translation catalog drifted from the
	// canonical en.json key set (AI.md PART 30 build-time key validation).
	if err := i18n.Validate(); err != nil {
		cprintf("❌ Translation validation failed: %v\n", err)
		os.Exit(1)
	}

	// Initialize the metrics registry from resolved config before any
	// collector is touched, then stamp app_info and start the background
	// system/runtime sampler (AI.md PART 20).
	metrics.Init(metrics.Options{
		DurationBuckets: cfg.Server.Metrics.DurationBuckets,
		SizeBuckets:     cfg.Server.Metrics.SizeBuckets,
		IncludeSystem:   cfg.Server.Metrics.IncludeSystem,
		IncludeRuntime:  cfg.Server.Metrics.IncludeRuntime,
		DataDir:         paths.DataDir(),
	})
	metrics.Get().SetBuildInfo(Version, CommitID, BuildDate)
	metrics.Get().StartCollectors()
	defer metrics.Get().StopCollectors()

	// Create server
	srv := server.New(cfg)

	// Wire SSL/TLS per AI.md PART 15 Port Configuration rules: a single
	// port of 443 is HTTPS-only, ssl.enabled forces HTTPS on any other
	// single port, and "port1,port2" runs HTTP on the first and HTTPS on
	// the second. DNS-01 multi-provider support is a separate, deferred
	// item (see TODO.AI.md).
	httpPort, httpsPort := resolvePorts(cfg)
	var httpsSrv *http.Server
	if httpsPort != "" {
		sslCertPath := cfg.Server.SSL.CertPath
		if sslCertPath == "" {
			sslCertPath = filepath.Join(paths.ConfigDir(), "ssl")
		}
		domain := cfg.Server.FQDN
		if domain == "" {
			domain = "localhost"
		}
		sslMgr := ssl.NewManager(ssl.Config{
			Enabled:  true,
			CertPath: sslCertPath,
			LetsEncrypt: ssl.LetsEncryptConfig{
				Enabled:   cfg.Server.SSL.LetsEncrypt.Enabled,
				Email:     cfg.Server.SSL.LetsEncrypt.Email,
				Challenge: cfg.Server.SSL.LetsEncrypt.Challenge,
			},
		})
		tlsConfig, err := sslMgr.GetTLSConfig([]string{domain})
		if err != nil {
			log.Printf("Failed to configure TLS: %v", err)
			os.Exit(exConfig)
		}
		httpsSrv = &http.Server{
			Addr:         fmt.Sprintf("%s:%s", cfg.Server.Address, httpsPort),
			Handler:      srv.Handler,
			TLSConfig:    tlsConfig,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		}
		if httpPort != "" {
			// The HTTP listener stays up solely to answer ACME HTTP-01
			// challenges; all normal traffic is served over HTTPS.
			srv.Addr = fmt.Sprintf("%s:%s", cfg.Server.Address, httpPort)
			srv.Handler = sslMgr.GetHTTPHandler(srv.Handler)
		} else {
			// Single port 443, or ssl.enabled on a single non-443 port:
			// HTTPS-only mode, no separate HTTP listener.
			srv = nil
		}
	}

	// The built-in scheduler is mandatory and always runs (AI.md PART 18).
	// There is no enable/disable toggle for the scheduler itself; individual
	// tasks carry their own enabled flag.
	schedOpts := scheduler.OptionsFromConfig(cfg.Server.Scheduler)
	// update_check compares the running build against the release feed, and
	// the backup tasks stamp the running version into every archive manifest,
	// so both identity fields come from the embedded ldflag values.
	schedOpts.Version = Version
	schedOpts.BuildEpoch = buildEpoch()
	// The scheduler shares the process-wide operator notifier so failed tasks
	// send scheduler_error (and its backup/SSL replacements) through the same
	// templates and recipients as every other PART 17 event.
	if notifier := email.DefaultNotifier(); notifier != nil {
		schedOpts.Notifier = notifier
		schedOpts.NotifyTo = notifier.Recipients()
	}
	sched := scheduler.NewWithOptions(schedOpts)
	sched.RegisterDefaultTasks()
	sched.Start()
	defer sched.Stop()
	handler.SetSchedulerProbe(sched.Health)
	// The health response advertises GeoIP as a feature only when the country
	// database is actually loaded - a configured-but-undownloaded database is
	// not a capability a client can rely on.
	handler.SetGeoIPProbe(geoip.Get().HasCountryDB)
	log.Println("Scheduler started with default tasks")

	// Start config file watcher for hot reload
	configWatcher := config.NewConfigWatcher(func(newCfg *config.Config) {
		log.Printf("Configuration reloaded")
		// Update global config - server will pick up changes via config.Get()
		config.Set(newCfg)
	})
	configWatcher.Start()
	defer configWatcher.Stop()

	// Channel to listen for errors
	errChan := make(chan error, 1)

	// Start server(s) in goroutine(s)
	go func() {
		printStartup(cfg, binaryName)
		if srv != nil {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errChan <- err
			}
		}
	}()
	if httpsSrv != nil {
		go func() {
			if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				errChan <- err
			}
		}()
	}

	// Start Tor hidden service, per AI.md PART 31. Tor is always optional -
	// a missing binary or any startup failure logs a warning and the server
	// continues running without it; nothing here may block or fail
	// server startup.
	var torMgr *tor.Manager
	if listenPort := httpPort; listenPort != "" || httpsPort != "" {
		if listenPort == "" {
			listenPort = httpsPort
		}
		if serverPort, err := strconv.Atoi(listenPort); err == nil {
			torCfg := tor.Config{
				Binary:                    cfg.Server.Tor.Binary,
				UseNetwork:                cfg.Server.Tor.UseNetwork,
				MaxCircuits:               cfg.Server.Tor.MaxCircuits,
				CircuitTimeout:            cfg.Server.Tor.CircuitTimeout,
				BootstrapTimeout:          cfg.Server.Tor.BootstrapTimeout,
				SafeLogging:               cfg.Server.Tor.SafeLogging,
				MaxStreamsPerCircuit:      cfg.Server.Tor.MaxStreamsPerCircuit,
				CloseCircuitOnStreamLimit: cfg.Server.Tor.CloseCircuitOnStreamLimit,
				BandwidthRate:             cfg.Server.Tor.BandwidthRate,
				BandwidthBurst:            cfg.Server.Tor.BandwidthBurst,
				MaxMonthlyBandwidth:       cfg.Server.Tor.MaxMonthlyBandwidth,
				NumIntroPoints:            cfg.Server.Tor.NumIntroPoints,
				VirtualPort:               cfg.Server.Tor.VirtualPort,
			}
			torMgr = tor.NewManager(serverPort, torCfg, paths.ConfigDir(), paths.DataDir())
			tor.Set(torMgr)

			go func() {
				// Confirm the HTTP(S) listener is actually accepting
				// connections before starting Tor, since the hidden
				// service forwards to it - bounded retry, never blocks
				// server startup.
				deadline := time.Now().Add(10 * time.Second)
				for time.Now().Before(deadline) {
					conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), 500*time.Millisecond)
					if dialErr == nil {
						conn.Close()
						break
					}
					time.Sleep(250 * time.Millisecond)
				}

				if err := torMgr.Start(context.Background()); err != nil {
					if errors.Is(err, tor.ErrBinaryNotFound) {
						log.Println("Tor binary not found, hidden service disabled")
					} else {
						log.Printf("Warning: Tor disabled - %v", err)
					}
				}
			}()
		}
	}

	// Wait for interrupt signal or error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Handle signals
	for {
		select {
		case sig := <-quit:
			if sig == syscall.SIGHUP {
				log.Printf("SIGHUP received, reloading configuration...")
				if err := config.Reload(); err != nil {
					log.Printf("Failed to reload config: %v", err)
				} else {
					log.Printf("Configuration reloaded")
				}
				continue
			}
			cprintln("\n🛑 Shutting down gracefully...")
		case err := <-errChan:
			log.Printf("Server error: %v", err)
		}
		break
	}

	// Stop Tor first - server owns the Tor process lifecycle.
	if torMgr != nil {
		if err := torMgr.Close(); err != nil {
			log.Printf("Tor: error during shutdown: %v", err)
		}
	}

	// Graceful shutdown with 30 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server forced to shutdown: %v", err)
		}
	}
	if httpsSrv != nil {
		if err := httpsSrv.Shutdown(ctx); err != nil {
			log.Printf("HTTPS server forced to shutdown: %v", err)
		}
	}

	cprintln("✅ Server stopped")
}

// initSMTP builds the process-wide email.Client from
// cfg.Server.Notifications.Email, auto-detecting a local relay when no host
// is configured (persisting the result to server.yml so restarts don't
// re-probe) or connection-testing an explicitly configured host every
// startup. Email features are fully disabled - not queued, not retried -
// when no working SMTP server is found; see AI.md PART 17 "SMTP
// Requirement".
func initSMTP(cfg *config.Config) {
	smtp := cfg.Server.Notifications.Email.SMTP
	found := smtp.Host != ""

	if !found {
		host, port, ok := email.AutoDetectSMTP()
		if ok {
			cfg.Server.Notifications.Email.SMTP.Host = host
			cfg.Server.Notifications.Email.SMTP.Port = port
			if err := config.Save(cfg); err != nil {
				log.Printf("Email: failed to persist auto-detected SMTP host: %v", err)
			}
			smtp = cfg.Server.Notifications.Email.SMTP
			found = true
		}
	} else {
		if err := email.TestConnection(smtp.Host, smtp.Port); err != nil {
			log.Printf("Email: configured SMTP host %s:%d unreachable: %v (email features disabled, will retry next startup)", smtp.Host, smtp.Port, err)
			found = false
		}
	}

	client := email.NewClient(email.ClientConfigFromConfig(
		cfg.Server.Notifications.Email,
		cfg.Server.Branding.Title,
		cfg.Server.FQDN,
		found,
	))
	email.Set(client)
	initNotifier(cfg, client)

	if found {
		log.Printf("Email: enabled via %s:%d", smtp.Host, smtp.Port)
	} else {
		log.Println("Email: disabled (no SMTP server detected/reachable)")
	}
}

// initCLINotifier registers the operator notifier for a one-shot CLI command
// so AI.md PART 17 events raised outside the running server can still reach
// the operator by email. Unlike initSMTP it never probes for a mail server: a
// CLI invocation must not stall on network auto-detection, so delivery is only
// enabled when an SMTP host is already configured in server.yml (or supplied
// through the SMTP_* environment overrides config.Load applies).
func initCLINotifier() {
	cfg, err := config.Load()
	if err != nil {
		return
	}

	client := email.NewClient(email.ClientConfigFromConfig(
		cfg.Server.Notifications.Email,
		cfg.Server.Branding.Title,
		cfg.Server.FQDN,
		cfg.Server.Notifications.Email.SMTP.Host != "",
	))
	email.Set(client)
	initNotifier(cfg, client)
}

// initNotifier registers the process-wide operator notifier used by every
// AI.md PART 17 event (backups, SSL, scheduler failures, updates, security
// alerts). Recipients come from server.contact.admin, which PART 12 makes the
// universal fallback for operator mail; template overrides are read from
// {config_dir}/template/email/. Registering the notifier even when SMTP is
// unavailable is deliberate: the notifier still logs every event, and email
// delivery stays disabled through the client's own enabled flag.
func initNotifier(cfg *config.Config, client *email.Client) {
	httpPort, httpsPort := resolvePorts(cfg)
	appURL := formatURL(cfg.Server.FQDN, httpPort, false)
	if httpPort == "" {
		appURL = formatURL(cfg.Server.FQDN, httpsPort, true)
	}

	vars := email.GlobalVars{
		AppName:             cfg.Server.Branding.Title,
		AppURL:              appURL,
		FQDN:                cfg.Server.FQDN,
		NotificationReplyTo: cfg.Server.Notifications.Email.ReplyTo,
	}
	if manager := tor.Get(); manager != nil {
		if onion := manager.OnionAddress(); onion != "" {
			vars.OnionAddress = onion
			vars.OnionURL = "http://" + onion
		}
	}

	var recipients []string
	if admin := cfg.Server.Contact.ResolveAdmin(); admin.Email != "" {
		recipients = []string{admin.Email}
	} else {
		log.Println("Email: server.contact.admin.email is empty, operator notifications will be logged only")
	}

	notifier := email.NewNotifier(client, paths.ConfigDir(), vars, email.EventTogglesFromConfig(cfg.Server.Notifications.Email.Events), recipients)
	email.SetDefaultNotifier(notifier)
}

func printHelp(binaryName string) {
	cprintf(`%s - Universal API Toolkit

Usage: %s [options]

Server Options:
  --help, -h              Show this help message
  --version, -v           Show version information
  --status                Check service status
  --mode MODE             Set application mode {production|development|debug}
  --config DIR            Set configuration directory
  --data DIR              Set data directory
  --log DIR               Set log directory
  --cache DIR             Set cache directory
  --backup DIR            Set backup directory
  --pid FILE              Set PID file path
  --address ADDR          Set listen address (default: 0.0.0.0)
  --port PORT             Set listen port (default: 64580)
  --baseurl URL           Set the public base URL
  --daemon                Daemonize (detach from terminal)
  --debug                 Enable debug mode (verbose logging, debug endpoints)
  --color MODE            Set color output (auto|yes|no)
  --lang CODE             Set interface language
  --shell ACTION [SHELL]  Shell integration (completions|init|help)

Service Management:
  --service start         Start the service
  --service stop          Stop the service
  --service restart       Restart the service
  --service reload        Reload configuration
  --service --install     Install as system service
  --service --uninstall   Uninstall system service
  --service --disable     Disable system service
  --service --help        Show service command help

Maintenance Commands:
  --maintenance backup [path]    Create backup
  --maintenance restore [path]   Restore from backup
  --maintenance update [setting] Update configuration
  --maintenance mode [mode]      Change application mode
  --maintenance setup            Run first-time setup
  --maintenance secret rotate <name>
                                 Rotate installation_secret or encryption_key

Update Commands:
  --update check                 Check for available updates
  --update yes                   Download and install updates
  --update branch stable         Switch to stable channel
  --update branch beta           Switch to beta channel
  --update branch daily          Switch to daily channel

Tor Commands:
  --tor status                   Show Tor hidden service status
  --tor validate                 Validate Tor configuration
  --tor restart                  Restart the Tor process
  --tor regenerate               Regenerate the .onion address
  --tor vanity start <prefix>    Start a vanity address search
  --tor vanity apply             Apply the last found vanity address
  --tor import-keys <path>       Import an existing hidden service key

Environment Variables:
  MODE                    Application mode
  CONFIG_DIR              Configuration directory
  DATA_DIR                Data directory
  CACHE_DIR               Cache directory
  LOG_DIR                 Log directory
  BACKUP_DIR              Backup directory
  PID_FILE                PID file path
  PORT                    Listen port
  LISTEN                  Listen address
  DEBUG                   Enable debug mode

Signals:
  SIGHUP                  Reload configuration (auto via file watcher)
  SIGTERM/SIGINT          Graceful shutdown
  SIGUSR1                 Reopen logs (for log rotation)
  SIGUSR2                 Dump status to log

Documentation: https://apimgr-api.readthedocs.io
`, binaryName, binaryName)
}

// resolvePorts implements the AI.md PART 15 Port Configuration table: a
// single port of 443 is HTTPS-only, ssl.enabled forces HTTPS on any other
// single port, and "port1,port2" runs HTTP on the first and HTTPS on the
// second. Returns the HTTP port and HTTPS port to bind, either of which
// may be empty.
func resolvePorts(cfg *config.Config) (httpPort, httpsPort string) {
	ports := strings.Split(cfg.Server.Port, ",")
	for i := range ports {
		ports[i] = strings.TrimSpace(ports[i])
	}
	switch {
	case len(ports) == 2:
		return ports[0], ports[1]
	case ports[0] == "443" || cfg.Server.SSL.Enabled:
		return "", ports[0]
	default:
		return ports[0], ""
	}
}

// formatURL applies the AI.md PART 15 URL Format Rule: :80 and :443 are
// always stripped from the displayed URL.
func formatURL(host, port string, isHTTPS bool) string {
	proto := "http"
	if isHTTPS || port == "443" {
		proto = "https"
	}
	if port == "" || port == "80" || port == "443" {
		return fmt.Sprintf("%s://%s", proto, host)
	}
	return fmt.Sprintf("%s://%s:%s", proto, host, port)
}

func printStartup(cfg *config.Config, binaryName string) {
	httpPort, httpsPort := resolvePorts(cfg)
	host := getDisplayAddress(cfg)

	cprintln()
	cprintf("✅ %s v%s started successfully\n", binaryName, Version)
	if httpPort != "" {
		cprintf("📡 Listening on %s\n", formatURL(host, httpPort, false))
		cprintf("📊 Swagger UI: %s/openapi\n", formatURL(host, httpPort, false))
		cprintf("🔮 GraphQL: %s/graphql\n", formatURL(host, httpPort, false))
		cprintf("📚 API Docs: %s/api\n", formatURL(host, httpPort, false))
	}
	if httpsPort != "" {
		cprintf("🔐 Listening on %s\n", formatURL(host, httpsPort, true))
		cprintf("📊 Swagger UI: %s/openapi\n", formatURL(host, httpsPort, true))
		cprintf("🔮 GraphQL: %s/graphql\n", formatURL(host, httpsPort, true))
		cprintf("📚 API Docs: %s/api\n", formatURL(host, httpsPort, true))
	}
	cprintln()
}

func getDisplayAddress(cfg *config.Config) string {
	if cfg.Server.FQDN != "" {
		return cfg.Server.FQDN
	}
	if cfg.Server.Address == "0.0.0.0" || cfg.Server.Address == "" {
		hostname, err := os.Hostname()
		if err == nil && hostname != "" {
			return hostname
		}
		return "localhost"
	}
	return cfg.Server.Address
}

func checkStatus() {
	// Try to connect to the server
	cfg, err := config.Load()
	if err != nil {
		cprintf("❌ Failed to load config: %v\n", err)
		os.Exit(2)
	}
	addr := fmt.Sprintf("http://localhost:%s/healthz", cfg.Server.Port)

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(addr)
	if err != nil {
		cprintln("❌ Service is not running")
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		cprintln("✅ Service is running")
		cprintf("   Port: %s\n", cfg.Server.Port)
		cprintf("   Config: %s\n", config.GetConfigPath())
		os.Exit(0)
	}

	cprintln("⚠️ Service returned unexpected status")
	os.Exit(2)
}

// Service management commands
func handleServiceCommand(cmd string, binaryName string) {
	switch strings.ToLower(cmd) {
	case "start":
		startService(binaryName)
	case "stop":
		stopService(binaryName)
	case "restart":
		restartService(binaryName)
	case "reload":
		reloadService(binaryName)
	case "--install", "install":
		installService(binaryName)
	case "--uninstall", "uninstall":
		uninstallService(binaryName)
	case "--disable", "disable":
		disableService(binaryName)
	case "--help", "help":
		printServiceHelp(binaryName)
	default:
		cprintf("Unknown service command: %s\n", cmd)
		cprintln("Valid commands: start, stop, restart, reload, --install, --uninstall, --disable, --help")
		os.Exit(1)
	}
}

func installService(binaryName string) {
	if err := sysservice.Install(); err != nil {
		cprintf("❌ Failed to install service: %v\n", err)
		os.Exit(1)
	}
	cprintln("✅ Service installed successfully")
	cprintf("   Run '%s --service start' to start the service\n", binaryName)
}

func uninstallService(binaryName string) {
	// Uninstall deletes all data, configs and the system user, so the
	// interactive [y/N] confirmation is never skipped (AI.md PART 23).
	if err := sysservice.Uninstall(false); err != nil {
		cprintf("❌ Failed to uninstall service: %v\n", err)
		os.Exit(1)
	}
	cprintln("✅ Service uninstalled successfully")
}

func startService(binaryName string) {
	if err := sysservice.Start(); err != nil {
		cprintf("❌ Failed to start service: %v\n", err)
		os.Exit(1)
	}
	cprintln("✅ Service started")
}

func stopService(binaryName string) {
	if err := sysservice.Stop(); err != nil {
		cprintf("❌ Failed to stop service: %v\n", err)
		os.Exit(1)
	}
	cprintln("✅ Service stopped")
}

func restartService(binaryName string) {
	if err := sysservice.Restart(); err != nil {
		cprintf("❌ Failed to restart service: %v\n", err)
		os.Exit(1)
	}
	cprintln("✅ Service restarted")
}

func reloadService(binaryName string) {
	if err := sysservice.Reload(); err != nil {
		cprintf("❌ Failed to reload service: %v\n", err)
		os.Exit(1)
	}
	cprintln("✅ Configuration reloaded")
}

func disableService(binaryName string) {
	if err := sysservice.Disable(); err != nil {
		cprintf("❌ Failed to disable service: %v\n", err)
		os.Exit(1)
	}
	cprintln("✅ Service disabled (will not start on boot)")
}

// printServiceHelp renders the PART 24 `--service --help` surface, including
// the live status block, from the sysservice package so the command list and
// the detected state never drift apart.
func printServiceHelp(binaryName string) {
	cprintf("%s", sysservice.HelpText(binaryName))
	cprintln("\nNote: Service commands require root/administrator privileges.")
}

// Maintenance commands
func handleMaintenanceCommand(cmd string, optionalArg string, args []string, binaryName string) {
	switch strings.ToLower(cmd) {
	case "backup":
		backupPath := optionalArg
		if backupPath == "" {
			backupPath = filepath.Join(paths.DataDir(), "backup", fmt.Sprintf("backup-%s.json", time.Now().Format("20060102-150405")))
		}
		handleBackup(backupPath, binaryName)

	case "restore":
		if optionalArg == "" {
			cprintln("❌ Restore requires a backup file path")
			cprintf("   Usage: %s --maintenance restore /path/to/backup.json\n", binaryName)
			os.Exit(1)
		}
		handleRestore(optionalArg, binaryName)

	case "update":
		if optionalArg == "" {
			cprintln("❌ Update requires a setting name and value")
			cprintf("   Usage: %s --maintenance update setting_name value\n", binaryName)
			os.Exit(1)
		}
		cprintf("⚠️ Configuration update via CLI not yet implemented\n")
		cprintf("   Edit server.yml directly, or use the API/CLI client to update settings\n")

	case "mode":
		if optionalArg == "" {
			cprintln("❌ Mode change requires a mode value")
			cprintf("   Usage: %s --maintenance mode {production|development|debug}\n", binaryName)
			os.Exit(1)
		}
		handleModeChange(optionalArg, binaryName)

	case "setup":
		cprintf("⚠️ Setup wizard is available via the CLI client\n")
		cprintf("   Run: %s-cli setup\n", strings.TrimSuffix(binaryName, "-cli"))

	case "secret":
		handleSecretCommand(args, binaryName)

	default:
		cprintf("Unknown maintenance command: %s\n", cmd)
		cprintln("Valid commands: backup, restore, update, mode, setup, secret")
		os.Exit(1)
	}
}

// checkForUpdate queries GitHub Releases for the newest release on the
// configured channel. Per AI.md PART 22 "Defer Semantics", this manual path
// deliberately calls CheckForUpdate (not CheckEligible) so an explicit
// operator action always sees the true latest release, bypassing defer_days.
// A nil result means the running version is already current.
func checkForUpdate() *update.Release {
	branch := update.BranchStable
	if cfg, err := config.Load(); err == nil && update.ValidBranch(cfg.Server.Update.Branch) {
		branch = cfg.Server.Update.Branch
	}

	cprintln("🔍 Checking for updates...")
	cprintf("   Current version: %s (%s channel)\n", Version, branch)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release, err := update.CheckForUpdate(ctx, Version, branch, buildEpoch())
	if err != nil {
		cprintf("❌ Update check failed: %v\n", err)
		os.Exit(1)
	}
	return release
}

// Update handling
func handleUpdateCommand(cmd string, optionalArg string, binaryName string) {
	switch strings.ToLower(cmd) {
	case "check":
		release := checkForUpdate()
		if release == nil {
			cprintf("✅ %s %s is up to date\n", binaryName, Version)
			return
		}
		cprintf("⬆️ Update available: %s (published %s)\n", release.TagName, release.PublishedAt.UTC().Format("2006-01-02"))
		cprintf("   Run '%s --update yes' to install it\n", binaryName)

	case "yes":
		release := checkForUpdate()
		if release == nil {
			cprintf("✅ %s %s is up to date\n", binaryName, Version)
			return
		}
		cprintf("⬇️ Installing %s...\n", release.TagName)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := update.Install(ctx, release); err != nil {
			cprintf("❌ Update failed: %v\n", err)
			os.Exit(1)
		}
		cprintf("✅ Updated to %s\n", release.TagName)
		// AI.md PART 17 requires an update_installed operator event for every
		// completed install, not just the scheduled one. The structured log
		// line is unconditional; the email only goes out when SMTP and the
		// per-event toggle allow it.
		log.Printf("Update: [INFO] update_installed previous_version=%s new_version=%s", Version, release.TagName)
		initCLINotifier()
		email.OperatorUpdateInstalled(Version, release.TagName)
		if err := update.RestartService(); err == nil {
			return
		}
		// No managed service to restart: re-exec the freshly written binary
		// in place so the running process picks up the new version.
		if err := update.Restart(); err != nil {
			cprintf("⚠️ Update installed but restart failed: %v\n", err)
			cprintln("   Restart the process manually to run the new version")
		}

	case "branch":
		if optionalArg == "" {
			cprintln("❌ Branch command requires a channel argument")
			cprintf("   Usage: %s --update branch {stable|beta|daily}\n", binaryName)
			os.Exit(1)
		}
		if !update.ValidBranch(optionalArg) {
			cprintf("❌ Unknown update channel: %s\n", optionalArg)
			cprintln("   Valid channels: stable, beta, daily")
			os.Exit(1)
		}
		{
			cfg, err := config.Load()
			if err != nil {
				cprintf("❌ Failed to load config: %v\n", err)
				os.Exit(1)
			}
			if err := update.SetBranch(&cfg.Server.Update, optionalArg); err != nil {
				cprintf("❌ %v\n", err)
				os.Exit(1)
			}
			if err := config.Save(cfg); err != nil {
				cprintf("❌ Failed to save config: %v\n", err)
				os.Exit(1)
			}
			cprintf("✅ Update channel set to: %s\n", optionalArg)
			cprintln("   This setting will be used for future update checks")
		}

	default:
		cprintf("Unknown update command: %s\n", cmd)
		cprintf("Usage: %s --update {check|yes|branch <channel>}\n", binaryName)
		os.Exit(1)
	}
}

// Tor management commands. Per AI.md PART 31.1, the server binary already
// owns the embedded Tor process, so this subcommand cannot touch Tor
// directly - it reaches the running server's internal, loopback-only
// /server/tor/* control channel over HTTP, resolving the server's port the
// same way --status does (config.Load + cfg.Server.Port on localhost).
func handleTorCommand(cmd string, args []string, binaryName string) {
	switch strings.ToLower(cmd) {
	case "status":
		torStatus(binaryName)
	case "validate":
		torValidate(binaryName)
	case "restart":
		torRestart(binaryName)
	case "regenerate":
		torRegenerate(binaryName)
	case "vanity":
		torVanity(args, binaryName)
	case "import-keys":
		if len(args) == 0 {
			cprintln("❌ import-keys requires a key file path")
			cprintf("   Usage: %s --tor import-keys <path>\n", binaryName)
			os.Exit(1)
		}
		torImportKeys(args[0], binaryName)
	case "--help", "help":
		printTorHelp(binaryName)
	default:
		cprintf("Unknown tor command: %s\n", cmd)
		cprintln("Valid commands: status, validate, restart, regenerate, vanity, import-keys, --help")
		os.Exit(1)
	}
}

// torServerAddr resolves the running server's loopback base URL using the
// same config-load + cfg.Server.Port mechanism checkStatus() uses.
func torServerAddr() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	return fmt.Sprintf("http://localhost:%s", cfg.Server.Port), nil
}

// torRequest issues a request to the running server's internal /server/tor/*
// control channel and decodes the {"ok":...} envelope. A nil error with a
// non-nil returned bool false means the server is unreachable (no running
// server); other errors mean the server responded with a failure envelope.
func torRequest(method, path string, body interface{}) (map[string]interface{}, bool, error) {
	base, err := torServerAddr()
	if err != nil {
		return nil, false, err
	}

	var reqBody *strings.Reader
	if body != nil {
		encoded, mErr := json.Marshal(body)
		if mErr != nil {
			return nil, true, fmt.Errorf("failed to encode request: %w", mErr)
		}
		reqBody = strings.NewReader(string(encoded))
	} else {
		reqBody = strings.NewReader("")
	}

	req, err := http.NewRequest(method, base+path, reqBody)
	if err != nil {
		return nil, true, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, nil
	}
	defer resp.Body.Close()

	var envelope map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, true, fmt.Errorf("failed to decode server response: %w", err)
	}

	ok, _ := envelope["ok"].(bool)
	if !ok {
		msg, _ := envelope["message"].(string)
		code, _ := envelope["error"].(string)
		return envelope, true, fmt.Errorf("%s: %s", code, msg)
	}

	data, _ := envelope["data"].(map[string]interface{})
	return data, true, nil
}

// requireRunningServer exits 1 with the AI.md-specified error message when
// a mutating tor subcommand cannot reach the running server.
func requireRunningServer(serverSeen bool) {
	if !serverSeen {
		cprintln("Error: no running server detected — start the server first")
		os.Exit(1)
	}
}

func torStatus(binaryName string) {
	data, seen, err := torRequest(http.MethodGet, "/server/tor/status", nil)
	if seen {
		if err != nil {
			cprintf("❌ Failed to get Tor status: %v\n", err)
			os.Exit(1)
		}
		running, _ := data["running"].(bool)
		address, _ := data["address"].(string)
		if running {
			cprintln("✅ Tor is running")
			cprintf("   Address: %s\n", address)
		} else {
			cprintln("⚠️ Tor is not running")
		}
		return
	}

	// No running server - fall back to on-disk state (read-only), per
	// AI.md PART 31.1.
	hostnamePath := filepath.Join(paths.DataDir(), "tor", "site", "hostname")
	hostBytes, err := os.ReadFile(hostnamePath)
	if err != nil {
		cprintln("⚠️ No running server detected and no saved Tor address found")
		cprintf("   Checked: %s\n", hostnamePath)
		os.Exit(1)
	}
	cprintln("⚠️ No running server detected - showing saved on-disk state")
	cprintf("   Address: %s\n", strings.TrimSpace(string(hostBytes)))
}

func torValidate(binaryName string) {
	data, seen, err := torRequest(http.MethodPost, "/server/tor/validate", nil)
	if seen {
		if err != nil {
			cprintf("❌ Failed to validate Tor configuration: %v\n", err)
			os.Exit(1)
		}
		valid, _ := data["valid"].(bool)
		if valid {
			cprintln("✅ Tor configuration is valid")
			return
		}
		cprintln("❌ Tor configuration has issues:")
		if issues, ok := data["issues"].([]interface{}); ok {
			for _, issue := range issues {
				cprintf("   - %v\n", issue)
			}
		}
		os.Exit(1)
	}

	// No running server - fall back to checking the torrc exists on disk.
	torrcPath := filepath.Join(paths.ConfigDir(), "tor", "torrc")
	if _, err := os.Stat(torrcPath); err != nil {
		cprintln("⚠️ No running server detected and no torrc found")
		cprintf("   Checked: %s\n", torrcPath)
		os.Exit(1)
	}
	cprintln("⚠️ No running server detected - torrc exists on disk")
	cprintf("   Path: %s\n", torrcPath)
}

func torRestart(binaryName string) {
	data, seen, err := torRequest(http.MethodPost, "/server/tor/restart", nil)
	requireRunningServer(seen)
	if err != nil {
		cprintf("❌ Failed to restart Tor: %v\n", err)
		os.Exit(1)
	}
	address, _ := data["address"].(string)
	cprintln("✅ Tor restarted")
	cprintf("   Address: %s\n", address)
}

func torRegenerate(binaryName string) {
	data, seen, err := torRequest(http.MethodPost, "/server/tor/regenerate", nil)
	requireRunningServer(seen)
	if err != nil {
		cprintf("❌ Failed to regenerate Tor address: %v\n", err)
		os.Exit(1)
	}
	address, _ := data["address"].(string)
	cprintln("✅ New .onion address generated")
	cprintf("   Address: %s\n", address)
}

func torVanity(args []string, binaryName string) {
	if len(args) == 0 {
		cprintln("❌ vanity requires a subcommand: start, apply")
		cprintf("   Usage: %s --tor vanity {start <prefix>|apply}\n", binaryName)
		os.Exit(1)
	}

	switch strings.ToLower(args[0]) {
	case "start":
		if len(args) < 2 {
			cprintln("❌ vanity start requires a prefix")
			cprintf("   Usage: %s --tor vanity start <prefix>\n", binaryName)
			os.Exit(1)
		}
		prefix := args[1]
		cprintf("🔍 Searching for a vanity address starting with %q (this may take a while)...\n", prefix)
		data, seen, err := torRequest(http.MethodPost, "/server/tor/vanity/start", map[string]interface{}{
			"prefix": prefix,
		})
		requireRunningServer(seen)
		if err != nil {
			cprintf("❌ Vanity search failed: %v\n", err)
			os.Exit(1)
		}
		address, _ := data["address"].(string)
		attempts, _ := data["attempts"].(float64)
		cprintln("✅ Vanity address found")
		cprintf("   Address: %s\n", address)
		cprintf("   Attempts: %.0f\n", attempts)
		cprintf("   Run '%s --tor vanity apply' to apply it\n", binaryName)

	case "apply":
		data, seen, err := torRequest(http.MethodPost, "/server/tor/vanity/apply", nil)
		requireRunningServer(seen)
		if err != nil {
			cprintf("❌ Failed to apply vanity address: %v\n", err)
			os.Exit(1)
		}
		address, _ := data["address"].(string)
		cprintln("✅ Vanity address applied")
		cprintf("   Address: %s\n", address)

	default:
		cprintf("Unknown vanity command: %s\n", args[0])
		cprintln("Valid commands: start, apply")
		os.Exit(1)
	}
}

func torImportKeys(path string, binaryName string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		cprintf("❌ Failed to read key file: %v\n", err)
		os.Exit(1)
	}

	data, seen, err := torRequest(http.MethodPost, "/server/tor/import-keys", map[string]interface{}{
		"key_blob": strings.TrimSpace(string(raw)),
	})
	requireRunningServer(seen)
	if err != nil {
		cprintf("❌ Failed to import Tor keys: %v\n", err)
		os.Exit(1)
	}
	address, _ := data["address"].(string)
	cprintln("✅ Tor keys imported")
	cprintf("   Address: %s\n", address)
}

func printTorHelp(binaryName string) {
	cprintf(`Tor Management

Available tor commands:
  %s --tor status                 Show Tor hidden service status
  %s --tor validate               Validate Tor configuration
  %s --tor restart                Restart the Tor process
  %s --tor regenerate             Regenerate the .onion address
  %s --tor vanity start <prefix>  Start a vanity address search
  %s --tor vanity apply           Apply the last found vanity address
  %s --tor import-keys <path>     Import an existing hidden service key
  %s --tor --help                 Show this help

Note: restart, regenerate, vanity, and import-keys require the server to
be running - they mutate a process the server owns.
`, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName)
}

// Mode change handling
func handleModeChange(newMode string, binaryName string) {
	// ParseMode accepts the canonical names plus the prod/dev/devel
	// shortcuts and normalizes them; "debug" is a first-class mode
	parsed, err := appmode.ParseMode(newMode)
	if err != nil {
		cprintf("❌ Unknown mode: %s\n", newMode)
		cprintln("   Valid modes: production, development, debug (shortcuts: prod, dev, devel)")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		cprintf("❌ Failed to load config: %v\n", err)
		os.Exit(1)
	}

	cfg.Server.Mode = parsed.String()
	if err := config.Save(cfg); err != nil {
		cprintf("❌ Failed to save config: %v\n", err)
		os.Exit(1)
	}

	cprintf("✅ Mode will be set to: %s\n", parsed.String())
	cprintln("   Mode change requires server restart to take effect")
}

// Backup handling
func handleBackup(backupPath string, binaryName string) {
	cprintf("📦 Creating backup to: %s\n", backupPath)

	// Create backup directory
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		cprintf("❌ Failed to create backup directory: %v\n", err)
		os.Exit(1)
	}

	// Collect files to backup
	backupData := map[string]interface{}{
		"version":    Version,
		"created_at": time.Now().Format(time.RFC3339),
		"config":     nil,
		"data_dir":   paths.DataDir(),
	}

	// Read current config
	if cfg, err := config.Load(); err == nil {
		backupData["config"] = cfg
	}

	// Write backup file
	data, err := json.MarshalIndent(backupData, "", "  ")
	if err != nil {
		cprintf("❌ Failed to create backup data: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		cprintf("❌ Failed to write backup file: %v\n", err)
		os.Exit(1)
	}

	cprintln("✅ Backup created successfully")
	cprintf("   Config: %s\n", config.GetConfigPath())
	cprintf("   Data: %s\n", paths.DataDir())
}

// Restore handling
func handleRestore(restorePath string, binaryName string) {
	cprintf("📥 Restoring from: %s\n", restorePath)

	// Read backup file
	data, err := os.ReadFile(restorePath)
	if err != nil {
		cprintf("❌ Failed to read backup file: %v\n", err)
		os.Exit(1)
	}

	var backupData map[string]interface{}
	if err := json.Unmarshal(data, &backupData); err != nil {
		cprintf("❌ Invalid backup file format: %v\n", err)
		os.Exit(1)
	}

	// Validate backup
	if _, ok := backupData["version"]; !ok {
		cprintln("❌ Invalid backup file: missing version")
		os.Exit(1)
	}

	cprintf("   Backup version: %v\n", backupData["version"])
	cprintf("   Created: %v\n", backupData["created_at"])

	// Restore config if present
	if cfgData, ok := backupData["config"]; ok && cfgData != nil {
		cfgBytes, _ := json.Marshal(cfgData)
		var cfg config.Config
		if err := json.Unmarshal(cfgBytes, &cfg); err == nil {
			if err := config.Save(&cfg); err != nil {
				cprintf("⚠️ Failed to restore config: %v\n", err)
			} else {
				cprintln("✅ Configuration restored")
			}
		}
	}

	cprintln("✅ Restore completed")
	cprintln("   Restart the service to apply changes")
}
