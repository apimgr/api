// Package critical routes critical runtime failures to the application
// maintenance controller without introducing package dependency cycles.
package critical

import (
	"os"
	"strings"
	"sync"
)

type failureHandler func(reason, message string)

var handlerState struct {
	sync.RWMutex
	handler        failureHandler
	pendingReason  string
	pendingMessage string
}

// RegisterFailureHandler installs the runtime's critical-failure handler.
func RegisterFailureHandler(handler func(reason, message string)) {
	handlerState.Lock()
	handlerState.handler = handler
	pendingReason, pendingMessage := handlerState.pendingReason, handlerState.pendingMessage
	if handler == nil || pendingReason != "" {
		handlerState.pendingReason = ""
		handlerState.pendingMessage = ""
	}
	handlerState.Unlock()
	if handler != nil && pendingReason != "" {
		handler(pendingReason, pendingMessage)
	}
}

// ReportWriteFailure reports a failed critical write using a safe, generic
// operator message that cannot disclose file contents or credentials.
func ReportWriteFailure(reason string) {
	if reason != "database_write" {
		reason = "file_write"
	}
	handlerState.Lock()
	handler := handlerState.handler
	if handler == nil {
		if handlerState.pendingReason == "" {
			handlerState.pendingReason = reason
			handlerState.pendingMessage = writeFailureMessage(reason)
		}
		handlerState.Unlock()
		return
	}
	handlerState.Unlock()
	handler(reason, writeFailureMessage(reason))
}

// ReportFileWriteFailure reports only filesystem conditions that indicate a
// critical write path is unavailable. Validation, encryption, and other
// application errors remain local to the operation that produced them.
func ReportFileWriteFailure(err error) bool {
	if err == nil || !IsCriticalFileWriteError(err) {
		return false
	}
	ReportWriteFailure("file_write")
	return true
}

// IsCriticalFileWriteError classifies permission, capacity, quota, and
// read-only filesystem failures without treating policy or data errors as
// server-wide maintenance triggers.
func IsCriticalFileWriteError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"no space left on device",
		"not enough space",
		"insufficient space",
		"disk is full",
		"disk full",
		"quota exceeded",
		"read-only file system",
		"read-only filesystem",
		"write protected",
		"write-protected",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func writeFailureMessage(reason string) string {
	message := "A critical file write failed. Check disk space, directory permissions, and mounted filesystems."
	if reason == "database_write" {
		message = "A database write failed. Check database availability, disk space, and write permissions."
	}
	return message
}
