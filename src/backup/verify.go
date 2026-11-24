package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/apimgr/api/src/pgp"
	_ "modernc.org/sqlite"
)

// Verification check names, used in audit events and error messages so an
// operator can see exactly which PART 21 check failed.
const (
	CheckFileExists        = "file_exists"
	CheckSize              = "size"
	CheckFormat            = "format"
	CheckDecrypt           = "decrypt"
	CheckManifest          = "manifest"
	CheckChecksum          = "checksum"
	CheckExtraction        = "content_extraction"
	CheckDatabaseIntegrity = "database_integrity"
	CheckPGPKeyPair        = "pgp_key_pair"
)

// sqliteMagic is the 16-byte header every SQLite database file starts with.
var sqliteMagic = []byte("SQLite format 3\x00")

// dbIntegrityTimeout bounds the PRAGMA integrity_check run on an extracted
// database.
const dbIntegrityTimeout = 30 * time.Second

// VerificationError names the PART 21 check that failed. Every check is
// fatal: a backup that fails any of them is deleted and never counted.
type VerificationError struct {
	// Check is one of the Check* constants.
	Check string
	// Path is the archive that failed.
	Path string
	// Err is the underlying cause.
	Err error
}

// Error renders the failed check and its cause.
func (e *VerificationError) Error() string {
	return fmt.Sprintf("backup verification failed (%s) for %s: %v", e.Check, filepath.Base(e.Path), e.Err)
}

// Unwrap exposes the underlying cause to errors.Is/errors.As.
func (e *VerificationError) Unwrap() error {
	return e.Err
}

// verificationFailure builds a VerificationError from a message.
func verificationFailure(check, path, message string) *VerificationError {
	return &VerificationError{Check: check, Path: path, Err: errors.New(message)}
}

// Verify runs every PART 21 verification check against an archive: the file
// exists, is non-empty, decrypts (when encrypted), carries a readable
// manifest, extracts completely, matches its recorded checksum, and holds
// only valid databases. All checks must pass; the first failure is returned.
func Verify(path, password string) (*Manifest, error) {
	return VerifyWithLimits(path, password, DefaultRestoreLimits())
}

