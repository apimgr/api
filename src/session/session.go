package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/apimgr/api/src/database"
)

const (
	// SessionCookieName is the name of the session cookie
	SessionCookieName = "session_id"

	// SessionIDPrefix for easy identification
	SessionIDPrefix = "ses_"

	// DefaultSessionDuration is the default session timeout
	DefaultSessionDuration = 24 * time.Hour

	// SessionCleanupInterval is how often to clean expired sessions
	SessionCleanupInterval = 1 * time.Hour
)

// Session represents a user session
type Session struct {
	ID           string
	AdminID      int
	Data         map[string]interface{}
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastActivity time.Time
}

// Create creates a new session for an admin
func Create(adminID int, duration time.Duration) (*Session, error) {
	// Generate session ID
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(duration)

	// Create session object
	session := &Session{
		ID:           sessionID,
		AdminID:      adminID,
		Data:         make(map[string]interface{}),
		CreatedAt:    now,
		ExpiresAt:    expiresAt,
		LastActivity: now,
	}

	// Serialize session data
	dataJSON, err := json.Marshal(session.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session data: %w", err)
	}

	// Store in database
	db := database.GetServerDB()
	_, err = db.Exec(`
		INSERT INTO sessions (id, admin_id, data, created_at, expires_at, last_activity)
		VALUES (?, ?, ?, ?, ?, ?)
	`, sessionID, adminID, string(dataJSON), now, expiresAt, now)

	if err != nil {
		return nil, fmt.Errorf("failed to store session: %w", err)
	}

	log.Printf("Session: Created for admin %d (expires in %v)", adminID, duration)
	return session, nil
}

// Get retrieves a session by ID
func Get(sessionID string) (*Session, error) {
	db := database.GetServerDB()

	var session Session
	var dataJSON string

	err := db.QueryRow(`
		SELECT id, admin_id, data, created_at, expires_at, last_activity
		FROM sessions
		WHERE id = ? AND expires_at > ?
	`, sessionID, time.Now()).Scan(
		&session.ID,
		&session.AdminID,
		&dataJSON,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.LastActivity,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found or expired")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query session: %w", err)
	}

	// Deserialize data
	if err := json.Unmarshal([]byte(dataJSON), &session.Data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	return &session, nil
}

// Update updates session data and refreshes last activity
func (s *Session) Update() error {
	s.LastActivity = time.Now()

	// Serialize data
	dataJSON, err := json.Marshal(s.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}

	// Update in database
	db := database.GetServerDB()
	_, err = db.Exec(`
		UPDATE sessions
		SET data = ?, last_activity = ?
		WHERE id = ?
	`, string(dataJSON), s.LastActivity, s.ID)

	return err
}

// Destroy removes a session
func Destroy(sessionID string) error {
	db := database.GetServerDB()
	_, err := db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	log.Printf("Session: Destroyed %s", sessionID)
	return nil
}

// CleanupExpired removes all expired sessions
// This is called by the scheduler hourly
func CleanupExpired() error {
	db := database.GetServerDB()
	result, err := db.Exec("DELETE FROM sessions WHERE expires_at < ?", time.Now())
	if err != nil {
		return fmt.Errorf("failed to cleanup sessions: %w", err)
	}

	count, _ := result.RowsAffected()
	if count > 0 {
		log.Printf("Session: Cleaned up %d expired sessions", count)
	}

	return nil
}

// GetFromRequest extracts session from HTTP request cookie
func GetFromRequest(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, fmt.Errorf("no session cookie")
	}

	return Get(cookie.Value)
}

// SetCookie sets the session cookie in the response
func SetCookie(w http.ResponseWriter, sessionID string, duration time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(duration.Seconds()),
		HttpOnly: true,
		Secure:   false, // Set to true when SSL is enabled
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie removes the session cookie
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// generateSessionID generates a secure random session ID
func generateSessionID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return SessionIDPrefix + hex.EncodeToString(bytes), nil
}

// Middleware is HTTP middleware that loads session from cookie
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to load session from cookie
		session, err := GetFromRequest(r)
		if err == nil && session != nil {
			// Update last activity
			session.Update()
			// TODO: Add session to request context
		}

		next.ServeHTTP(w, r)
	})
}
