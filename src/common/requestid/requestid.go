// Package requestid stores request-scoped IDs and propagates them on
// outbound HTTP calls.
package requestid

import (
	"context"
	"net/http"
)

type contextKey struct{}

// WithID stores a request ID in a context for downstream calls and logs.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext retrieves the request ID stored in ctx.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

// Do adds the context request ID to an outbound request before sending it.
func DoOutboundRequestWithID(client interface {
	Do(*http.Request) (*http.Response, error)
}, req *http.Request) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if req != nil {
		if id := RequestIDFromContext(req.Context()); id != "" {
			req.Header.Set("X-Request-ID", id)
		}
	}
	return client.Do(req)
}
