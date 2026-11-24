//go:build windows

package main

import (
	"fmt"
	"os"
)

// daemonize is a no-op on Windows - Windows does not support Unix-style
// fork/detach. Per AI.md PART 8 "Daemonization (Windows)", --daemon is
// ignored with a warning; use --service --install to run as a Windows
// Service instead. Installation enables and starts the service.
func daemonize() error {
	fmt.Fprintln(os.Stderr, "Warning: --daemon is not supported on Windows")
	fmt.Fprintln(os.Stderr, "Use --service --install to install and start the Windows Service")
	return nil
}
