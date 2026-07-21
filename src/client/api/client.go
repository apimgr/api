// Package api implements the HTTP client the CLI uses to talk to a
// running api server's REST endpoints, per AI.md PART 32.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UserAgent is the fixed User-Agent the client sends regardless of the
// installed binary's filename. Set from build info in cmd.
var UserAgent = "api-cli/dev"

// Client talks to a single api server.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	Debug   bool
}

// New creates a client for the given base URL (scheme + host, no
// trailing slash required).
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Error represents a non-2xx API response.
type Error struct {
	StatusCode int
	Body       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("server returned %d: %s", e.StatusCode, e.Body)
}

// Get issues a GET request against path with the given query parameters
// and returns the raw response body.
func (c *Client) Get(path string, query map[string]string) ([]byte, error) {
	return c.do(http.MethodGet, path, query, nil)
}

// PostJSON issues a POST request with a JSON body and returns the raw
// response body.
func (c *Client) PostJSON(path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}
	return c.do(http.MethodPost, path, nil, payload)
}

func (c *Client) do(method, path string, query map[string]string, body []byte) ([]byte, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("no server configured; use --server or set server.primary in cli.yml")
	}

	u, err := url.Parse(c.BaseURL + path)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	if len(query) > 0 {
		q := u.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = strings.NewReader(string(body))
	}

	req, err := http.NewRequest(method, u.String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to server at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return respBody, &Error{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	if resp.StatusCode == http.StatusNotFound {
		return respBody, &Error{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBody, &Error{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	return respBody, nil
}

// EncodePathSegment percent-encodes a single path segment (not a full
// path) so user input can never smuggle extra path components into a
// request URL, per PART 32 URL encoding rules.
func EncodePathSegment(s string) string {
	return url.PathEscape(s)
}
