package critical

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestReportWriteFailureUsesSafeMessageAndNormalizedReason(t *testing.T) {
	var gotReason, gotMessage string
	RegisterFailureHandler(func(reason, message string) {
		gotReason, gotMessage = reason, message
	})
	defer RegisterFailureHandler(nil)

	ReportWriteFailure("unexpected")
	if gotReason != "file_write" {
		t.Fatalf("reason = %q, want file_write", gotReason)
	}
	if gotMessage == "" {
		t.Fatal("message is empty")
	}
	ReportWriteFailure("database_write")
	if gotReason != "database_write" {
		t.Fatalf("reason = %q, want database_write", gotReason)
	}
	if gotMessage == "" {
		t.Fatal("database message is empty")
	}
}

func TestReportWriteFailureIsDeliveredWhenHandlerRegistersLater(t *testing.T) {
	RegisterFailureHandler(nil)
	ReportWriteFailure("database_write")
	var gotReason, gotMessage string
	RegisterFailureHandler(func(reason, message string) {
		gotReason, gotMessage = reason, message
	})
	defer RegisterFailureHandler(nil)
	if gotReason != "database_write" || gotMessage == "" {
		t.Fatalf("pending critical write report = %q, %q", gotReason, gotMessage)
	}
}

func TestCriticalFileWriteClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "permission", err: os.ErrPermission, want: true},
		{name: "wrapped disk full", err: fmt.Errorf("write archive: %w", errors.New("no space left on device")), want: true},
		{name: "read only", err: errors.New("read-only file system"), want: true},
		{name: "quota", err: errors.New("disk quota exceeded"), want: true},
		{name: "validation", err: errors.New("invalid archive manifest"), want: false},
		{name: "encryption", err: errors.New("incorrect password"), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCriticalFileWriteError(tc.err); got != tc.want {
				t.Fatalf("IsCriticalFileWriteError(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}
