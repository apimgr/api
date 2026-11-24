package server

import (
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apimgr/api/src/config"
)

type connectionTimeouts struct {
	read  time.Duration
	write time.Duration
	idle  time.Duration
}

type timeoutConnState uint8

const (
	timeoutConnNew timeoutConnState = iota
	timeoutConnActive
	timeoutConnIdle
	timeoutConnReading
	timeoutConnDetached
)

var (
	activeConnectionTimeouts atomic.Pointer[connectionTimeouts]
	timeoutConnectionsMu     sync.Mutex
	timeoutConnections       = make(map[*dynamicTimeoutConn]struct{})
)

func init() {
	SetHTTPTimeouts(config.DefaultServerLimitsConfig())
}

// SetHTTPTimeouts applies a validated timeout configuration to current and
// future HTTP connections without stopping or rebinding their listeners.
func SetHTTPTimeouts(limits config.ServerLimitsConfig) {
	limits, _ = limits.Normalized()
	read, _ := time.ParseDuration(limits.ReadTimeout)
	write, _ := time.ParseDuration(limits.WriteTimeout)
	idle, _ := time.ParseDuration(limits.IdleTimeout)
	activeConnectionTimeouts.Store(&connectionTimeouts{read: read, write: write, idle: idle})

	timeoutConnectionsMu.Lock()
	defer timeoutConnectionsMu.Unlock()
	for conn := range timeoutConnections {
		conn.refreshDeadlines()
	}
}

// TimeoutListener wraps an already-bound listener so its accepted connections
// use reloadable timeout values while preserving the listener and port.
func TimeoutListener(listener net.Listener) net.Listener {
	return &dynamicTimeoutListener{Listener: listener}
}

// TimeoutConnState is assigned to each HTTP server's ConnState callback.
func TimeoutConnState(conn net.Conn, state http.ConnState) {
	dynamic := unwrapDynamicTimeoutConn(conn)
	if dynamic == nil {
		return
	}
	switch state {
	case http.StateNew:
		dynamic.setState(timeoutConnNew)
	case http.StateActive:
		dynamic.setState(timeoutConnActive)
	case http.StateIdle:
		dynamic.setState(timeoutConnIdle)
	case http.StateHijacked, http.StateClosed:
		dynamic.detach()
	}
}

type dynamicTimeoutListener struct {
	net.Listener
}

func (listener *dynamicTimeoutListener) Accept() (net.Conn, error) {
	conn, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	dynamic := &dynamicTimeoutConn{Conn: conn, state: timeoutConnNew}
	timeoutConnectionsMu.Lock()
	timeoutConnections[dynamic] = struct{}{}
	timeoutConnectionsMu.Unlock()
	dynamic.refreshDeadlines()
	return dynamic, nil
}

type dynamicTimeoutConn struct {
	net.Conn
	mu    sync.Mutex
	state timeoutConnState
}

func (conn *dynamicTimeoutConn) Read(buffer []byte) (int, error) {
	conn.mu.Lock()
	if conn.state == timeoutConnIdle {
		conn.state = timeoutConnReading
		conn.applyDeadlinesLocked()
	}
	conn.mu.Unlock()
	return conn.Conn.Read(buffer)
}

func (conn *dynamicTimeoutConn) Close() error {
	conn.detach()
	return conn.Conn.Close()
}

func (conn *dynamicTimeoutConn) setState(state timeoutConnState) {
	conn.mu.Lock()
	conn.state = state
	conn.applyDeadlinesLocked()
	conn.mu.Unlock()
}

func (conn *dynamicTimeoutConn) refreshDeadlines() {
	conn.mu.Lock()
	conn.applyDeadlinesLocked()
	conn.mu.Unlock()
}

func (conn *dynamicTimeoutConn) applyDeadlinesLocked() {
	timeouts := activeConnectionTimeouts.Load()
	if timeouts == nil {
		return
	}
	now := time.Now()
	switch conn.state {
	case timeoutConnNew, timeoutConnReading:
		_ = conn.SetReadDeadline(now.Add(timeouts.read))
		_ = conn.SetWriteDeadline(time.Time{})
	case timeoutConnActive:
		_ = conn.SetReadDeadline(now.Add(timeouts.read))
		_ = conn.SetWriteDeadline(now.Add(timeouts.write))
	case timeoutConnIdle:
		_ = conn.SetReadDeadline(now.Add(timeouts.idle))
		_ = conn.SetWriteDeadline(time.Time{})
	case timeoutConnDetached:
		_ = conn.SetReadDeadline(time.Time{})
		_ = conn.SetWriteDeadline(time.Time{})
	}
}

func (conn *dynamicTimeoutConn) detach() {
	conn.mu.Lock()
	if conn.state == timeoutConnDetached {
		conn.mu.Unlock()
		return
	}
	conn.state = timeoutConnDetached
	conn.applyDeadlinesLocked()
	conn.mu.Unlock()
	timeoutConnectionsMu.Lock()
	delete(timeoutConnections, conn)
	timeoutConnectionsMu.Unlock()
}

func unwrapDynamicTimeoutConn(conn net.Conn) *dynamicTimeoutConn {
	for conn != nil {
		if dynamic, ok := conn.(*dynamicTimeoutConn); ok {
			return dynamic
		}
		wrapped, ok := conn.(interface{ NetConn() net.Conn })
		if !ok {
			return nil
		}
		next := wrapped.NetConn()
		if next == conn {
			return nil
		}
		conn = next
	}
	return nil
}
