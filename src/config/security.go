package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

const dnsCredentialCipherPrefix = "enc:v1:"

// EncryptDNSCredential protects one provider credential with the configured
// AES-256-GCM key. Values already encrypted with the active key are preserved.
func EncryptDNSCredential(value, encryptionKey string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, dnsCredentialCipherPrefix) {
		if _, err := DecryptDNSCredential(value, encryptionKey, ""); err == nil {
			return value, nil
		}
	}
	key, err := decodeEncryptionKey(encryptionKey)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), nil)
	return dnsCredentialCipherPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptDNSCredential opens an enc:v1 value with the active key or, during
// the configured grace period, the retained previous key. Errors never
// include credential material.
func DecryptDNSCredential(value, encryptionKey, previousKey string) (string, error) {
	if !strings.HasPrefix(value, dnsCredentialCipherPrefix) {
		return value, nil
	}
	encoded := strings.TrimPrefix(value, dnsCredentialCipherPrefix)
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("invalid encrypted DNS credential")
	}
	for _, candidate := range []string{encryptionKey, previousKey} {
		key, keyErr := decodeEncryptionKey(candidate)
		if keyErr != nil {
			continue
		}
		block, blockErr := aes.NewCipher(key)
		if blockErr != nil {
			continue
		}
		aead, aeadErr := cipher.NewGCM(block)
		if aeadErr != nil || len(sealed) < aead.NonceSize()+aead.Overhead() {
			continue
		}
		nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
		plain, openErr := aead.Open(nil, nonce, ciphertext, nil)
		if openErr == nil {
			return string(plain), nil
		}
	}
	return "", fmt.Errorf("cannot decrypt DNS credential")
}

func decodeEncryptionKey(value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		key, err = hex.DecodeString(value)
	}
	if err != nil || len(key) != encryptionKeyBytes {
		return nil, fmt.Errorf("invalid server.security.encryption_key")
	}
	return key, nil
}

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
	// Reports controls coordinated-disclosure report retention.
	Reports    SecurityReportsConfig `yaml:"reports"`
	Blocklists BlocklistsConfig      `yaml:"blocklists"`
	// EncryptionKey is the canonical AES-256-GCM key for all at-rest
	// sensitive data, generated on first run. Never logged, never returned
	// by any API, always included in backups.
	EncryptionKey string `yaml:"encryption_key"`
	// EncryptionKeyVersion identifies the encoding and algorithm for the
	// current at-rest encryption key.
	EncryptionKeyVersion int `yaml:"encryption_key_version"`
	// EncryptionKeyPrevious is read only to migrate legacy configurations to
	// the database-wrapped previous-key format; current rotations never write it.
	EncryptionKeyPrevious string `yaml:"encryption_key_previous,omitempty"`
	// EncryptionKeyPreviousExpires is the legacy expiry paired with the
	// previous key during migration.
	EncryptionKeyPreviousExpires string `yaml:"encryption_key_previous_expires,omitempty"`
}

type BlocklistsConfig struct {
	Enabled      bool              `yaml:"enabled"`
	ExtraSources []BlocklistSource `yaml:"extra_sources"`
}

type BlocklistSource struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	URL     string `yaml:"url"`
	Enabled *bool  `yaml:"enabled"`
}

var blocklistIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

func (c BlocklistsConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	seen := map[string]bool{"firehol_level1": true}
	for i, source := range c.ExtraSources {
		if !blocklistIDPattern.MatchString(source.ID) || seen[source.ID] {
			return fmt.Errorf("server.security.blocklists.extra_sources[%d].id is invalid or duplicated", i)
		}
		seen[source.ID] = true
		if source.Type != "ip" && source.Type != "domain" {
			return fmt.Errorf("server.security.blocklists.extra_sources[%d].type must be ip or domain", i)
		}
		parsed, err := url.Parse(source.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return fmt.Errorf("server.security.blocklists.extra_sources[%d].url must be HTTPS", i)
		}
	}
	return nil
}

func NormalizeBlocklistDomain(value string) (string, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "*."))
	value = strings.TrimSuffix(strings.ToLower(value), ".")
	if value == "" {
		return "", false
	}
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil || strings.ContainsAny(ascii, ":/@?# \\*") {
		return "", false
	}
	return strings.ToLower(ascii), true
}

func IsPublicSuffix(value string) bool {
	suffix, _ := publicsuffix.PublicSuffix(value)
	return strings.EqualFold(value, suffix)
}

// SecurityReportsConfig controls retention of closed disclosure reports.
type SecurityReportsConfig struct {
	RetentionDays int `yaml:"retention_days"`
}

