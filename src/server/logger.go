package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
)

// Logger handles all logging operations
type Logger struct {
	accessLog   *log.Logger
	serverLog   *log.Logger
	errorLog    *log.Logger
	auditLog    *log.Logger
	securityLog *log.Logger
	debugLog    *log.Logger
	config      *config.LogsConfig
}

// NewLogger creates a new logger instance
func NewLogger(cfg *config.LogsConfig) (*Logger, error) {
	logDir := paths.LogDir()

	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logger := &Logger{
		config: cfg,
	}

	// Initialize access log
	if err := logger.initAccessLog(logDir); err != nil {
		return nil, err
	}

	// Initialize server log
	if err := logger.initServerLog(logDir); err != nil {
		return nil, err
	}

	// Initialize error log
	if err := logger.initErrorLog(logDir); err != nil {
		return nil, err
	}

	// Initialize audit log
	if err := logger.initAuditLog(logDir); err != nil {
		return nil, err
	}

	// Initialize security log
	if err := logger.initSecurityLog(logDir); err != nil {
		return nil, err
	}

	// Initialize debug log (if enabled)
	if cfg.Debug.Enabled {
		if err := logger.initDebugLog(logDir); err != nil {
			return nil, err
		}
	}

	return logger, nil
}

// initAccessLog initializes the access log
func (l *Logger) initAccessLog(logDir string) error {
	logPath := filepath.Join(logDir, l.config.Access.Filename)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open access log: %w", err)
	}

	l.accessLog = log.New(f, "", 0)
	return nil
}

// initServerLog initializes the server log
func (l *Logger) initServerLog(logDir string) error {
	logPath := filepath.Join(logDir, l.config.Server.Filename)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open server log: %w", err)
	}

	l.serverLog = log.New(f, "", 0)
	return nil
}

// initErrorLog initializes the error log
func (l *Logger) initErrorLog(logDir string) error {
	logPath := filepath.Join(logDir, l.config.Error.Filename)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open error log: %w", err)
	}

	l.errorLog = log.New(f, "", 0)
	return nil
}

// initAuditLog initializes the audit log (JSON only)
func (l *Logger) initAuditLog(logDir string) error {
	logPath := filepath.Join(logDir, l.config.Audit.Filename)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open audit log: %w", err)
	}

	l.auditLog = log.New(f, "", 0)
	return nil
}

// initSecurityLog initializes the security log
func (l *Logger) initSecurityLog(logDir string) error {
	logPath := filepath.Join(logDir, l.config.Security.Filename)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open security log: %w", err)
	}

	l.securityLog = log.New(f, "", 0)
	return nil
}

// initDebugLog initializes the debug log
func (l *Logger) initDebugLog(logDir string) error {
	logPath := filepath.Join(logDir, l.config.Debug.Filename)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open debug log: %w", err)
	}

	l.debugLog = log.New(f, "", 0)
	return nil
}

// LogAccess logs HTTP access in the specified format
func (l *Logger) LogAccess(r *http.Request, status int, size int, duration time.Duration) {
	if l.accessLog == nil {
		return
	}

	switch l.config.Access.Format {
	case "apache", "combined":
		// Apache Combined Log Format
		// IP - - [datetime] "METHOD path HTTP/version" status size "referer" "user-agent"
		timestamp := time.Now().Format("02/Jan/2006:15:04:05 -0700")
		referer := r.Header.Get("Referer")
		if referer == "" {
			referer = "-"
		}
		userAgent := r.Header.Get("User-Agent")
		if userAgent == "" {
			userAgent = "-"
		}

		logLine := fmt.Sprintf("%s - - [%s] \"%s %s %s\" %d %d \"%s\" \"%s\"",
			r.RemoteAddr,
			timestamp,
			r.Method,
			r.URL.Path,
			r.Proto,
			status,
			size,
			referer,
			userAgent,
		)
		l.accessLog.Println(logLine)

	case "nginx":
		// Nginx Common Log Format
		timestamp := time.Now().Format("02/Jan/2006:15:04:05 -0700")
		logLine := fmt.Sprintf("%s - - [%s] \"%s %s %s\" %d %d",
			r.RemoteAddr,
			timestamp,
			r.Method,
			r.URL.Path,
			r.Proto,
			status,
			size,
		)
		l.accessLog.Println(logLine)

	case "json":
		// Structured JSON format
		entry := map[string]interface{}{
			"time":       time.Now().UTC().Format(time.RFC3339),
			"ip":         r.RemoteAddr,
			"method":     r.Method,
			"path":       r.URL.Path,
			"query":      r.URL.RawQuery,
			"status":     status,
			"size":       size,
			"latency_ms": duration.Milliseconds(),
			"ua":         r.Header.Get("User-Agent"),
			"referer":    r.Header.Get("Referer"),
			"request_id": r.Header.Get("X-Request-ID"),
		}
		data, _ := json.Marshal(entry)
		l.accessLog.Println(string(data))

	case "custom":
		// Custom format using variables
		logLine := l.formatCustom(l.config.Access.Custom, r, status, size, duration)
		l.accessLog.Println(logLine)
	}
}

