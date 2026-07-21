package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestContextWithRequestID_RoundTrip verifies a request ID stored via
// contextWithRequestID is retrievable via RequestIDFromContext, and that
// the returned context does not mutate its parent.
func TestContextWithRequestID_RoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		requestID string
	}{
		{"typical id", "abc123"},
		{"empty string id", ""},
		{"hex id", "0123456789abcdef0123456789abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := context.Background()
			ctx := contextWithRequestID(parent, tt.requestID)

			assert.Equal(t, tt.requestID, RequestIDFromContext(ctx))
			// Parent context must remain unaffected (context.WithValue
			// never mutates the parent).
			assert.Equal(t, "", RequestIDFromContext(parent))
		})
	}
}

// TestRequestIDFromContext_Missing covers contexts that never had a
// request ID set, and a context holding a wrong-typed value under the
// same key shape, both of which must fall back to "".
func TestRequestIDFromContext_Missing(t *testing.T) {
	t.Run("bare background context", func(t *testing.T) {
		assert.Equal(t, "", RequestIDFromContext(context.Background()))
	})

	t.Run("wrong value type under requestIDKey", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestIDKey, 12345)
		assert.Equal(t, "", RequestIDFromContext(ctx))
	})
}

// TestContextWithRequestID_Overwrite ensures a second call overwrites the
// first request ID rather than stacking/shadowing incorrectly.
func TestContextWithRequestID_Overwrite(t *testing.T) {
	ctx := contextWithRequestID(context.Background(), "first")
	ctx = contextWithRequestID(ctx, "second")

	assert.Equal(t, "second", RequestIDFromContext(ctx))
}
