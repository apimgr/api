//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// daemonize forks the current process, detaches it from the controlling
// terminal, and exits the parent, per AI.md PART 8 "Daemonization (--daemon
// flag)". The child re-execs the same binary with the same arguments
// (minus --daemon, to avoid an infinite fork loop) and a marker env var so
// it knows to continue running instead of forking again.
func daemonize() error {
	// Already the daemonized child - continue normal startup
	if os.Getenv("_DAEMON_CHILD") != "" {
		return nil
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	args := filterDaemonFlag(os.Args[1:])

	cmd := exec.Command(execPath, args...)
	cmd.Env = append(os.Environ(), "_DAEMON_CHILD=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Create new session (detach from controlling terminal)
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}

	fmt.Printf("Daemon started (pid %d)\n", cmd.Process.Pid)
	os.Exit(0)
	return nil
}

// filterDaemonFlag removes --daemon from args to prevent an infinite fork
// loop when the daemonized child re-execs itself.
func filterDaemonFlag(args []string) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "--daemon" {
			filtered = append(filtered, arg)
		}
	}
	return filtered
}
