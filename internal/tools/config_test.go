package tools

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	if got, want := config.Timeout, 30*time.Second; got != want {
		t.Fatalf("Timeout = %v, want %v", got, want)
	}

	if got, want := config.MaxTimeout, 5*time.Minute; got != want {
		t.Fatalf("MaxTimeout = %v, want %v", got, want)
	}
}
