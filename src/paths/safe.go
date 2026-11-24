package paths

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	// ErrPathTraversal is returned when a path attempts to escape its base.
	ErrPathTraversal = errors.New("path traversal attempt detected")
	// ErrInvalidPath is returned when a path contains unsupported characters.
	ErrInvalidPath = errors.New("invalid path characters")
	// ErrPathTooLong is returned when a path exceeds the supported limit.
	ErrPathTooLong   = errors.New("path exceeds maximum length")
	validPathSegment = regexp.MustCompile(`^[a-z0-9_-]+$`)
)

// SafePath validates and normalizes an application-relative URL or route path.
func SafePath(input string) (string, error) {
	if input == "" {
		return "", nil
	}
	if len(input) > 2048 {
		return "", ErrPathTooLong
	}
	if strings.Contains(input, "..") {
		return "", ErrPathTraversal
	}
	for _, segment := range strings.Split(strings.Trim(input, "/"), "/") {
		if segment == "" {
			continue
		}
		if segment == "." || segment == ".." {
			return "", ErrPathTraversal
		}
		if len(segment) > 64 {
			return "", ErrPathTooLong
		}
		if !validPathSegment.MatchString(segment) {
			return "", ErrInvalidPath
		}
	}
	return strings.Trim(path.Clean(input), "/"), nil
}

// SafeDirPath validates a filesystem directory override without applying URL
// segment restrictions. Relative paths are resolved against the current
// working directory so startup uses one stable absolute path.
func SafeDirPath(input string) (string, error) {
	if input == "" {
		return "", ErrInvalidPath
	}
	for _, char := range input {
		if unicode.IsControl(char) {
			return "", ErrInvalidPath
		}
	}
	for _, segment := range strings.Split(filepath.ToSlash(input), "/") {
		if segment == ".." {
			return "", ErrPathTraversal
		}
	}
	cleaned := filepath.Clean(input)
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// SafeFilePath joins a validated relative path to baseDir and verifies that
// the resulting absolute path remains inside the base directory.
func SafeFilePath(baseDir, userPath string) (string, error) {
	safe, err := SafePath(userPath)
	if err != nil {
		return "", err
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	basePath, err := resolvePathWithMissingTail(absBase)
	if err != nil {
		return "", err
	}
	absPath := filepath.Join(basePath, filepath.FromSlash(safe))
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(basePath, absPath)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrPathTraversal
	}
	// Reject symlinks in every existing path component beneath the base. A
	// lexical filepath.Rel check alone would accept a link that points outside
	// the directory and turns a safe-looking name into an escape path.
	for current := absPath; current != basePath; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", ErrPathTraversal
		}
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return "", statErr
		}
	}
	return absPath, nil
}

// resolvePathWithMissingTail resolves existing symlinked parents and then
// appends the remaining nonexistent components unchanged.
func resolvePathWithMissingTail(input string) (string, error) {
	current := input
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
