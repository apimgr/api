package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

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
	info, err := os.Stat(path)
	if err != nil {
		return nil, &VerificationError{Check: CheckFileExists, Path: path, Err: err}
	}
	if info.IsDir() {
		return nil, verificationFailure(CheckFileExists, path, "backup path is a directory")
	}
	if info.Size() == 0 {
		return nil, verificationFailure(CheckSize, path, "backup file is empty")
	}

	encrypted := IsEncryptedName(path)
	if encrypted && password == "" {
		return nil, verificationFailure(CheckDecrypt, path, "backup is encrypted but no password was supplied")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, &VerificationError{Check: CheckFileExists, Path: path, Err: err}
	}
	defer file.Close()

	var reader io.Reader = file
	if encrypted {
		decrypted, err := decrypt(file, password)
		if err != nil {
			return nil, &VerificationError{Check: CheckDecrypt, Path: path, Err: err}
		}
		reader = decrypted
	}

	// Reading the gzip header forces the first decryption read, so a wrong
	// password surfaces here rather than as a format problem.
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		check := CheckFormat
		if encrypted {
			check = CheckDecrypt
		}
		return nil, &VerificationError{Check: check, Path: path, Err: err}
	}
	defer gzReader.Close()

	// Extract every entry to a scratch directory to prove the archive is
	// readable end to end, then throw the copy away.
	scratch, err := scratchDir("verify")
	if err != nil {
		return nil, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
	}
	defer os.RemoveAll(scratch)

	manifest, extracted, err := extractForVerification(tar.NewReader(gzReader), scratch, path)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, verificationFailure(CheckManifest, path, "archive contains no manifest.json")
	}

	if subtle.ConstantTimeCompare([]byte(manifest.Checksum), []byte(extracted.checksum)) != 1 {
		return nil, verificationFailure(CheckChecksum, path, "payload checksum does not match the manifest")
	}

	for _, candidate := range extracted.databases {
		if err := checkDatabaseIntegrity(candidate); err != nil {
			return nil, &VerificationError{Check: CheckDatabaseIntegrity, Path: path, Err: err}
		}
	}

	return manifest, nil
}

// extractionResult carries what the extraction pass learned about an archive.
type extractionResult struct {
	// checksum is the recomputed payload digest.
	checksum string
	// databases are extracted files that carry the SQLite header.
	databases []string
}

// extractForVerification writes every payload entry into scratch, recomputes
// the payload checksum, and parses the manifest.
func extractForVerification(tarReader *tar.Reader, scratch, path string) (*Manifest, extractionResult, error) {
	digest := newPayloadDigest()
	result := extractionResult{}
	var manifest *Manifest

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		if header.Name == ManifestName {
			data, err := io.ReadAll(tarReader)
			if err != nil {
				return nil, result, &VerificationError{Check: CheckManifest, Path: path, Err: err}
			}
			parsed := &Manifest{}
			if err := json.Unmarshal(data, parsed); err != nil {
				return nil, result, &VerificationError{Check: CheckManifest, Path: path, Err: err}
			}
			manifest = parsed
			continue
		}

		if err := checkArchivePath(header.Name); err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}

		target := filepath.Join(scratch, relativeArchivePath(header.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}

		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return nil, result, &VerificationError{Check: CheckExtraction, Path: path, Err: err}
		}

		digest.entry(header.Name, header.Size)
		written, err := io.Copy(digest.writer(out), tarReader)
		out.Close()
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
	defer file.Close()

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
	defer db.Close()

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
