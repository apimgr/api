package server

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
)

// timingEntry is one named span recorded for the Server-Timing header, per
// AI.md PART 11 → "Server-Timing (Debug Mode Only)".
type timingEntry struct {
	name string
	dur  time.Duration
}

// serverTimingWriter wraps http.ResponseWriter to build and inject the
// Server-Timing response header the moment the first byte would otherwise
// leave the server (WriteHeader or an implicit-200 Write) — headers can
// only be set before that point. Named spans recorded up to that instant
// (via RecordTiming) plus a "total" span measured from the writer's
// creation are joined into the header value.
type serverTimingWriter struct {
	http.ResponseWriter
	start      time.Time
	enabled    bool
	mu         sync.Mutex
	timings    []timingEntry
	headerSent bool
}

// RecordTiming appends a named span (e.g. "render") to be included in the
// Server-Timing header. Safe to call before the header has been flushed;
// a call after flush is a no-op since the header line has already left.
func (w *serverTimingWriter) RecordTiming(name string, d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.headerSent {
		return
	}
	w.timings = append(w.timings, timingEntry{name: name, dur: d})
}

// flushServerTiming builds and sets the Server-Timing header exactly once,
// immediately before the first WriteHeader/Write call reaches the wire.
func (w *serverTimingWriter) flushServerTiming() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.headerSent {
		return
	}
	w.headerSent = true
	if !w.enabled {
		return
	}

	entries := make([]string, 0, len(w.timings)+1)
	for _, t := range w.timings {
		entries = append(entries, fmt.Sprintf("%s;dur=%.1f", t.name, msOf(t.dur)))
	}
	entries = append(entries, fmt.Sprintf("total;dur=%.1f", msOf(time.Since(w.start))))
	w.Header().Set("Server-Timing", strings.Join(entries, ", "))
}

// msOf converts a duration to fractional milliseconds at one decimal place
// of precision, matching the spec's example format (`dur=12.4`).
func msOf(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

// WriteHeader flushes the Server-Timing header (if not already sent) before
// forwarding the status code to the underlying writer.
func (w *serverTimingWriter) WriteHeader(status int) {
	w.flushServerTiming()
	w.ResponseWriter.WriteHeader(status)
}

// Write flushes the Server-Timing header (if not already sent, e.g. a
// handler that never calls WriteHeader explicitly and relies on the
// implicit 200) before forwarding the body bytes.
func (w *serverTimingWriter) Write(b []byte) (int, error) {
	w.flushServerTiming()
	return w.ResponseWriter.Write(b)
}

// Unwrap exposes the underlying http.ResponseWriter, both for
// http.ResponseController compatibility and so findServerTimingWriter can
// stop here when walking down a chain of wrapping writers.
func (w *serverTimingWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush forwards to the underlying writer's http.Flusher, if it implements
// one — needed so compress/streaming middleware layered around this writer
// keep working.
func (w *serverTimingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// findServerTimingWriter walks down a chain of wrapping http.ResponseWriter
// values (each implementing Unwrap() http.ResponseWriter, the Go 1.20+
// http.ResponseController convention already implemented by chi's
// middleware.Compress writer and this package's logging responseWriter) to
// find the *serverTimingWriter created by serverTimingMiddleware. Returns
// nil if none is found (e.g. debug mode is off, so no writer was created —
// see serverTimingMiddleware) or a route is invoked outside that chain.
func findServerTimingWriter(w http.ResponseWriter) *serverTimingWriter {
	for {
		if stw, ok := w.(*serverTimingWriter); ok {
			return stw
		}
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil
		}
		w = unwrapper.Unwrap()
	}
}

// recordServerTiming records a named span (e.g. "render") against the
// request's Server-Timing writer, if one is present in the chain. A no-op
// in production or when debug mode is off, since serverTimingMiddleware
// never creates the writer in that case — see below.
func recordServerTiming(w http.ResponseWriter, name string, d time.Duration) {
	if stw := findServerTimingWriter(w); stw != nil {
		stw.RecordTiming(name, d)
	}
}

// serverTimingMiddleware emits the debug-mode-only `Server-Timing` response
// header per AI.md PART 11 → "Server-Timing (Debug Mode Only)":
//
//	Server-Timing: db;dur=12.4, render;dur=3.1, total;dur=18.7
//
// Production NEVER emits this header — it leaks internal subsystem latency
// (Tier 3 information per the Public Endpoint Safety Principle, PART 11).
// Gated on the same debug-mode resolution used everywhere else in this
// codebase (mode.IsDebugEnabled(), CLI --debug / DEBUG=true), plus the
// existing cfg.Web.Headers.ServerTimingInDebugOnly operator toggle (default
// true) — an operator can still suppress the header even while running in
// debug mode, but nothing can force it on in production since
// mode.IsDebugEnabled() is independent of any config value.
//
// Registered first in the middleware chain (src/server/server.go) so the
// wrapping writer it installs sits under every other layer (logging,
// compress, security headers, rate limiting, CORS) and captures
// Server-Timing on every response, including early rejections.
//
// Only "total" (whole request duration) is implemented. "db" is
// intentionally NOT implemented: this project's HTTP handlers never call
// the database package directly (see src/database/database.go) — only
// src/main.go (bootstrap) and src/scheduler/tasks.go (background jobs, not
// per-request) touch GetServerDB(), so there is no per-request DB call site
// to accumulate query time from without inventing one.
//
// "render" was also investigated: src/server/server.go's renderPage is a
// realistic, single chokepoint to time. It was NOT wired up, though —
// timing it requires buffering template output (so the Server-Timing
// header can still be set before the first byte reaches the client), and
// buffering changes renderPage's error behavior: today, a template
// execution error part-way through a page write still gets flushed
// (implicit 200, truncated body) because bytes already reached the
// ResponseWriter before the error surfaced. Buffering instead makes that
// error a clean 500, which is more correct — but it also exposed a
// pre-existing, unrelated bug (`server.PageData` has no `Layout` field,
// while `partial/head.tmpl` unconditionally reads `.Layout`) that was
// previously masked by the truncated-200 behavior, breaking ~150
// pre-existing template-route tests. Fixing that bug is out of scope for
// this change; recordServerTiming/findServerTimingWriter remain here,
// fully implemented and unit-tested, ready for renderPage (or any other
// call site) to opt into a "render"/other named span once that bug is
// fixed separately (tracked in TODO.AI.md).
func serverTimingMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			enabled := mode.IsDebugEnabled() && cfg.Web.Headers.ServerTimingInDebugOnly
			if !enabled {
				next.ServeHTTP(w, r)
				return
			}

			stw := &serverTimingWriter{
				ResponseWriter: w,
				start:          time.Now(),
				enabled:        true,
			}
			next.ServeHTTP(stw, r)
			// In case the handler never wrote anything at all (e.g. an
			// empty 204/304 without an explicit WriteHeader call — rare
			// but possible), flush here so the header still goes out via
			// the deferred WriteHeader(200) net/http performs on Close.
			stw.flushServerTiming()
		})
	}
}
