package main

import "testing"

func TestUsageExitCodeMatchesContract(t *testing.T) {
	if exUsage != 2 {
		t.Fatalf("usage exit code = %d, want 2", exUsage)
	}
}