// encryptionKeyGrace is how long a rotated-out at-rest encryption key stays
// valid for decryption (AI.md PART 11 "Rotate encryption_key").
const encryptionKeyGrace = 30 * 24 * time.Hour

// RotateEncryptionKey prepares a new AES-256-GCM key, lets the caller persist
// recovery material, and then saves the key and its dependent config secrets.
func RotateEncryptionKey(cfg *Config, prepare func(previousKey, nextKey []byte, until time.Time) error) error {
	next, err := randomSecretString(encryptionKeyBytes, true)
	if err != nil {
		return err
	}
	previous, err := decodeEncryptionKey(cfg.Server.Security.EncryptionKey)
	if err != nil {
		return fmt.Errorf("decode current encryption key: %w", err)
	}
	nextBytes, err := decodeEncryptionKey(next)
	if err != nil {
		return fmt.Errorf("decode generated encryption key: %w", err)
	}
	until := time.Now().UTC().Add(encryptionKeyGrace)
	if prepare != nil {
		if err := prepare(previous, nextBytes, until); err != nil {
			return fmt.Errorf("prepare encryption-key rotation: %w", err)
		}
	}
	cfg.Server.Security.EncryptionKey = next
	cfg.Server.Security.EncryptionKeyPrevious = ""
	cfg.Server.Security.EncryptionKeyPreviousExpires = ""
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
		EncryptionKey:        "",
		EncryptionKeyVersion: 1,
		Reports:              SecurityReportsConfig{RetentionDays: 365},
		Blocklists:           BlocklistsConfig{Enabled: true, ExtraSources: []BlocklistSource{}},
	}
}

// ensureFirstRunSecrets fills the generated credentials after all config
// sources have merged. The caller persists them only to secrets.yml; values
// are never logged.
func ensureFirstRunSecrets(cfg *Config) bool {
	changed := false
	if cfg.Server.Security.EncryptionKey == "" {
		if key, err := randomSecretString(encryptionKeyBytes, true); err == nil {
			cfg.Server.Security.EncryptionKey = key
			cfg.Server.Security.EncryptionKeyVersion = 1
			cfg.GeneratedSecrets = append(cfg.GeneratedSecrets, "server.security.encryption_key")
			changed = true
		}
	}
	if cfg.Server.Security.EncryptionKeyVersion == 0 {
		cfg.Server.Security.EncryptionKeyVersion = 1
		changed = true
	}
	if cfg.Server.Token == "" {
		if token, err := randomBase62String(operatorTokenBytes); err == nil {
			cfg.Server.Token = "adm_" + token
			cfg.GeneratedSecrets = append(cfg.GeneratedSecrets, "server.token")
			changed = true
		}
	}
	metricsTokens := []*string{
		&cfg.Server.Metrics.Auth.Tokens.Prometheus,
		&cfg.Server.Metrics.Auth.Tokens.Grafana,
		&cfg.Server.Metrics.Auth.Tokens.Loki,
	}
	for i, token := range metricsTokens {
		if *token != "" {
			continue
		}
		generated, err := randomBase62String(32)
		if err != nil {
			continue
		}
		*token = "mtr_" + generated
		cfg.GeneratedSecrets = append(cfg.GeneratedSecrets, []string{
			"server.metrics.auth.tokens.prometheus",
			"server.metrics.auth.tokens.grafana",
			"server.metrics.auth.tokens.loki",
		}[i])
		changed = true
	}
	roles := []struct {
		name string
		cfg  *ContactWebhooksConfig
	}{
		{"admin", &cfg.Server.Contact.Admin.Webhooks},
		{"security", &cfg.Server.Contact.Security.Webhooks},
		{"abuse", &cfg.Server.Contact.Abuse.Webhooks},
		{"general", &cfg.Server.Contact.General.Webhooks},
	}
	for _, role := range roles {
		if *role.cfg == nil {
			*role.cfg = ContactWebhooksConfig{}
		}
		names := make([]string, 0, len(*role.cfg))
		for name := range *role.cfg {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			webhookURL := (*role.cfg)[name]
			if strings.HasSuffix(name, "_secret") || webhookURL == "" {
				continue
			}
			secretKey := name + "_secret"
			if (*role.cfg)[secretKey] != "" {
				continue
			}
			generated, err := randomSecretString(32, false)
			if err != nil {
				continue
			}
			(*role.cfg)[secretKey] = generated
			cfg.GeneratedSecrets = append(cfg.GeneratedSecrets, "server.contact."+role.name+".webhooks."+secretKey)
			changed = true
		}
	}
	return changed
}

func randomBase62String(length int) (string, error) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var result []byte
	for range length {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		result = append(result, alphabet[n.Int64()])
	}
	return string(result), nil
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
