package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vitaliipsl/crappy/internal/settings"
)

func TestRegistryFromSettingsPreservesLogicalProviderID(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistryFromSettings(settings.Settings{
		Providers: []settings.Provider{{ID: "work", API: "openai"}},
	})
	if err != nil {
		t.Fatalf("NewRegistryFromSettings() error = %v", err)
	}

	resolved, err := registry.Resolve(context.Background(), "work", "model-id")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got, want := resolved.Config().Provider, "work"; got != want {
		t.Fatalf("model provider = %q, want %q", got, want)
	}

	if got, want := resolved.Config().ID, "model-id"; got != want {
		t.Fatalf("model ID = %q, want %q", got, want)
	}
}

func TestRegistryFromSettingsRejectsUnsupportedAPI(t *testing.T) {
	t.Parallel()

	_, err := NewRegistryFromSettings(settings.Settings{
		Providers: []settings.Provider{{ID: "provider", API: "unsupported"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported API") {
		t.Fatalf("NewRegistryFromSettings() error = %v, want unsupported API error", err)
	}
}

func TestConfiguredAPIKeyEnvironmentVariableIsRequired(t *testing.T) {
	const environmentVariable = "CRAPPY_TEST_MISSING_API_KEY"

	t.Setenv(environmentVariable, "")

	registry, err := NewRegistryFromSettings(settings.Settings{
		Providers: []settings.Provider{{
			ID:        "provider",
			API:       "openai",
			APIKeyEnv: environmentVariable,
		}},
	})
	if err != nil {
		t.Fatalf("NewRegistryFromSettings() error = %v", err)
	}

	_, err = registry.Resolve(context.Background(), "provider", "model")
	if err == nil {
		t.Fatal("Resolve() error = nil, want missing environment variable error")
	}

	if errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("Resolve() error = %v, want provider creation error", err)
	}
}
