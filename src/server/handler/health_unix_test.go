//go:build !windows

package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCheckDisk_Unix exercises the real Unix syscall.Statfs("/") path. The
// test host always has a root filesystem, so the only two legitimate
// outcomes are "ok" or "warning" (under 10% free); "error" would only occur
// if Statfs itself failed, which isn't reproducible without root filesystem
// unavailability.
func TestCheckDisk_Unix(t *testing.T) {
	got := checkDisk()
	assert.Contains(t, []string{"ok", "warning"}, got)
}

// TestCheckDisk_Unix_Idempotent confirms repeated calls are safe and
// consistent (no shared mutable state, no panics on repeat invocation).
func TestCheckDisk_Unix_Idempotent(t *testing.T) {
	first := checkDisk()
	second := checkDisk()
	assert.Equal(t, first, second)
}
