package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, `
providers:
  - id: work
    api: openai
    base_url: https://example.com/v1
    api_key_env: WORK_API_KEY
  - id: personal
    api: anthropic
`)

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if got, want := len(loaded.Providers), 2; got != want {
		t.Fatalf("providers = %d, want %d", got, want)
	}

	provider := loaded.Providers[0]
	if got, want := provider.ID, "work"; got != want {
		t.Fatalf("provider ID = %q, want %q", got, want)
	}

	if got, want := provider.API, "openai"; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}

	if got, want := provider.BaseURL, "https://example.com/v1"; got != want {
		t.Fatalf("provider base URL = %q, want %q", got, want)
	}

	if got, want := provider.APIKeyEnv, "WORK_API_KEY"; got != want {
		t.Fatalf("provider API key environment variable = %q, want %q", got, want)
	}
}

func TestLoadFileRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, `
providers:
  - id: provider
    api: openai
    unknown: value
`)

	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile() error = nil, want unknown field error")
	}
}

func TestSettingsRejectsDuplicateProviderIDs(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, `
providers:
  - id: provider
    api: openai
  - id: provider
    api: anthropic
`)

	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile() error = nil, want duplicate provider error")
	}
}

func TestLoadFileResolvesConfigPathRelativeToSettings(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "settings.yaml")
	writeSettings(t, path, `
config_path: config/config.yaml
providers:
  - id: provider
    api: openai
`)

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	want := filepath.Join(directory, "config", "config.yaml")
	if loaded.ConfigPath != want {
		t.Fatalf("ConfigPath = %q, want %q", loaded.ConfigPath, want)
	}
}

func writeSettings(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}