// VerifyWithLimits validates the archive and fully reads its contents under
// the same bounds later applied by RestoreWithLimits.
func VerifyWithLimits(path, password string, limits RestoreLimits) (*Manifest, error) {
	limits = NormalizeRestoreLimits(limits)
	info, err := os.Stat(path)
	if err != nil {
		return nil, &VerificationError{Check: CheckFileExists, Path: path, Err: err}
	}
	if info.IsDir() {
		return nil, verificationFailure(CheckFileExists, path, "backup path is a directory")
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > limits.MaxArchiveSize {
		return nil, verificationFailure(CheckSize, path, "backup file is empty, not regular, or exceeds the configured archive limit")
	}

	encrypted := IsEncryptedName(path) || password != ""
	if encrypted && password == "" {
		return nil, verificationFailure(CheckDecrypt, path, "backup is encrypted but no password was supplied")
	}

	decryptPassword := ""
	if encrypted {
		decryptPassword = password
	}
	probeFile, probeGzip, _, err := openTarArchive(path, decryptPassword, limits.MaxArchiveSize)
	if err != nil {
		check := CheckFormat
		if encrypted {
			check = CheckDecrypt
		}
		return nil, &VerificationError{Check: check, Path: path, Err: err}
	}
	_ = probeGzip.Close()
	_ = probeFile.Close()
	if _, err := inspectArchiveLimits(path, decryptPassword, info.Size(), limits); err != nil {
		return nil, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
	}

	file, gzReader, tarReader, err := openTarArchive(path, decryptPassword, limits.MaxArchiveSize)
	if err != nil {
		check := CheckFormat
		if encrypted {
			check = CheckDecrypt
		}
		return nil, &VerificationError{Check: check, Path: path, Err: err}
	}
	defer func() { _ = file.Close() }()
	defer func() { _ = gzReader.Close() }()

	// Extract every entry to a scratch directory to prove the archive is
	// readable end to end, then throw the copy away.
	scratch, err := scratchDir("verify")
	if err != nil {
		return nil, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	manifest, extracted, err := extractForVerification(tarReader, scratch, path, limits, info.Size())
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, verificationFailure(CheckManifest, path, "archive contains no manifest.json")
	}

	if subtle.ConstantTimeCompare([]byte(manifest.Checksum), []byte(extracted.checksum)) != 1 {
		return nil, verificationFailure(CheckChecksum, path, "payload checksum does not match the manifest")
	}
	if manifest.SHA256 != "" && subtle.ConstantTimeCompare([]byte(manifest.SHA256), []byte(extracted.checksum)) != 1 {
		return nil, verificationFailure(CheckChecksum, path, "manifest SHA-256 does not match the payload")
	}
	if len(manifest.Files) != 0 {
		if len(manifest.Files) != len(extracted.records) {
			return nil, verificationFailure(CheckManifest, path, "manifest file list does not match archive contents")
		}
		for _, expected := range manifest.Files {
			if err := checkArchivePath(expected.Path); err != nil {
				return nil, verificationFailure(CheckManifest, path, "manifest contains an invalid file path")
			}
			actual, ok := extracted.records[expected.Path]
			if !ok {
				return nil, verificationFailure(CheckManifest, path, "manifest file is missing from archive contents: "+expected.Path)
			}
			if expected.Size != actual.Size {
				return nil, verificationFailure(CheckManifest, path, "manifest file size does not match archive contents: "+expected.Path)
			}
			if !expected.Modified.Equal(actual.Modified) {
				return nil, verificationFailure(CheckManifest, path, fmt.Sprintf("manifest file timestamp does not match archive contents for %s: %s != %s", expected.Path, expected.Modified.UTC().Format(time.RFC3339Nano), actual.Modified.UTC().Format(time.RFC3339Nano)))
			}
			if expected.SHA256 != actual.SHA256 {
				return nil, verificationFailure(CheckManifest, path, "manifest file checksum does not match archive contents: "+expected.Path)
			}
		}
	}
	if err := verifyIncrementalBase(path, password, manifest, limits); err != nil {
		return nil, err
	}

	for _, candidate := range extracted.databases {
		if err := checkDatabaseIntegrity(candidate); err != nil {
			return nil, &VerificationError{Check: CheckDatabaseIntegrity, Path: path, Err: err}
		}
	}
	typeName := manifest.Type
	if typeName == "" {
		typeName = manifestType(manifest.Kind)
	}
	if typeName == "full" {
		if err := verifyPGPKeyPair(extracted.files); err != nil {
			return nil, &VerificationError{Check: CheckPGPKeyPair, Path: path, Err: err}
		}
	}

	return manifest, nil
}

func verifyPGPKeyPair(files map[string]string) error {
	var privatePath, publicPath, databasePath, statePath string
	for archivePath, extractedPath := range files {
		base := filepath.Base(filepath.FromSlash(archivePath))
		switch base {
		case "pgp.priv.asc.enc":
			privatePath = extractedPath
		case "pgp.pub.asc":
			publicPath = extractedPath
		case "server.db":
			databasePath = extractedPath
		case "keyservers.state":
			statePath = extractedPath
		}
	}
	if privatePath == "" && publicPath == "" {
		return nil
	}
	if privatePath == "" || publicPath == "" || databasePath == "" || statePath == "" {
		return errors.New("backup is missing the PGP keypair, keyserver state, or application database")
	}
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return fmt.Errorf("read backed-up keyserver state: %w", err)
	}
	var keyserverState struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(stateData, &keyserverState); err != nil || keyserverState.Version != 1 {
		return errors.New("backed-up keyserver state is invalid")
	}
	sealedPrivate, err := os.ReadFile(privatePath)
	if err != nil {
		return fmt.Errorf("read backed-up PGP private key: %w", err)
	}
	publicKey, err := os.ReadFile(publicPath)
	if err != nil {
		return fmt.Errorf("read backed-up PGP public key: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open backed-up application database: %w", err)
	}
	defer func() { _ = db.Close() }()
	var encodedSecret string
	if err := db.QueryRow(`SELECT value FROM app_secrets WHERE name = ?`, "installation_secret").Scan(&encodedSecret); err != nil {
		return fmt.Errorf("read backed-up installation secret: %w", err)
	}
	installationSecret, err := base64.StdEncoding.DecodeString(encodedSecret)
	if err != nil {
		return fmt.Errorf("decode backed-up installation secret: %w", err)
	}
	return pgp.VerifyEncryptedKeyPair(sealedPrivate, installationSecret, publicKey)
}

