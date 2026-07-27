package server

import (
	"context"
)

type contextKey string

const (
	requestIDKey     contextKey = "requestID"
	privacyOptOutKey contextKey = "privacyOptOut"
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

// contextWithPrivacyOptOut marks the request as having sent a Sec-GPC or DNT
// opt-out signal that server.yml is configured to honor (AI.md PART 11 →
// "Privacy Signal Headers").
func contextWithPrivacyOptOut(ctx context.Context, optOut bool) context.Context {
	return context.WithValue(ctx, privacyOptOutKey, optOut)
}

// PrivacyOptOutFromContext reports whether the current request sent an
// honored privacy opt-out signal (Sec-GPC or DNT).
func PrivacyOptOutFromContext(ctx context.Context) bool {
	optOut, ok := ctx.Value(privacyOptOutKey).(bool)
	return ok && optOut
}
