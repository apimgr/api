package tor

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// ErrBinaryNotFound is returned when no Tor executable could be located.
// Callers must treat this as informational, never fatal - Tor is optional.
var ErrBinaryNotFound = errors.New("tor binary not found")

// findBinary resolves the Tor executable path per AI.md PART 31's detection
// order: configured path > PATH > common per-OS install locations.
func findBinary(configured string) (string, error) {
	if configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured, nil
		}
		return "", ErrBinaryNotFound
	}

	if p, err := exec.LookPath("tor"); err == nil {
		return p, nil
	}

	for _, candidate := range commonBinaryLocations() {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", ErrBinaryNotFound
}

// commonBinaryLocations returns the per-OS fallback paths checked when Tor
// is not on PATH.
func commonBinaryLocations() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			`C:\Program Files\Tor\tor.exe`,
			`C:\Program Files (x86)\Tor\tor.exe`,
		}
	case "darwin":
		return []string{
			"/usr/local/bin/tor",
			"/opt/homebrew/bin/tor",
		}
	default:
		// Linux and BSD
		return []string{
			"/usr/bin/tor",
			"/usr/local/bin/tor",
		}
	}
}
