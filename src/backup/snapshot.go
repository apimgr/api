package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// SnapshotFile binds a frozen file copy to its archive-relative path.
type SnapshotFile struct {
	ArchivePath string
	Path        string
}

// captureSources copies every regular source file into a private workspace
// before any archive is written, so all archives from one scheduled run use
// the same filesystem view. SQLite's online snapshot replaces its live path.
func captureSources(sources []string, workspace, sqliteSource, sqliteSnapshot string) ([]SnapshotFile, error) {
	files := make([]SnapshotFile, 0)
	seen := make(map[string]struct{})
	for _, source := range sources {
		info, err := os.Lstat(source)
		if err != nil {
			return nil, fmt.Errorf("inspect backup source %s: %w", filepath.Base(source), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.IsDir() {
			err = filepath.Walk(source, func(file string, current os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() {
					return nil
				}
				return captureSourceFile(source, file, current, workspace, sqliteSource, sqliteSnapshot, seen, &files)
			})
		} else if info.Mode().IsRegular() {
			err = captureSourceFile(source, source, info, workspace, sqliteSource, sqliteSnapshot, seen, &files)
		}
		if err != nil {
			return nil, fmt.Errorf("capture backup source %s: %w", filepath.Base(source), err)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ArchivePath < files[j].ArchivePath })
	return files, nil
}

func captureSupplementalSources(files []SnapshotFile, supplemental []SupplementalSource, workspace string) ([]SnapshotFile, error) {
	seen := make(map[string]struct{}, len(files)+len(supplemental))
	for _, file := range files {
		seen[file.ArchivePath] = struct{}{}
	}
	for _, source := range supplemental {
		if err := checkArchivePath(source.ArchivePath); err != nil {
			return nil, fmt.Errorf("invalid supplemental archive path")
		}
		if _, exists := seen[source.ArchivePath]; exists {
			return nil, fmt.Errorf("duplicate supplemental archive path: %s", source.ArchivePath)
		}
		info, err := os.Stat(source.Path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("supplemental backup source is not a regular file")
		}
		destination := filepath.Join(workspace, "files", filepath.FromSlash(source.ArchivePath))
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return nil, err
		}
		if err := copySnapshotFile(source.Path, destination, info); err != nil {
			return nil, err
		}
		files = append(files, SnapshotFile{ArchivePath: source.ArchivePath, Path: destination})
		seen[source.ArchivePath] = struct{}{}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ArchivePath < files[j].ArchivePath })
	return files, nil
}

func captureSourceFile(root, source string, sourceInfo os.FileInfo, workspace, sqliteSource, sqliteSnapshot string, seen map[string]struct{}, files *[]SnapshotFile) error {
	archiveName, err := archiveEntryName(root, source, true)
	if err != nil {
		return err
	}
	if err := checkArchivePath(archiveName); err != nil {
		return err
	}
	if _, exists := seen[archiveName]; exists {
		return fmt.Errorf("duplicate backup archive path: %s", archiveName)
	}
	seen[archiveName] = struct{}{}
	copySource := source
	copyInfo := sourceInfo
	if samePath(source, sqliteSource) && sqliteSnapshot != "" {
		copySource = sqliteSnapshot
		copyInfo, err = os.Stat(sqliteSnapshot)
		if err != nil {
			return fmt.Errorf("inspect SQLite snapshot: %w", err)
		}
	}
	destination := filepath.Join(workspace, "files", filepath.FromSlash(archiveName))
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	if err := copySnapshotFile(copySource, destination, copyInfo); err != nil {
		return err
	}
	*files = append(*files, SnapshotFile{ArchivePath: archiveName, Path: destination})
	return nil
}

func copySnapshotFile(source, destination string, expected os.FileInfo) (returnErr error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, input.Close()) }()
	before, err := input.Stat()
	if err != nil {
		return err
	}
	if before.Size() != expected.Size() || !before.ModTime().Equal(expected.ModTime()) {
		return fmt.Errorf("source changed while the backup snapshot was being captured")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Chmod(expected.Mode().Perm() & 0777)
	}
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return errors.Join(err, removeSnapshotFile(destination))
	}
	after, err := os.Stat(source)
	if err != nil {
		return errors.Join(err, removeSnapshotFile(destination))
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return errors.Join(fmt.Errorf("source changed during backup snapshot capture"), removeSnapshotFile(destination))
	}
	modTime := expected.ModTime()
	if err := os.Chtimes(destination, modTime, modTime); err != nil {
		return errors.Join(err, removeSnapshotFile(destination))
	}
	return nil
}

func removeSnapshotFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// fileSHA256 returns the archive hash used to bind incrementals to their base.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func snapshotRecord(path, archivePath string, info os.FileInfo) (ManifestFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return ManifestFile{}, err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ManifestFile{}, err
	}
	return ManifestFile{Path: archivePath, Size: info.Size(), Modified: info.ModTime().UTC(), SHA256: "sha256:" + hex.EncodeToString(hash.Sum(nil))}, nil
}

func diffSnapshotFiles(files []SnapshotFile, base []ManifestFile) ([]SnapshotFile, []string, error) {
	baseByPath := make(map[string]ManifestFile, len(base))
	for _, record := range base {
		baseByPath[record.Path] = record
	}
	changed := make([]SnapshotFile, 0, len(files))
	currentPaths := make(map[string]struct{}, len(files))
	for _, file := range files {
		info, err := os.Stat(file.Path)
		if err != nil {
			return nil, nil, err
		}
		current, err := snapshotRecord(file.Path, file.ArchivePath, info)
		if err != nil {
			return nil, nil, err
		}
		currentPaths[file.ArchivePath] = struct{}{}
		previous, exists := baseByPath[file.ArchivePath]
		if !exists || previous.Size != current.Size || !previous.Modified.Equal(current.Modified) || previous.SHA256 != current.SHA256 {
			changed = append(changed, file)
		}
	}
	deleted := make([]string, 0)
	for path := range baseByPath {
		if _, exists := currentPaths[path]; !exists {
			deleted = append(deleted, path)
		}
	}
	sort.Strings(deleted)
	return changed, deleted, nil
}
