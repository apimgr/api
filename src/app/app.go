package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"sync"
	"syscall"
	"time"

	"github.com/apimgr/api/src/common/banner"
	"github.com/apimgr/api/src/common/display"
	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/common/requestid"
	commonversion "github.com/apimgr/api/src/common/version"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/critical"
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
	Version  = "devel"
	CommitID = "N/A"
	// BuildDate is derived from BuildEpoch in init(); stays "N/A" when
	// BuildEpoch is unset (dev builds that skip -ldflags).
	BuildDate = "N/A"
	// BuildEpoch is the Unix build timestamp (seconds, UTC) embedded via
	// -ldflags at build time; "0" when unset. See AI.md "Embedded Build
	// Info" - BuildDate is derived from this, never embedded directly.
	BuildEpoch   = "0"
	OfficialSite = ""
)

var adminEmailWarningOnce sync.Once

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

func listenTCP(address, port string) (net.Listener, string, error) {
	listenAddress := strings.TrimSuffix(strings.TrimPrefix(address, "["), "]")
	listener, err := net.Listen("tcp", net.JoinHostPort(listenAddress, port))
	if err == nil || listenAddress != "::" || !strings.Contains(strings.ToLower(err.Error()), "address family not supported") {
		return listener, listenAddress, err
	}

	log.Printf("IPv6 is unavailable; listening on 0.0.0.0 instead")
	listener, err = net.Listen("tcp", net.JoinHostPort("0.0.0.0", port))
	return listener, "0.0.0.0", err
}

// Exit codes follow the CLI contract; usage errors use 2, with remaining
// operational failures retaining their sysexits-compatible values.
const (
	exUsage       = 2
	exUnavailable = 69 // Required service or resource unavailable
	exOSErr       = 71 // OS-level error (fork/daemonize failed, etc.)
	exCantCreat   = 73 // Output file cannot be created (PID file)
	exConfig      = 78 // Configuration error
)

// ExitError carries the process exit status for startup and CLI failures.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }

func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode returns the requested process status, or 1 for ordinary errors.
func ExitCode(err error) int {
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return 1
}

func shouldDaemonizeManually(requested bool, isContainer bool, manager sysservice.ServiceType, serviceContext bool) bool {
	return requested && !isContainer && runtime.GOOS != "windows" && manager == sysservice.ServiceUnknown && !serviceContext
}

func isServiceManagerContext(parentPID int, invocationID, rcServiceName, svDir, s6Logging string) bool {
	return parentPID == 1 || invocationID != "" || rcServiceName != "" || svDir != "" || s6Logging != ""
}

func Run(ctx context.Context, cfg *config.ServerConfig) error {
	signalCtx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	return run(signalCtx, cfg, nil, nil)
}