// LogServer logs application events
func (l *Logger) LogServer(level, message string) {
	if l.serverLog == nil {
		return
	}

	switch l.config.Server.Format {
	case "text":
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		l.serverLog.Printf("%s [%s] %s", timestamp, level, message)

	case "json":
		entry := map[string]interface{}{
			"time":  time.Now().UTC().Format(time.RFC3339),
			"level": level,
			"msg":   message,
		}
		data, _ := json.Marshal(entry)
		l.serverLog.Println(string(data))
	}
}

// LogError logs error messages
func (l *Logger) LogError(err error, context map[string]interface{}) {
	if l.errorLog == nil {
		return
	}

	switch l.config.Error.Format {
	case "text":
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		l.errorLog.Printf("%s [ERROR] %s", timestamp, err.Error())

	case "json":
		entry := map[string]interface{}{
			"time":  time.Now().UTC().Format(time.RFC3339),
			"level": "ERROR",
			"error": err.Error(),
		}
		// Merge context
		for k, v := range context {
			entry[k] = v
		}
		data, _ := json.Marshal(entry)
		l.errorLog.Println(string(data))
	}
}

// LogAudit logs audit events (JSON only)
func (l *Logger) LogAudit(event string, details map[string]interface{}) {
	if l.auditLog == nil || !l.config.Audit.Enabled {
		return
	}

	// Audit log is ALWAYS JSON
	entry := map[string]interface{}{
		"time":  time.Now().UTC().Format(time.RFC3339),
		"event": event,
	}

	// Merge details
	for k, v := range details {
		entry[k] = v
	}

	data, _ := json.Marshal(entry)
	l.auditLog.Println(string(data))
}

// LogSecurity logs security events
func (l *Logger) LogSecurity(event string, ip string, details map[string]interface{}) {
	if l.securityLog == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")

	switch l.config.Security.Format {
	case "fail2ban":
		// Fail2ban compatible format
		l.securityLog.Printf("%s [security] %s from %s", timestamp, event, ip)

	case "syslog":
		// RFC 5424 syslog format
		hostname, _ := os.Hostname()
		l.securityLog.Printf("<%d>1 %s %s api - - - %s ip=%s",
			14, // facility=user, severity=info
			time.Now().UTC().Format(time.RFC3339),
			hostname,
			event,
			ip,
		)

	case "json":
		entry := map[string]interface{}{
			"time":  time.Now().UTC().Format(time.RFC3339),
			"event": event,
			"ip":    ip,
		}
		for k, v := range details {
			entry[k] = v
		}
		data, _ := json.Marshal(entry)
		l.securityLog.Println(string(data))

	case "text":
		l.securityLog.Printf("%s [SECURITY] %s from %s", timestamp, event, ip)
	}
}

// LogDebug logs debug messages (only if debug enabled)
func (l *Logger) LogDebug(message string, context map[string]interface{}) {
	if l.debugLog == nil || !l.config.Debug.Enabled {
		return
	}

	switch l.config.Debug.Format {
	case "text":
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		l.debugLog.Printf("%s [DEBUG] %s", timestamp, message)

	case "json":
		entry := map[string]interface{}{
			"time":  time.Now().UTC().Format(time.RFC3339),
			"level": "DEBUG",
			"msg":   message,
		}
		for k, v := range context {
			entry[k] = v
		}
		data, _ := json.Marshal(entry)
		l.debugLog.Println(string(data))
	}
}

// formatCustom formats a custom log line using variables
func (l *Logger) formatCustom(format string, r *http.Request, status int, size int, duration time.Duration) string {
	// Replace variables with actual values
	result := format
	replacements := map[string]string{
		"{time}":       time.Now().Format("15:04:05"),
		"{date}":       time.Now().Format("2006-01-02"),
		"{datetime}":   time.Now().Format("2006-01-02 15:04:05"),
		"{remote_ip}":  r.RemoteAddr,
		"{method}":     r.Method,
		"{path}":       r.URL.Path,
		"{query}":      r.URL.RawQuery,
		"{status}":     fmt.Sprintf("%d", status),
		"{bytes}":      fmt.Sprintf("%d", size),
		"{latency}":    duration.String(),
		"{latency_ms}": fmt.Sprintf("%d", duration.Milliseconds()),
		"{user_agent}": r.Header.Get("User-Agent"),
		"{referer}":    r.Header.Get("Referer"),
		"{request_id}": r.Header.Get("X-Request-ID"),
		"{fqdn}":       r.Host,
		"{protocol}":   r.Proto,
	}

	for key, value := range replacements {
		result = strings.Replace(result, key, value, -1)
	}

	return result
}

// Global logger instance
var globalLogger *Logger

// InitLogger initializes the global logger
func InitLogger(cfg *config.LogsConfig) error {
	logger, err := NewLogger(cfg)
	if err != nil {
		return err
	}
	globalLogger = logger
	return nil
}

// GetLogger returns the global logger instance
func GetLogger() *Logger {
	return globalLogger
}
