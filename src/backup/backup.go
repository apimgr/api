package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/pbkdf2"
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

// Create creates a backup of the specified directories/files
func Create(backupPath string, sources []string, password string) error {
	log.Printf("Backup: Creating backup to %s", backupPath)

	// Ensure backup directory exists
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create backup metadata
	backup := Backup{
		Version:    "1.0",
		CreatedAt:  time.Now(),
		Encrypted:  password != "",
		Compressed: true,
		Files:      sources,
		Metadata: map[string]interface{}{
			"hostname": getHostname(),
		},
	}

	// Create temporary file for backup
	tmpFile := backupPath + ".tmp"
	file, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer os.Remove(tmpFile)

	var writer io.WriteCloser = file

	// Apply encryption if password provided
	if password != "" {
		encrypted, err := encrypt(file, password)
		if err != nil {
			file.Close()
			return fmt.Errorf("failed to setup encryption: %w", err)
		}
		writer = encrypted
	}

	// Apply compression
	gzWriter := gzip.NewWriter(writer)

	// Create tar archive
	tarWriter := tar.NewWriter(gzWriter)

	// Write metadata as first file
	metadataJSON, _ := json.Marshal(backup)
	if err := addToTar(tarWriter, "backup.json", metadataJSON); err != nil {
		tarWriter.Close()
		gzWriter.Close()
		writer.Close()
		file.Close()
		return err
	}

	// Add source files/directories
	for _, source := range sources {
		if err := addPathToTar(tarWriter, source); err != nil {
			tarWriter.Close()
			gzWriter.Close()
			writer.Close()
			file.Close()
			return err
		}
	}

	// Close all writers
	tarWriter.Close()
	gzWriter.Close()
	if password != "" {
		writer.Close()
	}
	file.Close()

	// Rename temp file to final name (atomic)
	if err := os.Rename(tmpFile, backupPath); err != nil {
		return fmt.Errorf("failed to finalize backup: %w", err)
	}

	// Get file size
	info, _ := os.Stat(backupPath)
	log.Printf("Backup: Created successfully (%d bytes, encrypted: %v)", info.Size(), backup.Encrypted)

	return nil
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

		// Skip metadata file
		if header.Name == "backup.json" {
			continue
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
	// Derive key from password using PBKDF2
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	key := pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)

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

	// Derive key from password
	key := pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)

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

	// TODO: Return a streaming cipher reader
	// For now, return original reader
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
	// Encrypt buffered data
	if len(ew.buf) > 0 {
		encrypted := ew.gcm.Seal(nil, ew.nonce, ew.buf, nil)
		if _, err := ew.w.Write(encrypted); err != nil {
			return err
		}
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

// addToTar adds a file to a tar archive
func addToTar(tw *tar.Writer, name string, data []byte) error {
	header := &tar.Header{
		Name:    name,
		Size:    int64(len(data)),
		Mode:    0644,
		ModTime: time.Now(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err := tw.Write(data)
	return err
}

// addPathToTar adds a file or directory to tar
func addPathToTar(tw *tar.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}

	// If it's a file, add it directly
	if !info.IsDir() {
		return addFileToTar(tw, path, info)
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

		return addFileToTar(tw, file, fi)
	})
}

// addFileToTar adds a single file to tar
func addFileToTar(tw *tar.Writer, path string, info os.FileInfo) error {
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
	if _, err := io.Copy(tw, file); err != nil {
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
