package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"golang.org/x/crypto/bcrypt"
)

// Session represents an admin session
type Session struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
}

// SessionStore manages admin sessions
type SessionStore struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// Global session store
var sessions = &SessionStore{
	sessions: make(map[string]*Session),
}

// NewSession creates a new admin session
func NewSession(username, ip, userAgent string, duration time.Duration) (*Session, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	session := &Session{
		ID:        sessionID,
		Username:  username,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(duration),
		IP:        ip,
		UserAgent: userAgent,
	}

	sessions.mu.Lock()
	sessions.sessions[sessionID] = session
	sessions.mu.Unlock()

	return session, nil
}

// GetSession retrieves a session by ID
func GetSession(sessionID string) *Session {
	sessions.mu.RLock()
	defer sessions.mu.RUnlock()

	session, ok := sessions.sessions[sessionID]
	if !ok {
		return nil
	}

	// Check expiration
	if time.Now().After(session.ExpiresAt) {
		return nil
	}

	return session
}

// DeleteSession removes a session
func DeleteSession(sessionID string) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	delete(sessions.sessions, sessionID)
}

// CleanExpiredSessions removes all expired sessions
func CleanExpiredSessions() {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()

	now := time.Now()
	for id, session := range sessions.sessions {
		if now.After(session.ExpiresAt) {
			delete(sessions.sessions, id)
		}
	}
}

// GetActiveSessions returns all active sessions
func GetActiveSessions() []*Session {
	sessions.mu.RLock()
	defer sessions.mu.RUnlock()

	active := make([]*Session, 0)
	now := time.Now()
	for _, session := range sessions.sessions {
		if now.Before(session.ExpiresAt) {
			active = append(active, session)
		}
	}
	return active
}

// generateSessionID generates a secure random session ID
func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// CSRF token management
var csrfTokens = &struct {
	tokens map[string]time.Time
	mu     sync.RWMutex
}{
	tokens: make(map[string]time.Time),
}

// GenerateCSRFToken creates a new CSRF token
func GenerateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.URLEncoding.EncodeToString(b)

	csrfTokens.mu.Lock()
	csrfTokens.tokens[token] = time.Now().Add(1 * time.Hour)
	csrfTokens.mu.Unlock()

	return token, nil
}

// ValidateCSRFToken validates a CSRF token
func ValidateCSRFToken(token string) bool {
	csrfTokens.mu.RLock()
	expiry, ok := csrfTokens.tokens[token]
	csrfTokens.mu.RUnlock()

	if !ok {
		return false
	}

	if time.Now().After(expiry) {
		csrfTokens.mu.Lock()
		delete(csrfTokens.tokens, token)
		csrfTokens.mu.Unlock()
		return false
	}

	return true
}

// ConsumeCSRFToken validates and removes a CSRF token (single-use)
func ConsumeCSRFToken(token string) bool {
	if !ValidateCSRFToken(token) {
		return false
	}

	csrfTokens.mu.Lock()
	delete(csrfTokens.tokens, token)
	csrfTokens.mu.Unlock()

	return true
}

// CleanExpiredCSRFTokens removes expired CSRF tokens
func CleanExpiredCSRFTokens() {
	csrfTokens.mu.Lock()
	defer csrfTokens.mu.Unlock()

	now := time.Now()
	for token, expiry := range csrfTokens.tokens {
		if now.After(expiry) {
			delete(csrfTokens.tokens, token)
		}
	}
}

// Authentication functions

// ValidateCredentials validates admin username and password
func ValidateCredentials(username, password string, cfg *config.Config) bool {
	// Compare username (constant time)
	usernameMatch := subtle.ConstantTimeCompare(
		[]byte(username),
		[]byte(cfg.Server.Admin.Username),
	) == 1

	if !usernameMatch {
		return false
	}

	// Check if password is already hashed (bcrypt)
	if strings.HasPrefix(cfg.Server.Admin.Password, "$2") {
		err := bcrypt.CompareHashAndPassword(
			[]byte(cfg.Server.Admin.Password),
			[]byte(password),
		)
		return err == nil
	}

	// Plain text comparison (constant time)
	return subtle.ConstantTimeCompare(
		[]byte(password),
		[]byte(cfg.Server.Admin.Password),
	) == 1
}

// ValidateToken validates admin API token
func ValidateToken(token string, cfg *config.Config) bool {
	return subtle.ConstantTimeCompare(
		[]byte(token),
		[]byte(cfg.Server.Admin.Token),
	) == 1
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// GenerateToken generates a secure random token
func GenerateToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Middleware for admin authentication

// RequireSession middleware checks for valid session cookie
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("admin_session")
		if err != nil {
			http.Redirect(w, r, "/auth/login?redirect="+r.URL.Path, http.StatusFound)
			return
		}

		session := GetSession(cookie.Value)
		if session == nil {
			http.Redirect(w, r, "/auth/login?redirect="+r.URL.Path, http.StatusFound)
			return
		}

		// Session valid, proceed
		next.ServeHTTP(w, r)
	})
}

// RequireToken middleware checks for valid Bearer token
func RequireToken(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check Authorization header
			auth := r.Header.Get("Authorization")
			if auth == "" {
				jsonError(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			// Extract Bearer token
			parts := strings.SplitN(auth, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				jsonError(w, "Invalid authorization format", http.StatusUnauthorized)
				return
			}

			token := parts[1]
			if !ValidateToken(token, cfg) {
				jsonError(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CSRFProtection middleware validates CSRF tokens on state-changing requests
func CSRFProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF check for safe methods
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Check CSRF token
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			token = r.FormValue("csrf_token")
		}

		if !ValidateCSRFToken(token) {
			jsonError(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Helper functions

func jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// GetClientIP extracts the client IP from a request
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For first
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// GeneratePasswordHash creates a SHA256 hash for display purposes
func GeneratePasswordHash(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}
