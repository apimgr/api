//go:build !windows

package app

import (
	"syscall"
	"testing"
)

func TestClassifyRuntimeSignals(t *testing.T) {
	for _, test := range []struct {
		name string
		sig  syscall.Signal
		want runtimeSignalAction
	}{
		{name: "quit shuts down", sig: syscall.SIGQUIT, want: runtimeSignalShutdown},
		{name: "hangup ignored", sig: syscall.SIGHUP, want: runtimeSignalIgnore},
		{name: "usr1 reopens logs", sig: syscall.SIGUSR1, want: runtimeSignalReopenLogs},
		{name: "usr2 dumps status", sig: syscall.SIGUSR2, want: runtimeSignalDumpStatus},
		{name: "linux stop signal shuts down", sig: syscall.Signal(37), want: runtimeSignalShutdown},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyRuntimeSignal(test.sig); got != test.want {
				t.Fatalf("classifyRuntimeSignal(%v) = %d, want %d", test.sig, got, test.want)
			}
		})
	}
}
