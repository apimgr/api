package config

import (
	"path/filepath"
	"testing"

	"github.com/apimgr/api/src/critical"
)

func TestAtomicConfigWriteFailureReportsCriticalFileEvent(t *testing.T) {
	var reason, message string
	critical.RegisterFailureHandler(func(gotReason, gotMessage string) {
		reason, message = gotReason, gotMessage
	})
	t.Cleanup(func() { critical.RegisterFailureHandler(nil) })

	err := writeConfigAtomically(filepath.Join(t.TempDir(), "missing", "server.yml"), []byte("server: {}\n"), 0600)
	if err == nil {
		t.Fatal("atomic config write unexpectedly succeeded")
	}
	if reason != "file_write" || message == "" {
		t.Fatalf("critical write report = %q, %q", reason, message)
	}
}
