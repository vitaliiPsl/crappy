package settings

import (
	"github.com/vitaliiPsl/crappy-adk/x/retry"

	"github.com/vitaliipsl/crappy/internal/mcp"
	"github.com/vitaliipsl/crappy/internal/memory"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/schedule"
	"github.com/vitaliipsl/crappy/internal/tools"
)

const (
	EnvPath             = "CRAPPY_SETTINGS"
	DefaultPath         = "~/.crappy/settings.yaml"
	DefaultConfigPath   = "~/.crappy/config.yaml"
	DefaultSessionsPath = "~/.crappy/sessions"
)

type Settings struct {
	ConfigPath   string           `yaml:"config_path,omitempty" validate:"required"`
	SessionsPath string           `yaml:"sessions_path,omitempty" validate:"required"`
	Scheduling   schedule.Config  `yaml:"scheduling"`
	Memory       memory.Config    `yaml:"memory"`
	MCP          mcp.Config       `yaml:"mcp"`
	Tools        tools.Config     `yaml:"tools"`
	Retry        retry.Config     `yaml:"retry"`
	Providers    []model.Provider `yaml:"providers" validate:"min=1,unique=ID,dive"`
}

func defaults() Settings {
	return Settings{
		ConfigPath:   DefaultConfigPath,
		SessionsPath: DefaultSessionsPath,
		Scheduling:   schedule.DefaultConfig(),
		Memory:       memory.DefaultConfig(),
		MCP:          mcp.DefaultConfig(),
		Tools:        tools.DefaultConfig(),
		Retry:        retry.DefaultConfig(),
		Providers: []model.Provider{
			{ID: "anthropic", API: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
			{ID: "google", API: "google", APIKeyEnv: "GOOGLE_API_KEY"},
			{ID: "openai", API: "openai", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
}
