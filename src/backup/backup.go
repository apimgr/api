package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Argon2id key-derivation parameters (PART 21: password -> 256-bit AES-256-GCM key)
const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024
	argon2Threads = 4
	argon2KeyLen  = 32
)

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
	// Sources are the files and directories to archive.
	Sources []string
	// Password enables AES-256-GCM encryption when non-empty. The plaintext
	// archive is never written to disk: it is sealed in memory and only the
	// ciphertext reaches the file.
	Password string
	// Kind records which PART 21 naming pattern this archive belongs to.
	Kind Kind
	// BaseBackup names the full backup an incremental is taken against.
	BaseBackup string
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
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	encrypted := opts.Password != ""

	// Create backup metadata
	backup := Backup{
		Version:    "1.0",
		CreatedAt:  time.Now(),
		Encrypted:  encrypted,
		Compressed: true,
		Files:      opts.Sources,
		Metadata: map[string]interface{}{
			"hostname": getHostname(),
		},
	}

	manifest := &Manifest{
		Version:    manifestVersion,
		CreatedAt:  backup.CreatedAt,
		CreatedBy:  currentOperator(),
		AppVersion: opts.AppVersion,
		Contents:   opts.Sources,
		Encrypted:  encrypted,
		Kind:       opts.Kind,
		BaseBackup: opts.BaseBackup,
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
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer os.Remove(tmpFile)

	var writer io.WriteCloser = file

	// Apply encryption if password provided
	if encrypted {
		encryptor, err := encrypt(file, opts.Password)
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to setup encryption: %w", err)
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
	// encrypted payload is sealed.
	tarWriter.Close()
	gzWriter.Close()
	if encrypted {
		writer.Close()
	}
	// The encrypting writer already closed the file; a second close simply
	// reports "file already closed" and is not an archive failure.
	file.Close()

	if writeErr != nil {
		return nil, writeErr
	}

	// Rename temp file to final name (atomic)
	if err := os.Rename(tmpFile, backupPath); err != nil {
		return nil, fmt.Errorf("failed to finalize backup: %w", err)
	}

	// Get file size
	info, _ := os.Stat(backupPath)
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
	for _, source := range opts.Sources {
		if err := addPathToTar(tarWriter, source, opts.ModifiedSince, digest); err != nil {
			return err
		}
	}

	manifest.Checksum = digest.Sum()
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}

	// The manifest is excluded from its own checksum, so it is added with a
	// nil digest.
	return addToTar(tarWriter, ManifestName, manifestJSON, nil)
}

// Restore restores a backup from the specified file
func Restore(backupPath string, password string) error {
	log.Printf("Backup: Restoring from %s", backupPath)

	// Open backup file
	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer file.Close()

	var reader io.Reader = file

	// Try to decrypt (will fail if not encrypted or wrong password)
	if password != "" {
		decrypted, err := decrypt(file, password)
		if err != nil {
			return fmt.Errorf("failed to decrypt backup (wrong password?): %w", err)
		}
		reader = decrypted
	}

	// Decompress
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("failed to decompress backup: %w", err)
	}
	defer gzReader.Close()

	// Extract tar archive
	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Skip the metadata and manifest entries, which describe the archive
		// rather than belonging to the restored tree.
		if header.Name == legacyMetadataName || header.Name == ManifestName {
			continue
		}

		// Only regular files are restored; symlinks and devices in an
		// archive are a path-escape vector.
		if header.Typeflag != tar.TypeReg {
			continue
		}

		if err := checkArchivePath(header.Name); err != nil {
			return err
		}

		// Extract file
		if err := extractFromTar(tarReader, header); err != nil {
			return err
		}
	}

	log.Println("Backup: Restore completed successfully")
	return nil
}

// encrypt encrypts data using AES-256-GCM
// Returns an io.WriteCloser that encrypts data as it's written
func encrypt(w io.Writer, password string) (io.WriteCloser, error) {
	// Derive key from password using Argon2id
	salt := make([]byte, 32)
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

	// Write salt and nonce first (needed for decryption)
	if _, err := w.Write(salt); err != nil {
		return nil, err
	}
	if _, err := w.Write(nonce); err != nil {
		return nil, err
	}

	// Return encrypted writer with buffering
	return &encryptedWriter{w: w, gcm: gcm, nonce: nonce, buf: make([]byte, 0, 65536)}, nil
}

