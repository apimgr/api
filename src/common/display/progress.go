package display

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Spinner is a stoppable progress indicator.
type Spinner interface {
	Start() error
	Stop() error
}

// TextSpinner provides a static text indicator without terminal control
// sequences.
type TextSpinner struct {
	message string
	writer  io.Writer
	mu      sync.Mutex
	started bool
}

// ANSISpinner animates a spinner in a terminal that supports ANSI control.
type ANSISpinner struct {
	message string
	writer  io.Writer
	stop    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	started bool
	stopped bool
	err     error
}

// NewSpinner selects an animated or text-only indicator from the detected
// terminal capabilities.
func NewSpinner(env DisplayEnv, message string) Spinner {
	if !CanUseANSI(env) {
		return &TextSpinner{message: message, writer: os.Stdout}
	}
	return &ANSISpinner{message: message, writer: os.Stdout}
}

// Start writes the static processing message.
func (s *TextSpinner) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	s.started = true
	_, err := fmt.Fprintf(s.writer, "%s...\n", s.message)
	return err
}

// Stop writes a completion message when Start was called.
func (s *TextSpinner) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil
	}
	s.started = false
	_, err := fmt.Fprintln(s.writer, "Done.")
	return err
}

// Start begins the ANSI animation.
func (s *ANSISpinner) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return nil
	}
	s.started = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.animate()
	return nil
}

func (s *ANSISpinner) animate() {
	defer close(s.done)
	frames := [...]string{"|", "/", "-", "\\"}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	index := 0
	for {
		select {
		case <-s.stop:
			if _, err := fmt.Fprintf(s.writer, "\r%s\r", strings.Repeat(" ", len(s.message)+4)); err != nil {
				s.setError(err)
			}
			return
		case <-ticker.C:
			if _, err := fmt.Fprintf(s.writer, "\r%s %s", frames[index], s.message); err != nil {
				s.setError(err)
				return
			}
			index = (index + 1) % len(frames)
		}
	}
}

// Stop ends the animation and waits for its goroutine to exit.
func (s *ANSISpinner) Stop() error {
	s.mu.Lock()
	if !s.started || s.stopped {
		err := s.err
		s.mu.Unlock()
		return err
	}
	s.stopped = true
	close(s.stop)
	done := s.done
	s.mu.Unlock()
	<-done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *ANSISpinner) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

// ShowProgress prints progress with an ANSI bar when supported, otherwise as
// a plain percentage. Percent is clamped to the documented 0–100 range.
func ShowProgress(env DisplayEnv, percent int) error {
	percent = max(0, min(percent, 100))
	if !CanUseANSI(env) {
		_, err := fmt.Printf("%d%% complete\n", percent)
		return err
	}
	_, err := fmt.Printf("\r[%-50s] %d%%", strings.Repeat("=", percent/2), percent)
	return err
}
