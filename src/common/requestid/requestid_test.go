package requestid

import (
	"context"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDoPropagatesContextRequestID(t *testing.T) {
	const id = "550e8400-e29b-41d4-a716-446655440000"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("X-Request-ID"); got != id {
			t.Fatalf("X-Request-ID = %q, want %q", got, id)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
	})}
	request, err := http.NewRequestWithContext(WithRequestID(context.Background(), id), http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DoOutboundRequestWithID(client, request); err != nil {
		t.Fatal(err)
	}
}

func TestDoLeavesRequestIDUnsetWithoutContextValue(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("X-Request-ID"); got != "" {
			t.Fatalf("unexpected X-Request-ID %q", got)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
	})}
	request, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DoOutboundRequestWithID(client, request); err != nil {
		t.Fatal(err)
	}
}
