//go:build !windows

package app

import (
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

type runtimeSignalAction uint8

const (
	runtimeSignalShutdown runtimeSignalAction = iota
	runtimeSignalReopenLogs
	runtimeSignalDumpStatus
	runtimeSignalIgnore
)

func watchRuntimeSignals() (<-chan os.Signal, func()) {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGQUIT, syscall.SIGUSR1, syscall.SIGUSR2)
	if runtime.GOOS == "linux" {
		signal.Notify(signals, syscall.Signal(37))
	}
	signal.Ignore(syscall.SIGHUP)
	return signals, func() {
		signal.Stop(signals)
		signal.Reset(syscall.SIGHUP)
	}
}

func classifyRuntimeSignal(sig os.Signal) runtimeSignalAction {
	switch sig {
	case syscall.SIGUSR1:
		return runtimeSignalReopenLogs
	case syscall.SIGUSR2:
		return runtimeSignalDumpStatus
	case syscall.SIGHUP:
		return runtimeSignalIgnore
	default:
		return runtimeSignalShutdown
	}
}
