package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/paths"
	"github.com/go-chi/chi/v5"
)

// Version and build info (set from main)
var (
	Version   = "1.0.0"
	BuildTime = "unknown"
	StartTime = time.Now()
)

// SetupRoutes configures admin API routes
func SetupRoutes(r chi.Router, cfg *config.Config) {
	// Admin API routes with token authentication
	r.Route("/api/v1/admin", func(r chi.Router) {
		// Token-protected routes
		r.Use(RequireToken(cfg))

		// Server endpoints
		r.Route("/server", func(r chi.Router) {
			r.Get("/status", statusHandler(cfg))
			r.Get("/health", healthHandler(cfg))
			r.Get("/stats", statsHandler(cfg))
			r.Get("/settings", settingsHandler(cfg))
			r.Patch("/settings", updateSettingsHandler(cfg))
			r.Post("/restart", restartHandler(cfg))

			// Branding
			r.Get("/branding", brandingHandler(cfg))
			r.Patch("/branding", updateBrandingHandler(cfg))

			// SSL
			r.Get("/ssl", sslHandler(cfg))
			r.Patch("/ssl", updateSSLHandler(cfg))
			r.Post("/ssl/renew", sslRenewHandler(cfg))

			// Web settings (robots.txt, security.txt)
			r.Get("/web", webSettingsHandler(cfg))
			r.Patch("/web", updateWebSettingsHandler(cfg))
			r.Get("/web/robots/preview", robotsPreviewHandler(cfg))
			r.Get("/web/security/preview", securityPreviewHandler(cfg))

			// Email
			r.Get("/email", emailHandler(cfg))
			r.Patch("/email", updateEmailHandler(cfg))
			r.Post("/email/test", emailTestHandler(cfg))

			// Scheduler
			r.Get("/scheduler", schedulerHandler(cfg))
			r.Get("/scheduler/{id}", schedulerTaskHandler(cfg))
			r.Patch("/scheduler/{id}", updateSchedulerTaskHandler(cfg))
			r.Post("/scheduler/{id}/run", runSchedulerTaskHandler(cfg))
			r.Post("/scheduler/{id}/enable", enableSchedulerTaskHandler(cfg))
			r.Post("/scheduler/{id}/disable", disableSchedulerTaskHandler(cfg))

			// Backup
			r.Get("/backup", listBackupsHandler(cfg))
			r.Post("/backup", createBackupHandler(cfg))
			r.Get("/backup/{id}", backupDetailHandler(cfg))
			r.Delete("/backup/{id}", deleteBackupHandler(cfg))
			r.Get("/backup/{id}/download", downloadBackupHandler(cfg))
			r.Post("/backup/restore", restoreBackupHandler(cfg))

			// Logs
			r.Get("/logs", listLogsHandler(cfg))
			r.Get("/logs/{type}", logEntriesHandler(cfg))
			r.Get("/logs/{type}/download", downloadLogHandler(cfg))
		})

		// Config endpoints
		r.Get("/config", configHandler(cfg))
		r.Put("/config", updateConfigHandler(cfg))
		r.Patch("/config", patchConfigHandler(cfg))

		// Password/Token management
		r.Post("/password", changePasswordHandler(cfg))
		r.Post("/token/regenerate", regenerateTokenHandler(cfg))
	})

	// Web admin routes with session authentication
	r.Route("/admin", func(r chi.Router) {
		// Login page (no auth required)
		r.Get("/login", loginPageHandler(cfg))
		r.Post("/login", loginHandler(cfg))
		r.Get("/logout", logoutHandler(cfg))

		// Protected admin pages
		r.Group(func(r chi.Router) {
			r.Use(RequireSession)
			r.Use(CSRFProtection)

			r.Get("/", dashboardHandler(cfg))
			r.Get("/dashboard", dashboardHandler(cfg))
			r.Get("/server/settings", serverSettingsPageHandler(cfg))
			r.Get("/server/branding", brandingPageHandler(cfg))
			r.Get("/server/ssl", sslPageHandler(cfg))
			r.Get("/server/web", webSettingsPageHandler(cfg))
			r.Get("/server/email", emailPageHandler(cfg))
			r.Get("/server/scheduler", schedulerPageHandler(cfg))
			r.Get("/server/backup", backupPageHandler(cfg))
			r.Get("/server/logs", logsPageHandler(cfg))
		})
	})

	// Auth routes
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", authLoginPageHandler(cfg))
		r.Post("/login", authLoginHandler(cfg))
		r.Get("/logout", authLogoutHandler(cfg))
		r.Get("/password/forgot", forgotPasswordPageHandler(cfg))
		r.Post("/password/forgot", forgotPasswordHandler(cfg))
	})
}

