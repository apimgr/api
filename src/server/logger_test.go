package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redactQuery must leave a query string untouched when it has no
// sensitive parameters (or is empty/unparseable), and must replace the
// value of any sensitive parameter - matched case-insensitively - with
// REDACTED while preserving unrelated parameters.
func TestRedactQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		check func(t *testing.T, out string)
	}{
		{
			name:  "empty query returned as-is",
			query: "",
			check: func(t *testing.T, out string) { assert.Equal(t, "", out) },
		},
		{
			name:  "no sensitive params unchanged",
			query: "page=2&sort=asc",
			check: func(t *testing.T, out string) {
				assert.Equal(t, "page=2&sort=asc", out)
			},
		},
		{
			name:  "token redacted",
			query: "token=abc123&page=2",
			check: func(t *testing.T, out string) {
				assert.Contains(t, out, "token=REDACTED")
				assert.Contains(t, out, "page=2")
				assert.NotContains(t, out, "abc123")
			},
		},
		{
			name:  "case-insensitive key match",
			query: "TOKEN=abc123",
			check: func(t *testing.T, out string) {
				assert.Contains(t, out, "REDACTED")
				assert.NotContains(t, out, "abc123")
			},
		},
		{
			name:  "multiple sensitive params all redacted",
			query: "password=hunter2&api_key=xyz&q=hello",
			check: func(t *testing.T, out string) {
				assert.NotContains(t, out, "hunter2")
				assert.NotContains(t, out, "xyz")
				assert.Contains(t, out, "q=hello")
			},
		},
		{
			name:  "unparseable query returned unchanged",
			query: "%zz",
			check: func(t *testing.T, out string) {
				assert.Equal(t, "%zz", out)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, redactQuery(tt.query))
		})
	}
}

// isHealthCheckPath must match only the exact registered health-check
// routes.
func TestIsHealthCheckPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/healthz", true},
		{"/server/healthz", true},
		{"/api/healthz", true},
		{"/api/v1/server/healthz", true},
		{"/healthz/", false},
		{"/api/v2/server/healthz", false},
		{"/", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, isHealthCheckPath(tt.path))
		})
	}
}

// newTestLogger builds a Logger with every sub-logger backed by its own
// in-memory buffer, bypassing NewLogger's filesystem I/O, so LogXxx
// methods can be exercised and their output asserted directly.
func newTestLogger(cfg *config.LogsConfig) (*Logger, map[string]*bytes.Buffer) {
	bufs := map[string]*bytes.Buffer{
		"access":   {},
		"server":   {},
		"error":    {},
		"app":      {},
		"auth":     {},
		"audit":    {},
		"security": {},
		"debug":    {},
	}
	l := &Logger{
		config:      cfg,
		accessLog:   log.New(bufs["access"], "", 0),
		serverLog:   log.New(bufs["server"], "", 0),
		errorLog:    log.New(bufs["error"], "", 0),
		appLog:      log.New(bufs["app"], "", 0),
		authLog:     log.New(bufs["auth"], "", 0),
		auditLog:    log.New(bufs["audit"], "", 0),
		securityLog: log.New(bufs["security"], "", 0),
		debugLog:    log.New(bufs["debug"], "", 0),
	}
	return l, bufs
}

