package server

import (
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/require"
)

type recordingDeadlineConn struct {
	net.Conn
	mu            sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
}

func (conn *recordingDeadlineConn) SetReadDeadline(deadline time.Time) error {
	conn.mu.Lock()
	conn.readDeadline = deadline
	conn.mu.Unlock()
	return nil
}

func (conn *recordingDeadlineConn) SetWriteDeadline(deadline time.Time) error {
	conn.mu.Lock()
	conn.writeDeadline = deadline
	conn.mu.Unlock()
	return nil
}

func (conn *recordingDeadlineConn) deadlines() (time.Time, time.Time) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.readDeadline, conn.writeDeadline
}

func TestTimeoutConnStateUsesConfiguredRequestAndIdleDeadlines(t *testing.T) {
	old := config.DefaultServerLimitsConfig()
	SetHTTPTimeouts(old)
	t.Cleanup(func() { SetHTTPTimeouts(old) })
	SetHTTPTimeouts(config.ServerLimitsConfig{ReadTimeout: "4s", WriteTimeout: "7s", IdleTimeout: "11s"})

	left, right := net.Pipe()
	defer func() { _ = left.Close() }()
	defer func() { _ = right.Close() }()
	recorder := &recordingDeadlineConn{Conn: left}
	dynamic := &dynamicTimeoutConn{Conn: recorder, state: timeoutConnNew}
	registerTimeoutConn(t, dynamic)

	TimeoutConnState(dynamic, http.StateNew)
	readDeadline, writeDeadline := recorder.deadlines()
	require.InDelta(t, 4*time.Second, time.Until(readDeadline), float64(250*time.Millisecond))
	require.True(t, writeDeadline.IsZero())

	TimeoutConnState(dynamic, http.StateActive)
	readDeadline, writeDeadline = recorder.deadlines()
	require.InDelta(t, 4*time.Second, time.Until(readDeadline), float64(250*time.Millisecond))
	require.InDelta(t, 7*time.Second, time.Until(writeDeadline), float64(250*time.Millisecond))

	TimeoutConnState(dynamic, http.StateIdle)
	readDeadline, writeDeadline = recorder.deadlines()
	require.InDelta(t, 11*time.Second, time.Until(readDeadline), float64(250*time.Millisecond))
	require.True(t, writeDeadline.IsZero())
}

func TestSetHTTPTimeoutsRefreshesExistingIdleConnections(t *testing.T) {
	old := config.DefaultServerLimitsConfig()
	SetHTTPTimeouts(old)
	t.Cleanup(func() { SetHTTPTimeouts(old) })
	SetHTTPTimeouts(config.ServerLimitsConfig{ReadTimeout: "4s", WriteTimeout: "7s", IdleTimeout: "11s"})

	left, right := net.Pipe()
	defer func() { _ = left.Close() }()
	defer func() { _ = right.Close() }()
	recorder := &recordingDeadlineConn{Conn: left}
	dynamic := &dynamicTimeoutConn{Conn: recorder, state: timeoutConnIdle}
	registerTimeoutConn(t, dynamic)
	dynamic.refreshDeadlines()
	before, _ := recorder.deadlines()
	require.InDelta(t, 11*time.Second, time.Until(before), float64(250*time.Millisecond))

	SetHTTPTimeouts(config.ServerLimitsConfig{ReadTimeout: "2s", WriteTimeout: "3s", IdleTimeout: "5s"})
	after, _ := recorder.deadlines()
	require.InDelta(t, 5*time.Second, time.Until(after), float64(250*time.Millisecond))
}

func registerTimeoutConn(t *testing.T, conn *dynamicTimeoutConn) {
	t.Helper()
	timeoutConnectionsMu.Lock()
	timeoutConnections[conn] = struct{}{}
	timeoutConnectionsMu.Unlock()
	t.Cleanup(func() {
		conn.detach()
	})
}
