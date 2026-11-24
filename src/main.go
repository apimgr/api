package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/apimgr/api/src/common/banner"
	"github.com/apimgr/api/src/common/display"
	"github.com/apimgr/api/src/common/i18n"
	commonversion "github.com/apimgr/api/src/common/version"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/email"
	"github.com/apimgr/api/src/geoip"
	"github.com/apimgr/api/src/i2p"
	"github.com/apimgr/api/src/metrics"
	appmode "github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/pgp"
	"github.com/apimgr/api/src/pidfile"
	"github.com/apimgr/api/src/scheduler"
	"github.com/apimgr/api/src/server"
	"github.com/apimgr/api/src/server/handler"
	"github.com/apimgr/api/src/ssl"
	"github.com/apimgr/api/src/sysservice"
	"github.com/apimgr/api/src/tor"
	"github.com/apimgr/api/src/update"
	"github.com/pires/go-proxyproto"
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
	BuildDate = (commonversion.BuildInfo{Version: Version, CommitID: CommitID, BuildEpoch: buildEpoch()}).BuildDate()
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
	appmode.SetLang(earlyLanguage(os.Args[1:]))

	// CLI flags (only -h and -v have short forms per spec)
	showHelp := flag.Bool("help", false, i18n.Translate(appmode.GetLang(), "cli.show_help"))
	flag.BoolVar(showHelp, "h", false, i18n.Translate(appmode.GetLang(), "cli.show_help"))
	showVersion := flag.Bool("version", false, i18n.Translate(appmode.GetLang(), "cli.show_version"))
	flag.BoolVar(showVersion, "v", false, i18n.Translate(appmode.GetLang(), "cli.show_version"))

	// Server configuration
	mode := flag.String("mode", "", i18n.Translate(appmode.GetLang(), "cli.flag_mode"))
	configDir := flag.String("config", "", i18n.Translate(appmode.GetLang(), "cli.flag_config"))
	dataDir := flag.String("data", "", i18n.Translate(appmode.GetLang(), "cli.flag_data"))
	logDir := flag.String("log", "", i18n.Translate(appmode.GetLang(), "cli.flag_log"))
	cacheDir := flag.String("cache", "", i18n.Translate(appmode.GetLang(), "cli.flag_cache"))
	backupDir := flag.String("backup", "", i18n.Translate(appmode.GetLang(), "cli.flag_backup"))
	pidFile := flag.String("pid", "", i18n.Translate(appmode.GetLang(), "cli.flag_pid"))
	address := flag.String("address", "", i18n.Translate(appmode.GetLang(), "cli.flag_address"))
	port := flag.String("port", "", i18n.Translate(appmode.GetLang(), "cli.flag_port"))
	baseURL := flag.String("baseurl", "", i18n.Translate(appmode.GetLang(), "cli.flag_baseurl"))
	i2pFlag := flag.Bool("i2p", false, i18n.Translate(appmode.GetLang(), "cli.flag_i2p"))
	daemon := flag.Bool("daemon", false, i18n.Translate(appmode.GetLang(), "cli.flag_daemon"))
	debug := flag.Bool("debug", false, i18n.Translate(appmode.GetLang(), "cli.flag_debug"))
	colorFlag := flag.String("color", "auto", i18n.Translate(appmode.GetLang(), "cli.flag_color"))
	languageOptions := make([]string, 0)
	for _, language := range i18n.AvailableLanguages() {
		languageOptions = append(languageOptions, fmt.Sprintf("%s (%s)", language.Code, language.NativeName))
	}
	langFlag := flag.String("lang", "", i18n.Translate(appmode.GetLang(), "cli.flag_lang")+": "+strings.Join(languageOptions, ", "))
	shellCmd := flag.String("shell", "", i18n.Translate(appmode.GetLang(), "cli.flag_shell"))

	// Status check
	showStatus := flag.Bool("status", false, i18n.Translate(appmode.GetLang(), "cli.flag_status"))

	// Service management
	serviceCmd := flag.String("service", "", i18n.Translate(appmode.GetLang(), "cli.flag_service"))

	// Maintenance commands
	maintenanceCmd := flag.String("maintenance", "", i18n.Translate(appmode.GetLang(), "cli.flag_maintenance"))

	// Update command
	updateCmd := flag.String("update", "", i18n.Translate(appmode.GetLang(), "cli.flag_update"))

	// Tor management commands
	torCmd := flag.String("tor", "", i18n.Translate(appmode.GetLang(), "cli.flag_tor"))

	switch commandFlagHelp(os.Args[1:], "shell", "service", "maintenance", "update", "tor") {
	case "shell":
		printShellHelp(binaryName)
		os.Exit(0)
	case "service":
		printServiceHelp(binaryName)
		os.Exit(0)
	case "maintenance":
		printMaintenanceHelp(binaryName)
		os.Exit(0)
	case "update":
		printUpdateHelp(binaryName)
		os.Exit(0)
	case "tor":
		printTorHelp(binaryName)
		os.Exit(0)
	}

	flag.Parse()

	// Handle help
	if *showHelp {
		switch {
		case *serviceCmd != "":
			printServiceHelp(binaryName)
		case *maintenanceCmd != "":
			printMaintenanceHelp(binaryName)
		case *updateCmd != "":
			printUpdateHelp(binaryName)
		case *shellCmd != "":
			printShellHelp(binaryName)
		case *torCmd != "":
			printTorHelp(binaryName)
		default:
			printHelp(binaryName)
		}
		os.Exit(0)
	}

	// Handle version
	if *showVersion {
		fmt.Print(commonversion.FormatOutput(binaryName, Version, CommitID, BuildDate, runtime.Version(), runtime.GOOS, runtime.GOARCH))
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
	for _, directory := range []*string{
		&resolvedConfigDir, &resolvedDataDir, &resolvedLogDir,
		&resolvedCacheDir, &resolvedBackupDir,
	} {
		if *directory == "" {
			continue
		}
		safe, err := paths.SafeDirPath(*directory)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: invalid directory path: %v\n", binaryName, err)
			os.Exit(exConfig)
		}
		*directory = safe
	}
	if resolvedPIDFile != "" {
		safePID, err := paths.SafeDirPath(resolvedPIDFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: invalid PID file path: %v\n", binaryName, err)
			os.Exit(exConfig)
		}
		resolvedPIDFile = safePID
	}

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
		appmode.SetLang(normalizeLanguage(*langFlag))
	} else if envLang := firstNonEmpty(os.Getenv("LC_ALL"), os.Getenv("LANG")); envLang != "" {
		appmode.SetLang(normalizeLanguage(envLang))
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

	// Handle the positional `email test` command (AI.md PART 17). It loads
	// config and opens the database itself because it needs the SMTP settings
	// and writes an audit entry, so it runs before the server bring-up below.
	if args := flag.Args(); len(args) > 0 && strings.ToLower(args[0]) == "email" {
		handleEmailCommand(args[1:], binaryName)
		os.Exit(0)
	}

	// Root setup (AI.md PART 8 startup sequence, PART 23 system user):
	// while still elevated, create the dedicated service account with its
	// home directory and every runtime directory. Ownership is transferred
	// and privileges are dropped further down, once the first-run config
	// has been written and the listeners are bound.
	serviceUID, serviceGID, err := sysservice.RootSetup()
	if err != nil {
		log.Printf("Failed to prepare the service account and runtime directories: %v", err)
		if errors.Is(err, sysservice.ErrAccountToolsUnavailable) {
			os.Exit(exUnavailable)
		}
		os.Exit(exOSErr)
	}

	// Load configuration first so the database driver/URL/token it
	// specifies (server.cache.* sibling, server.database.*) are known
	// before opening the database connection.
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Failed to load configuration: %v", err)
		os.Exit(exConfig)
	}
	if *langFlag == "" && cfg.Server.I18N.DefaultLanguage != "" {
		appmode.SetLang(normalizeLanguage(cfg.Server.I18N.DefaultLanguage))
	}

	// A root-started restart applies a service-user staged update before
	// dropping privileges. The staged binary and its checksum are rechecked
	// here, then Restart replaces this process with the new version.
	if paths.IsElevated() && runtime.GOOS != "windows" {
		if staged, applyErr := update.ApplyStaged(paths.DataDir(), Version); staged {
			if applyErr != nil {
				log.Printf("Update: staged update could not be applied: %v", applyErr)
			} else {
				log.Printf("Update: staged update restart returned unexpectedly")
			}
		}
	}

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
	i2pFlagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "i2p" {
			i2pFlagSet = true
		}
	})
	if i2pFlagSet {
		cfg.Server.I2P.Enabled = *i2pFlag
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
	appmode.SetProfilingRates(cfg.Server.Debug.BlockProfileRate, cfg.Server.Debug.MutexProfileFraction)
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

	// Bind the listening sockets while still elevated, so privileged ports
	// (<1024) can be claimed, then drop to the dedicated service account.
	// Binding has to happen after daemonize(), which re-execs the process
	// and would otherwise lose the sockets.
	httpPort, httpsPort := resolvePorts(cfg)
	var httpLn, httpsLn net.Listener
	if httpPort != "" {
		httpLn, err = net.Listen("tcp", net.JoinHostPort(cfg.Server.Address, httpPort))
		if err != nil {
			log.Printf("Failed to bind %s:%s: %v", cfg.Server.Address, httpPort, err)
			os.Exit(exUnavailable)
		}
	}
	if httpsPort != "" {
		httpsLn, err = net.Listen("tcp", net.JoinHostPort(cfg.Server.Address, httpsPort))
		if err != nil {
			log.Printf("Failed to bind %s:%s: %v", cfg.Server.Address, httpsPort, err)
			os.Exit(exUnavailable)
		}
	}

	// Everything that needed root is done: hand the runtime directories
	// (including the config file just written on first run) to the service
	// account and permanently drop privileges. A failed drop is fatal - the
	// process must never keep serving as root (AI.md PART 23/24).
	if serviceUID > 0 {
		if err := sysservice.ClaimRuntimeDirs(serviceUID, serviceGID); err != nil {
			log.Printf("Failed to transfer runtime directories to the %s account: %v", sysservice.ServiceUserName, err)
			os.Exit(exOSErr)
		}
		if err := sysservice.DropPrivileges(serviceUID, serviceGID); err != nil {
			log.Printf("Failed to drop privileges: %v", err)
			os.Exit(exOSErr)
		}
	}

	// Initialize database
	if err := database.Init(cfg.Server.Database, paths.DataDir()); err != nil {
		log.Printf("Failed to initialize database: %v", err)
		os.Exit(exUnavailable)
	}
	defer func() { _ = database.Close() }()
	cacheReadOnlyOffline := database.CacheReadOnlyOffline()
	if cacheReadOnlyOffline {
		log.Printf("Database: remote is unavailable; serving from the fresh local cache in read-only mode")
	} else {
		if err := database.EnsureCaptchaSchema(context.Background()); err != nil {
			log.Printf("Failed to initialize CAPTCHA challenge storage: %v", err)
			os.Exit(exUnavailable)
		}
		if err := database.EnsureRateLimitSchema(context.Background()); err != nil {
			log.Printf("Failed to initialize persisted rate-limit storage: %v", err)
			os.Exit(exUnavailable)
		}
	}
	var startupPreviousVersion, startupNewVersion string
	var startupFailureVersion, startupFailureReason string
	if !cacheReadOnlyOffline {
		var startupSuccessErr error
		startupPreviousVersion, startupNewVersion, startupSuccessErr = update.TakeStartupSuccess(paths.DataDir())
		if startupSuccessErr != nil {
			log.Printf("Update: failed to read update completion marker: %v", startupSuccessErr)
		}
		if startupNewVersion != "" {
			if err := database.WriteAuditEventWithSource(context.Background(), "update.installed", "system", "", "startup", map[string]interface{}{"previous_version": startupPreviousVersion, "new_version": startupNewVersion}); err != nil {
				log.Printf("Update: failed to record update.installed audit event: %v", err)
			}
		}
		startupFailureVersion, startupFailureReason = update.TakeStartupFailure()
		if startupFailureVersion != "" {
			if err := database.SetConfigValue(context.Background(), "scheduler.update_check.last_failed_version", startupFailureVersion); err != nil {
				log.Printf("Update: failed to record failed update version: %v", err)
			}
			if err := database.WriteAuditEventWithSource(context.Background(), "update.failed", "system", "", "startup", map[string]interface{}{"version": startupFailureVersion}); err != nil {
				log.Printf("Update: failed to record update.failed audit event: %v", err)
			}
		}
	}

	// Generate the project-level cryptographic secrets on first start and
	// apply their automatic rotation schedule (AI.md PART 11 "Cryptographic
	// Keys"). A failure here is fatal: cookie signing and CSRF both depend on
	// this material.
	if err := database.EnsureAppSecrets(context.Background()); err != nil {
		log.Printf("Failed to initialize project secrets: %v", err)
		os.Exit(exUnavailable)
	}
	if !cacheReadOnlyOffline {
		if err := pgp.ResumeInstallationSecretRotation(context.Background()); err != nil {
			log.Printf("Failed to finish installation-secret rotation: %v", err)
			os.Exit(exUnavailable)
		}
		if err := pgp.RecoverRotation(context.Background(), cfg); err != nil {
			log.Printf("Failed to recover PGP key rotation: %v", err)
			os.Exit(exUnavailable)
		}
		if err := resumeEncryptionKeyRotation(context.Background(), cfg); err != nil {
			log.Printf("Failed to finish encryption-key rotation: %v", err)
			os.Exit(exUnavailable)
		}
		if err := syncDNSConfigState(context.Background(), cfg); err != nil {
			log.Printf("Failed to synchronize DNS provider state: %v", err)
			os.Exit(exUnavailable)
		}
	}
	if cfg.Server.SSL.LetsEncrypt.Challenge == "dns-01" || cfg.Server.SSL.LetsEncrypt.DNSProvider != "" {
		if err := ssl.ValidateDNSProvider(cfg.Server.SSL.LetsEncrypt.DNSProvider, cfg.Server.SSL.LetsEncrypt.DNSCredentials); err != nil {
			log.Printf("SSL: [ERROR] DNS provider validation failed: %v", err)
			os.Exit(exConfig)
		}
	}

	// Set database for health checks
	handler.SetDatabase(database.ServerDBProbe{})

	// Re-resolve --color/NO_COLOR now that config is available, applying the
	// output.color/output.emoji config-file tier per AI.md PART 8's
	// priority order (CLI flag > config file > NO_COLOR env var > auto-detect).
	applyColorMode(*colorFlag, cfg.Output.Color)
	applyEmojiOverride(cfg.Output.Emoji)

	// Initialize logging system
	if err := server.InitLogger(&cfg.Server.Logs); err != nil {
		log.Printf("Warning: Failed to initialize logging system: %v", err)
	}
	if logger := server.GetLogger(); logger != nil {
		for _, key := range cfg.GeneratedSecrets {
			logger.LogAudit("config.secret_generated", map[string]interface{}{"key": key})
		}
	}
	if startupNewVersion != "" {
		log.Printf("Update: [INFO] update_installed previous_version=%s new_version=%s", startupPreviousVersion, startupNewVersion)
		if logger := server.GetLogger(); logger != nil {
			logger.LogAudit("update.installed", map[string]interface{}{"previous_version": startupPreviousVersion, "new_version": startupNewVersion})
		}
	}
	for _, key := range cfg.GeneratedSecrets {
		if key == "server.token" {
			_, _ = fmt.Fprintf(os.Stdout, "First-start operator token (save this now): %s\n", cfg.Server.Token)
			break
		}
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

	// Initialize GeoIP from the fixed data directory and start the required
	// first-run download without delaying server startup.
	if err := geoip.Get().LoadFromConfig(cfg.Server.GeoIP, paths.DataDir()); err != nil {
		log.Printf("Warning: Failed to load GeoIP database: %v", err)
	}
	geoip.RegisterHealthChecks(paths.DataDir())
	if cfg.Server.GeoIP.Enabled && geoip.NeedsInitialDownload(paths.DataDir()) {
		go func() {
			if err := geoip.DownloadFromConfig(cfg.Server.GeoIP, paths.DataDir()); err != nil {
				log.Printf("GeoIP: first-run database download failed: %v", err)
				return
			}
			currentConfig := config.Get()
			if err := geoip.Get().LoadFromConfig(currentConfig.Server.GeoIP, paths.DataDir()); err != nil {
				log.Printf("GeoIP: failed to load first-run databases: %v", err)
			}
		}()
	}

	// Initialize outbound email (PART 17): auto-detect a local SMTP relay
	// if none is configured, or connection-test the configured host every
	// startup; never blocks startup either way.
	initSMTP(cfg)
	if startupNewVersion != "" {
		if notifier := email.DefaultNotifier(); notifier != nil {
			if nerr := notifier.NotifyUpdateInstalled(notifier.Recipients(), startupPreviousVersion, startupNewVersion); nerr != nil {
				log.Printf("Update: failed to send update_installed notification: %v", nerr)
			}
		}
	}
	if startupFailureVersion != "" {
		if notifier := email.DefaultNotifier(); notifier != nil {
			if nerr := notifier.NotifyUpdateFailed(notifier.Recipients(), startupFailureVersion, update.SanitizeUpdateFailure(errors.New(startupFailureReason))); nerr != nil {
				log.Printf("Update: failed to send update_failed notification: %v", nerr)
			}
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
	defer func() {
		if err := pidfile.RemovePIDFile(pidPath); err != nil {
			log.Printf("Failed to remove PID file: %v", err)
		}
	}()

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
		OwnerTokenCounts: func() (float64, float64, error) {
			return database.ActiveOwnerTokenCounts(context.Background(), database.GetServerDB())
		},
	})
	metrics.Get().SetBuildInfo(Version, CommitID, BuildDate)
	metrics.Get().StartCollectors()
	defer metrics.Get().StopCollectors()

	// Create server
	srv := server.New(cfg)
	i2pBackendHandler := server.I2PBackend(srv.Handler)

	// Wire SSL/TLS per AI.md PART 15 Port Configuration rules: a single
	// port of 443 is HTTPS-only, ssl.enabled forces HTTPS on any other
	// single port, and "port1,port2" runs HTTP on the first and HTTPS on
	// the second. The listeners themselves were already bound
	// above, while the process still had the privileges to claim a port
	// below 1024.
	var httpsSrv *http.Server
	var sslManager *ssl.Manager
	if httpsPort != "" {
		sslCertPath := cfg.Server.SSL.CertPath
		if sslCertPath == "" {
			sslCertPath = filepath.Join(paths.ConfigDir(), "ssl")
		}
		domain := server.GetFQDN(paths.ProjectName)
		sslMgr := ssl.NewManager(ssl.Config{
			Enabled:  true,
			CertPath: sslCertPath,
			OnDNSCertificateIssued: func(validatedAt time.Time) {
				if err := database.SetAppSecretValue(context.Background(), database.SecretDNSValidatedAt, validatedAt.UTC().Format(time.RFC3339)); err != nil {
					log.Printf("SSL: failed to record DNS provider validation time: %v", err)
				}
			},
			LetsEncrypt: ssl.LetsEncryptConfig{
				Enabled:        cfg.Server.SSL.LetsEncrypt.Enabled,
				Email:          cfg.Server.SSL.LetsEncrypt.Email,
				Challenge:      cfg.Server.SSL.LetsEncrypt.Challenge,
				DNSProvider:    cfg.Server.SSL.LetsEncrypt.DNSProvider,
				DNSCredentials: cfg.Server.SSL.LetsEncrypt.DNSCredentials,
			},
		})
		sslManager = sslMgr
		tlsConfig, err := sslMgr.GetTLSConfig([]string{domain})
		if err != nil {
			log.Printf("Failed to configure TLS: %v", err)
			os.Exit(exConfig)
		}
		httpsSrv = &http.Server{
			Addr:      fmt.Sprintf("%s:%s", cfg.Server.Address, httpsPort),
			Handler:   srv.Handler,
			TLSConfig: tlsConfig,
			ConnState: server.TimeoutConnState,
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

	// The I2P manager owns a separate loopback-only backend listener and
	// returns immediately while the SAM router builds tunnels.
	i2pManager := i2p.NewManager(cfg.Server.I2P, i2pBackendHandler)
	i2p.Set(i2pManager)
	i2pManager.Start(context.Background())
	var torMgr *tor.Manager

	// The built-in scheduler is mandatory and always runs (AI.md PART 18).
	// There is no enable/disable toggle for the scheduler itself; individual
	// tasks carry their own enabled flag.
	schedOpts := scheduler.OptionsFromConfig(cfg.Server.Scheduler)
	// update_check compares the running build against the release feed, and
	// the backup tasks stamp the running version into every archive manifest,
	// so both identity fields come from the embedded ldflag values.
	schedOpts.Version = Version
	schedOpts.BuildEpoch = buildEpoch()
	if sslManager != nil {
		if managedCertificatePath := sslManager.ManagedCertificatePath(); managedCertificatePath != "" {
			schedOpts.SSLRenewer = sslManager.RenewCertificate
			schedOpts.SSLCertificatePath = managedCertificatePath
			schedOpts.SSLDomain = sslManager.ManagedDomain()
		}
	}
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
	server.SetDebugSchedulerStatus(func() any { return sched.GetTasks() })
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
		if err := geoip.Get().LoadFromConfig(newCfg.Server.GeoIP, paths.DataDir()); err != nil {
			log.Printf("GeoIP: configuration reload failed: %v", err)
		} else if newCfg.Server.GeoIP.Enabled && geoip.NeedsInitialDownload(paths.DataDir()) {
			go func(geoConfig config.GeoIPConfig) {
				if err := geoip.DownloadFromConfig(geoConfig, paths.DataDir()); err != nil {
					log.Printf("GeoIP: first-run database download failed after configuration reload: %v", err)
					return
				}
				currentConfig := config.Get()
				if err := geoip.Get().LoadFromConfig(currentConfig.Server.GeoIP, paths.DataDir()); err != nil {
					log.Printf("GeoIP: failed to load databases after configuration reload: %v", err)
				}
			}(newCfg.Server.GeoIP)
		}
		server.SetHTTPTimeouts(newCfg.Server.Limits)
		initSMTP(newCfg)
		server.LogConfiguredPageContent(newCfg)
		i2pManager.UpdateConfig(newCfg.Server.I2P)
		if torMgr != nil {
			if err := torMgr.UpdateConfig(context.Background(), mapTorConfig(newCfg.Server.Tor)); err != nil {
				log.Printf("Tor: configuration reload failed: %v", err)
			}
		}
	})
	configWatcher.Start()
	defer configWatcher.Stop()

	// Channel to listen for errors
	errChan := make(chan error, 1)

	if err := printStartup(cfg, binaryName); err != nil {
		log.Printf("Startup banner write failed: %v", err)
	}
	torBackendServer := srv
	if torBackendServer == nil {
		torBackendServer = httpsSrv
	}
	if torBackendServer != nil {
		torBackendServer.ConnContext = server.TorConnContext(torBackendServer.ConnContext)
	}
	// Start server(s) in goroutine(s) after listeners and privilege drop are ready.
	if srv != nil && httpLn != nil {
		go func() {
			if err := srv.Serve(server.TimeoutListener(httpLn)); err != nil && err != http.ErrServerClosed {
				errChan <- err
			}
		}()
	}
	if httpsSrv != nil && httpsLn != nil {
		go func() {
			if err := httpsSrv.ServeTLS(server.TimeoutListener(httpsLn), "", ""); err != nil && err != http.ErrServerClosed {
				errChan <- err
			}
		}()
	}
	if notifier := email.DefaultNotifier(); notifier != nil {
		hostname, _ := os.Hostname()
		log.Printf("Server started version=%s mode=%s", Version, cfg.Server.Mode)
		if err := notifier.NotifyStartup(notifier.Recipients(), hostname, Version, cfg.Server.Mode); err != nil {
			log.Printf("Email: failed to send startup notification: %v", err)
		}
	}

	// Start Tor hidden service, per AI.md PART 31. Tor is always optional -
	// a missing binary or any startup failure logs a warning and the server
	// continues running without it; nothing here may block or fail
	// server startup.
	if listenPort := httpPort; listenPort != "" || httpsPort != "" {
		if listenPort == "" {
			listenPort = httpsPort
		}
		if serverPort, err := strconv.Atoi(listenPort); err == nil {
			torCfg := mapTorConfig(cfg.Server.Tor)
			torMgr = tor.NewManager(torCfg, paths.ConfigDir(), paths.DataDir())
			tor.Set(torMgr)
			if torBackendServer != nil {
				torMgr.SetBackendListenerHandler(func(backend net.Listener) {
					proxyListener := &proxyproto.Listener{
						Listener:          backend,
						ReadHeaderTimeout: 2 * time.Second,
						ConnPolicy: func(options proxyproto.ConnPolicyOptions) (proxyproto.Policy, error) {
							upstream, ok := options.Upstream.(*net.TCPAddr)
							if !ok || !upstream.IP.IsLoopback() {
								return proxyproto.REJECT, fmt.Errorf("tor backend accepts loopback connections only")
							}
							return proxyproto.REQUIRE, nil
						},
					}
					go func() {
						if serveErr := torBackendServer.Serve(proxyListener); serveErr != nil && !errors.Is(serveErr, net.ErrClosed) && serveErr != http.ErrServerClosed {
							errChan <- serveErr
						}
					}()
				})
			}

			go func() {
				// Confirm the HTTP(S) listener is actually accepting
				// connections before starting Tor, since the hidden
				// service forwards to it - bounded retry, never blocks
				// server startup.
				deadline := time.Now().Add(10 * time.Second)
				for time.Now().Before(deadline) {
					conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), 500*time.Millisecond)
					if dialErr == nil {
						_ = conn.Close()
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
					return
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
				previousConfig := config.Get()
				if err := config.Reload(); err != nil {
					log.Printf("Failed to reload config: %v", err)
				} else {
					newConfig := config.Get()
					if err := resumeEncryptionKeyRotation(context.Background(), newConfig); err != nil {
						config.Set(previousConfig)
						log.Printf("Failed to finish encryption-key rotation; configuration reload rejected: %v", err)
						continue
					}
					if newConfig.Server.SSL.LetsEncrypt.Challenge == "dns-01" || newConfig.Server.SSL.LetsEncrypt.DNSProvider != "" {
						if err := ssl.ValidateDNSProvider(newConfig.Server.SSL.LetsEncrypt.DNSProvider, newConfig.Server.SSL.LetsEncrypt.DNSCredentials); err != nil {
							config.Set(previousConfig)
							log.Printf("SSL: [ERROR] DNS provider validation failed; configuration reload rejected: %v", err)
							continue
						}
					}
					if err := syncDNSConfigState(context.Background(), newConfig); err != nil {
						config.Set(previousConfig)
						log.Printf("Failed to synchronize DNS provider state; configuration reload rejected: %v", err)
						continue
					}
					server.SetHTTPTimeouts(newConfig.Server.Limits)
					initSMTP(newConfig)
					if torMgr != nil {
						if err := torMgr.UpdateConfig(context.Background(), mapTorConfig(newConfig.Server.Tor)); err != nil {
							log.Printf("Tor: configuration reload failed: %v", err)
						}
					}
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
	i2pCtx, i2pCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := i2pManager.Close(i2pCtx); err != nil {
		log.Printf("I2P: error during shutdown: %v", err)
	}
	i2pCancel()

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
	shutdownReason := "signal"
	if update.RestartPending() {
		shutdownReason = "update restart"
	}
	log.Printf("Server stopped version=%s mode=%s reason=%s", Version, cfg.Server.Mode, shutdownReason)
	if notifier := email.DefaultNotifier(); notifier != nil {
		hostname, _ := os.Hostname()
		finished := make(chan error, 1)
		go func() {
			finished <- notifier.NotifyShutdown(notifier.Recipients(), hostname, Version, cfg.Server.Mode, shutdownReason)
		}()
		select {
		case err := <-finished:
			if err != nil {
				log.Printf("Email: failed to send shutdown notification: %v", err)
			}
		case <-time.After(5 * time.Second):
			log.Printf("Email: shutdown notification exceeded its 5 second delivery window")
		}
	}
	if err := server.CloseRateLimiters(); err != nil {
		log.Printf("Failed to flush rate-limit counters during shutdown: %v", err)
	}

	cprintln("✅ Server stopped")
	if update.RestartPending() {
		sched.Stop()
		if err := database.Close(); err != nil {
			log.Printf("Failed to close database before staged-update restart: %v", err)
		}
		os.Exit(75)
	}
}

// initSMTP builds the process-wide email.Client from
// cfg.Server.Notifications.Email, auto-detecting a local relay when no host
// is configured (persisting the result to server.yml so restarts don't
// re-probe) or connection-testing an explicitly configured host every
// startup. Email features are fully disabled - not queued, not retried -
// when no working SMTP server is found; see AI.md PART 17 "SMTP
// Requirement".
func initSMTP(cfg *config.Config) {
	fqdn := server.GetFQDN(paths.ProjectName)
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
	}
	if found {
		probeConfig := email.ClientConfigFromConfig(cfg.Server.Notifications.Email, cfg.Server.Branding.Title, fqdn, true)
		if err := email.TestConnectionWithConfig(probeConfig); err != nil {
			log.Printf("Email: configured SMTP host %s:%d unreachable: %v (email features disabled, will retry next startup)", smtp.Host, smtp.Port, err)
			found = false
		}
	}

	client := email.NewClient(email.ClientConfigFromConfig(
		cfg.Server.Notifications.Email,
		cfg.Server.Branding.Title,
		fqdn,
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
		server.GetFQDN(paths.ProjectName),
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
	fqdn := server.GetFQDN(paths.ProjectName)
	appURL := server.GetDisplayURL(paths.ProjectName, portNumber(httpPort), false)
	if httpPort == "" {
		appURL = server.GetDisplayURL(paths.ProjectName, portNumber(httpsPort), true)
	}

	vars := email.GlobalVars{
		AppName:             cfg.Server.Branding.Title,
		AppURL:              appURL,
		FQDN:                fqdn,
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
	notifier.SetLanguage(cfg.Server.I18N.DefaultLanguage)
	email.SetDefaultNotifier(notifier)
}

func printHelp(binaryName string) {
	cprintf("%s", i18n.TranslateFormat(appmode.GetLang(), "cli.server_help", map[string]string{"binary_name": binaryName}))
}

func normalizeLanguage(value string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	if len(parts) == 0 {
		return i18n.DefaultLanguage
	}
	code := strings.ToLower(parts[0])
	if !i18n.IsSupported(code) {
		return i18n.DefaultLanguage
	}
	return code
}

func earlyLanguage(args []string) string {
	for index, argument := range args {
		if strings.HasPrefix(argument, "--lang=") {
			return normalizeLanguage(strings.TrimPrefix(argument, "--lang="))
		}
		if argument == "--lang" && index+1 < len(args) {
			return normalizeLanguage(args[index+1])
		}
	}
	return normalizeLanguage(firstNonEmpty(os.Getenv("LC_ALL"), os.Getenv("LANG")))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// syncDNSConfigState invalidates the server-written DNS validation timestamp
// whenever the configured provider or any credential changes. The persisted
// comparison value is an HMAC, so it does not expose a guessable digest of a
// low-entropy credential.
func syncDNSConfigState(ctx context.Context, cfg *config.Config) error {
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return fmt.Errorf("load installation secret for DNS state: %w", err)
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Server.SSL.LetsEncrypt.DNSProvider))
	credentials := cfg.Server.SSL.LetsEncrypt.DNSCredentials
	names := make([]string, 0, len(credentials))
	for name := range credentials {
		names = append(names, name)
	}
	sort.Strings(names)
	mac := hmac.New(sha256.New, secret.Value)
	_, _ = fmt.Fprintf(mac, "provider:%d:%s\n", len(provider), provider)
	for _, name := range names {
		value := credentials[name]
		_, _ = fmt.Fprintf(mac, "credential:%d:%s:%d:%s\n", len(name), name, len(value), value)
	}
	fingerprint := hex.EncodeToString(mac.Sum(nil))
	previous, err := database.GetAppSecretValue(ctx, database.SecretDNSConfigFingerprint)
	if err != nil && !errors.Is(err, database.ErrSecretNotFound) {
		return err
	}
	if err == nil && previous != fingerprint {
		if err := database.DeleteAppSecretValue(ctx, database.SecretDNSValidatedAt); err != nil {
			return err
		}
	}
	return database.SetAppSecretValue(ctx, database.SecretDNSConfigFingerprint, fingerprint)
}

// resumeEncryptionKeyRotation eagerly completes any interrupted database
// migration left by the previous key rotation attempt.
func resumeEncryptionKeyRotation(ctx context.Context, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	now := time.Now().UTC()
	currentKey, err := decodeAtRestKey(cfg.Server.Security.EncryptionKey)
	if err != nil {
		return fmt.Errorf("decode current encryption key: %w", err)
	}
	legacyStored := cfg.Server.Security.EncryptionKeyPrevious != ""
	legacyPrevious := cfg.Server.Security.PreviousEncryptionKey(now)
	if legacyPrevious != "" {
		previousKey, err := decodeAtRestKey(legacyPrevious)
		if err != nil {
			return fmt.Errorf("decode retained encryption key: %w", err)
		}
		until, parseErr := time.Parse(time.RFC3339, cfg.Server.Security.EncryptionKeyPreviousExpires)
		if parseErr != nil {
			return fmt.Errorf("parse retained encryption key expiry: %w", parseErr)
		}
		if err := database.StorePreviousEncryptionKey(ctx, previousKey, currentKey, until); err != nil {
			return fmt.Errorf("wrap retained encryption key in database: %w", err)
		}
	}
	previousKey, err := database.LoadPreviousEncryptionKey(ctx, currentKey, now)
	if err != nil {
		nextKey, until, pendingErr := database.LoadPendingEncryptionKey(ctx, currentKey, now)
		if pendingErr == nil {
			previousKey = currentKey
			if err := database.StorePreviousEncryptionKey(ctx, previousKey, nextKey, until); err != nil {
				return fmt.Errorf("store recovered previous encryption key: %w", err)
			}
			cfg.Server.Security.EncryptionKey = base64.StdEncoding.EncodeToString(nextKey)
			currentKey = nextKey
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("persist recovered encryption key: %w", err)
			}
		} else if errors.Is(err, database.ErrSecretNotFound) && errors.Is(pendingErr, database.ErrSecretNotFound) {
			if legacyStored {
				cfg.Server.Security.EncryptionKeyPrevious = ""
				cfg.Server.Security.EncryptionKeyPreviousExpires = ""
				if err := config.Save(cfg); err != nil {
					return fmt.Errorf("remove expired plaintext previous encryption key: %w", err)
				}
			}
			return nil
		} else if !errors.Is(pendingErr, database.ErrSecretNotFound) {
			return fmt.Errorf("recover pending encryption key after retained-key load failed: %w", errors.Join(err, pendingErr))
		} else {
			return fmt.Errorf("load retained encryption key: %w", err)
		}
	}
	if err := database.DeletePendingEncryptionKey(ctx); err != nil {
		return fmt.Errorf("clear pending encryption-key recovery state: %w", err)
	}
	if err := database.ReencryptOwnerTokens(ctx, database.GetServerDB(), previousKey, currentKey); err != nil {
		return fmt.Errorf("reencrypt owner tokens: %w", err)
	}
	if err := database.ReencryptSecurityReportData(ctx, database.GetServerDB(), previousKey, currentKey); err != nil {
		return fmt.Errorf("reencrypt security reports: %w", err)
	}
	if legacyStored {
		cfg.Server.Security.EncryptionKeyPrevious = ""
		cfg.Server.Security.EncryptionKeyPreviousExpires = ""
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("remove legacy plaintext previous encryption key: %w", err)
		}
	}
	return nil
}

func decodeAtRestKey(value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		key, err = hex.DecodeString(value)
	}
	if err != nil || len(key) != 32 {
		return nil, errors.New("at-rest encryption key must decode to 32 bytes")
	}
	return key, nil
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

func printStartup(cfg *config.Config, binaryName string) error {
	httpPort, httpsPort := resolvePorts(cfg)
	urls := make([]string, 0, 8)
	if httpPort != "" {
		base := server.GetDisplayURL(paths.ProjectName, portNumber(httpPort), false)
		urls = append(urls, base, base+"/server/docs/swagger", base+"/server/docs/graphql")
	}
	if httpsPort != "" {
		base := server.GetDisplayURL(paths.ProjectName, portNumber(httpsPort), true)
		urls = append(urls, base, base+"/server/docs/swagger", base+"/server/docs/graphql")
	}
	lang := appmode.GetLang()
	return banner.PrintStartup(banner.Config{
		AppName: binaryName, Version: Version, AppMode: cfg.Server.Mode,
		Debug: appmode.IsDebugEnabled(), URLs: urls, Writer: os.Stdout, Lang: lang,
		Emojis: emojiEnabled, Colors: colorEnabled,
		Terminal:    display.DetectDisplayEnv(),
		StartupTime: time.Now().Format("January 02, 2006 at 15:04:05 MST"),
	})
}

func portNumber(value string) int {
	port, _ := strconv.Atoi(value)
	return port
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		cprintln("✅ Service is running")
		cprintf("   Port: %s\n", cfg.Server.Port)
		cprintf("   Config: %s\n", config.GetConfigPath())
		if statusResp, statusErr := client.Get("http://localhost:" + cfg.Server.Port + "/server/i2p/status"); statusErr == nil {
			defer func() { _ = statusResp.Body.Close() }()
			var payload struct {
				Data struct {
					State    string `json:"state"`
					Provider string `json:"provider"`
					Address  string `json:"address"`
				} `json:"data"`
			}
			if json.NewDecoder(statusResp.Body).Decode(&payload) == nil {
				switch payload.Data.State {
				case "running":
					cprintf("   I2P Eepsite: Running (%s)\n", payload.Data.Provider)
				case "no_provider":
					cprintln("   I2P Eepsite: No Provider")
				case "error":
					cprintln("   I2P Eepsite: Error")
				case "starting", "building":
					cprintf("   I2P Eepsite: %s\n", payload.Data.State)
				default:
					cprintln("   I2P Eepsite: Disabled")
				}
				if payload.Data.Address != "" {
					cprintf("     Address: %s\n", payload.Data.Address)
				}
			}
		}
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
		if errors.Is(err, sysservice.ErrAccountToolsUnavailable) {
			os.Exit(exUnavailable)
		}
		os.Exit(1)
	}
	cprintln(i18n.TranslateFormat(appmode.GetLang(), "cli.service_install_success", map[string]string{"binary_name": binaryName}))
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
	cprintf("%s", sysservice.HelpText(binaryName, appmode.GetLang()))
	cprintln(i18n.Translate(appmode.GetLang(), "cli.service_privilege_note"))
}

// Maintenance commands
func handleMaintenanceCommand(cmd string, optionalArg string, args []string, binaryName string) {
	switch strings.ToLower(cmd) {
	case "help", "--help":
		printMaintenanceHelp(binaryName)
		return

	case "backup":
		if len(args) > 0 && strings.EqualFold(args[0], "test") {
			handleBackupTest(args[1:], binaryName)
			return
		}
		filename := ""
		includeSSL, includeData := false, false
		for _, argument := range args {
			switch argument {
			case "--include-ssl":
				includeSSL = true
			case "--include-data":
				includeData = true
			default:
				if filename != "" {
					cprintf("❌ Unexpected backup argument: %s\n", argument)
					os.Exit(exUsage)
				}
				filename = argument
			}
		}
		handleBackup(filename, binaryName, includeSSL, includeData)

	case "restore":
		if optionalArg == "" {
			cprintln("❌ Restore requires a backup file path")
			cprintf("   Usage: %s --maintenance restore /path/to/api_backup_YYYY-MM-DD.tar.gz[.enc]\n", binaryName)
			os.Exit(1)
		}
		handleRestore(optionalArg, binaryName)

	case "update":
		if len(args) == 0 {
			cprintf("Usage: %s --maintenance update {check|yes|branch <stable|beta|devel>}\n", binaryName)
			os.Exit(exUsage)
		}
		updateArg := ""
		if len(args) > 1 {
			updateArg = args[1]
		}
		handleUpdateCommand(args[0], updateArg, binaryName)

	case "mode":
		if optionalArg == "" {
			cprintln("❌ Mode change requires a mode value")
			cprintf("   Usage: %s --maintenance mode {production|development|debug}\n", binaryName)
			os.Exit(1)
		}
		handleModeChange(optionalArg, binaryName)

	case "setup":
		handleSetupCommand(binaryName)

	case "secret":
		handleSecretCommand(args, binaryName)

	case "security":
		handleSecurityMaintenance(args, binaryName)

	case "pgp":
		handlePGPCommand(args, binaryName)

	default:
		cprintf("Unknown maintenance command: %s\n", cmd)
		cprintln("Valid commands: backup, restore, update, mode, setup, secret, security, pgp")
		os.Exit(1)
	}
}

func handleSetupCommand(binaryName string) {
	hadConfigFiles := config.HasSourceFiles()
	if !hadConfigFiles && !databaseIsEmpty(config.DefaultConfig()) && !paths.IsElevated() {
		fmt.Fprintf(os.Stderr, "Error: setup already completed. To reconfigure, edit server.yml and restart the server, or run as root: sudo %s --maintenance setup\n", binaryName)
		os.Exit(77)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load configuration for setup: %v\n", err)
		os.Exit(exConfig)
	}
	firstRun := databaseIsEmpty(cfg)
	elevated := paths.IsElevated()
	if !firstRun && !elevated {
		fmt.Fprintf(os.Stderr, "Error: setup already completed. To reconfigure, edit server.yml and restart the server, or run as root: sudo %s --maintenance setup\n", binaryName)
		os.Exit(77)
	}
	if elevated {
		if !confirmSetup() {
			cprintln("Setup cancelled; no configuration files were changed.")
			os.Exit(0)
		}
	}
	if hadConfigFiles {
		fmt.Fprintf(os.Stderr, "Error: existing server.yml/yaml.d reset scope is awaiting a documented decision; no configuration files were changed.\n")
		os.Exit(1)
	}
	if !firstRun {
		fmt.Fprintf(os.Stderr, "Error: setup reset requires the approved reset scope; no configuration files were changed.\n")
		os.Exit(1)
	}
	cprintln("✅ First-run server configuration is initialized with defaults")
}

func confirmSetup() bool {
	cprintf("Reset server configuration to defaults? Continue? [y/N] ")
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func printMaintenanceHelp(binaryName string) {
	fmt.Print(i18n.TranslateFormat(appmode.GetLang(), "cli.maintenance_help", map[string]string{"binary_name": binaryName}))
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
	case "help", "--help":
		printUpdateHelp(binaryName)
		return

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
		if !paths.IsElevated() {
			cprintf("❌ Updating the installed binary requires an elevated process. Retry with: sudo %s --update yes\n", binaryName)
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
			cprintf("   Usage: %s --update branch {stable|beta|devel}\n", binaryName)
			os.Exit(1)
		}
		if !update.ValidBranch(optionalArg) {
			cprintf("❌ Unknown update channel: %s\n", optionalArg)
			cprintln("   Valid channels: stable, beta, devel (aliases: daily, rolling, dev, development)")
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

func printUpdateHelp(binaryName string) {
	fmt.Print(i18n.TranslateFormat(appmode.GetLang(), "cli.update_help", map[string]string{"binary_name": binaryName, "version": Version}))
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
	defer func() { _ = resp.Body.Close() }()

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
	cprintf("%s", i18n.TranslateFormat(appmode.GetLang(), "cli.tor_help", map[string]string{"binary_name": binaryName}))
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

func mapTorConfig(cfg config.TorConfig) tor.Config {
	return tor.Config{
		Binary:                    cfg.Binary,
		UseNetwork:                cfg.UseNetwork,
		MaxCircuits:               cfg.MaxCircuits,
		StartupTimeout:            cfg.StartupTimeout,
		CircuitTimeout:            cfg.CircuitTimeout,
		BootstrapTimeout:          cfg.BootstrapTimeout,
		SafeLogging:               cfg.SafeLogging,
		MaxStreamsPerCircuit:      cfg.MaxStreamsPerCircuit,
		CloseCircuitOnStreamLimit: cfg.CloseCircuitOnStreamLimit,
		BandwidthRate:             cfg.BandwidthRate,
		BandwidthBurst:            cfg.BandwidthBurst,
		MaxMonthlyBandwidth:       cfg.MaxMonthlyBandwidth,
		NumIntroPoints:            cfg.NumIntroPoints,
		VirtualPort:               cfg.VirtualPort,
	}
}