// LogAccess must format each configured access-log format correctly,
// must suppress successful (2xx) health-check requests unless debug mode
// is enabled, and must never suppress a non-2xx health-check response.
func TestLogAccessFormats(t *testing.T) {
	mode.SetDebugEnabled(false)
	t.Cleanup(func() { mode.SetDebugEnabled(false) })

	newReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/thing?token=secret&page=2", nil)
		r.RemoteAddr = "203.0.113.5:1234"
		r.Header.Set("User-Agent", "test-agent")
		r.Header.Set("Referer", "https://example.com")
		r.Header.Set("X-Request-ID", "req-1")
		return r
	}

	t.Run("apache format", func(t *testing.T) {
		cfg := &config.LogsConfig{Access: config.LogConfig{Format: "apache"}}
		l, bufs := newTestLogger(cfg)
		l.LogAccess(newReq(), 200, 512, 15*time.Millisecond)

		out := bufs["access"].String()
		assert.Contains(t, out, "203.0.113.5:1234")
		assert.Contains(t, out, `"GET /api/v1/thing HTTP/1.1"`, "apache format logs URL.Path only, not the query string")
		assert.Contains(t, out, " 200 512 ")
		assert.Contains(t, out, `"https://example.com"`)
		assert.Contains(t, out, `"test-agent"`)
	})

	t.Run("combined format is an alias for apache", func(t *testing.T) {
		cfg := &config.LogsConfig{Access: config.LogConfig{Format: "combined"}}
		l, bufs := newTestLogger(cfg)
		l.LogAccess(newReq(), 200, 10, time.Millisecond)
		assert.Contains(t, bufs["access"].String(), "\" 200 10 ")
	})

	t.Run("apache format defaults referer/UA to dash when absent", func(t *testing.T) {
		cfg := &config.LogsConfig{Access: config.LogConfig{Format: "apache"}}
		l, bufs := newTestLogger(cfg)
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		l.LogAccess(req, 200, 0, 0)
		assert.Contains(t, bufs["access"].String(), `"-" "-"`)
	})

	t.Run("nginx format", func(t *testing.T) {
		cfg := &config.LogsConfig{Access: config.LogConfig{Format: "nginx"}}
		l, bufs := newTestLogger(cfg)
		l.LogAccess(newReq(), 404, 0, 0)
		out := bufs["access"].String()
		assert.Contains(t, out, "404 0")
		assert.NotContains(t, out, "test-agent", "nginx common format omits UA/referer")
	})

	t.Run("json format redacts sensitive query params", func(t *testing.T) {
		cfg := &config.LogsConfig{Access: config.LogConfig{Format: "json"}}
		l, bufs := newTestLogger(cfg)
		l.LogAccess(newReq(), 200, 100, 25*time.Millisecond)

		var entry map[string]interface{}
		require.NoError(t, json.Unmarshal(bufs["access"].Bytes(), &entry))
		assert.Equal(t, "GET", entry["method"])
		assert.Equal(t, float64(200), entry["status"])
		assert.Equal(t, float64(25), entry["latency_ms"])
		assert.Contains(t, entry["query"], "REDACTED")
		assert.NotContains(t, entry["query"], "secret")
		assert.Equal(t, "req-1", entry["request_id"])
	})

	t.Run("custom format substitutes variables", func(t *testing.T) {
		cfg := &config.LogsConfig{Access: config.LogConfig{
			Format: "custom",
			Custom: "{method} {path} {status}",
		}}
		l, bufs := newTestLogger(cfg)
		l.LogAccess(newReq(), 201, 0, 0)
		assert.Equal(t, "GET /api/v1/thing 201\n", bufs["access"].String())
	})

	t.Run("2xx health-check path suppressed by default", func(t *testing.T) {
		cfg := &config.LogsConfig{Access: config.LogConfig{Format: "nginx"}}
		l, bufs := newTestLogger(cfg)
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		l.LogAccess(req, 200, 0, 0)
		assert.Empty(t, bufs["access"].String())
	})

	t.Run("non-2xx health-check path is NOT suppressed", func(t *testing.T) {
		cfg := &config.LogsConfig{Access: config.LogConfig{Format: "nginx"}}
		l, bufs := newTestLogger(cfg)
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		l.LogAccess(req, 503, 0, 0)
		assert.NotEmpty(t, bufs["access"].String())
	})

	t.Run("health-check suppression lifted when debug mode enabled", func(t *testing.T) {
		mode.SetDebugEnabled(true)
		defer mode.SetDebugEnabled(false)

		cfg := &config.LogsConfig{Access: config.LogConfig{Format: "nginx"}}
		l, bufs := newTestLogger(cfg)
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		l.LogAccess(req, 200, 0, 0)
		assert.NotEmpty(t, bufs["access"].String())
	})

	t.Run("nil accessLog is a no-op, never panics", func(t *testing.T) {
		l := &Logger{config: &config.LogsConfig{Access: config.LogConfig{Format: "json"}}}
		assert.NotPanics(t, func() {
			l.LogAccess(newReq(), 200, 0, 0)
		})
	})

	t.Run("unrecognized format writes nothing", func(t *testing.T) {
		cfg := &config.LogsConfig{Access: config.LogConfig{Format: "bogus"}}
		l, bufs := newTestLogger(cfg)
		l.LogAccess(newReq(), 200, 0, 0)
		assert.Empty(t, bufs["access"].String())
	})
}

