package model

import (
	"context"
	"errors"
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

type wrappedModel struct {
	kit.Model
	label string
}

func staticRegistry(t *testing.T, model kit.Model) *Registry {
	t.Helper()

	registry, err := newRegistry(map[string]ProviderFactory{
		"provider": ProviderFactoryFunc(func(context.Context, string) (kit.Model, error) {
			return model, nil
		}),
	})
	if err != nil {
		t.Fatalf("newRegistry() error = %v", err)
	}

	return registry
}

func label(name string) Middleware {
	return func(resolved kit.Model) kit.Model {
		return wrappedModel{Model: resolved, label: name}
	}
}

func TestResolverAppliesMiddleware(t *testing.T) {
	t.Parallel()

	base := kittest.NewModel(t)
	registry := staticRegistry(t, base)

	resolved, err := registry.Resolver("provider", "model", label("only")).Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	wrapped, ok := resolved.(wrappedModel)
	if !ok {
		t.Fatalf("Resolve() = %T, want a wrapped model", resolved)
	}

	if wrapped.Model != base {
		t.Fatal("middleware did not wrap the resolved model")
	}
}

func TestResolverAppliesMiddlewareInRegistrationOrder(t *testing.T) {
	t.Parallel()

	registry := staticRegistry(t, kittest.NewModel(t))

	resolved, err := registry.Resolver(
		"provider",
		"model",
		label("outer"),
		label("inner"),
	).Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	outer, ok := resolved.(wrappedModel)
	if !ok || outer.label != "outer" {
		t.Fatalf("outermost = %#v, want the first registered middleware", resolved)
	}

	inner, ok := outer.Model.(wrappedModel)
	if !ok || inner.label != "inner" {
		t.Fatalf("innermost = %#v, want the last registered middleware", outer.Model)
	}
}

func TestResolverSkipsMiddlewareWhenResolutionFails(t *testing.T) {
	t.Parallel()

	registry, err := newRegistry(map[string]ProviderFactory{
		"provider": ProviderFactoryFunc(func(context.Context, string) (kit.Model, error) {
			return nil, errors.New("unavailable")
		}),
	})
	if err != nil {
		t.Fatalf("newRegistry() error = %v", err)
	}

	called := false
	resolver := registry.Resolver("provider", "model", func(resolved kit.Model) kit.Model {
		called = true

		return resolved
	})

	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("Resolve() error = nil, want a resolution error")
	}

	if called {
		t.Fatal("middleware ran despite a failed resolution")
	}
}
