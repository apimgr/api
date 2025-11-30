package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/server"
)

var (
	Version   = "1.0.0"
	BuildTime = "unknown"
)

func main() {
	// CLI flags
	showHelp := flag.Bool("help", false, "Show help")
	showVersion := flag.Bool("version", false, "Show version")
	showStatus := flag.Bool("status", false, "Show service status")
	configDir := flag.String("config", "", "Configuration directory")
	dataDir := flag.String("data", "", "Data directory")
	address := flag.String("address", "", "Listen address")
	port := flag.String("port", "", "Listen port")

	flag.Parse()

	// Handle help
	if *showHelp {
		printHelp()
		os.Exit(0)
	}

	// Handle version
	if *showVersion {
		fmt.Printf("CasTools v%s (built: %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	// Handle status check
	if *showStatus {
		checkStatus()
		os.Exit(0)
	}

	// Initialize paths
	paths.Init(*configDir, *dataDir)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Override config with CLI flags
	if *address != "" {
		cfg.Server.Address = *address
	}
	if *port != "" {
		cfg.Server.Port = *port
	}

	// Create server
	srv := server.New(cfg)

	// Channel to listen for errors
	errChan := make(chan error, 1)

	// Start server in goroutine
	go func() {
		printStartup(cfg)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for interrupt signal or error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		fmt.Println("\n🛑 Shutting down gracefully...")
	case err := <-errChan:
		log.Printf("Server error: %v", err)
	}

	// Graceful shutdown with 30 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	fmt.Println("✅ Server stopped")
}

func printHelp() {
	fmt.Println(`CasTools - Universal API Toolkit

Usage: api [options]

Options:
  --help          Show this help message
  --version       Show version information
  --status        Check service status
  --config DIR    Set configuration directory
  --data DIR      Set data directory
  --address ADDR  Set listen address (default: 0.0.0.0)
  --port PORT     Set listen port (default: auto)

Features:
  - Text utilities (UUID, hash, encode/decode, lorem ipsum)
  - Cryptography tools (bcrypt, TOTP, password generation)
  - Network utilities (IP info, DNS lookup)
  - Date/Time tools (timestamp conversion, timezone)

Documentation: https://api.apimgr.us`)
}

func printStartup(cfg *config.Config) {
	fmt.Println()
	fmt.Printf("✅ CasTools v%s started successfully\n", Version)
	fmt.Printf("📡 Listening on http://%s:%s\n", getDisplayAddress(cfg), cfg.Server.Port)
	fmt.Printf("📊 Swagger UI: http://%s:%s/swagger\n", getDisplayAddress(cfg), cfg.Server.Port)
	fmt.Printf("📚 API Docs: http://%s:%s/api\n", getDisplayAddress(cfg), cfg.Server.Port)
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
		os.Exit(0)
	}

	fmt.Println("⚠️ Service returned unexpected status")
	os.Exit(2)
}
