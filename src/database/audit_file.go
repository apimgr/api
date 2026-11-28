package database

import (
	"bufio"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
)

const zeroAuditHash = "0000000000000000000000000000000000000000000000000000000000000000"
const auditULIDAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

type auditActor struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	IP   string `json:"ip,omitempty"`
}

type auditFileRecord struct {
	ID       string         `json:"id"`
	Time     string         `json:"time"`
	Event    string         `json:"event"`
	Category string         `json:"category"`
	Severity string         `json:"severity"`
	Actor    auditActor     `json:"actor"`
	Target   map[string]any `json:"target,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	Source   string         `json:"source,omitempty"`
	Result   string         `json:"result"`
	Prev     string         `json:"prev"`
}

type auditFileState struct {
	mu             sync.Mutex
	path           string
	rotate         string
	keep           string
	compress       bool
	enabled        bool
	groupID        int
	groupEnabled   bool
	groupWarned    bool
	initialized    bool
	lastHash       string
	lastLineNumber int
	brokenReported bool
	forceRotate    bool
	pendingBreak   map[string]any
}

var auditFile = auditFileState{
	path:    filepath.Join(paths.LogDir(), "audit.log"),
	rotate:  "daily",
	keep:    "none",
	enabled: true,
}

// ConfigureAuditFile configures the authoritative JSONL audit log. The
// database mirror remains available for queries but is never the source of
// the hash chain.
func ConfigureAuditFile(cfg config.AuditLogConfig) error {
	filename := strings.TrimSpace(cfg.Filename)
	if filename == "" {
		filename = "audit.log"
	}
	if filepath.Base(filename) != filename || filename == "." || filename == ".." {
		return fmt.Errorf("audit: filename must be a file name within the log directory")
	}
	rotate := strings.TrimSpace(cfg.Rotate)
	if rotate == "" {
		rotate = "daily"
	}
	keep := strings.TrimSpace(cfg.Keep)
	if keep == "" {
		keep = "none"
	}
	groupID, groupEnabled, groupWarning := resolveAuditGroup(strings.TrimSpace(cfg.Group), paths.IsElevated())
	if groupWarning != "" {
		log.Printf("audit: %s", sanitize.SanitizeLogText(groupWarning))
	}
	auditFile.mu.Lock()
	defer auditFile.mu.Unlock()
	auditFile.path = filepath.Join(paths.LogDir(), filename)
	auditFile.rotate = rotate
	auditFile.keep = keep
	auditFile.compress = cfg.Compress
	auditFile.enabled = cfg.Enabled
	auditFile.groupID = groupID
	auditFile.groupEnabled = groupEnabled
	auditFile.groupWarned = groupWarning != ""
	auditFile.initialized = false
	auditFile.lastHash = ""
	auditFile.lastLineNumber = 0
	auditFile.brokenReported = false
	auditFile.forceRotate = false
	auditFile.pendingBreak = nil
	return nil
}

func appendAuditFile(record auditFileRecord) error {
	auditFile.mu.Lock()
	defer auditFile.mu.Unlock()
	if !auditFile.enabled {
		return nil
	}
	if err := initializeAuditFileLocked(); err != nil {
		return err
	}
	if err := detectActiveTailChangeLocked(); err != nil {
		return err
	}
	if auditFile.forceRotate || shouldRotateAuditFileLocked(time.Now().UTC()) {
		if err := rotateAuditFileLocked(); err != nil {
			log.Printf("audit: rotation failed: %s", sanitize.SanitizeLogText(err.Error()))
			if anchor, anchorErr := readAuditAnchorLocked(); anchorErr == nil && anchor == auditFile.lastHash {
				if info, statErr := os.Stat(auditFile.path); statErr == nil && info.Size() > 0 {
					if recoveryErr := finishInterruptedAuditRotationLocked(); recoveryErr != nil {
						return fmt.Errorf("audit: recover interrupted rotation: %w", recoveryErr)
					}
				} else if errors.Is(statErr, os.ErrNotExist) {
					if createErr := createActiveAuditFileLocked(); createErr != nil {
						return createErr
					}
					auditFile.lastLineNumber = 0
				}
			}
		} else {
			auditFile.forceRotate = false
		}
	}
	if auditFile.pendingBreak != nil {
		if err := appendPendingAuditBreakLocked(); err != nil {
			return err
		}
	}
	record.Prev = auditFile.lastHash
	return appendAuditRecordLocked(record)
}

func appendPendingAuditBreakLocked() error {
	pending := auditFile.pendingBreak
	auditFile.pendingBreak = nil
	broken, err := buildAuditRecord("security.audit_chain_broken", "system", "", "", "system", pending)
	if err != nil {
		auditFile.pendingBreak = pending
		return err
	}
	broken.Severity = "critical"
	broken.Result = "failure"
	broken.Prev = auditFile.lastHash
	if err := appendAuditRecordLocked(broken); err != nil {
		auditFile.pendingBreak = pending
		return err
	}
	return nil
}

func auditFileEnabled() bool {
	auditFile.mu.Lock()
	defer auditFile.mu.Unlock()
	return auditFile.enabled
}

func initializeAuditFileLocked() error {
	if auditFile.initialized {
		return nil
	}
	directoryMode := os.FileMode(0700)
	if auditFile.groupEnabled {
		directoryMode = 0750
	}
	if err := os.MkdirAll(filepath.Dir(auditFile.path), directoryMode); err != nil {
		return fmt.Errorf("audit: create log directory: %w", err)
	}
	if err := applyAuditPathPermissionsLocked(filepath.Dir(auditFile.path), true); err != nil {
		return fmt.Errorf("audit: secure log directory: %w", err)
	}
	file, err := os.OpenFile(auditFile.path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("audit: open log: %w", err)
	}
	if err := applyAuditPathPermissionsLocked(auditFile.path, false); err != nil {
		_ = file.Close()
		return fmt.Errorf("audit: secure log permissions: %w", err)
	}
	anchor, anchorErr := readAuditAnchorLocked()
	anchorInvalid := anchorErr != nil && !errors.Is(anchorErr, os.ErrNotExist)
	anchorExists := anchorErr == nil
	if anchorInvalid {
		anchor = ""
	}
	lines, err := readAuditLines(file)
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("audit: read log for verification: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("audit: close verified log: %w", err)
	}
	startHash := zeroAuditHash
	if anchorExists {
		startHash = anchor
	}
	expected := startHash
	var broken map[string]any
	if anchorInvalid {
		broken = map[string]any{"file": filepath.Base(auditFile.path), "line": 0, "reason": "anchor_mismatch"}
	}
	for i, line := range lines {
		var entry struct {
			Prev string `json:"prev"`
		}
		if err := json.Unmarshal(line, &entry); err != nil || entry.Prev != expected {
			reason := "anchor_mismatch"
			if i == 0 && !anchorExists && !anchorInvalid {
				reason = "anchor_missing"
			}
			broken = map[string]any{"file": filepath.Base(auditFile.path), "line": i + 1, "reason": reason}
			break
		}
		hash := sha256.Sum256(line)
		expected = hex.EncodeToString(hash[:])
	}
	auditFile.lastLineNumber = len(lines)
	auditFile.lastHash = startHash
	if len(lines) > 0 {
		hash := sha256.Sum256(lines[len(lines)-1])
		auditFile.lastHash = hex.EncodeToString(hash[:])
	}
	if broken != nil && anchorErr == nil && auditFile.lastHash == anchor {
		// The anchor is advanced before rename. Matching the active tail means
		// a crash stopped between those durable rotation steps; finish it.
		if err := finishInterruptedAuditRotationLocked(); err != nil {
			return err
		}
		broken = nil
	}
	auditFile.pendingBreak = broken
	auditFile.brokenReported = broken != nil
	auditFile.forceRotate = broken != nil
	auditFile.initialized = true
	return nil
}

func readAuditLines(file *os.File) ([][]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	lines := make([][]byte, 0)
	for scanner.Scan() {
		lines = append(lines, append([]byte(nil), scanner.Bytes()...))
	}
	return lines, scanner.Err()
}

func appendAuditRecordLocked(record auditFileRecord) error {
	if record.Prev == "" {
		record.Prev = zeroAuditHash
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("audit: encode JSONL entry: %w", err)
	}
	file, err := os.OpenFile(auditFile.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("audit: open log for append: %w", err)
	}
	if err := applyAuditPathPermissionsLocked(auditFile.path, false); err != nil {
		_ = file.Close()
		return fmt.Errorf("audit: secure log permissions: %w", err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("audit: append entry: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("audit: sync entry: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("audit: close log: %w", err)
	}
	hash := sha256.Sum256(line)
	auditFile.lastHash = hex.EncodeToString(hash[:])
	auditFile.lastLineNumber++
	return nil
}

func detectActiveTailChangeLocked() error {
	if auditFile.lastLineNumber == 0 || auditFile.brokenReported {
		return nil
	}
	file, err := os.Open(auditFile.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			auditFile.pendingBreak = map[string]any{"file": filepath.Base(auditFile.path), "line": auditFile.lastLineNumber, "reason": "anchor_mismatch"}
			auditFile.brokenReported = true
			auditFile.forceRotate = true
			created, createErr := os.OpenFile(auditFile.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if createErr != nil {
				return createErr
			}
			if closeErr := created.Close(); closeErr != nil {
				return closeErr
			}
			return nil
		}
		return err
	}
	lines, readErr := readAuditLines(file)
	closeErr := file.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(lines) == 0 {
		auditFile.pendingBreak = map[string]any{"file": filepath.Base(auditFile.path), "line": 0, "reason": "anchor_mismatch"}
		auditFile.brokenReported = true
		auditFile.forceRotate = true
		return nil
	}
	hash := sha256.Sum256(lines[len(lines)-1])
	if hex.EncodeToString(hash[:]) != auditFile.lastHash {
		auditFile.pendingBreak = map[string]any{"file": filepath.Base(auditFile.path), "line": len(lines), "reason": "anchor_mismatch"}
		auditFile.brokenReported = true
		auditFile.forceRotate = true
	}
	return nil
}

func readAuditAnchorLocked() (string, error) {
	path := filepath.Join(filepath.Dir(auditFile.path), "audit.anchor")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return "", fmt.Errorf("audit: secure anchor permissions: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if len(value) != 64 {
		return "", fmt.Errorf("audit: invalid anchor file")
	}
	if _, err := hex.DecodeString(value); err != nil || strings.ToLower(value) != value {
		return "", fmt.Errorf("audit: invalid anchor file")
	}
	return value, nil
}

func writeAuditAnchorLocked(hash string) error {
	dir := filepath.Dir(auditFile.path)
	file, err := os.CreateTemp(dir, ".audit.anchor-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := io.WriteString(file, hash+"\n"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, "audit.anchor")); err != nil {
		return err
	}
	return syncAuditDirectory(dir)
}

func syncAuditDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}

func generateAuditULID(now time.Time) (string, error) {
	var raw [16]byte
	ms := uint64(now.UTC().UnixMilli())
	for i := 5; i >= 0; i-- {
		raw[i] = byte(ms)
		ms >>= 8
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", err
	}
	var encoded [26]byte
	for i := range encoded {
		var value byte
		for j := 0; j < 5; j++ {
			bit := i*5 + j - 2
			value <<= 1
			if bit >= 0 && bit < 128 {
				value |= (raw[bit/8] >> (7 - uint(bit%8))) & 1
			}
		}
		encoded[i] = auditULIDAlphabet[value]
	}
	return string(encoded[:]), nil
}

func shouldRotateAuditFileLocked(now time.Time) bool {
	info, err := os.Stat(auditFile.path)
	if err != nil || info.Size() == 0 {
		return false
	}
	parts := strings.Split(auditFile.rotate, ",")
	for _, raw := range parts {
		part := strings.TrimSpace(strings.ToLower(raw))
		switch part {
		case "never":
		case "daily":
			if !sameUTCDate(info.ModTime(), now) {
				return true
			}
		case "weekly":
			oldYear, oldWeek := info.ModTime().UTC().ISOWeek()
			newYear, newWeek := now.ISOWeek()
			if oldYear != newYear || oldWeek != newWeek {
				return true
			}
		case "monthly":
			old, current := info.ModTime().UTC(), now
			if old.Year() != current.Year() || old.Month() != current.Month() {
				return true
			}
		case "yearly":
			if info.ModTime().UTC().Year() != now.Year() {
				return true
			}
		default:
			if maxBytes, ok := parseAuditSize(part); ok && info.Size() >= maxBytes {
				return true
			}
		}
	}
	return false
}

func sameUTCDate(a, b time.Time) bool {
	a, b = a.UTC(), b.UTC()
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func parseAuditSize(value string) (int64, bool) {
	var amount int64
	var suffix string
	if _, err := fmt.Sscanf(strings.ToUpper(value), "%d%s", &amount, &suffix); err != nil || amount < 1 {
		return 0, false
	}
	multiplier := int64(0)
	switch suffix {
	case "MB":
		multiplier = 1 << 20
	case "GB":
		multiplier = 1 << 30
	}
	if multiplier == 0 || amount > int64(^uint64(0)>>1)/multiplier {
		return 0, false
	}
	return amount * multiplier, true
}

func rotateAuditFileLocked() error {
	file, err := os.Open(auditFile.path)
	if err != nil {
		return err
	}
	lines, err := readAuditLines(file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if len(lines) == 0 {
		return nil
	}
	anchor, anchorErr := readAuditAnchorLocked()
	start := zeroAuditHash
	if anchorErr == nil {
		start = anchor
	} else if !errors.Is(anchorErr, os.ErrNotExist) && !auditFile.brokenReported {
		auditFile.pendingBreak = map[string]any{"file": filepath.Base(auditFile.path), "line": 0, "reason": "anchor_mismatch"}
		auditFile.brokenReported = true
	}
	expected := start
	for i, line := range lines {
		var entry struct {
			Prev string `json:"prev"`
		}
		if err := json.Unmarshal(line, &entry); err != nil || entry.Prev != expected {
			if !auditFile.brokenReported {
				auditFile.pendingBreak = map[string]any{"file": filepath.Base(auditFile.path), "line": i + 1, "reason": "anchor_mismatch"}
				auditFile.brokenReported = true
			}
			break
		}
		hash := sha256.Sum256(line)
		expected = hex.EncodeToString(hash[:])
	}
	tail := sha256.Sum256(lines[len(lines)-1])
	tailHash := hex.EncodeToString(tail[:])
	if err := writeAuditAnchorLocked(tailHash); err != nil {
		return fmt.Errorf("write anchor: %w", err)
	}
	rotated := rotatedAuditPath()
	if err := os.Rename(auditFile.path, rotated); err != nil {
		return err
	}
	if err := syncAuditDirectory(filepath.Dir(auditFile.path)); err != nil {
		return err
	}
	if auditFile.compress {
		if err := compressAuditFile(rotated); err != nil {
			log.Printf("audit: compression failed: %s", sanitize.SanitizeLogText(err.Error()))
		}
	}
	if err := createActiveAuditFileLocked(); err != nil {
		return err
	}
	auditFile.lastHash = tailHash
	auditFile.lastLineNumber = 0
	if err := syncAuditDirectory(filepath.Dir(auditFile.path)); err != nil {
		return err
	}
	return pruneAuditFilesLocked()
}

func finishInterruptedAuditRotationLocked() error {
	rotated := rotatedAuditPath()
	if err := os.Rename(auditFile.path, rotated); err != nil {
		return err
	}
	if auditFile.compress {
		if err := compressAuditFile(rotated); err != nil {
			log.Printf("audit: compression failed: %s", sanitize.SanitizeLogText(err.Error()))
		}
	}
	if err := createActiveAuditFileLocked(); err != nil {
		return err
	}
	auditFile.lastLineNumber = 0
	if err := syncAuditDirectory(filepath.Dir(auditFile.path)); err != nil {
		return err
	}
	return pruneAuditFilesLocked()
}

func rotatedAuditPath() string {
	return auditFile.path + "." + time.Now().UTC().Format("2006-01-02.150405.000000000")
}

func compressAuditFile(path string) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(path), ".audit-gzip-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := applyAuditPathPermissionsLocked(tmp.Name(), false); err != nil {
		_ = tmp.Close()
		return err
	}
	writer := gzip.NewWriter(tmp)
	if _, err := io.Copy(writer, input); err != nil {
		_ = writer.Close()
		_ = tmp.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path+".gz"); err != nil {
		return err
	}
	return os.Remove(path)
}

func createActiveAuditFileLocked() error {
	file, err := os.OpenFile(auditFile.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	permissionErr := applyAuditPathPermissionsLocked(auditFile.path, false)
	closeErr := file.Close()
	if permissionErr != nil {
		return permissionErr
	}
	return closeErr
}

func applyAuditPathPermissionsLocked(path string, directory bool) error {
	if auditFile.groupEnabled {
		if err := setAuditPathPermissions(path, directory, true, auditFile.groupID); err == nil {
			return nil
		} else if !auditFile.groupWarned {
			log.Printf("audit: configured group permissions are unavailable; using owner-only permissions")
			auditFile.groupWarned = true
		}
		auditFile.groupEnabled = false
	}
	return setAuditPathPermissions(path, directory, false, 0)
}

func pruneAuditFilesLocked() error {
	if strings.EqualFold(strings.TrimSpace(auditFile.keep), "forever") {
		return nil
	}
	dir := filepath.Dir(auditFile.path)
	base := filepath.Base(auditFile.path) + "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	rotated := make([]os.DirEntry, 0)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), base) && !strings.HasPrefix(entry.Name(), ".") {
			rotated = append(rotated, entry)
		}
	}
	sort.Slice(rotated, func(i, j int) bool { return rotated[i].Name() < rotated[j].Name() })
	keep := strings.TrimSpace(strings.ToLower(auditFile.keep))
	if keep == "" || keep == "none" {
		for _, entry := range rotated {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return nil
	}
	if count, err := fmt.Sscanf(keep, "%d", new(int)); err == nil && count == 1 && !strings.ContainsAny(keep, "dwm") {
		var limit int
		_, _ = fmt.Sscanf(keep, "%d", &limit)
		if limit < len(rotated) {
			for _, entry := range rotated[:len(rotated)-limit] {
				if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return pruneAuditByAge(dir, rotated, keep)
}

func pruneAuditByAge(dir string, entries []os.DirEntry, keep string) error {
	var amount int
	var unit string
	if _, err := fmt.Sscanf(keep, "%d%s", &amount, &unit); err != nil || amount < 1 {
		return nil
	}
	duration := time.Duration(amount) * 24 * time.Hour
	switch unit {
	case "w":
		duration *= 7
	case "m":
		duration *= 30
	case "d":
	default:
		return nil
	}
	cutoff := time.Now().Add(-duration)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
