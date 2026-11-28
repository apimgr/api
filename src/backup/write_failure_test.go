package backup

import (
	"errors"
	"testing"

	"github.com/apimgr/api/src/critical"
)

type failingBackupWriter struct{ err error }

func (w failingBackupWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCriticalWriteReporterReportsOnlyFilesystemWriteFailures(t *testing.T) {
	var reasons []string
	critical.RegisterFailureHandler(func(reason, _ string) { reasons = append(reasons, reason) })
	t.Cleanup(func() { critical.RegisterFailureHandler(nil) })

	_, err := (criticalWriteReporter{writer: failingBackupWriter{err: errors.New("no space left on device")}}).Write([]byte("archive"))
	if err == nil {
		t.Fatal("expected injected archive write failure")
	}
	if len(reasons) != 1 || reasons[0] != "file_write" {
		t.Fatalf("critical reasons after disk-full write = %v, want [file_write]", reasons)
	}

	_, err = (criticalWriteReporter{writer: failingBackupWriter{err: errors.New("invalid encrypted archive")}}).Write([]byte("archive"))
	if err == nil {
		t.Fatal("expected injected validation failure")
	}
	if len(reasons) != 1 {
		t.Fatalf("critical reasons after validation error = %v, want no additional report", reasons)
	}
}