func verifyIncrementalBase(path, password string, manifest *Manifest, limits RestoreLimits) error {
	typeName := manifest.Type
	if typeName == "" {
		typeName = manifestType(manifest.Kind)
	}
	if typeName != "incremental" {
		return nil
	}
	if manifest.BaseBackup == "" || filepath.Base(manifest.BaseBackup) != manifest.BaseBackup || manifest.BaseSHA256 == "" {
		return &VerificationError{Check: CheckManifest, Path: path, Err: errors.New("incremental manifest is missing a safe base name or base SHA-256")}
	}
	for _, deleted := range manifest.Deleted {
		if err := checkArchivePath(deleted); err != nil {
			return &VerificationError{Check: CheckManifest, Path: path, Err: errors.New("incremental manifest contains an invalid deleted path")}
		}
	}
	basePath := filepath.Join(filepath.Dir(path), manifest.BaseBackup)
	info, err := os.Lstat(basePath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrBaseMissing, filepath.Base(basePath))
	}
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrBaseCorrupt, filepath.Base(basePath))
	}
	baseSHA, err := fileSHA256(basePath)
	if err != nil || subtle.ConstantTimeCompare([]byte(baseSHA), []byte(manifest.BaseSHA256)) != 1 {
		return fmt.Errorf("%w: %s", ErrBaseCorrupt, filepath.Base(basePath))
	}
	baseManifest, err := VerifyWithLimits(basePath, password, limits)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBaseCorrupt, filepath.Base(basePath))
	}
	baseType := baseManifest.Type
	if baseType == "" {
		baseType = manifestType(baseManifest.Kind)
	}
	if baseType != "full" {
		return fmt.Errorf("%w: incremental archive names a non-full base", ErrBaseCorrupt)
	}
	if manifest.Encrypted != baseManifest.Encrypted {
		return fmt.Errorf("%w: incremental and base encryption states differ", ErrBaseCorrupt)
	}
	return nil
}

// extractionResult carries what the extraction pass learned about an archive.
type extractionResult struct {
	// checksum is the recomputed payload digest.
	checksum string
	// databases are extracted files that carry the SQLite header.
	databases []string
	files     map[string]string
	records   map[string]ManifestFile
}

