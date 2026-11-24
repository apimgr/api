// Package notify builds and sends the outbound webhook formats defined by
// AI.md Part 12.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Transport is the configured adapter name. Additional transports can be
// installed through RegisterAdapter.
type Transport string

const (
	Telegram   Transport = "telegram"
	Discord    Transport = "discord"
	Slack      Transport = "slack"
	Mattermost Transport = "mattermost"
	Pushover   Transport = "pushover"
	Gotify     Transport = "gotify"
	Generic    Transport = "generic"
)

// Webhook is one named destination and its private signing secret.
type Webhook struct {
	Name      string
	Transport Transport
	URL       string
	Secret    string
}

// Event is the caller-composed delivery payload. Message is used by chat
// transports; the structured fields are used by generic and custom adapters.
// Timestamp is serialized as RFC3339Nano in the generic JSON payload.
type Event struct {
	Role           string
	Event          string
	Subject        string
	Body           string
	Message        string
	Severity       string
	Timestamp      time.Time
	ProjectName    string
	ProjectVersion string
	AppURL         string
	TrackingID     string
	Priority       int
}

// HTTPDoer is implemented by http.Client and test or application clients.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Adapter prepares a provider-specific URL, body, and content type. The
// package adds the common Part 12 signature and delivery headers afterwards.
// Implementations receive a copy of the validated HTTPS endpoint.
type Adapter interface {
	Build(*url.URL, Event) (*url.URL, []byte, string, error)
}

var adapters = struct {
	sync.RWMutex
	byTransport map[Transport]Adapter
}{byTransport: make(map[Transport]Adapter)}

// RegisterAdapter registers an additional transport adapter. Built-in
// transports cannot be replaced. It is safe to register during initialization.
func RegisterAdapter(transport Transport, adapter Adapter) error {
	if transport == "" || adapter == nil || isNative(transport) {
		return errors.New("invalid or reserved webhook transport adapter")
	}
	adapters.Lock()
	defer adapters.Unlock()
	if _, exists := adapters.byTransport[transport]; exists {
		return fmt.Errorf("webhook adapter %q is already registered", transport)
	}
	adapters.byTransport[transport] = adapter
	return nil
}

var retrySchedule = [...]time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	time.Hour,
	6 * time.Hour,
	24 * time.Hour,
}

// RetryDelay returns the delay for a zero-based failed-attempt index. Once
// exhausted, the delivery is dropped.
func RetryDelay(failedAttempts int) (time.Duration, bool) {
	if failedAttempts < 0 || failedAttempts >= len(retrySchedule) {
		return 0, false
	}
	return retrySchedule[failedAttempts], true
}

// NewID creates the UUID v7 delivery identifier required for webhook
// idempotency. Errors are returned rather than silently generating another
// UUID version.
func NewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create webhook UUID v7: %w", err)
	}
	return id.String(), nil
}

// BuildRequest constructs a signed request. Pass the same id and timestamp
// when rebuilding a retry so the receiver can recognize it as the same event.
// An empty id creates a UUID v7; an empty timestamp uses the current time.
func BuildRequest(ctx context.Context, hook Webhook, event Event, id string, timestamp time.Time) (*http.Request, error) {
	if hook.Secret == "" {
		return nil, errors.New("webhook signing secret is required")
	}
	endpoint, err := parseHTTPSURL(hook.URL)
	if err != nil {
		return nil, err
	}
	if event.Event == "" {
		return nil, errors.New("webhook event type is required")
	}
	if id == "" {
		id, err = NewID()
		if err != nil {
			return nil, err
		}
	}
	parsedID, err := uuid.Parse(id)
	if err != nil || parsedID.Version() != 7 {
		return nil, errors.New("webhook id must be a UUID v7")
	}
	if timestamp.IsZero() {
		timestamp = event.Timestamp
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	timestamp = timestamp.UTC()

	var body []byte
	contentType := "application/json"
	switch hook.Transport {
	case Telegram:
		appendQuery(endpoint, "text", event.Message)
		body = nil
	case Discord:
		body, err = json.Marshal(struct {
			Content  string `json:"content"`
			Username string `json:"username"`
		}{event.Message, event.ProjectName})
	case Slack, Mattermost:
		body, err = json.Marshal(struct {
			Text string `json:"text"`
		}{event.Message})
	case Pushover, Gotify:
	case Generic:
		body, err = json.Marshal(genericPayload{
			Role: event.Role, Event: event.Event, Subject: event.Subject,
			Body: event.Body, Severity: event.Severity,
			Timestamp:   timestamp.Format(time.RFC3339Nano),
			ProjectName: event.ProjectName, ProjectVersion: event.ProjectVersion,
			AppURL: event.AppURL, TrackingID: event.TrackingID,
		})
	default:
		adapter := lookupAdapter(hook.Transport)
		if adapter == nil {
			return nil, fmt.Errorf("unsupported webhook transport %q", hook.Transport)
		}
		endpoint, body, contentType, err = adapter.Build(endpoint, event)
	}
	if hook.Transport == Pushover || hook.Transport == Gotify {
		adapter := lookupAdapter(hook.Transport)
		if adapter == nil {
			return nil, fmt.Errorf("webhook transport %q requires a registered provider adapter", hook.Transport)
		}
		endpoint, body, contentType, err = adapter.Build(endpoint, event)
	}
	if err != nil {
		return nil, fmt.Errorf("encode webhook payload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("build webhook request")
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("User-Agent", fmt.Sprintf("%s/%s (+%s)", event.ProjectName, event.ProjectVersion, event.AppURL))
	request.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(timestamp.Unix(), 10))
	request.Header.Set("X-Webhook-ID", id)
	request.Header.Set("X-Webhook-Event", event.Event)
	request.Header.Set("X-Webhook-Signature", Signature(hook.Secret, body))
	return request, nil
}

type genericPayload struct {
	Role           string `json:"role"`
	Event          string `json:"event"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	Severity       string `json:"severity"`
	Timestamp      string `json:"timestamp"`
	ProjectName    string `json:"project_name"`
	ProjectVersion string `json:"project_version"`
	AppURL         string `json:"app_url"`
	TrackingID     string `json:"tracking_id,omitempty"`
}

// Signature returns the Part 12 signature for the exact outgoing body bytes.
func Signature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Send delivers a request and closes the response body. Non-2xx status codes
// return a status-only error; response bodies and destination URLs are not
// included to avoid exposing user content or webhook secrets.
func Send(client HTTPDoer, request *http.Request) error {
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("webhook request failed")
	}
	if response.Body != nil {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook returned HTTP status %d", response.StatusCode)
	}
	return nil
}

func parseHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("webhook URL must be an absolute HTTPS URL without user information")
	}
	return parsed, nil
}

func appendQuery(endpoint *url.URL, key, value string) {
	query := endpoint.Query()
	query.Set(key, value)
	endpoint.RawQuery = query.Encode()
}

func lookupAdapter(transport Transport) Adapter {
	adapters.RLock()
	defer adapters.RUnlock()
	return adapters.byTransport[transport]
}

func isNative(transport Transport) bool {
	switch transport {
	case Telegram, Discord, Slack, Mattermost, Generic:
		return true
	default:
		return false
	}
}