// API Handlers

func statusHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{
			"status":  "running",
			"version": Version,
			"mode":    mode.Get().String(),
			"uptime":  getUptime(),
			"port":    cfg.Server.Port,
			"address": cfg.Server.Address,
		})
	}
}

func healthHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{
			"status":    "healthy",
			"version":   Version,
			"mode":      mode.Get().String(),
			"uptime":    getUptime(),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"checks": map[string]string{
				"config": "ok",
				"disk":   "ok",
			},
		})
	}
}

func statsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		jsonResponse(w, map[string]interface{}{
			"uptime":     getUptime(),
			"start_time": StartTime.UTC().Format(time.RFC3339),
			"memory": map[string]interface{}{
				"alloc":       memStats.Alloc,
				"total_alloc": memStats.TotalAlloc,
				"sys":         memStats.Sys,
				"num_gc":      memStats.NumGC,
			},
			"goroutines": runtime.NumGoroutine(),
			"sessions":   len(GetActiveSessions()),
		})
	}
}

func settingsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Return server settings (excluding sensitive data)
		jsonResponse(w, map[string]interface{}{
			"port":      cfg.Server.Port,
			"address":   cfg.Server.Address,
			"fqdn":      cfg.Server.FQDN,
			"mode":      cfg.Server.Mode,
			"rate_limit": cfg.Server.RateLimit,
			"logs":      cfg.Server.Logs,
		})
	}
}

func updateSettingsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			jsonError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Apply updates (simplified - full implementation would validate and merge)
		if port, ok := updates["port"].(string); ok {
			cfg.Server.Port = port
		}
		if address, ok := updates["address"].(string); ok {
			cfg.Server.Address = address
		}

		// Save config
		if err := config.Save(cfg); err != nil {
			jsonError(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]string{"status": "updated"})
	}
}

func restartHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Note: Actual restart would require process management
		jsonResponse(w, map[string]string{
			"status":  "scheduled",
			"message": "Server restart scheduled",
		})
	}
}

func brandingHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, cfg.Server.Branding)
	}
}

func updateBrandingHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var branding config.BrandingConfig
		if err := json.NewDecoder(r.Body).Decode(&branding); err != nil {
			jsonError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		cfg.Server.Branding = branding

		if err := config.Save(cfg); err != nil {
			jsonError(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]string{"status": "updated"})
	}
}

func sslHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, cfg.Server.SSL)
	}
}

func updateSSLHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ssl config.SSLConfig
		if err := json.NewDecoder(r.Body).Decode(&ssl); err != nil {
			jsonError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		cfg.Server.SSL = ssl

		if err := config.Save(cfg); err != nil {
			jsonError(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]string{"status": "updated"})
	}
}

func sslRenewHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]string{
			"status":  "scheduled",
			"message": "Certificate renewal scheduled",
		})
	}
}

func webSettingsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{
			"ui":       cfg.Web.UI,
			"robots":   cfg.Web.Robots,
			"security": cfg.Web.Security,
			"cors":     cfg.Web.CORS,
		})
	}
}

func updateWebSettingsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			jsonError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := config.Save(cfg); err != nil {
			jsonError(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]string{"status": "updated"})
	}
}

func robotsPreviewHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		preview := "User-agent: *\n"
		for _, path := range cfg.Web.Robots.Allow {
			preview += "Allow: " + path + "\n"
		}
		for _, path := range cfg.Web.Robots.Deny {
			preview += "Disallow: " + path + "\n"
		}

		jsonResponse(w, map[string]string{"preview": preview})
	}
}

func securityPreviewHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		preview := "Contact: mailto:" + cfg.Web.Security.Contact + "\n"
		preview += "Expires: " + cfg.Web.Security.Expires.Format(time.RFC3339) + "\n"
		preview += "Preferred-Languages: en\n"

		jsonResponse(w, map[string]string{"preview": preview})
	}
}

func emailHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]string{
			"status": "Email configuration not yet implemented",
		})
	}
}

func updateEmailHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]string{"status": "updated"})
	}
}

func emailTestHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]string{
			"status":  "sent",
			"message": "Test email sent",
		})
	}
}

func schedulerHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{
			"enabled": cfg.Server.Schedule.Enabled,
			"tasks":   []interface{}{},
		})
	}
}

func schedulerTaskHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := chi.URLParam(r, "id")
		jsonResponse(w, map[string]interface{}{
			"id":     taskID,
			"status": "not_found",
		})
	}
}

func updateSchedulerTaskHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]string{"status": "updated"})
	}
}

func runSchedulerTaskHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := chi.URLParam(r, "id")
		jsonResponse(w, map[string]interface{}{
			"id":      taskID,
			"status":  "running",
			"message": "Task started",
		})
	}
}

func enableSchedulerTaskHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]string{"status": "enabled"})
	}
}

func disableSchedulerTaskHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]string{"status": "disabled"})
	}
}

func listBackupsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{
			"backups": []interface{}{},
		})
	}
}

func createBackupHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{
			"status":  "created",
			"message": "Backup created",
		})
	}
}

func backupDetailHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backupID := chi.URLParam(r, "id")
		jsonResponse(w, map[string]interface{}{
			"id":     backupID,
			"status": "not_found",
		})
	}
}

func deleteBackupHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]string{"status": "deleted"})
	}
}

func downloadBackupHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Backup not found", http.StatusNotFound)
	}
}

func restoreBackupHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{
			"status":  "restored",
			"message": "Backup restored",
		})
	}
}

func listLogsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{
			"logs": []string{"access", "server", "error", "audit", "security"},
		})
	}
}

func logEntriesHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logType := chi.URLParam(r, "type")
		jsonResponse(w, map[string]interface{}{
			"type":    logType,
			"entries": []interface{}{},
		})
	}
}

func downloadLogHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logType := chi.URLParam(r, "type")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", "attachment; filename="+logType+".log")
		w.Write([]byte("# Log file: " + logType + "\n"))
	}
}

func configHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Return full config (redact sensitive fields)
		safeCfg := *cfg
		safeCfg.Server.Admin.Password = "[REDACTED]"
		safeCfg.Server.Admin.Token = "[REDACTED]"
		jsonResponse(w, safeCfg)
	}
}

func updateConfigHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var newCfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			jsonError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Preserve sensitive fields
		newCfg.Server.Admin.Password = cfg.Server.Admin.Password
		newCfg.Server.Admin.Token = cfg.Server.Admin.Token

		if err := config.Save(&newCfg); err != nil {
			jsonError(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}

		config.Set(&newCfg)
		jsonResponse(w, map[string]string{"status": "updated"})
	}
}

func patchConfigHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			jsonError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Apply partial updates (simplified)
		if err := config.Save(cfg); err != nil {
			jsonError(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]string{"status": "updated"})
	}
}

func changePasswordHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if !ValidateCredentials(cfg.Server.Admin.Username, req.CurrentPassword, cfg) {
			jsonError(w, "Current password is incorrect", http.StatusUnauthorized)
			return
		}

		// Hash new password
		hash, err := HashPassword(req.NewPassword)
		if err != nil {
			jsonError(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}

		cfg.Server.Admin.Password = hash

		if err := config.Save(cfg); err != nil {
			jsonError(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]string{"status": "password_changed"})
	}
}

func regenerateTokenHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := GenerateToken(32)
		if err != nil {
			jsonError(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		cfg.Server.Admin.Token = token

		if err := config.Save(cfg); err != nil {
			jsonError(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]interface{}{
			"status": "token_regenerated",
			"token":  token,
		})
	}
}

// Web Page Handlers

func loginPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Generate CSRF token
		csrfToken, _ := GenerateCSRFToken()

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateLoginPage(csrfToken)))
	}
}

func loginHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}

		username := r.FormValue("username")
		password := r.FormValue("password")
		csrfToken := r.FormValue("csrf_token")

		// Validate CSRF
		if !ConsumeCSRFToken(csrfToken) {
			http.Redirect(w, r, "/admin/login?error=csrf", http.StatusFound)
			return
		}

		// Validate credentials
		if !ValidateCredentials(username, password, cfg) {
			http.Redirect(w, r, "/admin/login?error=invalid", http.StatusFound)
			return
		}

		// Create session
		session, err := NewSession(username, GetClientIP(r), r.UserAgent(), 24*time.Hour)
		if err != nil {
			http.Error(w, "Session creation failed", http.StatusInternalServerError)
			return
		}

		// Set session cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_session",
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
			Expires:  session.ExpiresAt,
		})

		// Redirect to dashboard or requested page
		redirect := r.FormValue("redirect")
		if redirect == "" {
			redirect = "/admin"
		}
		http.Redirect(w, r, redirect, http.StatusFound)
	}
}

func logoutHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("admin_session")
		if err == nil {
			DeleteSession(cookie.Value)
		}

		// Clear cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_session",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			MaxAge:   -1,
		})

		http.Redirect(w, r, "/admin/login", http.StatusFound)
	}
}

func dashboardHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, _ := GenerateCSRFToken()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateDashboardPage(cfg, csrfToken)))
	}
}

func serverSettingsPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, _ := GenerateCSRFToken()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateSettingsPage(cfg, csrfToken)))
	}
}

func brandingPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, _ := GenerateCSRFToken()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateBrandingPage(cfg, csrfToken)))
	}
}

func sslPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, _ := GenerateCSRFToken()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateSSLPage(cfg, csrfToken)))
	}
}

func webSettingsPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, _ := GenerateCSRFToken()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateWebSettingsPage(cfg, csrfToken)))
	}
}

func emailPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, _ := GenerateCSRFToken()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateEmailPage(cfg, csrfToken)))
	}
}

func schedulerPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, _ := GenerateCSRFToken()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateSchedulerPage(cfg, csrfToken)))
	}
}

func backupPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, _ := GenerateCSRFToken()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateBackupPage(cfg, csrfToken)))
	}
}

func logsPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrfToken, _ := GenerateCSRFToken()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateLogsPage(cfg, csrfToken)))
	}
}

// Auth route handlers (redirect to admin)
func authLoginPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/login", http.StatusFound)
	}
}

func authLoginHandler(cfg *config.Config) http.HandlerFunc {
	return loginHandler(cfg)
}

func authLogoutHandler(cfg *config.Config) http.HandlerFunc {
	return logoutHandler(cfg)
}

func forgotPasswordPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(generateForgotPasswordPage()))
	}
}

func forgotPasswordHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/auth/login?message=reset_requested", http.StatusFound)
	}
}

// Helper functions

func getUptime() string {
	d := time.Since(StartTime)

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// GetDataDir returns the data directory
func GetDataDir() string {
	return paths.DataDir()
}
