//go:build windows

package app

import "os"

type runtimeSignalAction uint8

const (
	runtimeSignalShutdown runtimeSignalAction = iota
	runtimeSignalReopenLogs
	runtimeSignalDumpStatus
	runtimeSignalIgnore
)

func watchRuntimeSignals() (<-chan os.Signal, func()) {
	return nil, func() {}
}

func classifyRuntimeSignal(os.Signal) runtimeSignalAction {
	return runtimeSignalIgnore
}
