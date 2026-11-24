package database

import (
	"context"
	"time"
)

type requestTimingKey struct{}

type requestTimingRecorder interface {
	RecordDatabaseTiming(time.Duration)
}

// WithRequestTimingRecorder attaches a response timing recorder to database
// work initiated by one HTTP request.
func WithRequestTimingRecorder(ctx context.Context, recorder interface {
	RecordDatabaseTiming(time.Duration)
}) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, requestTimingKey{}, recorder)
}

func recordRequestDatabaseTiming(ctx context.Context, started time.Time) {
	if recorder, ok := ctx.Value(requestTimingKey{}).(requestTimingRecorder); ok {
		recorder.RecordDatabaseTiming(time.Since(started))
	}
}
