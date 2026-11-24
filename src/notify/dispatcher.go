package notify

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

var failedDeliveryLogs = struct {
	sync.Mutex
	last map[string]time.Time
}{last: make(map[string]time.Time)}

// Dispatch starts a background delivery and retries failed HTTP attempts
// using the Part 12 schedule. The immutable event and UUID v7 delivery ID are
// shared across retries; each attempt gets a fresh replay-protection header.
func Dispatch(hook Webhook, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	id, err := NewID()
	if err != nil {
		logDeliveryFailure(hook, err)
		return
	}
	go func() {
		if err := sendWithRetry(context.Background(), nil, hook, event, id, waitForRetry, time.Now); err != nil {
			logDeliveryFailure(hook, err)
		}
	}()
}

func sendWithRetry(ctx context.Context, client HTTPDoer, hook Webhook, event Event, id string, wait func(context.Context, time.Duration) bool, now func() time.Time) error {
	failedAttempts := 0
	for {
		request, err := BuildRequest(ctx, hook, event, id, now().UTC())
		if err != nil {
			return err
		}
		sendErr := Send(client, request)
		if sendErr == nil {
			return nil
		}
		delay, ok := RetryDelay(failedAttempts)
		if !ok {
			return sendErr
		}
		failedAttempts++
		if !wait(ctx, delay) {
			return ctx.Err()
		}
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func logDeliveryFailure(hook Webhook, err error) {
	role := strings.TrimSpace(hook.Name)
	if role == "" {
		role = "unknown"
	}
	key := role + "." + string(hook.Transport)
	now := time.Now()
	failedDeliveryLogs.Lock()
	last := failedDeliveryLogs.last[key]
	if !last.IsZero() && now.Sub(last) < time.Minute {
		failedDeliveryLogs.Unlock()
		return
	}
	failedDeliveryLogs.last[key] = now
	failedDeliveryLogs.Unlock()
	log.Printf("notify.webhook_failed role=%s transport=%s error=%v", role, hook.Transport, err)
}
