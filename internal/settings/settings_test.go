package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFileSeedsDefaults(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "settings.yaml")

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if got, want := len(loaded.Providers), 3; got != want {
		t.Fatalf("providers = %d, want %d", got, want)
	}

	if !filepath.IsAbs(loaded.ConfigPath) {
		t.Fatalf("ConfigPath = %q, want absolute path", loaded.ConfigPath)
	}

	if !filepath.IsAbs(loaded.SessionsPath) {
		t.Fatalf("SessionsPath = %q, want absolute path", loaded.SessionsPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seeded settings: %v", err)
	}

	if !strings.Contains(string(data), "config_path:") {
		t.Fatalf("seeded settings = %q, want config path", data)
	}

	if !strings.Contains(string(data), "sessions_path:") {
		t.Fatalf("seeded settings = %q, want sessions path", data)
	}

	if !strings.Contains(string(data), "timeout: 30s") {
		t.Fatalf("seeded settings = %q, want tool timeout", data)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat seeded settings: %v", err)
	}

	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode = %o, want %o", got, want)
	}
}

func TestLoadFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, `
providers:
  - id: work
    api: openai
    base_url: https://example.com/v1
    api_key: work-key
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

	if got, want := loaded.Tools.Timeout, 30*time.Second; got != want {
		t.Fatalf("tool timeout = %v, want %v", got, want)
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

	if got, want := provider.APIKey, "work-key"; got != want {
		t.Fatalf("provider API key = %q, want %q", got, want)
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

func TestLoadFileRejectsInvalidToolTimeouts(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, `
tools:
  timeout: 2m
  max_timeout: 1m
providers:
  - id: provider
    api: openai
`)

	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile() error = nil, want invalid tool timeout error")
	}
}

func TestLoadFileResolvesPathsRelativeToSettings(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "settings.yaml")
	writeSettings(t, path, `
config_path: config/config.yaml
sessions_path: data/sessions
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

	want = filepath.Join(directory, "data", "sessions")
	if loaded.SessionsPath != want {
		t.Fatalf("SessionsPath = %q, want %q", loaded.SessionsPath, want)
	}
}

func writeSettings(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}
