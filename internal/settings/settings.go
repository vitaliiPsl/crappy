package settings

import (
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/tools"
)

const (
	EnvPath           = "CRAPPY_SETTINGS"
	DefaultPath       = "~/.crappy/settings.yaml"
	DefaultConfigPath = "~/.crappy/config.yaml"
)

type Settings struct {
	ConfigPath string           `yaml:"config_path,omitempty" validate:"required"`
	Tools      tools.Config     `yaml:"tools"`
	Providers  []model.Provider `yaml:"providers" validate:"min=1,unique=ID,dive"`
}

func defaults() Settings {
	return Settings{
		ConfigPath: DefaultConfigPath,
		Tools:      tools.DefaultConfig(),
		Providers: []model.Provider{
			{ID: "anthropic", API: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
			{ID: "google", API: "google", APIKeyEnv: "GOOGLE_API_KEY"},
			{ID: "openai", API: "openai", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
}
