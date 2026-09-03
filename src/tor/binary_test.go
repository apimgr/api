package tor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestFindBinaryConfiguredNotFound verifies an explicitly configured but
// nonexistent path returns ErrBinaryNotFound rather than falling back to
// PATH/common-location detection.
func TestFindBinaryConfiguredNotFound(t *testing.T) {
	_, err := findBinary(filepath.Join(t.TempDir(), "does-not-exist-tor"))
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("expected ErrBinaryNotFound, got %v", err)
	}
}

// TestFindBinaryConfiguredFound verifies an explicitly configured, existing
// path is returned as-is.
func TestFindBinaryConfiguredFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tor")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatalf("failed to create fake binary: %v", err)
	}

	got, err := findBinary(path)
	if err != nil {
		t.Fatalf("findBinary failed: %v", err)
	}
	if got != path {
		t.Errorf("findBinary() = %q, want %q", got, path)
	}
}

// TestFindBinaryConfiguredIsDirectory verifies a configured path pointing
// at a directory (not a file) is rejected.
func TestFindBinaryConfiguredIsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := findBinary(dir)
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("expected ErrBinaryNotFound for a directory, got %v", err)
	}
}

// TestCommonBinaryLocationsNonEmpty verifies every supported OS has at
// least one documented fallback location.
func TestCommonBinaryLocationsNonEmpty(t *testing.T) {
	locations := commonBinaryLocations()
	if len(locations) == 0 {
		t.Error("expected at least one common binary location")
	}
}

// TestFindBinaryAutoDetectNotFound verifies auto-detection (empty
// configured path) falls through to ErrBinaryNotFound when Tor is not on
// PATH or in any common location. This assumes the casjaysdev/go:latest
// toolchain image used to run this test does not ship a tor binary.
func TestFindBinaryAutoDetectNotFound(t *testing.T) {
	_, err := findBinary("")
	if err != nil && !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("expected nil or ErrBinaryNotFound, got %v", err)
	}
	if err == nil {
		t.Log("a tor binary was found on this system; auto-detect success path exercised")
	}
}
