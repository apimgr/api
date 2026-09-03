package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"time"
)

// encryptionKeyBytes is the AES-256-GCM key length for
// server.security.encryption_key.
const encryptionKeyBytes = 32

// operatorTokenBytes is the entropy behind the generated server.token.
const operatorTokenBytes = 32

// ServerSecurityConfig holds the server.security tree from AI.md PART 11:
// the IP allowlist, permanent IP blocks, abuse detection thresholds, and
// the canonical at-rest encryption key.
type ServerSecurityConfig struct {
	// Allowlist entries bypass blocklists, rate limiting, GeoIP country
	// blocking and auto-block. They never bypass CSRF, path-traversal
	// checks, or TLS enforcement.
	Allowlist []AllowlistEntry `yaml:"allowlist"`
	// BlockedIPs are permanent operator-managed blocks; temporary blocks
	// live in the database and auto-release on expiry.
	BlockedIPs []BlockedIPEntry `yaml:"blocked_ips"`
	// AbuseDetection configures the automatic flood detection thresholds.
	AbuseDetection AbuseDetectionConfig `yaml:"abuse_detection"`
	// EncryptionKey is the canonical AES-256-GCM key for all at-rest
	// sensitive data, generated on first run. Never logged, never returned
	// by any API, always included in backups.
	EncryptionKey string `yaml:"encryption_key"`
	// EncryptionKeyPrevious is the outgoing key retained after a manual
	// rotation so data not yet re-encrypted still decrypts.
	EncryptionKeyPrevious string `yaml:"encryption_key_previous,omitempty"`
	// EncryptionKeyPreviousExpires is the RFC 3339 instant at which the
	// retained previous key stops being accepted. PART 11 grants a 30 day
	// grace window.
	EncryptionKeyPreviousExpires string `yaml:"encryption_key_previous_expires,omitempty"`
}

// encryptionKeyGrace is how long a rotated-out at-rest encryption key stays
// valid for decryption (AI.md PART 11 "Rotate encryption_key").
const encryptionKeyGrace = 30 * 24 * time.Hour

// RotateEncryptionKey generates a fresh AES-256-GCM key, retains the outgoing
// key for its 30 day grace window, and persists the result to server.yml.
func RotateEncryptionKey(cfg *Config) error {
	next, err := randomSecretString(encryptionKeyBytes, true)
	if err != nil {
		return err
	}
	previous := cfg.Server.Security.EncryptionKey
	cfg.Server.Security.EncryptionKey = next
	if previous != "" {
		cfg.Server.Security.EncryptionKeyPrevious = previous
		cfg.Server.Security.EncryptionKeyPreviousExpires = time.Now().UTC().Add(encryptionKeyGrace).Format(time.RFC3339)
	}
	return Save(cfg)
}

// PreviousEncryptionKey returns the retained previous at-rest key while it is
// still inside its grace window, and an empty string once it has expired.
func (s ServerSecurityConfig) PreviousEncryptionKey(now time.Time) string {
	if s.EncryptionKeyPrevious == "" || s.EncryptionKeyPreviousExpires == "" {
		return ""
	}
	expires, err := time.Parse(time.RFC3339, s.EncryptionKeyPreviousExpires)
	if err != nil || !now.Before(expires) {
		return ""
	}
	return s.EncryptionKeyPrevious
}

// AllowlistEntry is one always-trusted IP or CIDR. A bare address with no
// prefix length is treated as /32 (IPv4) or /128 (IPv6).
type AllowlistEntry struct {
	CIDR        string    `yaml:"cidr" json:"cidr"`
	Description string    `yaml:"description" json:"description"`
	AddedAt     time.Time `yaml:"-" json:"added_at"`
	AddedBy     string    `yaml:"-" json:"added_by"`
}

// BlockedIPEntry is one permanently blocked IP or CIDR.
type BlockedIPEntry struct {
	CIDR    string    `yaml:"cidr" json:"cidr"`
	Reason  string    `yaml:"reason" json:"reason"`
	AddedAt time.Time `yaml:"-" json:"added_at"`
	AddedBy string    `yaml:"-" json:"added_by"`
}

// AbuseDetectionConfig configures automatic abuse detection and response.
type AbuseDetectionConfig struct {
	RequestFlood RequestFloodConfig `yaml:"request_flood"`
	// AutoBlockIP temporarily blocks an offending address on detection.
	AutoBlockIP bool `yaml:"auto_block_ip"`
	// AutoAlert notifies the admin contact role on detection.
	AutoAlert bool `yaml:"auto_alert"`
}

// RequestFloodConfig defines when sustained request volume counts as a
// flood, expressed as a multiple of the configured rate limit.
type RequestFloodConfig struct {
	Multiplier    int           `yaml:"multiplier"`
	BlockDuration time.Duration `yaml:"block_duration"`
}

// defaultServerSecurityConfig returns the PART 11 defaults: no allowlist,
// no blocks, flood detection at 10x the rate limit with a one hour block.
func defaultServerSecurityConfig() ServerSecurityConfig {
	return ServerSecurityConfig{
		Allowlist:  []AllowlistEntry{},
		BlockedIPs: []BlockedIPEntry{},
		AbuseDetection: AbuseDetectionConfig{
			RequestFlood: RequestFloodConfig{
				Multiplier:    10,
				BlockDuration: time.Hour,
			},
			AutoBlockIP: true,
			AutoAlert:   true,
		},
		EncryptionKey: "",
	}
}

// ensureFirstRunSecrets fills in the two server.yml-resident secrets that are
// auto-generated on first run: the AES-256-GCM at-rest encryption key and the
// global operator token. It reports whether anything changed so the caller
// can persist the config. Neither value is ever logged.
func ensureFirstRunSecrets(cfg *Config) bool {
	changed := false
	if cfg.Server.Security.EncryptionKey == "" {
		if key, err := randomSecretString(encryptionKeyBytes, true); err == nil {
			cfg.Server.Security.EncryptionKey = key
			changed = true
		}
	}
	if cfg.Server.Token == "" {
		if token, err := randomSecretString(operatorTokenBytes, false); err == nil {
			cfg.Server.Token = token
			changed = true
		}
	}
	return changed
}

// randomSecretString returns cryptographically random material encoded as
// base64 when b64 is true, or lowercase hex otherwise.
func randomSecretString(length int, b64 bool) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	if b64 {
		return base64.StdEncoding.EncodeToString(buf), nil
	}
	return hex.EncodeToString(buf), nil
}
