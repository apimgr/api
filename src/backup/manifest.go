package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os/user"
	"sort"
	"strings"
	"time"
)

var (
	ErrBaseMissing = errors.New("ERR_BASE_MISSING: incremental base archive is missing")
	ErrBaseCorrupt = errors.New("ERR_BASE_CORRUPT: incremental base archive failed verification")
)

// ManifestName is the archive entry holding the PART 21 manifest.
const ManifestName = "manifest.json"

// legacyMetadataName is retained as a compatibility metadata entry and is
// excluded from the canonical content-file checksum.
const legacyMetadataName = "backup.json"

// EncryptionMethod is the only algorithm PART 21 permits for backups.
const EncryptionMethod = "AES-256-GCM"

// manifestVersion is the manifest schema version, independent of the app
// version recorded alongside it.
const manifestVersion = "1.0.0"

// Manifest is the PART 21 manifest.json stored as the final entry of every
// archive.
type Manifest struct {
	Version    string    `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	SnapshotAt time.Time `json:"snapshot_at"`
	CreatedBy  string    `json:"created_by"`
	AppVersion string    `json:"app_version"`
	Contents   []string  `json:"contents"`
	Type       string    `json:"type,omitempty"`
	Encrypted  bool      `json:"encrypted"`
	// EncryptionMethod is omitted entirely for unencrypted archives.
	EncryptionMethod string `json:"encryption_method,omitempty"`
	// SHA256 is the canonical digest of the sorted per-file manifest lines.
	SHA256 string `json:"sha256"`
	// Kind records which PART 21 naming pattern produced this archive.
	Kind Kind `json:"kind"`
	// BaseBackup names the full backup an incremental was taken against.
	BaseBackup string         `json:"base_backup,omitempty"`
	BaseSHA256 string         `json:"base_sha256,omitempty"`
	Deleted    []string       `json:"deleted,omitempty"`
	Files      []ManifestFile `json:"files"`
	// ModifiedSince is the cutoff an incremental archive was built from.
	ModifiedSince *time.Time `json:"modified_since,omitempty"`
}

// ManifestFile records the content and stable metadata for one archived file.
type ManifestFile struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"-"`
	SHA256   string    `json:"sha256"`
}

func (file ManifestFile) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Path   string `json:"path"`
		Size   int64  `json:"size"`
		MTime  int64  `json:"mtime"`
		SHA256 string `json:"sha256"`
	}{Path: file.Path, Size: file.Size, MTime: file.Modified.UTC().Unix(), SHA256: file.SHA256})
}

func (file *ManifestFile) UnmarshalJSON(data []byte) error {
	var wire struct {
		Path   string `json:"path"`
		Size   int64  `json:"size"`
		MTime  int64  `json:"mtime"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	file.Path = wire.Path
	file.Size = wire.Size
	file.Modified = time.Unix(wire.MTime, 0).UTC()
	file.SHA256 = wire.SHA256
	return nil
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

// ManifestSHA256 computes the canonical PART 21 digest from sorted file rows.
func ManifestSHA256(files []ManifestFile) string {
	ordered := append([]ManifestFile(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	lines := make([]string, 0, len(ordered))
	for _, file := range ordered {
		lines = append(lines, strings.TrimPrefix(file.SHA256, "sha256:")+"  "+file.Path)
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

type payloadDigest struct{ hash hash.Hash }

func newPayloadDigest() *payloadDigest { return &payloadDigest{hash: sha256.New()} }

func (d *payloadDigest) entry(name string, size int64) {
	if d != nil {
		_, _ = fmt.Fprintf(d.hash, "%s\x00%d\n", name, size)
	}
}

func (d *payloadDigest) Write(data []byte) (int, error) { return d.hash.Write(data) }

func (d *payloadDigest) Sum() string { return hex.EncodeToString(d.hash.Sum(nil)) }

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
