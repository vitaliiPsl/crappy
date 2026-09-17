package model

import (
	"context"
	"errors"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
)

func TestRegistryResolvesProviderModels(t *testing.T) {
	t.Parallel()

	first := kittest.NewModel(t)
	second := kittest.NewModel(t)
	calls := make(map[string]int)

	registry, err := NewRegistry(map[string]ProviderFactory{
		"provider": ProviderFactoryFunc(func(
			_ context.Context,
			modelID string,
		) (kit.Model, error) {
			calls[modelID]++
			if modelID == "first" {
				return first, nil
			}

			return second, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	for range 2 {
		resolved, err := registry.Resolve(context.Background(), "provider", "first")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}

		if resolved != first {
			t.Fatal("Resolve() returned the wrong model")
		}
	}

	resolved, err := registry.Resolve(context.Background(), "provider", "second")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved != second {
		t.Fatal("Resolve() returned the wrong model")
	}

	if got, want := calls["first"], 2; got != want {
		t.Fatalf("first model factory calls = %d, want %d", got, want)
	}

	if got, want := calls["second"], 1; got != want {
		t.Fatalf("second model factory calls = %d, want %d", got, want)
	}
}

func TestRegistrySeparatesProviders(t *testing.T) {
	t.Parallel()

	first := kittest.NewModel(t)
	second := kittest.NewModel(t)

	registry, err := NewRegistry(map[string]ProviderFactory{
		"first": ProviderFactoryFunc(func(context.Context, string) (kit.Model, error) {
			return first, nil
		}),
		"second": ProviderFactoryFunc(func(context.Context, string) (kit.Model, error) {
			return second, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	resolved, err := registry.Resolve(context.Background(), "first", "model")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved != first {
		t.Fatal("Resolve() returned the wrong model")
	}

	resolved, err = registry.Resolve(context.Background(), "second", "model")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved != second {
		t.Fatal("Resolve() returned the wrong model")
	}
}

func TestRegistryRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(nil)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	_, err = registry.Resolve(context.Background(), "missing", "model")
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrProviderNotFound", err)
	}
}
