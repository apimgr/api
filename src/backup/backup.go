package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id key-derivation parameters (PART 21: password -> 256-bit AES-256-GCM key)
const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024
	argon2Threads = 4
	argon2KeyLen  = 32
)

// RestoreLimits bounds accepted archive size and extracted payloads.
type RestoreLimits struct {
	MaxArchiveSize  int64
	MaxExpandedSize int64
	MaxFiles        int
	MaxFileSize     int64
	MaxRatio        int64
}

// DefaultRestoreLimits returns the PART 16 archive extraction limits.
func DefaultRestoreLimits() RestoreLimits {
	return RestoreLimits{MaxArchiveSize: 1 << 30, MaxExpandedSize: 4 << 30, MaxFiles: 10000, MaxFileSize: 512 << 20, MaxRatio: 100}
}

func (limits RestoreLimits) normalized() RestoreLimits {
	defaults := DefaultRestoreLimits()
	if limits.MaxArchiveSize <= 0 {
		limits.MaxArchiveSize = defaults.MaxArchiveSize
	}
	if limits.MaxExpandedSize <= 0 {
		limits.MaxExpandedSize = defaults.MaxExpandedSize
	}
	if limits.MaxFiles <= 0 {
		limits.MaxFiles = defaults.MaxFiles
	}
	if limits.MaxFileSize <= 0 {
		limits.MaxFileSize = defaults.MaxFileSize
	}
	if limits.MaxRatio <= 0 {
		limits.MaxRatio = defaults.MaxRatio
	}
	return limits
}

// NormalizeRestoreLimits fills omitted or invalid fields from the spec defaults.
func NormalizeRestoreLimits(limits RestoreLimits) RestoreLimits { return limits.normalized() }