// LogServer must format text and json server-log entries, and be a
// no-op when the sub-logger is nil.
func TestLogServer(t *testing.T) {
	t.Run("text format", func(t *testing.T) {
		cfg := &config.LogsConfig{Server: config.LogConfig{Format: "text"}}
		l, bufs := newTestLogger(cfg)
		l.LogServer("INFO", "server started")
		assert.Contains(t, bufs["server"].String(), "[INFO] server started")
	})

	t.Run("json format", func(t *testing.T) {
		cfg := &config.LogsConfig{Server: config.LogConfig{Format: "json"}}
		l, bufs := newTestLogger(cfg)
		l.LogServer("WARN", "disk low")
		var entry map[string]interface{}
		require.NoError(t, json.Unmarshal(bufs["server"].Bytes(), &entry))
		assert.Equal(t, "WARN", entry["level"])
		assert.Equal(t, "disk low", entry["msg"])
	})

	t.Run("nil logger no-op", func(t *testing.T) {
		l := &Logger{config: &config.LogsConfig{}}
		assert.NotPanics(t, func() { l.LogServer("INFO", "x") })
	})
}

// LogError must format text and json error-log entries, must merge the
// supplied context map into the JSON body, and be a no-op when nil.
func TestLogError(t *testing.T) {
	t.Run("text format", func(t *testing.T) {
		cfg := &config.LogsConfig{Error: config.LogConfig{Format: "text"}}
		l, bufs := newTestLogger(cfg)
		l.LogError(errors.New("boom"), nil)
		assert.Contains(t, bufs["error"].String(), "[ERROR] boom")
	})

	t.Run("json format merges context", func(t *testing.T) {
		cfg := &config.LogsConfig{Error: config.LogConfig{Format: "json"}}
		l, bufs := newTestLogger(cfg)
		l.LogError(errors.New("boom"), map[string]interface{}{"user_id": "u1"})

		var entry map[string]interface{}
		require.NoError(t, json.Unmarshal(bufs["error"].Bytes(), &entry))
		assert.Equal(t, "boom", entry["error"])
		assert.Equal(t, "ERROR", entry["level"])
		assert.Equal(t, "u1", entry["user_id"])
	})

	t.Run("nil logger no-op", func(t *testing.T) {
		l := &Logger{config: &config.LogsConfig{}}
		assert.NotPanics(t, func() { l.LogError(errors.New("x"), nil) })
	})
}

// LogApp must always emit logfmt regardless of config.Format, quoting the
// message and appending each field as key=value.
func TestLogApp(t *testing.T) {
	cfg := &config.LogsConfig{}
	l, bufs := newTestLogger(cfg)
	l.LogApp("INFO", "user created", map[string]interface{}{"id": "abc123"})

	out := bufs["app"].String()
	assert.Contains(t, out, `level=INFO`)
	assert.Contains(t, out, `msg="user created"`)
	assert.Contains(t, out, `id=abc123`)
	assert.True(t, strings.HasPrefix(out, "time="))

	t.Run("nil logger no-op", func(t *testing.T) {
		l := &Logger{config: &config.LogsConfig{}}
		assert.NotPanics(t, func() { l.LogApp("INFO", "x", nil) })
	})
}

// LogAuth must always emit RFC 3164-style syslog output with a pid and
// the auth: prefix, appending each field as key=value.
func TestLogAuth(t *testing.T) {
	cfg := &config.LogsConfig{}
	l, bufs := newTestLogger(cfg)
	l.LogAuth(map[string]interface{}{"result": "fail", "reason": "invalid_token"})

	out := bufs["auth"].String()
	assert.Contains(t, out, "auth:")
	assert.Contains(t, out, "result=fail")
	assert.Contains(t, out, "reason=invalid_token")

	t.Run("nil logger no-op", func(t *testing.T) {
		l := &Logger{config: &config.LogsConfig{}}
		assert.NotPanics(t, func() { l.LogAuth(nil) })
	})
}

// LogAudit must be a no-op unless config.Audit.Enabled is true, must
// always emit JSON regardless of any format setting, and must merge the
// details map.
func TestLogAudit(t *testing.T) {
	t.Run("disabled: no-op", func(t *testing.T) {
		cfg := &config.LogsConfig{Audit: config.AuditLogConfig{Enabled: false}}
		l, bufs := newTestLogger(cfg)
		l.LogAudit("user.created", map[string]interface{}{"id": "u1"})
		assert.Empty(t, bufs["audit"].String())
	})

	t.Run("enabled: emits JSON with merged details", func(t *testing.T) {
		cfg := &config.LogsConfig{Audit: config.AuditLogConfig{Enabled: true}}
		l, bufs := newTestLogger(cfg)
		l.LogAudit("user.created", map[string]interface{}{"id": "u1"})

		var entry map[string]interface{}
		require.NoError(t, json.Unmarshal(bufs["audit"].Bytes(), &entry))
		assert.Equal(t, "user.created", entry["event"])
		assert.Equal(t, "u1", entry["id"])
	})

	t.Run("nil logger no-op", func(t *testing.T) {
		l := &Logger{config: &config.LogsConfig{Audit: config.AuditLogConfig{Enabled: true}}}
		assert.NotPanics(t, func() { l.LogAudit("x", nil) })
	})
}

