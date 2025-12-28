package server

import (
	"context"
)

type contextKey string

const (
	requestIDKey contextKey = "requestID"
)

// contextWithRequestID adds a request ID to the context
func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext retrieves the request ID from context
func RequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}
