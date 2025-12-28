package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/geoip"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/scheduler"
	"github.com/apimgr/api/src/server"
	"github.com/apimgr/api/src/server/handler"
)

var (
	Version   = "1.0.0"
	BuildTime = "unknown"
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
	mode := flag.String("mode", "", "Application mode: production or development")
	configDir := flag.String("config", "", "Configuration directory")
	dataDir := flag.String("data", "", "Data directory")
	logDir := flag.String("log", "", "Log directory")
	pidFile := flag.String("pid", "", "PID file path")
	address := flag.String("address", "", "Listen address")
	port := flag.String("port", "", "Listen port")
	daemon := flag.Bool("daemon", false, "Daemonize (detach from terminal)")
	debug := flag.Bool("debug", false, "Enable debug mode")

	// Status check
	showStatus := flag.Bool("status", false, "Show service status")

	// Service management
	serviceCmd := flag.String("service", "", "Service command: start, restart, stop, reload, --install, --uninstall, --disable, --help")

	// Maintenance commands
	maintenanceCmd := flag.String("maintenance", "", "Maintenance command: backup, restore, update, mode, setup")

	// Update command
	updateCmd := flag.String("update", "", "Update command: check, yes, or branch {stable|beta|daily}")

	flag.Parse()

	// Handle help
	if *showHelp {
		printHelp(binaryName)
		os.Exit(0)
	}

	// Handle version
	if *showVersion {
		fmt.Printf("%s v%s\n", binaryName, Version)
		fmt.Printf("Built: %s\n", BuildTime)
		fmt.Printf("Go: %s\n", runtime.Version())
		fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Initialize paths early for commands that need them
	paths.Init(*configDir, *dataDir, *logDir)

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
		handleMaintenanceCommand(*maintenanceCmd, optionalArg, binaryName)
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

	// Initialize database
	if err := database.Init(paths.DataDir()); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Run database migrations
	if err := database.RunMigrations(); err != nil {
		log.Fatalf("Failed to run database migrations: %v", err)
	}

	// Set database for health checks
	handler.SetDatabase(database.GetServerDB())

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logging system
	if err := server.InitLogger(&cfg.Server.Logs); err != nil {
		log.Printf("Warning: Failed to initialize logging system: %v", err)
	}

	// Initialize GeoIP database (load if exists, or will download on first use)
	if err := geoip.Get().Load(paths.DataDir()); err != nil {
		log.Printf("Warning: Failed to load GeoIP database: %v (will auto-download on first request)", err)
	}

	// Override config with CLI flags (flags have highest priority)
	if *mode != "" {
		cfg.Server.Mode = *mode
	}
	if *address != "" {
		cfg.Server.Address = *address
	}
	if *port != "" {
		cfg.Server.Port = *port
	}

	// Set debug mode if flag provided
	if *debug {
		os.Setenv("DEBUG", "true")
	}

	// TODO: Handle --daemon flag (requires platform-specific fork/detach code)
	// TODO: Handle --pid flag (write PID file)
	_ = daemon
	_ = pidFile

	// Create server
	srv := server.New(cfg)

	// Initialize and start scheduler (if enabled in config)
	if cfg.Server.Schedule.Enabled {
		sched := scheduler.New()
		sched.RegisterDefaultTasks()
		sched.Start()
		defer sched.Stop()
		log.Println("✅ Scheduler started with default tasks")
	}

	// Start config file watcher for hot reload
	configWatcher := config.NewConfigWatcher(func(newCfg *config.Config) {
		log.Printf("🔄 Configuration reloaded")
		// Update global config - server will pick up changes via config.Get()
		config.Set(newCfg)
	})
	configWatcher.Start()
	defer configWatcher.Stop()

	// Channel to listen for errors
	errChan := make(chan error, 1)

	// Start server in goroutine
	go func() {
		printStartup(cfg, binaryName)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for interrupt signal or error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Handle signals
	for {
		select {
		case sig := <-quit:
			if sig == syscall.SIGHUP {
				log.Printf("🔄 SIGHUP received, reloading configuration...")
				if err := config.Reload(); err != nil {
					log.Printf("Failed to reload config: %v", err)
				} else {
					log.Printf("✅ Configuration reloaded")
				}
				continue
			}
			fmt.Println("\n🛑 Shutting down gracefully...")
		case err := <-errChan:
			log.Printf("Server error: %v", err)
		}
		break
	}

	// Graceful shutdown with 30 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	fmt.Println("✅ Server stopped")
}

func printHelp(binaryName string) {
	fmt.Printf(`%s - Universal API Toolkit

Usage: %s [options]

Server Options:
  --help, -h              Show this help message
  --version, -v           Show version information
  --status                Check service status
  --mode MODE             Set application mode (production|development)
  --config DIR            Set configuration directory
  --data DIR              Set data directory
  --log DIR               Set log directory
  --pid FILE              Set PID file path
  --address ADDR          Set listen address (default: 0.0.0.0)
  --port PORT             Set listen port (default: 64580)
  --daemon                Daemonize (detach from terminal)
  --debug                 Enable debug mode (verbose logging, debug endpoints)

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

Update Commands:
  --update check                 Check for available updates
  --update yes                   Download and install updates
  --update branch stable         Switch to stable channel
  --update branch beta           Switch to beta channel
  --update branch daily          Switch to daily channel

Environment Variables:
  API_MODE                Application mode
  API_CONFIG              Configuration directory
  API_DATA                Data directory
  API_LOG                 Log directory
  API_DEBUG               Enable debug mode

Signals:
  SIGHUP                  Reload configuration (auto via file watcher)
  SIGTERM/SIGINT          Graceful shutdown
  SIGUSR1                 Reopen logs (for log rotation)
  SIGUSR2                 Dump status to log

Documentation: https://apimgr-api.readthedocs.io
`, binaryName, binaryName)
}

func printStartup(cfg *config.Config, binaryName string) {
	fmt.Println()
	fmt.Printf("✅ %s v%s started successfully\n", binaryName, Version)
	fmt.Printf("📡 Listening on http://%s:%s\n", getDisplayAddress(cfg), cfg.Server.Port)
	fmt.Printf("📊 Swagger UI: http://%s:%s/openapi\n", getDisplayAddress(cfg), cfg.Server.Port)
	fmt.Printf("🔮 GraphQL: http://%s:%s/graphql\n", getDisplayAddress(cfg), cfg.Server.Port)
	fmt.Printf("📚 API Docs: http://%s:%s/api\n", getDisplayAddress(cfg), cfg.Server.Port)
	fmt.Printf("🔧 Admin Panel: http://%s:%s/admin\n", getDisplayAddress(cfg), cfg.Server.Port)
	fmt.Println()
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
	cfg, _ := config.Load()
	addr := fmt.Sprintf("http://localhost:%s/healthz", cfg.Server.Port)

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(addr)
	if err != nil {
		fmt.Println("❌ Service is not running")
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("✅ Service is running")
		fmt.Printf("   Port: %s\n", cfg.Server.Port)
		fmt.Printf("   Config: %s\n", config.GetConfigPath())
		os.Exit(0)
	}

	fmt.Println("⚠️ Service returned unexpected status")
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
		fmt.Printf("Unknown service command: %s\n", cmd)
		fmt.Println("Valid commands: start, stop, restart, reload, --install, --uninstall, --disable, --help")
		os.Exit(1)
	}
}

func installService(binaryName string) {
	if runtime.GOOS != "linux" {
		fmt.Println("❌ Service installation is only supported on Linux")
		os.Exit(1)
	}

	execPath, err := os.Executable()
	if err != nil {
		fmt.Printf("❌ Failed to get executable path: %v\n", err)
		os.Exit(1)
	}

	serviceName := "api"
	serviceContent := fmt.Sprintf(`[Unit]
Description=API - Universal API Toolkit
After=network.target

[Service]
Type=simple
User=root
ExecStart=%s
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, execPath)

	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		fmt.Printf("❌ Failed to write service file: %v\n", err)
		os.Exit(1)
	}

	// Reload systemd
	exec.Command("systemctl", "daemon-reload").Run()

	fmt.Println("✅ Service installed successfully")
	fmt.Printf("   Run '%s --service start' to start the service\n", binaryName)
	fmt.Printf("   Run 'systemctl enable %s' to start on boot\n", serviceName)
}

func uninstallService(binaryName string) {
	if runtime.GOOS != "linux" {
		fmt.Println("❌ Service uninstallation is only supported on Linux")
		os.Exit(1)
	}

	serviceName := "api"

	// Stop the service first
	exec.Command("systemctl", "stop", serviceName).Run()
	exec.Command("systemctl", "disable", serviceName).Run()

	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("❌ Failed to remove service file: %v\n", err)
		os.Exit(1)
	}

	exec.Command("systemctl", "daemon-reload").Run()

	fmt.Println("✅ Service uninstalled successfully")
}

func startService(binaryName string) {
	if runtime.GOOS != "linux" {
		fmt.Println("❌ Service management is only supported on Linux")
		os.Exit(1)
	}

	serviceName := "api"
	if err := exec.Command("systemctl", "start", serviceName).Run(); err != nil {
		fmt.Printf("❌ Failed to start service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Service started")
}

func stopService(binaryName string) {
	if runtime.GOOS != "linux" {
		fmt.Println("❌ Service management is only supported on Linux")
		os.Exit(1)
	}

	serviceName := "api"
	if err := exec.Command("systemctl", "stop", serviceName).Run(); err != nil {
		fmt.Printf("❌ Failed to stop service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Service stopped")
}

func restartService(binaryName string) {
	if runtime.GOOS != "linux" {
		fmt.Println("❌ Service management is only supported on Linux")
		os.Exit(1)
	}

	serviceName := "api"
	if err := exec.Command("systemctl", "restart", serviceName).Run(); err != nil {
		fmt.Printf("❌ Failed to restart service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Service restarted")
}

func reloadService(binaryName string) {
	if runtime.GOOS != "linux" {
		fmt.Println("❌ Service management is only supported on Linux")
		os.Exit(1)
	}

	serviceName := "api"
	if err := exec.Command("systemctl", "reload-or-restart", serviceName).Run(); err != nil {
		fmt.Printf("❌ Failed to reload service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Configuration reloaded")
}

func disableService(binaryName string) {
	if runtime.GOOS != "linux" {
		fmt.Println("❌ Service management is only supported on Linux")
		os.Exit(1)
	}

	serviceName := "api"
	if err := exec.Command("systemctl", "disable", serviceName).Run(); err != nil {
		fmt.Printf("❌ Failed to disable service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Service disabled (will not start on boot)")
}

func printServiceHelp(binaryName string) {
	fmt.Printf(`Service Management

Available service commands:
  %s --service start         Start the service
  %s --service stop          Stop the service
  %s --service restart       Restart the service
  %s --service reload        Reload configuration
  %s --service --install     Install as system service
  %s --service --uninstall   Uninstall system service
  %s --service --disable     Disable auto-start on boot
  %s --service --help        Show this help

Note: Service commands require root/administrator privileges.
`, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName)
}

// Maintenance commands
func handleMaintenanceCommand(cmd string, optionalArg string, binaryName string) {
	switch strings.ToLower(cmd) {
	case "backup":
		backupPath := optionalArg
		if backupPath == "" {
			backupPath = filepath.Join(paths.DataDir(), "backup", fmt.Sprintf("backup-%s.json", time.Now().Format("20060102-150405")))
		}
		handleBackup(backupPath, binaryName)

	case "restore":
		if optionalArg == "" {
			fmt.Println("❌ Restore requires a backup file path")
			fmt.Printf("   Usage: %s --maintenance restore /path/to/backup.json\n", binaryName)
			os.Exit(1)
		}
		handleRestore(optionalArg, binaryName)

	case "update":
		if optionalArg == "" {
			fmt.Println("❌ Update requires a setting name and value")
			fmt.Printf("   Usage: %s --maintenance update setting_name value\n", binaryName)
			os.Exit(1)
		}
		fmt.Printf("⚠️ Configuration update via CLI not yet implemented\n")
		fmt.Printf("   Use the admin panel at /admin to update settings\n")

	case "mode":
		if optionalArg == "" {
			fmt.Println("❌ Mode change requires a mode value")
			fmt.Printf("   Usage: %s --maintenance mode {production|development}\n", binaryName)
			os.Exit(1)
		}
		handleModeChange(optionalArg, binaryName)

	case "setup":
		fmt.Printf("⚠️ Setup wizard is available at the web interface\n")
		fmt.Printf("   Visit http://localhost:64580/admin/setup\n")

	default:
		fmt.Printf("Unknown maintenance command: %s\n", cmd)
		fmt.Println("Valid commands: backup, restore, update, mode, setup")
		os.Exit(1)
	}
}

// Update handling
func handleUpdateCommand(cmd string, optionalArg string, binaryName string) {
	switch strings.ToLower(cmd) {
	case "check":
		fmt.Println("🔍 Checking for updates...")
		fmt.Printf("   Current version: %s\n", Version)
		fmt.Println("   ℹ️ Update checking requires internet connectivity")
		fmt.Println("   ℹ️ Check https://github.com/apimgr/api/releases for latest version")

	case "yes":
		fmt.Println("🔍 Checking for updates...")
		fmt.Printf("   Current version: %s\n", Version)
		fmt.Println("\n⚠️ Automatic updates not yet implemented")
		fmt.Println("   Please download the latest release manually from:")
		fmt.Println("   https://github.com/apimgr/api/releases/latest")

	case "branch":
		if optionalArg == "" {
			fmt.Println("❌ Branch command requires a channel argument")
			fmt.Printf("   Usage: %s --update branch {stable|beta|daily}\n", binaryName)
			os.Exit(1)
		}
		switch optionalArg {
		case "stable", "beta", "daily":
			fmt.Printf("✅ Update channel set to: %s\n", optionalArg)
			fmt.Println("   This setting will be used for future update checks")
			// TODO: Store update channel preference in config
		default:
			fmt.Printf("❌ Unknown update channel: %s\n", optionalArg)
			fmt.Println("   Valid channels: stable, beta, daily")
			os.Exit(1)
		}

	default:
		fmt.Printf("Unknown update command: %s\n", cmd)
		fmt.Printf("Usage: %s --update {check|yes|branch <channel>}\n", binaryName)
		os.Exit(1)
	}
}

// Mode change handling
func handleModeChange(newMode string, binaryName string) {
	switch strings.ToLower(newMode) {
	case "production", "development":
		fmt.Printf("✅ Mode will be set to: %s\n", newMode)
		fmt.Println("   Mode change requires server restart to take effect")
		// TODO: Update config file with new mode
	default:
		fmt.Printf("❌ Unknown mode: %s\n", newMode)
		fmt.Println("   Valid modes: production, development")
		os.Exit(1)
	}
}

// Backup handling
func handleBackup(backupPath string, binaryName string) {
	fmt.Printf("📦 Creating backup to: %s\n", backupPath)

	// Create backup directory
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		fmt.Printf("❌ Failed to create backup directory: %v\n", err)
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
		fmt.Printf("❌ Failed to create backup data: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		fmt.Printf("❌ Failed to write backup file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Backup created successfully")
	fmt.Printf("   Config: %s\n", config.GetConfigPath())
	fmt.Printf("   Data: %s\n", paths.DataDir())
}

// Restore handling
func handleRestore(restorePath string, binaryName string) {
	fmt.Printf("📥 Restoring from: %s\n", restorePath)

	// Read backup file
	data, err := os.ReadFile(restorePath)
	if err != nil {
		fmt.Printf("❌ Failed to read backup file: %v\n", err)
		os.Exit(1)
	}

	var backupData map[string]interface{}
	if err := json.Unmarshal(data, &backupData); err != nil {
		fmt.Printf("❌ Invalid backup file format: %v\n", err)
		os.Exit(1)
	}

	// Validate backup
	if _, ok := backupData["version"]; !ok {
		fmt.Println("❌ Invalid backup file: missing version")
		os.Exit(1)
	}

	fmt.Printf("   Backup version: %v\n", backupData["version"])
	fmt.Printf("   Created: %v\n", backupData["created_at"])

	// Restore config if present
	if cfgData, ok := backupData["config"]; ok && cfgData != nil {
		cfgBytes, _ := json.Marshal(cfgData)
		var cfg config.Config
		if err := json.Unmarshal(cfgBytes, &cfg); err == nil {
			if err := config.Save(&cfg); err != nil {
				fmt.Printf("⚠️ Failed to restore config: %v\n", err)
			} else {
				fmt.Println("✅ Configuration restored")
			}
		}
	}

	fmt.Println("✅ Restore completed")
	fmt.Println("   Restart the service to apply changes")
}

// Generate secure password