// LogSecurity must format each of the fail2ban/syslog/cef/json/text
// variants, and be a no-op when nil.
func TestLogSecurity(t *testing.T) {
	details := map[string]interface{}{"attempts": 5}

	t.Run("fail2ban format", func(t *testing.T) {
		cfg := &config.LogsConfig{Security: config.SecurityLogConfig{Format: "fail2ban"}}
		l, bufs := newTestLogger(cfg)
		l.LogSecurity("login_failed", "203.0.113.5", details)
		assert.Contains(t, bufs["security"].String(), "login_failed from 203.0.113.5")
	})

	t.Run("syslog format", func(t *testing.T) {
		cfg := &config.LogsConfig{Security: config.SecurityLogConfig{Format: "syslog"}}
		l, bufs := newTestLogger(cfg)
		l.LogSecurity("login_failed", "203.0.113.5", details)
		out := bufs["security"].String()
		assert.Contains(t, out, "<14>1")
		assert.Contains(t, out, "ip=203.0.113.5")
	})

	t.Run("cef format", func(t *testing.T) {
		cfg := &config.LogsConfig{Security: config.SecurityLogConfig{Format: "cef"}}
		l, bufs := newTestLogger(cfg)
		l.LogSecurity("login_failed", "203.0.113.5", details)
		out := bufs["security"].String()
		assert.Contains(t, out, "CEF:0|apimgr|api|1.0|login_failed|login_failed|5|")
		assert.Contains(t, out, "src=203.0.113.5")
		assert.Contains(t, out, "attempts=5")
	})

	t.Run("json format", func(t *testing.T) {
		cfg := &config.LogsConfig{Security: config.SecurityLogConfig{Format: "json"}}
		l, bufs := newTestLogger(cfg)
		l.LogSecurity("login_failed", "203.0.113.5", details)

		var entry map[string]interface{}
		require.NoError(t, json.Unmarshal(bufs["security"].Bytes(), &entry))
		assert.Equal(t, "login_failed", entry["event"])
		assert.Equal(t, "203.0.113.5", entry["ip"])
		assert.Equal(t, float64(5), entry["attempts"])
	})

	t.Run("text format", func(t *testing.T) {
		cfg := &config.LogsConfig{Security: config.SecurityLogConfig{Format: "text"}}
		l, bufs := newTestLogger(cfg)
		l.LogSecurity("login_failed", "203.0.113.5", nil)
		assert.Contains(t, bufs["security"].String(), "[SECURITY] login_failed from 203.0.113.5")
	})

	t.Run("unrecognized format writes nothing", func(t *testing.T) {
		cfg := &config.LogsConfig{Security: config.SecurityLogConfig{Format: "bogus"}}
		l, bufs := newTestLogger(cfg)
		l.LogSecurity("x", "1.2.3.4", nil)
		assert.Empty(t, bufs["security"].String())
	})

	t.Run("nil logger no-op", func(t *testing.T) {
		l := &Logger{config: &config.LogsConfig{}}
		assert.NotPanics(t, func() { l.LogSecurity("x", "1.2.3.4", nil) })
	})
}

// LogDebug must be a no-op unless config.Debug.Enabled is true, and must
// format text/json variants when enabled.
func TestLogDebug(t *testing.T) {
	t.Run("disabled: no-op", func(t *testing.T) {
		cfg := &config.LogsConfig{Debug: config.DebugLogConfig{Enabled: false, Format: "text"}}
		l, bufs := newTestLogger(cfg)
		l.LogDebug("verbose message", nil)
		assert.Empty(t, bufs["debug"].String())
	})

	t.Run("enabled text format", func(t *testing.T) {
		cfg := &config.LogsConfig{Debug: config.DebugLogConfig{Enabled: true, Format: "text"}}
		l, bufs := newTestLogger(cfg)
		l.LogDebug("verbose message", nil)
		assert.Contains(t, bufs["debug"].String(), "[DEBUG] verbose message")
	})

	t.Run("enabled json format merges context", func(t *testing.T) {
		cfg := &config.LogsConfig{Debug: config.DebugLogConfig{Enabled: true, Format: "json"}}
		l, bufs := newTestLogger(cfg)
		l.LogDebug("verbose message", map[string]interface{}{"trace": "t1"})

		var entry map[string]interface{}
		require.NoError(t, json.Unmarshal(bufs["debug"].Bytes(), &entry))
		assert.Equal(t, "DEBUG", entry["level"])
		assert.Equal(t, "t1", entry["trace"])
	})

	t.Run("nil logger no-op", func(t *testing.T) {
		l := &Logger{config: &config.LogsConfig{Debug: config.DebugLogConfig{Enabled: true}}}
		assert.NotPanics(t, func() { l.LogDebug("x", nil) })
	})
}

