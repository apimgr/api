//go:build windows

package main

import (
	"fmt"
	"os"
)

// daemonize is a no-op on Windows - Windows does not support Unix-style
// fork/detach. Per AI.md PART 8 "Daemonization (Windows)", --daemon is
// ignored with a warning; use --service --install && --service start to
// run as a Windows Service instead.
func daemonize() error {
	fmt.Fprintln(os.Stderr, "Warning: --daemon is not supported on Windows")
	fmt.Fprintln(os.Stderr, "Use --service --install && --service start for Windows Service")
	return nil
}
