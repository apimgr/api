package tor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitControlPortRequiresCompleteLoopbackPortLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control_port")
	process := &torProcess{done: make(chan struct{}), output: &processOutput{}}
	if err := os.WriteFile(path, []byte("PORT=127.0.0.1:9051\n"), 0600); err != nil {
		t.Fatal(err)
	}
	address, err := process.waitControlPort(context.Background(), path, time.Second)
	if err != nil || address != "127.0.0.1:9051" {
		t.Fatalf("waitControlPort() = (%q, %v), want (127.0.0.1:9051, nil)", address, err)
	}
}

func TestWaitControlPortRejectsPartialAndNonLoopbackLines(t *testing.T) {
	for _, contents := range []string{
		"PORT=127.0.0.1:9051",
		"PORT=0.0.0.0:9051\n",
		"PORT=127.0.0.1:0\n",
		"PORT=127.0.0.1:99999\n",
	} {
		t.Run(contents, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "control_port")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			process := &torProcess{done: make(chan struct{}), output: &processOutput{}}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			if address, err := process.waitControlPort(ctx, path, time.Second); err == nil || address != "" {
				t.Fatalf("waitControlPort() = (%q, %v), want timeout", address, err)
			}
		})
	}
}

func TestWaitHostnameRequiresValidV3Address(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hostname")
	address := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.onion"
	if err := os.WriteFile(path, []byte(address+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := waitHostname(context.Background(), path, time.Second)
	if err != nil || got != address {
		t.Fatalf("waitHostname() = (%q, %v), want (%q, nil)", got, err, address)
	}
}
