package server

import (
	"errors"
	"io"
	"log"
)

// ReopenLogger replaces the current file-backed logger, allowing external
// log rotation to rename files before the process reopens them.
func ReopenLogger() error {
	current := globalLogger
	if current == nil {
		return errors.New("logger is not initialized")
	}
	replacement, err := NewLogger(current.config)
	if err != nil {
		return err
	}
	oldFiles := current.fileClosers()
	setLogOutput(current.accessLog, replacement.accessLog)
	setLogOutput(current.serverLog, replacement.serverLog)
	setLogOutput(current.errorLog, replacement.errorLog)
	setLogOutput(current.appLog, replacement.appLog)
	setLogOutput(current.authLog, replacement.authLog)
	setLogOutput(current.securityLog, replacement.securityLog)
	setLogOutput(current.debugLog, replacement.debugLog)
	return closeLogFiles(oldFiles)
}

func (l *Logger) fileClosers() []io.Closer {
	var closers []io.Closer
	seen := make(map[io.Closer]struct{})
	for _, output := range []*log.Logger{l.accessLog, l.serverLog, l.errorLog, l.appLog, l.authLog, l.securityLog, l.debugLog} {
		if output == nil {
			continue
		}
		writer, ok := output.Writer().(*criticalLogWriter)
		if !ok {
			continue
		}
		closer, ok := writer.writer.(io.Closer)
		if !ok {
			continue
		}
		if _, ok := seen[closer]; ok {
			continue
		}
		seen[closer] = struct{}{}
		closers = append(closers, closer)
	}
	return closers
}

func setLogOutput(current, replacement *log.Logger) {
	if current != nil && replacement != nil {
		current.SetOutput(replacement.Writer())
	}
}

func closeLogFiles(files []io.Closer) error {
	var closeErrors []error
	for _, file := range files {
		if err := file.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}
