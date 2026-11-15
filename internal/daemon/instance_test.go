package daemon

import (
	"errors"
	"testing"
)

func TestInstanceKeepsOwnershipAfterListenerCloses(t *testing.T) {
	directory := newSocketRoot(t)

	instance, err := Acquire(directory)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = instance.Close() })

	listener, err := instance.Listen()
	if err != nil {
		t.Fatal(err)
	}

	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	if other, err := Acquire(directory); !errors.Is(err, ErrRunning) {
		if other != nil {
			_ = other.Close()
		}

		t.Fatalf("ownership released with listener: %v", err)
	}

	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}

	other, err := Acquire(directory)
	if err != nil {
		t.Fatalf("ownership not released: %v", err)
	}

	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
}