func run(ctx context.Context, cfg *config.ServerConfig, testListener net.Listener, ready chan<- struct{}) error {
	Version = cfg.Build.Version
	CommitID = cfg.Build.CommitID
	BuildEpoch = cfg.Build.BuildEpoch
	OfficialSite = cfg.Build.OfficialSite
	BuildDate = cfg.Build.BuildDate
	binaryName := filepath.Base(os.Args[0])
	options := cfg.Runtime
	appmode.SetLang(earlyLanguage(os.Args[1:]))
	showHelp := &options.Help
	showVersion := &options.Version
	mode := &options.Mode
	daemon := &options.Daemon
	debug := &options.Debug
	colorFlag := &options.Color
	langFlag := &options.Language
	shellCmd := &options.Shell
	showStatus := &options.Status
	serviceCmd := &options.Service
	maintenanceCmd := &options.Maintenance
	updateCmd := &options.Update
	var err error

	switch options.CommandHelp {
	case "shell":
		printShellHelp(binaryName)
		return nil
	case "service":
		printServiceHelp(binaryName)
		return nil
	case "maintenance":
		printMaintenanceHelp(binaryName)
		return nil
	case "update":
		printUpdateHelp(binaryName)
		return nil
	case "tor":
		printTorHelp(binaryName)
		return nil
	}

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
		default:
			printHelp(binaryName)
		}
		return nil
	}

	if *showVersion {
		fmt.Print(commonversion.FormatOutput(binaryName, Version, CommitID, BuildDate, runtime.Version(), runtime.GOOS, runtime.GOARCH))
		return nil
	}

	applyColorMode(*colorFlag, nil)
	if *langFlag != "" {
		appmode.SetLang(normalizeLanguage(*langFlag))
	} else if envLang := firstNonEmpty(os.Getenv("LC_ALL"), os.Getenv("LANG")); envLang != "" {
		appmode.SetLang(normalizeLanguage(envLang))
	}

	if *shellCmd != "" {
		shellArg := ""
		if len(options.Args) > 0 {
			shellArg = options.Args[0]
		}
		handleShellCommand(*shellCmd, shellArg, binaryName)
		return nil
	}
	if *showStatus {
		checkStatus()
		return nil
	}
	if *serviceCmd != "" {
		handleServiceCommand(*serviceCmd, binaryName)
		return nil
	}
	if *maintenanceCmd != "" {
		optionalArg := ""
		if len(options.Args) > 0 {
			optionalArg = options.Args[0]
		}
		handleMaintenanceCommand(*maintenanceCmd, optionalArg, options.Args, binaryName)
		return nil
	}
	if *updateCmd != "" {
		optionalArg := ""
		if len(options.Args) > 0 {
			optionalArg = options.Args[0]
		}
		handleUpdateCommand(*updateCmd, optionalArg, binaryName)
		return nil
	}
	if len(options.Args) > 0 && strings.EqualFold(options.Args[0], "email") {
		handleEmailCommand(options.Args[1:], binaryName)
		return nil
	}

	// Root setup (AI.md PART 8 startup sequence, PART 23 system user):
	// while still elevated, create the dedicated service account with its
	// home directory and every runtime directory. Ownership is transferred
	// and privileges are dropped further down, once the first-run config
	// has been written and the listeners are bound.
	var serviceUID, serviceGID int
	if testListener == nil {
		serviceUID, serviceGID, err = sysservice.RootSetup()
		if err != nil {
			log.Printf("Failed to prepare the service account and runtime directories: %v", err)
			if errors.Is(err, sysservice.ErrAccountToolsUnavailable) {
				return &ExitError{Code: exUnavailable, Err: err}
			}
			return &ExitError{Code: exOSErr, Err: err}
		}
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

	// Determine whether --debug was explicitly passed (as opposed to
	// defaulting to false), so it can win over DEBUG env / debug mode
	// per the priority order in mode.Initialize.
	debugFlagSet := options.DebugSet

	// Daemonize (detach from terminal) before writing the PID file, so the
	// PID file reflects the actual detached process, not the parent. Per
	// AI.md PART 8, --daemon is a manual-start-only concern: --service
	// start decides for itself via shouldDaemonize/detectServiceManager.
	manualDaemonize := shouldDaemonizeManually(
		*daemon || cfg.Server.Daemonize,
		paths.IsRunningInContainer(),
		sysservice.DetectServiceManager(),
		isServiceManagerContext(os.Getppid(), os.Getenv("INVOCATION_ID"), os.Getenv("RC_SVCNAME"), os.Getenv("SVDIR"), os.Getenv("S6_LOGGING")),
	)
	if (*daemon || cfg.Server.Daemonize) && manualDaemonize && runtime.GOOS != "windows" {
		if err := daemonize(); err != nil {
			log.Printf("Failed to daemonize: %v", err)
			return &ExitError{Code: exOSErr, Err: err}
		}
	} else if (*daemon || cfg.Server.Daemonize) && runtime.GOOS == "windows" {
		log.Printf("Daemonization is unavailable on Windows; continuing in the foreground")
	}

	// Bind the listening sockets while still elevated, so privileged ports
	// (<1024) can be claimed, then drop to the dedicated service account.
	// Binding has to happen after daemonize(), which re-execs the process
	// and would otherwise lose the sockets.
	httpPort, httpsPort := resolvePorts(cfg)
	listenAddress := cfg.Server.Address
	var httpLn, httpsLn net.Listener
	if httpPort != "" {
		if testListener != nil {
			httpLn = testListener
			if tcpAddress, ok := testListener.Addr().(*net.TCPAddr); ok {
				listenAddress = tcpAddress.IP.String()
			}
		} else {
			httpLn, listenAddress, err = listenTCP(listenAddress, httpPort)
			if err != nil {
				log.Printf("Failed to bind %s:%s: %v", listenAddress, httpPort, err)
				return &ExitError{Code: exUnavailable, Err: err}
			}
		}
	}
	if httpsPort != "" && testListener == nil {
		httpsLn, listenAddress, err = listenTCP(listenAddress, httpsPort)
		if err != nil {
			log.Printf("Failed to bind %s:%s: %v", listenAddress, httpsPort, err)
			return &ExitError{Code: exUnavailable, Err: err}
		}
	}
	defer func() {
		if httpLn != nil {
			_ = httpLn.Close()
		}
		if httpsLn != nil {
			_ = httpsLn.Close()
		}
	}()

	// Everything that needed root is done: hand the runtime directories
	// (including the config file just written on first run) to the service
	// account and permanently drop privileges. A failed drop is fatal - the
	// process must never keep serving as root (AI.md PART 23/24).
	if serviceUID > 0 {
		if err := sysservice.ClaimRuntimeDirs(serviceUID, serviceGID); err != nil {
			log.Printf("Failed to transfer runtime directories to the %s account: %v", sysservice.ServiceUserName, err)
			return &ExitError{Code: exOSErr, Err: err}
		}
		if err := sysservice.DropPrivileges(serviceUID, serviceGID); err != nil {
			log.Printf("Failed to drop privileges: %v", err)
			return &ExitError{Code: exOSErr, Err: err}
		}
	}

	// Initialize a default logger before PID handling. The full log settings
	// are intentionally unavailable until after the PID file is checked.
	if err := server.InitLogger(&config.DefaultConfig().Server.Logs); err != nil {
		log.Printf("Warning: failed to initialize startup logging: %v", err)
	} else if logger := server.GetLogger(); logger != nil {
		logger.LogServer("info", "Server starting, version "+Version)
	}

	pidPath := options.PIDFile
	if pidPath == "" {
		pidPath = paths.DefaultPIDPath()
	}
	usePIDFile := testListener == nil && cfg.Server.PIDFile && runtime.GOOS != "windows" && !paths.IsRunningInContainer()
	if usePIDFile {
		if err := pidfile.WritePIDFile(pidPath); err != nil {
			log.Printf("Failed to write PID file: %v", err)
			return &ExitError{Code: exCantCreat, Err: err}
		}
		defer func() {
			if err := pidfile.RemovePIDFile(pidPath); err != nil {
				log.Printf("Failed to remove PID file: %v", err)
			}
		}()
	}

	// PART 8 startup step 13: full config loading happens only after the PID
	// file check/write. The early config object carries the restricted peek
	// values and binary metadata into this stage.
	cfg, err = config.LoadFull(cfg)
	if err != nil {
		log.Printf("Failed to load full configuration: %v", err)
		return &ExitError{Code: exConfig, Err: err}
	}
	auditConfig := cfg.Server.Logs.Audit
	auditConfig.Keep = cfg.Server.Compliance.EffectiveAuditRetention(auditConfig.Keep)
	if err := database.ConfigureAuditFile(auditConfig); err != nil {
		log.Printf("Failed to configure audit logging: %v", err)
		return &ExitError{Code: exConfig, Err: err}
	}
	options = cfg.Runtime
	if *langFlag == "" && cfg.Server.I18N.DefaultLanguage != "" {
		appmode.SetLang(normalizeLanguage(cfg.Server.I18N.DefaultLanguage))
	}
	if err := appmode.Initialize(*mode, *debug, debugFlagSet, cfg.Server.Mode); err != nil {
		log.Printf("Invalid --mode value: %v", err)
		return &ExitError{Code: exUsage, Err: err}
	}
	appmode.SetProfilingRates(cfg.Server.Debug.BlockProfileRate, cfg.Server.Debug.MutexProfileFraction)
	cfg.Server.Mode = appmode.GetCurrentMode().String()

	// A database connection or schema write failure is critical but recoverable.
	// Keep health and read-only routes available while the maintenance monitor
	// retries initialization in the background.
	var startupMaintenanceReason, startupMaintenanceMessage string
	databaseReady := true
	if err := database.InitDatabase(cfg.Server.Database, paths.DataDir()); err != nil {
		log.Printf("Failed to initialize database; entering maintenance recovery: %v", err)
		startupMaintenanceReason = "database_connection"
		startupMaintenanceMessage = "Database connection failed. Verify database availability, network access, and credentials."
		databaseReady = false
	}
	defer func() { _ = database.Close() }()
	requestStatsStarted := false
	defer func() {
		if !requestStatsStarted {
			return
		}
		statsCtx, statsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer statsCancel()
		if err := handler.StopRequestStats(statsCtx); err != nil {
			log.Printf("Failed to flush request statistics before closing the database: %v", err)
		}
	}()
	cacheReadOnlyOffline := !databaseReady || database.CacheReadOnlyOffline()
	if cacheReadOnlyOffline {
		if databaseReady {
			log.Printf("Database: remote is unavailable; serving from the fresh local cache in read-only mode")
		}
	} else {
		if err := database.EnsureCaptchaSchema(context.Background()); err != nil {
			log.Printf("Failed to initialize CAPTCHA challenge storage; entering maintenance recovery: %v", err)
			startupMaintenanceReason = "database_write"
			startupMaintenanceMessage = "The database rejected a required startup write. Check database availability and write permissions."
			databaseReady = false
			cacheReadOnlyOffline = true
		}
		if databaseReady {
			if err := database.EnsureRateLimitSchema(context.Background()); err != nil {
				log.Printf("Failed to initialize persisted rate-limit storage; entering maintenance recovery: %v", err)
				startupMaintenanceReason = "database_write"
				startupMaintenanceMessage = "The database rejected a required startup write. Check database availability and write permissions."
				databaseReady = false
				cacheReadOnlyOffline = true
			}
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
	if databaseReady {
		if err := database.EnsureAppSecrets(context.Background()); err != nil {
			log.Printf("Failed to initialize project secrets; entering maintenance recovery: %v", err)
			startupMaintenanceReason = "database_write"
			startupMaintenanceMessage = "The database rejected a required startup write. Check database availability and write permissions."
			databaseReady = false
			cacheReadOnlyOffline = true
		}
	}
	if databaseReady {
		if err := database.SyncComplianceStandards(context.Background(), cfg.Server.Compliance.StandardStates()); err != nil {
			log.Printf("Could not record compliance standard changes: %v", err)
		}
	}
	if !cacheReadOnlyOffline {
		if err := pgp.ResumeInstallationSecretRotation(context.Background()); err != nil {
			log.Printf("Failed to finish installation-secret rotation: %v", err)
			return &ExitError{Code: exUnavailable, Err: err}
		}
		if err := pgp.RecoverRotation(context.Background(), cfg); err != nil {
			log.Printf("Failed to recover PGP key rotation: %v", err)
			return &ExitError{Code: exUnavailable, Err: err}
		}
		if err := resumeEncryptionKeyRotation(context.Background(), cfg); err != nil {
			log.Printf("Failed to finish encryption-key rotation: %v", err)
			return &ExitError{Code: exUnavailable, Err: err}
		}
		if err := syncDNSConfigState(context.Background(), cfg); err != nil {
			log.Printf("Failed to synchronize DNS provider state: %v", err)
			return &ExitError{Code: exUnavailable, Err: err}
		}
	}
	if cfg.Server.SSL.LetsEncrypt.Challenge == "dns-01" || cfg.Server.SSL.LetsEncrypt.DNSProvider != "" {
		if err := ssl.ValidateDNSProvider(cfg.Server.SSL.LetsEncrypt.DNSProvider, cfg.Server.SSL.LetsEncrypt.DNSCredentials); err != nil {
			log.Printf("SSL: [ERROR] DNS provider validation failed: %v", err)
			return &ExitError{Code: exConfig, Err: err}
		}
	}

	// Set database for health checks
	handler.SetDatabase(database.ServerDBProbe{})
	if err := handler.StartRequestStats(ctx); err != nil {
		log.Printf("Warning: failed to load persisted request statistics; continuing with in-memory counts: %v", err)
	}
	requestStatsStarted = true

	// Re-resolve --color/NO_COLOR now that config is available, applying the
	// output.color/output.emoji config-file tier per AI.md PART 8's
	// priority order (CLI flag > config file > NO_COLOR env var > auto-detect).
	applyColorMode(*colorFlag, cfg.Output.Color)
	applyEmojiOverride(cfg.Output.Emoji)

	// Initialize logging system
	if err := server.InitLogger(&cfg.Server.Logs); err != nil {
		log.Printf("Warning: Failed to initialize logging system: %v", err)
		if startupMaintenanceReason == "" {
			startupMaintenanceReason = "file_write"
			startupMaintenanceMessage = "A required log file could not be opened. Check the log directory, disk space, and file permissions."
		}
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
	// the setup audit log now that the logger is available (config.Load(args)
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
	if err := geoip.GetGeoIPDatabase().LoadFromConfig(cfg.Server.GeoIP, paths.DataDir()); err != nil {
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
			if err := geoip.GetGeoIPDatabase().LoadFromConfig(currentConfig.Server.GeoIP, paths.DataDir()); err != nil {
				log.Printf("GeoIP: failed to load first-run databases: %v", err)
			}
		}()
	}

	// Initialize outbound email (PART 17): auto-detect a local SMTP relay
	// if none is configured, or connection-test the configured host every
	// startup; never blocks startup either way.
	initSMTP(cfg)
	maintenanceMonitor := server.StartMaintenanceMonitor(ctx, cfg, func(recoveryCtx context.Context) error {
		_ = database.Close()
		if err := database.InitDatabase(cfg.Server.Database, paths.DataDir()); err != nil {
			return err
		}
		if err := database.EnsureCaptchaSchema(recoveryCtx); err != nil {
			return err
		}
		if err := database.EnsureRateLimitSchema(recoveryCtx); err != nil {
			return err
		}
		if err := database.EnsureAppSecrets(recoveryCtx); err != nil {
			return err
		}
		if err := pgp.ResumeInstallationSecretRotation(recoveryCtx); err != nil {
			return err
		}
		if err := pgp.RecoverRotation(recoveryCtx, cfg); err != nil {
			return err
		}
		if err := resumeEncryptionKeyRotation(recoveryCtx, cfg); err != nil {
			return err
		}
		return syncDNSConfigState(recoveryCtx, cfg)
	})
	critical.RegisterFailureHandler(server.TriggerMaintenance)
	defer critical.RegisterFailureHandler(nil)
	defer maintenanceMonitor.Stop()
	if startupMaintenanceReason != "" {
		server.TriggerMaintenance(startupMaintenanceReason, startupMaintenanceMessage)
	}
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
		return err
	}

	// Initialize the metrics registry from resolved config before any
	// collector is touched, then stamp app_info and start the background
	// system/runtime sampler (AI.md PART 20).
	metrics.InitMetrics(metrics.MetricsOptions{
		DurationBuckets: cfg.Server.Metrics.DurationBuckets,
		SizeBuckets:     cfg.Server.Metrics.SizeBuckets,
		IncludeSystem:   cfg.Server.Metrics.IncludeSystem,
		IncludeRuntime:  cfg.Server.Metrics.IncludeRuntime,
		DataDir:         paths.DataDir(),
		OwnerTokenCounts: func() (float64, float64, error) {
			return database.ActiveOwnerTokenCounts(context.Background(), database.GetServerDB())
		},
	})
	metrics.GetMetrics().SetBuildInfo(Version, CommitID, BuildDate)
	if cfg.Server.Metrics.Enabled {
		metrics.GetMetrics().StartCollectors()
		defer metrics.GetMetrics().StopCollectors()
	}

	// Create server
	srv := server.NewHTTPServer(cfg)
	stopCacheWarming := startCacheWarming(ctx, cfg.Server.Cache)
	defer stopCacheWarming()
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
		sslMgr := ssl.NewManager(ssl.SSLConfig{
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
			return &ExitError{Code: exConfig, Err: err}
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
	schedOpts := scheduler.SchedulerOptionsFromConfig(cfg.Server.Scheduler)
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
	server.SetDebugSchedulerStatus(func() any { return sched.Status() })
	defer sched.Stop()
	handler.SetSchedulerProbe(sched.Health)
	// The health response advertises GeoIP as a feature only when the country
	// database is actually loaded - a configured-but-undownloaded database is
	// not a capability a client can rely on.
	handler.SetGeoIPProbe(geoip.GetGeoIPDatabase().HasCountryDB)
	log.Println("Scheduler started with default tasks")

	// Start config file watcher for hot reload
	configWatcher := config.NewConfigWatcher(func(newCfg *config.ServerConfig) {
		log.Printf("Configuration reloaded")
		previousCfg := config.Get()
		if databaseReady {
			auditSecurityPolicyChanges(context.Background(), previousCfg, newCfg)
			if err := database.SyncComplianceStandards(context.Background(), newCfg.Server.Compliance.StandardStates()); err != nil {
				log.Printf("Could not record compliance standard changes after config reload: %v", err)
			}
		}
		// Update global config - server will pick up changes via config.Get()
		config.Set(newCfg)
		if err := geoip.GetGeoIPDatabase().LoadFromConfig(newCfg.Server.GeoIP, paths.DataDir()); err != nil {
			log.Printf("GeoIP: configuration reload failed: %v", err)
		} else if newCfg.Server.GeoIP.Enabled && geoip.NeedsInitialDownload(paths.DataDir()) {
			go func(geoConfig config.GeoIPConfig) {
				if err := geoip.DownloadFromConfig(geoConfig, paths.DataDir()); err != nil {
					log.Printf("GeoIP: first-run database download failed after configuration reload: %v", err)
					return
				}
				currentConfig := config.Get()
				if err := geoip.GetGeoIPDatabase().LoadFromConfig(currentConfig.Server.GeoIP, paths.DataDir()); err != nil {
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
	if ready != nil {
		close(ready)
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
	if listenPort := httpPort; testListener == nil && (listenPort != "" || httpsPort != "") {
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

	// SIGHUP is ignored because the configuration watcher applies file changes.
	// Unix-specific controls reopen logs, dump status, and handle SIGQUIT.
	runtimeSignals, stopRuntimeSignals := watchRuntimeSignals()
	defer stopRuntimeSignals()

	for {
		select {
		case <-ctx.Done():
			cprintln("\n🛑 Shutting down gracefully...")
		case sig := <-runtimeSignals:
			switch classifyRuntimeSignal(sig) {
			case runtimeSignalReopenLogs:
				if err := server.ReopenLogger(); err != nil {
					log.Printf("Failed to reopen log files: %v", err)
				} else if logger := server.GetLogger(); logger != nil {
					logger.LogServer("info", "SIGUSR1 received; log files reopened")
				}
				continue
			case runtimeSignalDumpStatus:
				dumpRuntimeStatus(cfg, sched, httpLn, httpsLn)
				continue
			case runtimeSignalIgnore:
				continue
			default:
				cprintln("\n🛑 Shutting down gracefully...")
			}
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if srv != nil {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server forced to shutdown: %v", err)
		}
	}
	if httpsSrv != nil {
		if err := httpsSrv.Shutdown(shutdownCtx); err != nil {
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
	if requestStatsStarted {
		if err := handler.StopRequestStats(shutdownCtx); err != nil {
			log.Printf("Failed to flush request statistics during shutdown: %v", err)
		} else {
			requestStatsStarted = false
		}
	}

	cprintln("✅ Server stopped")
	if update.RestartPending() {
		sched.Stop()
		if err := database.Close(); err != nil {
			log.Printf("Failed to close database before staged-update restart: %v", err)
		}
		return &ExitError{Code: 75, Err: fmt.Errorf("staged update restart is pending")}
	}
	return nil
}

// initSMTP builds the process-wide email.Client from
// cfg.Server.Notifications.Email, auto-detecting a local relay when no host
// is configured (persisting the result to server.yml so restarts don't
// re-probe) or connection-testing an explicitly configured host every
// startup. Email features are fully disabled - not queued, not retried -
// when no working SMTP server is found; see AI.md PART 17 "SMTP
// Requirement".
func initSMTP(cfg *config.ServerConfig) {
	fqdn := server.GetFQDN(paths.ProjectName)
	smtp := cfg.Server.Notifications.Email.SMTP
	found := smtp.Host != "" && fqdn != ""
	if fqdn == "" {
		log.Println("Email: disabled because the server FQDN is unavailable; configure a valid FQDN before enabling email")
	}

	if !found && fqdn != "" {
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
	cfg, err := config.Load(nil)
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
func initNotifier(cfg *config.ServerConfig, client *email.Client) {
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
	notifierClient := client
	if admin := cfg.Server.Contact.ResolveAdmin(); admin.Email != "" {
		recipients = []string{admin.Email}
	} else {
		notifierClient = nil
		adminEmailWarningOnce.Do(func() {
			log.Println("Email: server.contact.admin.email is empty; operator email notifications are disabled until it is configured")
		})
	}

	notifier := email.NewNotifier(notifierClient, paths.ConfigDir(), vars, email.EventTogglesFromConfig(cfg.Server.Notifications.Email.Events), recipients)
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
func syncDNSConfigState(ctx context.Context, cfg *config.ServerConfig) error {
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
func resumeEncryptionKeyRotation(ctx context.Context, cfg *config.ServerConfig) error {
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
func resolvePorts(cfg *config.ServerConfig) (httpPort, httpsPort string) {
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

func printStartup(cfg *config.ServerConfig, binaryName string) error {
	httpPort, httpsPort := resolvePorts(cfg)
	if server.GetFQDN(paths.ProjectName) == "" {
		log.Print("Warning: public hostname is unresolved; set DOMAIN to configure a public host")
	}
	urls := buildStartupURLs(httpPort, httpsPort, func(port string, isHTTPS bool) string {
		return server.GetDisplayURL(paths.ProjectName, portNumber(port), isHTTPS)
	})
	lang := appmode.GetLang()
	return banner.PrintStartup(banner.BannerConfig{
		AppName: binaryName, Version: Version, AppMode: cfg.Server.Mode,
		Debug: appmode.IsDebugEnabled(), URLs: urls, Writer: os.Stdout, Lang: lang,
		Emojis: emojiEnabled, Colors: colorEnabled,
		Terminal:    display.DetectDisplayEnv(),
		StartupTime: time.Now().Format("January 02, 2006 at 15:04:05 MST"),
	})
}

func buildStartupURLs(httpPort, httpsPort string, displayURL func(port string, isHTTPS bool) string) []string {
	urls := make([]string, 0, 6)
	if httpPort != "" {
		base := displayURL(httpPort, false)
		if base != "" {
			urls = append(urls, base, base+"/server/docs/swagger", base+"/server/docs/graphql")
		}
	}
	if httpsPort != "" {
		base := displayURL(httpsPort, true)
		if base != "" {
			urls = append(urls, base, base+"/server/docs/swagger", base+"/server/docs/graphql")
		}
	}
	return urls
}

func portNumber(value string) int {
	port, _ := strconv.Atoi(value)
	return port
}

func checkStatus() {
	// Try to connect to the server
	cfg, err := config.Load(nil)
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
			handleUpdateCommand("yes", "", binaryName)
			return
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

	case "data", "compliance":
		if exitCode := handleComplianceMaintenance(strings.ToLower(cmd), args, binaryName); exitCode != 0 {
			os.Exit(exitCode)
		}

	case "scheduler":
		if exitCode := handleSchedulerMaintenance(args, binaryName); exitCode != 0 {
			os.Exit(exitCode)
		}

	case "pgp":
		handlePGPCommand(args, binaryName)

	case "tor":
		if len(args) == 0 || strings.EqualFold(args[0], "help") || args[0] == "--help" || args[0] == "-h" {
			printTorHelp(binaryName)
			return
		}
		handleTorCommand(args[0], args[1:], binaryName)

	default:
		cprintf("Unknown maintenance command: %s\n", cmd)
		cprintln("Valid commands: backup, restore, update, mode, setup, secret, security, data, compliance, pgp, tor")
		os.Exit(1)
	}
}

func handleSetupCommand(binaryName string) {
	hadConfigFiles := config.HasSourceFiles()
	if !hadConfigFiles && !databaseIsEmpty(config.DefaultConfig()) && !paths.IsElevated() {
		fmt.Fprintf(os.Stderr, "Error: setup already completed. To reconfigure, edit server.yml and restart the server, or run as root: sudo %s --maintenance setup\n", binaryName)
		os.Exit(77)
	}
	cfg, err := config.Load(nil)
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
	printDataComplianceHelp(binaryName)
}

// checkForUpdate queries GitHub Releases for the newest release on the
// configured channel. Per AI.md PART 22 "Defer Semantics", this manual path
// deliberately calls CheckForUpdate (not CheckEligible) so an explicit
// operator action always sees the true latest release, bypassing defer_days.
// A nil result means the running version is already current.
func checkForUpdate() *update.Release {
	branch := update.BranchStable
	if cfg, err := config.Load(nil); err == nil && update.ValidBranch(cfg.Server.Update.Branch) {
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
			cfg, err := config.Load(nil)
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
			cprintf("   Usage: %s --maintenance tor import-keys <path>\n", binaryName)
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
	cfg, err := config.Load(nil)
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
	resp, err := requestid.DoOutboundRequestWithID(client, req)
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
		cprintf("   Usage: %s --maintenance tor vanity {start <prefix>|apply}\n", binaryName)
		os.Exit(1)
	}

	switch strings.ToLower(args[0]) {
	case "start":
		if len(args) < 2 {
			cprintln("❌ vanity start requires a prefix")
			cprintf("   Usage: %s --maintenance tor vanity start <prefix>\n", binaryName)
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
		cprintf("   Run '%s --maintenance tor vanity apply' to apply it\n", binaryName)

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

	cfg, err := config.Load(nil)
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

func mapTorConfig(cfg config.TorConfig) tor.TorConfig {
	return tor.TorConfig{
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