// formatCustom must substitute every documented {variable} token and
// leave unrecognized tokens untouched.
func TestFormatCustom(t *testing.T) {
	cfg := &config.LogsConfig{}
	l, _ := newTestLogger(cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/thing?token=secret", nil)
	req.Host = "example.com"
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("Referer", "https://ref.example")
	req.Header.Set("X-Request-ID", "req-42")

	out := l.formatCustom("{method} {path} {query} {status} {bytes} {latency_ms}ms {user_agent} {referer} {request_id} {fqdn} {protocol} {unknown}",
		req, 200, 1024, 42*time.Millisecond)

	assert.Contains(t, out, "POST /api/v1/thing token=REDACTED 200 1024 42ms test-agent https://ref.example req-42 example.com HTTP/1.1")
	assert.Contains(t, out, "{unknown}", "unrecognized tokens must be left untouched")
}

// NewLogger must create every configured log file under the log
// directory and return a fully wired Logger; when debug logging is
// disabled, the debug sub-logger must stay nil.
func TestNewLogger(t *testing.T) {
	tmpDir := t.TempDir()
	paths.Init("", "", tmpDir)
	t.Cleanup(func() { paths.Init("", "", "") })

	cfg := &config.LogsConfig{
		Access:   config.LogConfig{Filename: "access.log", Format: "nginx"},
		Server:   config.LogConfig{Filename: "server.log", Format: "text"},
		Error:    config.LogConfig{Filename: "error.log", Format: "text"},
		App:      config.LogConfig{Filename: "app.log"},
		Auth:     config.LogConfig{Filename: "auth.log"},
		Audit:    config.AuditLogConfig{Filename: "audit.log", Enabled: true},
		Security: config.SecurityLogConfig{Filename: "security.log", Format: "text"},
		Debug:    config.DebugLogConfig{Filename: "debug.log", Enabled: false},
	}

	l, err := NewLogger(cfg)
	require.NoError(t, err)
	require.NotNil(t, l)
	assert.Nil(t, l.debugLog, "debug log must stay nil when Debug.Enabled is false")

	l.LogServer("INFO", "hello")
	assert.FileExists(t, tmpDir+"/server.log")
}

// NewLogger must initialize the debug log when Debug.Enabled is true.
func TestNewLoggerWithDebugEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	paths.Init("", "", tmpDir)
	t.Cleanup(func() { paths.Init("", "", "") })

	cfg := &config.LogsConfig{
		Access:   config.LogConfig{Filename: "access.log"},
		Server:   config.LogConfig{Filename: "server.log"},
		Error:    config.LogConfig{Filename: "error.log"},
		App:      config.LogConfig{Filename: "app.log"},
		Auth:     config.LogConfig{Filename: "auth.log"},
		Audit:    config.AuditLogConfig{Filename: "audit.log"},
		Security: config.SecurityLogConfig{Filename: "security.log"},
		Debug:    config.DebugLogConfig{Filename: "debug.log", Enabled: true},
	}

	l, err := NewLogger(cfg)
	require.NoError(t, err)
	assert.NotNil(t, l.debugLog)
}

// InitLogger/GetLogger must wire the package-level global logger.
func TestInitAndGetLogger(t *testing.T) {
	tmpDir := t.TempDir()
	paths.Init("", "", tmpDir)
	t.Cleanup(func() { paths.Init("", "", "") })

	cfg := &config.LogsConfig{
		Access:   config.LogConfig{Filename: "access.log"},
		Server:   config.LogConfig{Filename: "server.log"},
		Error:    config.LogConfig{Filename: "error.log"},
		App:      config.LogConfig{Filename: "app.log"},
		Auth:     config.LogConfig{Filename: "auth.log"},
		Audit:    config.AuditLogConfig{Filename: "audit.log"},
		Security: config.SecurityLogConfig{Filename: "security.log"},
		Debug:    config.DebugLogConfig{Filename: "debug.log"},
	}

	require.NoError(t, InitLogger(cfg))
	assert.NotNil(t, GetLogger())
}
