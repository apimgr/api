package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuildRequestBuiltInPayloadsAndHeaders(t *testing.T) {
	timestamp := time.Date(2026, 11, 24, 12, 30, 0, 0, time.UTC)
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	event := Event{
		Role: "security", Event: "security.report_received", Subject: "Report",
		Body: "full report", Message: "A message", Severity: "high",
		Timestamp: timestamp, ProjectName: "api", ProjectVersion: "1.2.3",
		AppURL: "https://example.test", TrackingID: "track-1",
	}
	tests := []struct {
		name      string
		transport Transport
		wantBody  string
		check     func(*testing.T, *url.URL, []byte)
	}{
		{
			name: "telegram", transport: Telegram,
			check: func(t *testing.T, target *url.URL, body []byte) {
				t.Helper()
				if got := target.Query().Get("text"); got != event.Message {
					t.Fatalf("text query = %q, want %q", got, event.Message)
				}
				if len(body) != 0 {
					t.Fatalf("telegram body = %q, want empty", body)
				}
			},
		},
		{
			name: "discord", transport: Discord,
			wantBody: `{"content":"A message","username":"api"}`,
		},
		{
			name: "slack", transport: Slack,
			wantBody: `{"text":"A message"}`,
		},
		{
			name: "mattermost", transport: Mattermost,
			wantBody: `{"text":"A message"}`,
		},
		{
			name: "generic", transport: Generic,
			wantBody: `{"role":"security","event":"security.report_received","subject":"Report","body":"full report","severity":"high","timestamp":"2026-11-24T12:30:00Z","project_name":"api","project_version":"1.2.3","app_url":"https://example.test","tracking_id":"track-1"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := Webhook{Transport: tt.transport, URL: "https://hooks.example.test/path?keep=yes", Secret: "test-secret"}
			req, err := BuildRequest(context.Background(), hook, event, id, timestamp)
			if err != nil {
				t.Fatal(err)
			}
			if req.Method != "POST" {
				t.Fatalf("method = %q, want POST", req.Method)
			}
			rawBody, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if tt.check != nil {
				target, err := url.Parse(req.URL.String())
				if err != nil {
					t.Fatal(err)
				}
				tt.check(t, target, rawBody)
			} else if got := string(rawBody); got != tt.wantBody {
				t.Fatalf("body = %s, want %s", got, tt.wantBody)
			}
			if tt.transport == Telegram && req.URL.Query().Get("keep") != "yes" {
				t.Fatal("telegram adapter did not preserve existing query parameters")
			}
			if got, want := req.Header.Get("X-Webhook-Timestamp"), "1795523400"; got != want {
				t.Fatalf("timestamp header = %q, want %q", got, want)
			}
			if got := req.Header.Get("X-Webhook-ID"); got != id {
				t.Fatalf("id header = %q, want %q", got, id)
			}
			if got, want := req.Header.Get("X-Webhook-Event"), event.Event; got != want {
				t.Fatalf("event header = %q, want %q", got, want)
			}
			if got, want := req.Header.Get("User-Agent"), "api/1.2.3 (+https://example.test)"; got != want {
				t.Fatalf("User-Agent = %q, want %q", got, want)
			}
			mac := hmac.New(sha256.New, []byte(hook.Secret))
			_, _ = mac.Write(rawBody)
			wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
			if got := req.Header.Get("X-Webhook-Signature"); got != wantSignature {
				t.Fatalf("signature = %q, want %q", got, wantSignature)
			}
		})
	}
}

func TestBuildRequestRetryReusesIDAndEventTimestamp(t *testing.T) {
	eventTime := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	event := Event{Event: "backup.failed", Message: "failed", Timestamp: eventTime}
	hook := Webhook{Transport: Discord, URL: "https://hooks.example.test", Secret: "secret"}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, 11, 24, 12, 30, 0, 0, time.UTC)
	first, err := BuildRequest(context.Background(), hook, event, id, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := BuildRequest(context.Background(), hook, event, id, timestamp.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if first.Header.Get("X-Webhook-ID") != retry.Header.Get("X-Webhook-ID") {
		t.Fatal("retry changed webhook ID")
	}
	if first.Header.Get("X-Webhook-Timestamp") == retry.Header.Get("X-Webhook-Timestamp") {
		t.Fatal("retry reused the previous attempt timestamp")
	}
	if first.Header.Get("X-Webhook-Signature") != retry.Header.Get("X-Webhook-Signature") {
		t.Fatal("retry changed signature")
	}
}

func TestSendWithRetryKeepsPayloadTimeAndIDButRefreshesAttemptTime(t *testing.T) {
	eventTime := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	event := Event{Role: "admin", Event: "backup.failed", Subject: "Backup failed", Body: "summary", Timestamp: eventTime}
	hook := Webhook{Transport: Generic, URL: "https://hooks.example.test", Secret: "secret", Name: "admin"}
	client := &retryCaptureClient{statuses: []int{http.StatusServiceUnavailable, http.StatusInternalServerError, http.StatusOK}}
	waits := []time.Duration{}
	attemptTime := eventTime
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	err = sendWithRetry(context.Background(), client, hook, event, id, func(_ context.Context, delay time.Duration) bool {
		waits = append(waits, delay)
		attemptTime = attemptTime.Add(time.Second)
		return true
	}, func() time.Time { return attemptTime })
	if err != nil {
		t.Fatalf("sendWithRetry() error = %v", err)
	}
	if len(client.requests) != 3 || len(waits) != 2 || waits[0] != time.Minute || waits[1] != 5*time.Minute {
		t.Fatalf("attempts=%d waits=%v, want 3 attempts and 1m/5m backoff", len(client.requests), waits)
	}
	var firstPayload, lastPayload genericPayload
	if err := json.Unmarshal(client.bodies[0], &firstPayload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(client.bodies[2], &lastPayload); err != nil {
		t.Fatal(err)
	}
	if firstPayload.Timestamp != "2026-11-24T12:00:00Z" || lastPayload.Timestamp != firstPayload.Timestamp {
		t.Fatalf("payload timestamps = %q, %q; want stable event time", firstPayload.Timestamp, lastPayload.Timestamp)
	}
	firstHeader := client.requests[0].Header.Get("X-Webhook-Timestamp")
	for i, request := range client.requests {
		if request.Header.Get("X-Webhook-ID") != id {
			t.Fatal("retry changed webhook ID")
		}
		if i > 0 && request.Header.Get("X-Webhook-Timestamp") == firstHeader {
			t.Fatal("retry reused X-Webhook-Timestamp")
		}
	}
}

type retryCaptureClient struct {
	statuses []int
	requests []*http.Request
	bodies   [][]byte
}

func (c *retryCaptureClient) Do(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	c.requests = append(c.requests, request.Clone(request.Context()))
	c.bodies = append(c.bodies, body)
	status := c.statuses[len(c.requests)-1]
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}

func TestBuildRequestRejectsNonHTTPSURLs(t *testing.T) {
	hook := Webhook{Transport: Generic, URL: "http://hooks.example.test", Secret: "secret"}
	if _, err := BuildRequest(context.Background(), hook, Event{Event: "test"}, "", time.Time{}); err == nil {
		t.Fatal("BuildRequest accepted a non-HTTPS URL")
	}
}

func TestBuiltInPushoverAndGotifyTransports(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	event := Event{Event: "security.report_received", Subject: "Report", Message: "summary", Severity: "critical"}
	t.Run("pushover", func(t *testing.T) {
		hook := Webhook{Transport: Pushover, URL: "https://api.pushover.net/1/messages.json?user=user-id&token=api-token", Secret: "secret"}
		req, err := BuildRequest(context.Background(), hook, event, id, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.RawQuery != "" || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatalf("Pushover request URL/content type = %q / %q", req.URL, req.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{"user": "user-id", "token": "api-token", "title": "Report", "message": "summary", "priority": "1"} {
			if got := form.Get(key); got != want {
				t.Errorf("Pushover %s = %q, want %q", key, got, want)
			}
		}
	})
	t.Run("gotify", func(t *testing.T) {
		hook := Webhook{Transport: Gotify, URL: "https://push.example.test/base?token=api-token", Secret: "secret"}
		req, err := BuildRequest(context.Background(), hook, event, id, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.Path != "/base/message" || req.URL.Query().Get("token") != "api-token" {
			t.Fatalf("Gotify URL = %q, want /base/message with token", req.URL)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["priority"] != float64(8) || body["title"] != "Report" || body["message"] != "summary" {
			t.Fatalf("Gotify payload = %#v", body)
		}
	})
}

func TestRegisterAdapter(t *testing.T) {
	const transport Transport = "test-webhook-adapter"
	adapter := testAdapter{}
	if err := RegisterAdapter(transport, adapter); err != nil {
		t.Fatal(err)
	}
	hook := Webhook{Transport: transport, URL: "https://hooks.example.test/base", Secret: "secret"}
	req, err := BuildRequest(context.Background(), hook, Event{Event: "test", Message: "hello"}, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/adapted" || req.Header.Get("Content-Type") != "application/custom" {
		t.Fatalf("registered adapter not applied: URL=%s content-type=%s", req.URL, req.Header.Get("Content-Type"))
	}
	if err := RegisterAdapter(transport, adapter); err == nil {
		t.Fatal("RegisterAdapter accepted a duplicate")
	}
	if err := RegisterAdapter(Discord, adapter); err == nil {
		t.Fatal("RegisterAdapter accepted a reserved built-in")
	}
}

type testAdapter struct{}

func (testAdapter) Build(endpoint *url.URL, _ Event) (*url.URL, []byte, string, error) {
	endpoint.Path = "/adapted"
	body, err := json.Marshal(map[string]string{"ok": "yes"})
	return endpoint, body, "application/custom", err
}

func TestRetryDelay(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour}
	for attempt, expected := range want {
		got, ok := RetryDelay(attempt)
		if !ok || got != expected {
			t.Fatalf("RetryDelay(%d) = (%s, %t), want (%s, true)", attempt, got, ok, expected)
		}
	}
	for _, attempt := range []int{-1, len(want)} {
		if got, ok := RetryDelay(attempt); ok {
			t.Fatalf("RetryDelay(%d) = (%s, true), want exhausted/invalid", attempt, got)
		}
	}
}