// extractForVerification writes every payload entry into scratch, recomputes
// the payload checksum, and parses the manifest.
func extractForVerification(tarReader *tar.Reader, scratch, path string, limits RestoreLimits, archiveSize int64) (*Manifest, extractionResult, error) {
	digest := newPayloadDigest()
	result := extractionResult{files: map[string]string{}, records: map[string]ManifestFile{}}
	var manifest *Manifest
	var expanded int64
	var count int
	seen := make(map[string]struct{})
	manifestSeen := false

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}

		if header.Typeflag != tar.TypeReg {
			return nil, result, verificationFailure(CheckExtraction, path, "archive contains a non-regular entry")
		}
		if header.Mode&06000 != 0 {
			return nil, result, verificationFailure(CheckExtraction, path, "archive entry contains setuid or setgid bits")
		}
		if header.Size < 0 || header.Size > limits.MaxFileSize || expanded > limits.MaxExpandedSize-header.Size {
			return nil, result, verificationFailure(CheckExtraction, path, "archive exceeds configured size limits")
		}
		count++
		if count > limits.MaxFiles {
			return nil, result, verificationFailure(CheckExtraction, path, "archive exceeds configured file-count limit")
		}
		var fileBytes int64
		onBytes := func(n int64) error {
			fileBytes += n
			expanded += n
			if fileBytes > limits.MaxFileSize || expanded > limits.MaxExpandedSize || exceedsRatio(expanded, archiveSize, limits.MaxRatio) {
				return fmt.Errorf("archive exceeds configured size or compression-ratio limit")
			}
			return nil
		}

		if header.Name == ManifestName {
			if manifestSeen {
				return nil, result, verificationFailure(CheckManifest, path, "archive contains multiple manifests")
			}
			manifestSeen = true
			var data bytes.Buffer
			written, err := io.Copy(archiveBoundWriter{writer: &data, onBytes: onBytes}, io.LimitReader(tarReader, header.Size+1))
			if err != nil {
				return nil, result, &VerificationError{Check: CheckManifest, Path: path, Err: err}
			}
			if written != header.Size {
				return nil, result, verificationFailure(CheckManifest, path, "manifest entry is truncated or oversized")
			}
			parsed := &Manifest{}
			if err := json.Unmarshal(data.Bytes(), parsed); err != nil {
				return nil, result, &VerificationError{Check: CheckManifest, Path: path, Err: err}
			}
			manifest = parsed
			continue
		}

		if err := checkArchivePath(header.Name); err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}

		target := filepath.Join(scratch, relativeArchivePath(header.Name))
		if _, duplicate := seen[target]; duplicate {
			return nil, result, verificationFailure(CheckExtraction, path, "archive contains a duplicate path")
		}
		seen[target] = struct{}{}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}

		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}

		digest.entry(header.Name, header.Size)
		fileHash := sha256.New()
		written, err := io.Copy(archiveBoundWriter{writer: io.MultiWriter(digest.writer(out), fileHash), onBytes: onBytes}, io.LimitReader(tarReader, header.Size+1))
		if err := out.Close(); err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}
		if err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}
		if written != header.Size {
			return nil, result, verificationFailure(CheckExtraction, path,
				fmt.Sprintf("entry %s is truncated (%d of %d bytes)", header.Name, written, header.Size))
		}

		if isSQLiteFile(target) {
			result.databases = append(result.databases, target)
		}
		result.files[header.Name] = target
		if header.Name != legacyMetadataName {
			result.records[header.Name] = ManifestFile{Path: header.Name, Size: header.Size, Modified: header.ModTime.UTC(), SHA256: "sha256:" + hex.EncodeToString(fileHash.Sum(nil))}
		}
	}

	result.checksum = digest.Sum()
	return manifest, result, nil
}

// isSQLiteFile reports whether an extracted file starts with the SQLite
// header, which is what makes it subject to the integrity check.
func isSQLiteFile(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	header := make([]byte, len(sqliteMagic))
	if _, err := io.ReadFull(file, header); err != nil {
		return false
	}
	return bytes.Equal(header, sqliteMagic)
}

// checkDatabaseIntegrity runs PRAGMA integrity_check against an extracted
// SQLite database, which is the PART 21 "database integrity" check.
func checkDatabaseIntegrity(path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), dbIntegrityTimeout)
	defer cancel()

	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("integrity check failed for %s: %w", filepath.Base(path), err)
	}
	if result != "ok" {
		return fmt.Errorf("integrity check reported %q for %s", result, filepath.Base(path))
	}
	return nil
}

// scratchDir creates a temp directory under the org-prefixed structure the
// project mandates for scratch space.
func scratchDir(purpose string) (string, error) {
	parent := filepath.Join(os.TempDir(), "apimgr")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, AppName+"-"+purpose+"-")
}
