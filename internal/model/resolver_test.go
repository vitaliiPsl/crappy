package model

import (
	"context"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
)

func TestResolverUsesBoundModel(t *testing.T) {
	t.Parallel()

	firstModel := kittest.NewModel(t)
	secondModel := kittest.NewModel(t)

	registry, err := newRegistry(map[string]ProviderFactory{
		"first": ProviderFactoryFunc(func(context.Context, string) (kit.Model, error) {
			return firstModel, nil
		}),
		"second": ProviderFactoryFunc(func(context.Context, string) (kit.Model, error) {
			return secondModel, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	resolver := registry.Resolver("first", "first-model")

	resolved, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("first Resolve() error = %v", err)
	}

	if resolved != firstModel {
		t.Fatal("first Resolve() returned the wrong model")
	}

	resolver = registry.Resolver("second", "second-model")

	resolved, err = resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("second Resolve() error = %v", err)
	}

	if resolved != secondModel {
		t.Fatal("second Resolve() returned the wrong model")
	}
}
