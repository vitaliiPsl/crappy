package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestLoadFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, `
provider_id: provider
model_id: model
instructions: Be concise.
generation:
  thinking: high
  temperature: 0.4
  max_output_tokens: 2048
`)

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if got, want := loaded.ProviderID, "provider"; got != want {
		t.Fatalf("provider ID = %q, want %q", got, want)
	}

	if got, want := loaded.ModelID, "model"; got != want {
		t.Fatalf("model ID = %q, want %q", got, want)
	}

	if got, want := loaded.Instructions, "Be concise."; got != want {
		t.Fatalf("instructions = %q, want %q", got, want)
	}

	if got, want := loaded.Generation.Thinking, "high"; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}

	if got, want := *loaded.Generation.Temperature, float32(0.4); got != want {
		t.Fatalf("temperature = %v, want %v", got, want)
	}

	if got, want := *loaded.Generation.MaxOutputTokens, int32(2048); got != want {
		t.Fatalf("max output tokens = %d, want %d", got, want)
	}

	agentConfig := loaded.AgentConfig()
	if agentConfig.Thinking == nil || *agentConfig.Thinking != kit.ThinkingLevelHigh {
		t.Fatalf("agent thinking = %v, want %q", agentConfig.Thinking, kit.ThinkingLevelHigh)
	}
}

func TestLoadFileRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, `
provider_id: provider
model_id: model
generation:
  thinking: extreme
`)

	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile() error = nil, want validation error")
	}
}

func writeConfig(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