// Backup represents a backup file
type Backup struct {
	Version    string                 `json:"version"`
	CreatedAt  time.Time              `json:"created_at"`
	Encrypted  bool                   `json:"encrypted"`
	Compressed bool                   `json:"compressed"`
	Files      []string               `json:"files"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// CreateOptions describes one archive to build. It carries everything PART
// 21's manifest records plus the incremental cutoff used by the daily and
// hourly incremental archives.
type CreateOptions struct {
	// SnapshotAt is the UTC instant represented by this archive. A zero value
	// uses the current time.
	SnapshotAt time.Time
	// Sources are the files and directories to archive.
	Sources []string
	// SnapshotFiles is a frozen source set captured once at the beginning of
	// a scheduled run. When present it replaces live filesystem traversal.
	SnapshotFiles []SnapshotFile
	// SQLiteSourcePath is replaced in the archive by SQLiteSnapshotPath,
	// which must be created through the database store before archive writing.
	SQLiteSourcePath   string
	SQLiteSnapshotPath string
	// Password enables AES-256-GCM encryption when non-empty. The plaintext
	// archive is never written to disk: it is sealed in memory and only the
	// ciphertext reaches the file.
	Password string
	// Kind records which PART 21 naming pattern this archive belongs to.
	Kind Kind
	// BaseBackup names the full backup an incremental is taken against.
	BaseBackup string
	BaseSHA256 string
	Deleted    []string
	// ModifiedSince, when non-zero, restricts the archive to source files
	// modified at or after that moment - the "changes since full" content of
	// an incremental archive.
	ModifiedSince time.Time
	// AppVersion is stamped into the manifest.
	AppVersion string
}

// Create creates a backup of the specified directories/files
func Create(backupPath string, sources []string, password string) error {
	_, err := CreateWithOptions(backupPath, CreateOptions{
		Sources:  sources,
		Password: password,
		Kind:     KindManual,
	})
	return err
}

// CreateWithOptions builds an archive and returns the manifest that was
// written into it as the final entry.
func CreateWithOptions(backupPath string, opts CreateOptions) (*Manifest, error) {
	log.Printf("Backup: Creating backup to %s", backupPath)

	// Ensure backup directory exists
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		reportCriticalWriteError(err)
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	encrypted := opts.Password != ""

	// Create backup metadata
	snapshotAt := opts.SnapshotAt
	if snapshotAt.IsZero() {
		snapshotAt = time.Now().UTC()
	} else {
		snapshotAt = snapshotAt.UTC()
	}
	appVersion := opts.AppVersion
	if strings.TrimSpace(appVersion) == "" {
		appVersion = "unknown"
	}
	backup := Backup{
		Version:    "1.0",
		CreatedAt:  snapshotAt,
		Encrypted:  encrypted,
		Compressed: true,
		Files:      archiveSourceNames(opts.Sources),
		Metadata: map[string]interface{}{
			"hostname": getHostname(),
		},
	}

	contents := archiveSourceNames(opts.Sources)
	if len(contents) == 0 {
		contents = archiveContentsFromFiles(opts.SnapshotFiles, opts.Deleted)
	}
	manifest := &Manifest{
		Version:    manifestVersion,
		CreatedAt:  backup.CreatedAt,
		SnapshotAt: snapshotAt,
		CreatedBy:  currentOperator(),
		AppVersion: appVersion,
		Contents:   contents,
		Files:      []ManifestFile{},
		Type:       manifestType(opts.Kind),
		Encrypted:  encrypted,
		Kind:       opts.Kind,
		BaseBackup: opts.BaseBackup,
		BaseSHA256: opts.BaseSHA256,
		Deleted:    append([]string(nil), opts.Deleted...),
	}
	if encrypted {
		manifest.EncryptionMethod = EncryptionMethod
	}
	if !opts.ModifiedSince.IsZero() {
		cutoff := opts.ModifiedSince
		manifest.ModifiedSince = &cutoff
	}

	// Create temporary file for backup
	tmpFile := backupPath + ".tmp"
	file, err := os.Create(tmpFile)
	if err != nil {
		reportCriticalWriteError(err)
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() {
		if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
			log.Printf("Backup: failed to remove temporary archive %s: %v", filepath.Base(tmpFile), err)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		reportCriticalWriteError(err)
		return nil, errors.Join(fmt.Errorf("failed to set backup permissions: %w", err), file.Close())
	}

	fileWriter := criticalFileWriter{file: file}
	var writer io.WriteCloser = fileWriter

	// Apply encryption if password provided
	if encrypted {
		encryptor, err := encrypt(fileWriter, opts.Password, opts.BaseBackup)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("failed to setup encryption: %w", err), file.Close())
		}
		writer = encryptor
	}

	// Apply compression
	gzWriter := gzip.NewWriter(writer)

	// Create tar archive
	tarWriter := tar.NewWriter(gzWriter)

	// The digest covers every payload entry; the manifest carrying it is
	// written last so nothing has to be buffered to compute the checksum.
	digest := newPayloadDigest()

	writeErr := writeArchive(tarWriter, backup, manifest, opts, digest)

	// Close writers innermost first so buffered data is flushed before the
	// encrypted payload is sealed. Every close can report an incomplete archive.
	closeErr := errors.Join(tarWriter.Close(), gzWriter.Close())
	if encrypted {
		closeErr = errors.Join(closeErr, writer.Close())
	} else {
		closeErr = errors.Join(closeErr, writer.Close())
	}
	if err := errors.Join(writeErr, closeErr); err != nil {
		return nil, err
	}

	// Rename temp file to final name (atomic)
	if err := replaceBackupFile(tmpFile, backupPath); err != nil {
		reportCriticalWriteError(err)
		return nil, fmt.Errorf("failed to finalize backup: %w", err)
	}

	// Get file size
	info, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat finalized backup: %w", err)
	}
	log.Printf("Backup: Created successfully (%d bytes, encrypted: %v)", info.Size(), backup.Encrypted)

	return manifest, nil
}

// writeArchive writes the legacy metadata entry, the payload, and finally the
// manifest holding the payload checksum.
func writeArchive(tarWriter *tar.Writer, backup Backup, manifest *Manifest, opts CreateOptions, digest *payloadDigest) error {
	metadataJSON, err := json.Marshal(backup)
	if err != nil {
		return fmt.Errorf("failed to encode backup metadata: %w", err)
	}
	if err := addToTar(tarWriter, legacyMetadataName, metadataJSON, digest); err != nil {
		return err
	}

	// Add source files/directories
	seenSources := make(map[string]struct{})
	seenNames := make(map[string]struct{})
	if opts.SnapshotFiles != nil {
		for _, snapshot := range opts.SnapshotFiles {
			info, err := os.Stat(snapshot.Path)
			if err != nil {
				return fmt.Errorf("stat snapshotted source %s: %w", filepath.Base(snapshot.Path), err)
			}
			if err := addFileToTar(tarWriter, snapshot.Path, snapshot.ArchivePath, info, digest, seenSources, seenNames, &manifest.Files); err != nil {
				return err
			}
		}
	} else {
		for _, source := range opts.Sources {
			if err := addPathToTar(tarWriter, source, opts.ModifiedSince, digest, true, seenSources, seenNames, opts.SQLiteSourcePath, opts.SQLiteSnapshotPath, &manifest.Files); err != nil {
				return err
			}
		}
	}

	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	manifest.SHA256 = ManifestSHA256(manifest.Files)
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}

	// The manifest is excluded from its own checksum, so it is added with a
	// nil digest.
	return addToTar(tarWriter, ManifestName, manifestJSON, nil)
}

// Restore restores a backup from the specified file
func Restore(backupPath string, password string, allowedRoots ...string) (returnErr error) {
	return RestoreWithLimits(backupPath, password, DefaultRestoreLimits(), allowedRoots...)
}

// RestoreWithLimits verifies archive bounds before writing and enforces them
// again against the bytes actually streamed to disk.
func RestoreWithLimits(backupPath string, password string, limits RestoreLimits, allowedRoots ...string) (returnErr error) {
	return restoreWithLimits(backupPath, password, limits, nil, nil, allowedRoots...)
}

// restoreWithLimitsTransaction also invokes rollback after restoring the
// original files when commit fails. rollback is intended for coordinated
// external state such as reopening the prior database after a file swap.
func restoreWithLimitsTransaction(backupPath string, password string, limits RestoreLimits, commit, rollbackCommit func() error, allowedRoots ...string) (returnErr error) {
	return restoreWithLimits(backupPath, password, limits, commit, rollbackCommit, allowedRoots...)
}

func restoreWithLimits(backupPath string, password string, limits RestoreLimits, commit, rollbackCommit func() error, allowedRoots ...string) (returnErr error) {
	if len(allowedRoots) == 0 {
		return errors.New("restore requires at least one permitted destination")
	}
	limits = limits.normalized()
	manifest, err := VerifyWithLimits(backupPath, password, limits)
	if err != nil {
		return err
	}
	typeName := manifest.Type
	if typeName == "" {
		typeName = manifestType(manifest.Kind)
	}
	if typeName == "incremental" {
		return restoreIncrementalWithLimits(backupPath, password, limits, allowedRoots, manifest, commit, rollbackCommit)
	}
	log.Printf("Backup: Restoring from %s", backupPath)
	archiveInfo, err := os.Stat(backupPath)
	if err != nil {
		return fmt.Errorf("failed to inspect backup file: %w", err)
	}
	if !archiveInfo.Mode().IsRegular() {
		return errors.New("restore requires a regular backup file")
	}
	if archiveInfo.Size() <= 0 || archiveInfo.Size() > limits.MaxArchiveSize {
		return fmt.Errorf("backup archive size %d exceeds the allowed limit", archiveInfo.Size())
	}
	if _, err := inspectArchiveLimits(backupPath, password, archiveInfo.Size(), limits); err != nil {
		return err
	}

	file, gzReader, tarReader, err := openTarArchive(backupPath, password, limits.MaxArchiveSize)
	if err != nil {
		return err
	}
	var extracted int64
	var count int
	var createdDirs []string
	var staged []stagedRestoreFile
	commitAttempted := false
	destinations := make(map[string]struct{})
	rollback := func() error {
		var rollbackErr error
		for i := len(staged) - 1; i >= 0; i-- {
			item := staged[i]
			if item.backup != "" {
				if err := os.Remove(item.destination); err != nil && !errors.Is(err, os.ErrNotExist) {
					rollbackErr = errors.Join(rollbackErr, err)
				}
				renameErr := os.Rename(item.backup, item.destination)
				reportCriticalWriteError(renameErr)
				rollbackErr = errors.Join(rollbackErr, renameErr)
			} else if item.committed {
				if err := os.Remove(item.destination); err != nil && !errors.Is(err, os.ErrNotExist) {
					rollbackErr = errors.Join(rollbackErr, err)
				}
			}
			if item.temporary != "" {
				rollbackErr = errors.Join(rollbackErr, os.Remove(item.temporary))
			}
		}
		for i := len(createdDirs) - 1; i >= 0; i-- {
			rollbackErr = errors.Join(rollbackErr, os.Remove(createdDirs[i]))
		}
		return rollbackErr
	}
	defer func() {
		if returnErr != nil {
			rollbackErr := rollback()
			if commitAttempted && rollbackCommit != nil {
				rollbackErr = errors.Join(rollbackErr, rollbackCommit())
			}
			returnErr = errors.Join(returnErr, rollbackErr)
		}
	}()
	defer func() { returnErr = errors.Join(returnErr, gzReader.Close(), file.Close()) }()

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("refusing non-regular archive entry")
		}
		if header.Size < 0 || header.Size > limits.MaxFileSize || extracted > limits.MaxExpandedSize-header.Size {
			return fmt.Errorf("backup exceeds configured extraction size limits")
		}
		count++
		if count > limits.MaxFiles {
			return fmt.Errorf("backup exceeds the maximum file count of %d", limits.MaxFiles)
		}
		if header.Name == legacyMetadataName || header.Name == ManifestName {
			var fileBytes int64
			onBytes := func(n int64) error {
				fileBytes += n
				extracted += n
				if fileBytes > limits.MaxFileSize || extracted > limits.MaxExpandedSize || exceedsRatio(extracted, archiveInfo.Size(), limits.MaxRatio) {
					return fmt.Errorf("backup exceeds configured archive size or compression-ratio limit")
				}
				return nil
			}
			written, err := io.Copy(archiveBoundWriter{writer: io.Discard, onBytes: onBytes}, io.LimitReader(tarReader, header.Size+1))
			if err != nil {
				return fmt.Errorf("failed to read archive metadata: %w", err)
			}
			if written != header.Size {
				return fmt.Errorf("archive metadata entry is truncated or oversized")
			}
			continue
		}
		if header.Name == "app_secrets.json" {
			var fileBytes int64
			onBytes := func(n int64) error {
				fileBytes += n
				extracted += n
				if fileBytes > limits.MaxFileSize || extracted > limits.MaxExpandedSize || exceedsRatio(extracted, archiveInfo.Size(), limits.MaxRatio) {
					return fmt.Errorf("backup exceeds configured extraction size limits")
				}
				return nil
			}
			written, err := io.Copy(archiveBoundWriter{writer: io.Discard, onBytes: onBytes}, io.LimitReader(tarReader, header.Size+1))
			if err != nil || written != header.Size {
				return fmt.Errorf("invalid app_secrets.json archive payload")
			}
			continue
		}

		if err := checkArchivePath(header.Name); err != nil {
			return err
		}
		if header.Mode&06000 != 0 {
			return fmt.Errorf("refusing archive entry with setuid or setgid bits")
		}
		restoreName := header.Name
		externalTLS := strings.HasPrefix(header.Name, "ssl-external/")
		if externalTLS {
			if header.Name != "ssl-external/cert.pem" && header.Name != "ssl-external/key.pem" {
				return fmt.Errorf("refusing unknown external TLS archive path")
			}
			restoreName = filepath.ToSlash(filepath.Join("ssl", filepath.Base(filepath.FromSlash(header.Name))))
		}
		destination, err := prepareRestoreDestination(restoreName, allowedRoots)
		if err != nil {
			return fmt.Errorf("refusing archive entry outside permitted restore roots: %s", filepath.Base(header.Name))
		}

		// Extract file
		if err := ensureRestoreParents(filepath.Dir(destination), &createdDirs); err != nil {
			return err
		}
		if err := checkRestoreTarget(destination); err != nil {
			return err
		}
		if _, duplicate := destinations[destination]; duplicate {
			return fmt.Errorf("duplicate archive destination: %s", filepath.Base(destination))
		}
		destinations[destination] = struct{}{}
		var fileBytes int64
		restoreHeader := *header
		if externalTLS && filepath.Base(filepath.FromSlash(header.Name)) == "key.pem" {
			restoreHeader.Mode = 0600
		}
		stage, err := extractFromTar(tarReader, &restoreHeader, destination, func(n int64) error {
			fileBytes += n
			extracted += n
			if fileBytes > limits.MaxFileSize || extracted > limits.MaxExpandedSize || exceedsRatio(extracted, archiveInfo.Size(), limits.MaxRatio) {
				return fmt.Errorf("backup exceeds configured archive size or compression-ratio limit")
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(externalTLSConfigPaths(allowedRoots, manifest.Files)) > 0 && strings.HasSuffix(strings.ToLower(header.Name), ".yml") {
			ensure := filepath.Base(filepath.FromSlash(header.Name)) == "server.yml"
			if err := rewriteRestoredTLSConfig(stage.temporary, externalTLSConfigPaths(allowedRoots, manifest.Files), ensure); err != nil {
				return errors.Join(err, os.Remove(stage.temporary))
			}
		}
		staged = append(staged, stage)
	}
	commitHook := commit
	if commit != nil {
		commitHook = func() error {
			commitAttempted = true
			return commit()
		}
	}
	if err := commitRestoreFiles(staged, commitHook); err != nil {
		return err
	}

	log.Println("Backup: Restore completed successfully")
	return nil
}

func openTarArchive(path, password string, maxArchiveSize int64) (*os.File, *gzip.Reader, *tar.Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to open backup file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("failed to inspect backup file: %w", err)
	}
	if !info.Mode().IsRegular() || maxArchiveSize > 0 && info.Size() > maxArchiveSize {
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("backup file exceeds the configured archive limit")
	}
	reader := io.LimitReader(file, maxArchiveSize)
	if password != "" {
		reader, err = decrypt(reader, password)
		if err != nil {
			_ = file.Close()
			return nil, nil, nil, fmt.Errorf("failed to decrypt backup (wrong password?): %w", err)
		}
	}
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("failed to decompress backup: %w", err)
	}
	return file, gzReader, tar.NewReader(gzReader), nil
}

func inspectArchiveLimits(path, password string, archiveSize int64, limits RestoreLimits) (int64, error) {
	file, gz, tr, err := openTarArchive(path, password, limits.MaxArchiveSize)
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()
	defer func() { _ = gz.Close() }()
	var expanded int64
	var count int
	for {
		h, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return 0, fmt.Errorf("failed to inspect archive entry: %w", nextErr)
		}
		if h.Typeflag != tar.TypeReg {
			return 0, errors.New("refusing non-regular archive entry")
		}
		if h.Mode&06000 != 0 {
			return 0, errors.New("refusing archive entry with setuid or setgid bits")
		}
		if h.Name != legacyMetadataName && h.Name != ManifestName {
			if err := checkArchivePath(h.Name); err != nil {
				return 0, err
			}
		}
		if h.Size < 0 || h.Size > limits.MaxFileSize {
			return 0, fmt.Errorf("archive file exceeds the per-file limit")
		}
		count++
		if count > limits.MaxFiles || expanded > limits.MaxExpandedSize-h.Size {
			return 0, errors.New("archive exceeds configured file count or expanded-size limit")
		}
		expanded += h.Size
		if exceedsRatio(expanded, archiveSize, limits.MaxRatio) {
			return 0, errors.New("archive exceeds configured compression-ratio limit")
		}
	}
	return expanded, nil
}

func exceedsRatio(expanded, archiveSize, maxRatio int64) bool {
	if archiveSize <= 0 || maxRatio <= 0 {
		return true
	}
	whole := expanded / archiveSize
	return whole > maxRatio || whole == maxRatio && expanded%archiveSize != 0
}

type archiveBoundWriter struct {
	writer  io.Writer
	onBytes func(int64) error
}

func (writer archiveBoundWriter) Write(p []byte) (int, error) {
	if err := writer.onBytes(int64(len(p))); err != nil {
		return 0, err
	}
	return writer.writer.Write(p)
}

// encrypt encrypts data using AES-256-GCM
// Returns an io.WriteCloser that encrypts data as it's written
func encrypt(w io.Writer, password, baseBackup string) (io.WriteCloser, error) {
	if baseBackup != "" && (filepath.Base(baseBackup) != baseBackup || strings.ContainsAny(baseBackup, `/\\`) || !utf8.ValidString(baseBackup) || len(baseBackup) > 255) {
		return nil, errors.New("invalid incremental base name for encrypted archive header")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	header := make([]byte, 0, 55+len(baseBackup))
	header = append(header, []byte("BKUP1")...)
	header = append(header, salt...)
	var parameter [4]byte
	for _, value := range []uint32{argon2Memory, argon2Time, argon2Threads} {
		binary.BigEndian.PutUint32(parameter[:], value)
		header = append(header, parameter[:]...)
	}
	header = append(header, nonce...)
	keyIDMAC := hmac.New(sha256.New, key)
	_, _ = keyIDMAC.Write([]byte("key-id"))
	keyID := hex.EncodeToString(keyIDMAC.Sum(nil)[:4])
	header = append(header, keyID...)
	var baseLength [2]byte
	binary.BigEndian.PutUint16(baseLength[:], uint16(len(baseBackup)))
	header = append(header, baseLength[:]...)
	header = append(header, []byte(baseBackup)...)
	if _, err := w.Write(header); err != nil {
		return nil, err
	}

	// Return encrypted writer with buffering
	return &encryptedWriter{w: w, gcm: gcm, nonce: nonce, header: header, buf: make([]byte, 0, 65536)}, nil
}

// decrypt decrypts data using AES-256-GCM
func decrypt(r io.Reader, password string) (io.Reader, error) {
	header, salt, nonce, params, keyID, err := readEncryptionHeader(r)
	if err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(password), salt, params.time, params.memory, uint8(params.parallelism), argon2KeyLen)
	keyIDMAC := hmac.New(sha256.New, key)
	_, _ = keyIDMAC.Write([]byte("key-id"))
	derivedID := hex.EncodeToString(keyIDMAC.Sum(nil)[:4])
	if subtle.ConstantTimeCompare([]byte(derivedID), []byte(keyID)) != 1 {
		return nil, errors.New("encrypted archive key id does not match the supplied password")
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid encrypted archive nonce size")
	}

	// Return a reader that decrypts the buffered ciphertext on first read
	return &decryptedReader{r: r, gcm: gcm, nonce: nonce, header: header}, nil
}

type argonParameters struct{ memory, time, parallelism uint32 }

func readEncryptionHeader(r io.Reader) ([]byte, []byte, []byte, argonParameters, string, error) {
	fail := func(err error) ([]byte, []byte, []byte, argonParameters, string, error) {
		return nil, nil, nil, argonParameters{}, "", err
	}
	header := make([]byte, 55)
	if _, err := io.ReadFull(r, header); err != nil {
		return fail(fmt.Errorf("read encrypted archive header: %w", err))
	}
	if string(header[:5]) != "BKUP1" {
		return fail(errors.New("invalid encrypted archive magic"))
	}
	params := argonParameters{memory: binary.BigEndian.Uint32(header[21:25]), time: binary.BigEndian.Uint32(header[25:29]), parallelism: binary.BigEndian.Uint32(header[29:33])}
	if params.memory < 8192 || params.memory > 1048576 || params.time < 1 || params.time > 10 || params.parallelism < 1 || params.parallelism > 16 || params.memory < 8*params.parallelism {
		return fail(errors.New("encrypted archive Argon2id parameters exceed the allowed limits"))
	}
	baseLength := binary.BigEndian.Uint16(header[53:55])
	if baseLength > 255 {
		return fail(errors.New("encrypted archive base name exceeds 255 bytes"))
	}
	if baseLength > 0 {
		name := make([]byte, int(baseLength))
		if _, err := io.ReadFull(r, name); err != nil {
			return fail(errors.New("encrypted archive header ended before the base name"))
		}
		baseName := string(name)
		if !utf8.Valid(name) || filepath.Base(baseName) != baseName || strings.ContainsAny(baseName, `/\\`) {
			return fail(errors.New("encrypted archive base name is invalid"))
		}
		header = append(header, name...)
	}
	keyID := string(header[45:53])
	if _, err := hex.DecodeString(keyID); err != nil || strings.ToLower(keyID) != keyID {
		return fail(errors.New("encrypted archive key id is invalid"))
	}
	return header, append([]byte(nil), header[5:21]...), append([]byte(nil), header[33:45]...), params, keyID, nil
}

func encryptionPasswordMatches(path, password string) (bool, error) {
	if !IsEncryptedName(path) {
		if password != "" {
			return false, nil
		}
		file, err := os.Open(path)
		if err != nil {
			return false, err
		}
		defer func() { _ = file.Close() }()
		magic := make([]byte, 5)
		_, err = io.ReadFull(file, magic)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return false, err
		}
		if string(magic) == "BKUP1" {
			return false, errors.New("encrypted header found on an unencrypted backup filename")
		}
		return true, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	_, salt, _, params, keyID, err := readEncryptionHeader(file)
	if err != nil {
		return false, err
	}
	key := argon2.IDKey([]byte(password), salt, params.time, params.memory, uint8(params.parallelism), argon2KeyLen)
	keyMAC := hmac.New(sha256.New, key)
	_, _ = keyMAC.Write([]byte("key-id"))
	derivedID := hex.EncodeToString(keyMAC.Sum(nil)[:4])
	return subtle.ConstantTimeCompare([]byte(derivedID), []byte(keyID)) == 1, nil
}

func encryptedArchiveBaseName(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	header, _, _, _, _, err := readEncryptionHeader(file)
	if err != nil {
		return "", err
	}
	baseLength := int(binary.BigEndian.Uint16(header[53:55]))
	return string(header[55 : 55+baseLength]), nil
}

// encryptedWriter wraps a writer with encryption
type encryptedWriter struct {
	w      io.Writer
	gcm    cipher.AEAD
	nonce  []byte
	header []byte
	buf    []byte
}

func (ew *encryptedWriter) Write(p []byte) (n int, err error) {
	// Buffer data
	ew.buf = append(ew.buf, p...)
	return len(p), nil
}

func (ew *encryptedWriter) Close() error {
	// Encrypt buffered data. Seal must run even for an empty payload:
	// GCM always appends an authentication tag, so skipping the write
	// for zero-length input leaves the stream with no tag at all and
	// decryption later fails with "message authentication failed".
	encrypted := ew.gcm.Seal(nil, ew.nonce, ew.buf, ew.header)
	_, writeErr := ew.w.Write(encrypted)
	var closeErr error
	if closer, ok := ew.w.(io.Closer); ok {
		closeErr = closer.Close()
	}
	return errors.Join(writeErr, closeErr)
}

// decryptedReader wraps a reader with decryption
type decryptedReader struct {
	r         io.Reader
	gcm       cipher.AEAD
	nonce     []byte
	header    []byte
	decrypted []byte
	pos       int
}

func (dr *decryptedReader) Read(p []byte) (n int, err error) {
	// If first read, decrypt all data
	if dr.decrypted == nil {
		// Read all encrypted data
		encrypted, err := io.ReadAll(dr.r)
		if err != nil {
			return 0, err
		}

		// Decrypt data
		dr.decrypted, err = dr.gcm.Open(nil, dr.nonce, encrypted, dr.header)
		if err != nil {
			return 0, fmt.Errorf("decryption failed: %w", err)
		}
		dr.pos = 0
	}

	// Return decrypted data
	if dr.pos >= len(dr.decrypted) {
		return 0, io.EOF
	}

	n = copy(p, dr.decrypted[dr.pos:])
	dr.pos += n
	return n, nil
}

// addToTar adds a file to a tar archive, mixing it into the payload digest
// when one is supplied.
func addToTar(tw *tar.Writer, name string, data []byte, digest *payloadDigest) error {
	header := &tar.Header{
		Name:    name,
		Size:    int64(len(data)),
		Mode:    0644,
		ModTime: time.Now(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	digest.entry(name, header.Size)
	_, err := digest.writer(tw).Write(data)
	return err
}

// addPathToTar adds a file or directory to tar. A non-zero modifiedSince
// restricts the archive to entries changed at or after that moment, which is
// how incremental archives capture "changes since full".
func addPathToTar(tw *tar.Writer, path string, modifiedSince time.Time, digest *payloadDigest, prefixRoot bool, seenSources, seenNames map[string]struct{}, sqliteSource, sqliteSnapshot string, records *[]ManifestFile) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}

	// If it's a file, add it directly
	if !info.IsDir() {
		if samePath(path, sqliteSource) && sqliteSnapshot != "" {
			snapshotInfo, err := os.Stat(sqliteSnapshot)
			if err != nil {
				return fmt.Errorf("stat SQLite snapshot: %w", err)
			}
			return addFileToTar(tw, sqliteSnapshot, filepath.Base(path), snapshotInfo, digest, seenSources, seenNames, records)
		}
		if skipUnmodified(info, modifiedSince) {
			return nil
		}
		return addFileToTar(tw, path, filepath.Base(path), info, digest, seenSources, seenNames, records)
	}

	// If it's a directory, walk it
	return filepath.Walk(path, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories (they're created automatically when files are extracted)
		if fi.IsDir() {
			return nil
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			return nil
		}

		isSQLiteSource := samePath(file, sqliteSource) && sqliteSnapshot != ""
		if skipUnmodified(fi, modifiedSince) && !isSQLiteSource {
			return nil
		}

		name, err := archiveEntryName(path, file, prefixRoot)
		if err != nil {
			return err
		}
		if isSQLiteSource {
			snapshotInfo, err := os.Stat(sqliteSnapshot)
			if err != nil {
				return fmt.Errorf("stat SQLite snapshot: %w", err)
			}
			return addFileToTar(tw, sqliteSnapshot, name, snapshotInfo, digest, seenSources, seenNames, records)
		}
		return addFileToTar(tw, file, name, fi, digest, seenSources, seenNames, records)
	})
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

// skipUnmodified reports whether an incremental archive should leave a file
// out because it has not changed since the cutoff.
func skipUnmodified(info os.FileInfo, modifiedSince time.Time) bool {
	if modifiedSince.IsZero() {
		return false
	}
	return info.ModTime().Before(modifiedSince)
}

// addFileToTar adds a single file to tar
func addFileToTar(tw *tar.Writer, path, archiveName string, info os.FileInfo, digest *payloadDigest, seenSources, seenNames map[string]struct{}, records *[]ManifestFile) (returnErr error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, exists := seenSources[absolute]; exists {
		return nil
	}
	if _, exists := seenNames[archiveName]; exists {
		return fmt.Errorf("duplicate backup archive path: %s", archiveName)
	}
	seenSources[absolute] = struct{}{}
	seenNames[archiveName] = struct{}{}
	// Open file
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()

	// Create tar header
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("failed to create header for %s: %w", path, err)
	}
	// The archive format stores modification times at whole-second precision.
	header.ModTime = header.ModTime.Round(time.Second)

	// Set name to relative path
	header.Name = archiveName

	// Write header
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write header for %s: %w", path, err)
	}

	// Copy file contents
	digest.entry(header.Name, header.Size)
	fileHash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(digest.writer(tw), fileHash), file); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}
	*records = append(*records, ManifestFile{Path: archiveName, Size: header.Size, Modified: header.ModTime.UTC(), SHA256: hex.EncodeToString(fileHash.Sum(nil))})

	log.Printf("Backup: Added %s to archive", path)
	return nil
}

// extractFromTar extracts a file from tar
type stagedRestoreFile struct {
	temporary   string
	destination string
	backup      string
	committed   bool
}

func extractFromTar(tr *tar.Reader, header *tar.Header, destination string, onBytes func(int64) error) (stagedRestoreFile, error) {
	if header.Mode&06000 != 0 {
		return stagedRestoreFile{}, fmt.Errorf("refusing archive entry with setuid or setgid bits")
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".restore-*.tmp")
	if err != nil {
		reportCriticalWriteError(err)
		return stagedRestoreFile{}, fmt.Errorf("failed to stage restored file %s: %w", filepath.Base(destination), err)
	}
	temporary := file.Name()
	if err := file.Chmod(0600); err != nil {
		reportCriticalWriteError(err)
		_ = file.Close()
		_ = os.Remove(temporary)
		return stagedRestoreFile{}, err
	}
	written, copyErr := io.Copy(archiveBoundWriter{writer: criticalWriteReporter{writer: file}, onBytes: onBytes}, io.LimitReader(tr, header.Size+1))
	if copyErr != nil {
		return stagedRestoreFile{}, errors.Join(fmt.Errorf("failed to extract %s: %w", filepath.Base(destination), copyErr), file.Close(), os.Remove(temporary))
	}
	if written != header.Size {
		return stagedRestoreFile{}, errors.Join(fmt.Errorf("archive entry %s has a truncated or oversized body", filepath.Base(destination)), file.Close(), os.Remove(temporary))
	}
	if err := file.Sync(); err != nil {
		reportCriticalWriteError(err)
		return stagedRestoreFile{}, errors.Join(err, file.Close(), os.Remove(temporary))
	}
	if err := file.Close(); err != nil {
		reportCriticalWriteError(err)
		return stagedRestoreFile{}, errors.Join(fmt.Errorf("failed to close staged file %s: %w", filepath.Base(destination), err), os.Remove(temporary))
	}
	return stagedRestoreFile{temporary: temporary, destination: destination}, nil
}

func commitRestoreFiles(files []stagedRestoreFile, commit func() error) error {
	for i := range files {
		item := &files[i]
		if err := checkRestoreTarget(item.destination); err != nil {
			return err
		}
		if _, err := os.Lstat(item.destination); err == nil {
			backup, err := os.CreateTemp(filepath.Dir(item.destination), ".restore-backup-*.tmp")
			if err != nil {
				reportCriticalWriteError(err)
				return fmt.Errorf("stage existing restore target: %w", err)
			}
			item.backup = backup.Name()
			if err := backup.Close(); err != nil {
				reportCriticalWriteError(err)
				_ = os.Remove(item.backup)
				item.backup = ""
				return err
			}
			if err := os.Remove(item.backup); err != nil {
				return err
			}
			if err := os.Rename(item.destination, item.backup); err != nil {
				reportCriticalWriteError(err)
				item.backup = ""
				return fmt.Errorf("preserve existing restore target: %w", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(item.temporary, item.destination); err != nil {
			reportCriticalWriteError(err)
			return fmt.Errorf("install restored file %s: %w", filepath.Base(item.destination), err)
		}
		item.temporary = ""
		item.committed = true
	}
	if commit != nil {
		if err := commit(); err != nil {
			return fmt.Errorf("restore commit hook: %w", err)
		}
	}
	for i := range files {
		if files[i].backup != "" {
			if err := os.Remove(files[i].backup); err != nil {
				log.Printf("Backup: unable to remove preserved restore file %s: %v", filepath.Base(files[i].backup), err)
			} else {
				files[i].backup = ""
			}
		}
		log.Printf("Backup: Extracted %s", filepath.Base(files[i].destination))
	}
	return nil
}

func checkRestoreTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("restore target is not a regular file: %s", filepath.Base(path))
	}
	return nil
}

func ensureRestoreParents(path string, created *[]string) error {
	path = filepath.Clean(path)
	var missing []string
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("unsafe restore parent: %s", current)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		} else {
			missing = append(missing, current)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0700); err != nil {
			reportCriticalWriteError(err)
			return err
		}
		*created = append(*created, missing[i])
	}
	return nil
}

func archiveSourceNames(sources []string) []string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, filepath.Base(filepath.Clean(source)))
	}
	return names
}

func archiveContentsFromFiles(files []SnapshotFile, deleted []string) []string {
	seen := make(map[string]struct{})
	for _, file := range files {
		addArchiveContentRoot(seen, file.ArchivePath)
	}
	for _, file := range deleted {
		addArchiveContentRoot(seen, file)
	}
	contents := make([]string, 0, len(seen))
	for name := range seen {
		contents = append(contents, name)
	}
	sort.Strings(contents)
	return contents
}

func addArchiveContentRoot(seen map[string]struct{}, archivePath string) {
	if archivePath == "" || archivePath == "app_secrets.json" || strings.HasPrefix(archivePath, "ssl-external/") {
		return
	}
	parts := strings.Split(filepath.ToSlash(archivePath), "/")
	if parts[0] != "" {
		seen[parts[0]] = struct{}{}
	}
}

func archiveEntryName(root, file string, prefixRoot bool) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return filepath.ToSlash(filepath.Base(root)), nil
	}
	relative, err := filepath.Rel(root, file)
	if err != nil {
		return "", err
	}
	if relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", fmt.Errorf("source file escapes archive root")
	}
	if prefixRoot {
		relative = filepath.Join(filepath.Base(filepath.Clean(root)), relative)
	}
	return filepath.ToSlash(relative), nil
}

// checkArchivePath rejects archive entries that would escape their intended
// destination through a parent-directory traversal.
func checkArchivePath(name string) error {
	slashed := strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.ContainsRune(name, '\x00') || path.Clean(slashed) == "." || strings.HasPrefix(slashed, "/") || filepath.IsAbs(name) {
		return fmt.Errorf("refusing invalid archive entry path")
	}
	if len(slashed) >= 2 && ((slashed[0] >= 'A' && slashed[0] <= 'Z') || (slashed[0] >= 'a' && slashed[0] <= 'z')) && slashed[1] == ':' {
		return fmt.Errorf("refusing archive entry with volume prefix")
	}
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." || part == "." || part == "" {
			return fmt.Errorf("refusing archive entry with parent traversal")
		}
	}
	return nil
}

func prepareRestoreDestination(name string, roots []string) (string, error) {
	if err := checkArchivePath(name); err != nil {
		return "", err
	}
	entry := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(name, "\\", "/")))
	if filepath.IsAbs(entry) {
		return "", fmt.Errorf("absolute archive paths are forbidden")
	}
	parts := strings.Split(filepath.ToSlash(entry), "/")
	for _, root := range roots {
		absoluteRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absoluteRoot = filepath.Clean(absoluteRoot)
		baseName := filepath.Base(absoluteRoot)
		resolvedRoot, err := canonicalRestoreRoot(absoluteRoot)
		if err != nil {
			continue
		}
		var relative string
		if len(parts) > 0 && parts[0] == baseName {
			relative = filepath.Join(parts[1:]...)
			if relative == "." {
				relative = ""
			}
			if relative != "" && !isDirectoryTarget(resolvedRoot) {
				continue
			}
		} else if len(roots) == 1 {
			relative = entry
		} else {
			continue
		}
		destination := resolvedRoot
		if relative != "" {
			destination = filepath.Join(resolvedRoot, relative)
		}
		rel, err := filepath.Rel(resolvedRoot, destination)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		return destination, nil
	}
	return "", fmt.Errorf("path outside permitted roots")
}

func canonicalRestoreRoot(root string) (string, error) {
	current := filepath.Clean(root)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func isDirectoryTarget(path string) bool {
	info, err := os.Lstat(path)
	if err == nil {
		return info.IsDir() && info.Mode()&os.ModeSymlink == 0
	}
	return filepath.Ext(path) == ""
}

// relativeArchivePath turns an archive entry name into a path that can be
// safely joined onto a destination directory.
func relativeArchivePath(name string) string {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if volume := filepath.VolumeName(cleaned); volume != "" {
		cleaned = cleaned[len(volume):]
	}
	return strings.TrimLeft(cleaned, string(os.PathSeparator))
}

// getHostname returns the system hostname
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}
