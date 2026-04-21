package main

import (
	"testing"
	"time"
)

func TestBuildHermesTimeout_default(t *testing.T) {
	t.Setenv("HERMES_TIMEOUT_SECONDS", "")

	if got, want := buildHermesTimeout(), 3*time.Minute; got != want {
		t.Fatalf("buildHermesTimeout default: got %v, want %v", got, want)
	}
}

func TestBuildHermesTimeout_fromEnv(t *testing.T) {
	t.Setenv("HERMES_TIMEOUT_SECONDS", "240")

	if got, want := buildHermesTimeout(), 240*time.Second; got != want {
		t.Fatalf("buildHermesTimeout from env: got %v, want %v", got, want)
	}
}

func TestBuildHermesTimeout_invalidFallsBack(t *testing.T) {
	t.Setenv("HERMES_TIMEOUT_SECONDS", "nope")

	if got, want := buildHermesTimeout(), 3*time.Minute; got != want {
		t.Fatalf("buildHermesTimeout invalid fallback: got %v, want %v", got, want)
	}
}
