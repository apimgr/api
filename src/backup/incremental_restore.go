package backup

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func readAppSecretsPayload(path, password string, limits RestoreLimits, manifest *Manifest) (payload []byte, returnErr error) {
	workspace, err := scratchDir("app-secrets-restore")
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, os.RemoveAll(workspace)) }()
	root := filepath.Join(workspace, "files")
	if err := os.Mkdir(root, 0700); err != nil {
		return nil, err
	}
	typeName := manifest.Type
	if typeName == "" {
		typeName = manifestType(manifest.Kind)
	}
	budget := &treeExtractionBudget{}
	if typeName == "incremental" {
		base := filepath.Join(filepath.Dir(path), manifest.BaseBackup)
		if err := extractArchiveTree(base, password, limits, root, budget); err != nil {
			return nil, err
		}
	}
	if err := extractArchiveTree(path, password, limits, root, budget); err != nil {
		return nil, err
	}
	if typeName == "incremental" {
		for _, deleted := range manifest.Deleted {
			if err := checkArchivePath(deleted); err != nil {
				return nil, err
			}
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(deleted))); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
	}
	payload, err = os.ReadFile(filepath.Join(root, "app_secrets.json"))
	if err != nil {
		return nil, fmt.Errorf("backup is missing required app_secrets.json: %w", err)
	}
	return payload, nil
}

func restoreIncrementalWithLimits(path, password string, limits RestoreLimits, roots []string, manifest *Manifest, commit, rollbackCommit func() error) (returnErr error) {
	basePath := filepath.Join(filepath.Dir(path), manifest.BaseBackup)
	baseManifest, err := VerifyWithLimits(basePath, password, limits)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBaseCorrupt, filepath.Base(basePath))
	}
	baseType := baseManifest.Type
	if baseType == "" {
		baseType = manifestType(baseManifest.Kind)
	}
	if baseType != "full" {
		return fmt.Errorf("%w: incremental base is not a full archive", ErrBaseCorrupt)
	}
	if manifest.Encrypted != baseManifest.Encrypted {
		return fmt.Errorf("%w: incremental and base encryption states differ", ErrBaseCorrupt)
	}
	baseInfo, err := os.Stat(basePath)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBaseMissing, filepath.Base(basePath))
	}
	incrementalInfo, err := os.Stat(path)
	if err != nil {
		return err
	}
	if baseInfo.Size() > limits.MaxArchiveSize-incrementalInfo.Size() {
		return errors.New("combined incremental archives exceed configured archive size limit")
	}

	workspace, err := scratchDir("incremental-restore")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(workspace); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	merged := filepath.Join(workspace, "merged")
	if err := os.Mkdir(merged, 0700); err != nil {
		reportCriticalWriteError(err)
		return err
	}
	budget := &treeExtractionBudget{}
	if err := extractArchiveTree(basePath, password, limits, merged, budget); err != nil {
		return err
	}
	if err := extractArchiveTree(path, password, limits, merged, budget); err != nil {
		return err
	}
	for _, deleted := range manifest.Deleted {
		if err := checkArchivePath(deleted); err != nil {
			return fmt.Errorf("invalid incremental deleted path")
		}
		target := filepath.Join(merged, filepath.FromSlash(deleted))
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("apply incremental deletion for %s: %w", filepath.Base(target), err)
		}
	}
	if err := enforceCombinedRestoreLimits(merged, baseInfo.Size()+incrementalInfo.Size(), limits); err != nil {
		return err
	}
	sources, err := snapshotRoots(merged)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return errors.New("incremental backup reconstructs to an empty file set")
	}
	combinedPath := filepath.Join(workspace, "combined.tar.gz")
	combinedPassword := passwordForArchive(path, password)
	if combinedPassword != "" {
		combinedPath += ".enc"
	}
	if _, err := CreateWithOptions(combinedPath, CreateOptions{
		SnapshotAt: manifest.SnapshotAt,
		Sources:    sources,
		Password:   combinedPassword,
		Kind:       KindFull,
		AppVersion: manifest.AppVersion,
	}); err != nil {
		return fmt.Errorf("prepare reconstructed restore: %w", err)
	}
	combinedInfo, err := os.Stat(combinedPath)
	if err != nil {
		return err
	}
	combinedLimits := limits
	combinedLimits.MaxArchiveSize = combinedInfo.Size()
	return restoreWithLimits(combinedPath, passwordForArchive(combinedPath, password), combinedLimits, commit, rollbackCommit, roots...)
}

func passwordForArchive(path, password string) string {
	if IsEncryptedName(path) || password != "" {
		return password
	}
	return ""
}

type treeExtractionBudget struct {
	expanded    int64
	archiveSize int64
	count       int
}

func extractArchiveTree(path, password string, limits RestoreLimits, destination string, budget *treeExtractionBudget) error {
	limits = limits.normalized()
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= 0 || info.Size() > limits.MaxArchiveSize || budget.archiveSize > limits.MaxArchiveSize-info.Size() {
		return errors.New("incremental reconstruction exceeds configured archive-size limit")
	}
	budget.archiveSize += info.Size()
	archivePassword := passwordForArchive(path, password)
	file, gz, reader, err := openTarArchive(path, archivePassword, limits.MaxArchiveSize)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	defer func() { _ = gz.Close() }()
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Name == legacyMetadataName || header.Name == ManifestName {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 {
			return errors.New("refusing invalid entry while reconstructing incremental backup")
		}
		budget.count++
		if header.Size > limits.MaxFileSize || budget.count > limits.MaxFiles || budget.expanded > limits.MaxExpandedSize-header.Size {
			return errors.New("incremental reconstruction exceeds configured extraction limits")
		}
		budget.expanded += header.Size
		if err := checkArchivePath(header.Name); err != nil {
			return err
		}
		target := filepath.Join(destination, relativeArchivePath(header.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			reportCriticalWriteError(err)
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			reportCriticalWriteError(err)
			return err
		}
		written, copyErr := io.Copy(criticalWriteReporter{writer: output}, io.LimitReader(reader, header.Size+1))
		if copyErr == nil && written != header.Size {
			copyErr = errors.New("incremental archive entry is truncated or oversized")
		}
		if copyErr == nil {
			copyErr = output.Chmod(os.FileMode(header.Mode & 0777))
			reportCriticalWriteError(copyErr)
		}
		closeErr := output.Close()
		reportCriticalWriteError(closeErr)
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		modTime := header.ModTime
		if modTime.IsZero() {
			modTime = time.Unix(0, 0).UTC()
		}
		if err := os.Chtimes(target, modTime, modTime); err != nil {
			return err
		}
		if exceedsRatio(budget.expanded, budget.archiveSize, limits.MaxRatio) {
			return errors.New("incremental reconstruction exceeds configured compression-ratio limit")
		}
	}
}

func enforceCombinedRestoreLimits(root string, archiveSize int64, limits RestoreLimits) error {
	var expanded int64
	var count int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("reconstructed backup contains a non-regular file")
		}
		count++
		if count > limits.MaxFiles || info.Size() > limits.MaxFileSize || expanded > limits.MaxExpandedSize-info.Size() {
			return errors.New("combined incremental backup exceeds configured extraction limits")
		}
		expanded += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	if exceedsRatio(expanded, archiveSize, limits.MaxRatio) {
		return errors.New("combined incremental backup exceeds configured compression-ratio limit")
	}
	return nil
}

func snapshotRoots(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	roots := make([]string, 0, len(entries))
	for _, entry := range entries {
		roots = append(roots, filepath.Join(root, entry.Name()))
	}
	return roots, nil
}
