package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os/user"
	"time"
)

var (
	ErrBaseMissing = errors.New("ERR_BASE_MISSING: incremental base archive is missing")
	ErrBaseCorrupt = errors.New("ERR_BASE_CORRUPT: incremental base archive failed verification")
)

// ManifestName is the archive entry holding the PART 21 manifest.
const ManifestName = "manifest.json"

// legacyMetadataName is the pre-manifest metadata entry. It is still written
// so older restores keep working, and it is part of the checksummed payload.
const legacyMetadataName = "backup.json"

// EncryptionMethod is the only algorithm PART 21 permits for backups.
const EncryptionMethod = "AES-256-GCM"

// manifestVersion is the manifest schema version, independent of the app
// version recorded alongside it.
const manifestVersion = "1.0.0"

// Manifest is the PART 21 manifest.json stored as the final entry of every
// archive. It is written last so the payload checksum can be streamed rather
// than buffering the whole archive in memory.
type Manifest struct {
	Version    string    `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	SnapshotAt time.Time `json:"snapshot_at,omitempty"`
	CreatedBy  string    `json:"created_by"`
	AppVersion string    `json:"app_version"`
	Contents   []string  `json:"contents"`
	Type       string    `json:"type,omitempty"`
	Encrypted  bool      `json:"encrypted"`
	// EncryptionMethod is omitted entirely for unencrypted archives.
	EncryptionMethod string `json:"encryption_method,omitempty"`
	// Checksum is "sha256:" plus the hex digest of every payload entry.
	Checksum string `json:"checksum"`
	SHA256   string `json:"sha256,omitempty"`
	// Kind records which PART 21 naming pattern produced this archive.
	Kind Kind `json:"kind"`
	// BaseBackup names the full backup an incremental was taken against.
	BaseBackup string         `json:"base_backup,omitempty"`
	BaseSHA256 string         `json:"base_sha256,omitempty"`
	Deleted    []string       `json:"deleted,omitempty"`
	Files      []ManifestFile `json:"files,omitempty"`
	// ModifiedSince is the cutoff an incremental archive was built from.
	ModifiedSince *time.Time `json:"modified_since,omitempty"`
}

// ManifestFile records the content and stable metadata for one archived file.
type ManifestFile struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified_at"`
	SHA256   string    `json:"sha256"`
}

func manifestType(kind Kind) string {
	switch kind {
	case KindFull, KindManual:
		return "full"
	case KindDailyIncremental, KindHourlyIncremental:
		return "incremental"
	default:
		return "full"
	}
}

// payloadDigest accumulates the SHA-256 checksum recorded in the manifest.
// Every archive entry except the manifest itself contributes its name, its
// declared size and its bytes, so both reordering and truncation are caught.
type payloadDigest struct {
	hash hash.Hash
}

// newPayloadDigest returns a digest ready to accept archive entries.
func newPayloadDigest() *payloadDigest {
	return &payloadDigest{hash: sha256.New()}
}

// entry mixes an archive entry's identity into the digest before its bytes.
func (d *payloadDigest) entry(name string, size int64) {
	if d == nil {
		return
	}
	_, _ = fmt.Fprintf(d.hash, "%s\x00%d\n", name, size)
}

// Write mixes payload bytes into the digest, satisfying io.Writer so entries
// can be streamed through an io.MultiWriter.
func (d *payloadDigest) Write(p []byte) (int, error) {
	if d == nil {
		return len(p), nil
	}
	return d.hash.Write(p)
}

// Sum returns the manifest checksum string.
func (d *payloadDigest) Sum() string {
	return "sha256:" + hex.EncodeToString(d.hash.Sum(nil))
}

// writer returns the sink an entry's bytes should be copied through.
func (d *payloadDigest) writer(w io.Writer) io.Writer {
	if d == nil {
		return w
	}
	return io.MultiWriter(w, d)
}

// currentOperator names the account that created a backup, matching the
// manifest's created_by field.
func currentOperator() string {
	current, err := user.Current()
	if err != nil || current.Username == "" {
		return "operator"
	}
	return current.Username
}