// decrypt decrypts data using AES-256-GCM
func decrypt(r io.Reader, password string) (io.Reader, error) {
	// Read salt
	salt := make([]byte, 32)
	if _, err := io.ReadFull(r, salt); err != nil {
		return nil, err
	}

	// Derive key from password using Argon2id
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

	// Read nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(r, nonce); err != nil {
		return nil, err
	}

	// Return a reader that decrypts the buffered ciphertext on first read
	return &decryptedReader{r: r, gcm: gcm, nonce: nonce}, nil
}

// encryptedWriter wraps a writer with encryption
type encryptedWriter struct {
	w     io.Writer
	gcm   cipher.AEAD
	nonce []byte
	buf   []byte
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
	encrypted := ew.gcm.Seal(nil, ew.nonce, ew.buf, nil)
	if _, err := ew.w.Write(encrypted); err != nil {
		return err
	}

	// Close underlying writer if possible
	if closer, ok := ew.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// decryptedReader wraps a reader with decryption
type decryptedReader struct {
	r         io.Reader
	gcm       cipher.AEAD
	nonce     []byte
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
		dr.decrypted, err = dr.gcm.Open(nil, dr.nonce, encrypted, nil)
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
func addPathToTar(tw *tar.Writer, path string, modifiedSince time.Time, digest *payloadDigest) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}

	// If it's a file, add it directly
	if !info.IsDir() {
		if skipUnmodified(info, modifiedSince) {
			return nil
		}
		return addFileToTar(tw, path, info, digest)
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

		if skipUnmodified(fi, modifiedSince) {
			return nil
		}

		return addFileToTar(tw, file, fi, digest)
	})
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
func addFileToTar(tw *tar.Writer, path string, info os.FileInfo, digest *payloadDigest) error {
	// Open file
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer file.Close()

	// Create tar header
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("failed to create header for %s: %w", path, err)
	}

	// Set name to relative path
	header.Name = path

	// Write header
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write header for %s: %w", path, err)
	}

	// Copy file contents
	digest.entry(header.Name, header.Size)
	if _, err := io.Copy(digest.writer(tw), file); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	log.Printf("Backup: Added %s to archive", path)
	return nil
}

// extractFromTar extracts a file from tar
func extractFromTar(tr *tar.Reader, header *tar.Header) error {
	// Create parent directories
	dir := filepath.Dir(header.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Extract file
	file, err := os.Create(header.Name)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", header.Name, err)
	}
	defer file.Close()

	// Copy contents
	if _, err := io.Copy(file, tr); err != nil {
		return fmt.Errorf("failed to extract %s: %w", header.Name, err)
	}

	// Set permissions
	if err := os.Chmod(header.Name, os.FileMode(header.Mode)); err != nil {
		log.Printf("Warning: Failed to set permissions on %s: %v", header.Name, err)
	}

	log.Printf("Backup: Extracted %s", header.Name)
	return nil
}

// checkArchivePath rejects archive entries that would escape their intended
// destination through a parent-directory traversal.
func checkArchivePath(name string) error {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	for _, part := range strings.Split(cleaned, string(os.PathSeparator)) {
		if part == ".." {
			return fmt.Errorf("refusing archive entry with parent traversal: %s", name)
		}
	}
	return nil
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

// CleanupOldBackups removes old backups keeping only the specified count
func CleanupOldBackups(backupDir string, keepCount int) error {
	log.Printf("Backup: Cleanup (keep last %d backups)", keepCount)

	// List all backup files
	files, err := filepath.Glob(filepath.Join(backupDir, "backup-*.tar.gz"))
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	// If we have fewer backups than keepCount, nothing to clean
	if len(files) <= keepCount {
		log.Printf("Backup: %d backups found, no cleanup needed", len(files))
		return nil
	}

	// Sort by modification time (oldest first)
	type fileInfo struct {
		path    string
		modTime time.Time
	}

	infos := make([]fileInfo, 0, len(files))
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		infos = append(infos, fileInfo{path: file, modTime: info.ModTime()})
	}

	// Sort by modification time
	for i := 0; i < len(infos)-1; i++ {
		for j := i + 1; j < len(infos); j++ {
			if infos[i].modTime.After(infos[j].modTime) {
				infos[i], infos[j] = infos[j], infos[i]
			}
		}
	}

	// Delete oldest backups (keep only keepCount newest)
	deleteCount := len(infos) - keepCount
	for i := 0; i < deleteCount; i++ {
		if err := os.Remove(infos[i].path); err != nil {
			log.Printf("Warning: Failed to delete old backup %s: %v", infos[i].path, err)
		} else {
			log.Printf("Backup: Deleted old backup %s", filepath.Base(infos[i].path))
		}
	}

	log.Printf("Backup: Cleanup complete (%d backups deleted, %d kept)", deleteCount, keepCount)
	return nil
}
