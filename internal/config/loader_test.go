package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestLoadFileSeedsDefaults(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "config.yaml")

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if got, want := loaded.ProviderID, defaultProviderID; got != want {
		t.Fatalf("ProviderID = %q, want %q", got, want)
	}

	if got, want := loaded.ModelID, defaultModelID; got != want {
		t.Fatalf("ModelID = %q, want %q", got, want)
	}

	if got, want := loaded.Compaction.ThresholdTokens, defaultCompactionThresholdTokens; got != want {
		t.Fatalf("compaction threshold = %d, want %d", got, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seeded config: %v", err)
	}

	if !strings.Contains(string(data), "provider_id: "+defaultProviderID) {
		t.Fatalf("seeded config = %q, want default provider", data)
	}

	if !strings.Contains(string(data), "threshold_tokens:") {
		t.Fatalf("seeded config = %q, want compaction threshold", data)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat seeded config: %v", err)
	}

	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode = %o, want %o", got, want)
	}
}

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
compaction:
  threshold_tokens: 120000
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

	if got, want := loaded.Compaction.ThresholdTokens, int64(120_000); got != want {
		t.Fatalf("compaction threshold = %d, want %d", got, want)
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

func TestLoadFileRejectsNegativeCompactionThreshold(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, `
provider_id: provider
model_id: model
compaction:
  threshold_tokens: -1
`)

	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile() error = nil, want validation error")
	}
}

func TestLoadFileDefaultsCompactionThreshold(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, `
provider_id: provider
model_id: model
`)

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if got, want := loaded.Compaction.ThresholdTokens, defaultCompactionThresholdTokens; got != want {
		t.Fatalf("compaction threshold = %d, want %d", got, want)
	}

	if got, want := loaded.Compaction.RetainedTokens, defaultCompactionRetainedTokens; got != want {
		t.Fatalf("compaction retained tokens = %d, want %d", got, want)
	}
}

func TestLoadFileDisablesCompaction(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, `
provider_id: provider
model_id: model
compaction:
  threshold_tokens: 0
`)

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if got, want := loaded.Compaction.ThresholdTokens, int64(0); got != want {
		t.Fatalf("compaction threshold = %d, want %d", got, want)
	}
}

func writeConfig(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
