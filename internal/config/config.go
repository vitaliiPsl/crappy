package config

import "github.com/vitaliiPsl/crappy-adk/kit"

const (
	defaultProviderID = "openai"
	defaultModelID    = "gpt-5.6-luna"
)

type Config struct {
	Instructions string     `yaml:"instructions,omitempty"`
	ProviderID   string     `yaml:"provider_id" validate:"required"`
	ModelID      string     `yaml:"model_id" validate:"required"`
	Generation   Generation `yaml:"generation,omitempty"`
}

type Generation struct {
	Thinking        string   `yaml:"thinking,omitempty" validate:"omitempty,oneof=disabled low medium high"`
	Temperature     *float32 `yaml:"temperature,omitempty"`
	MaxOutputTokens *int32   `yaml:"max_output_tokens,omitempty" validate:"omitempty,gt=0"`
}

func (c Config) AgentConfig() kit.AgentConfig {
	configured := kit.AgentConfig{
		Instructions:    c.Instructions,
		Temperature:     c.Generation.Temperature,
		MaxOutputTokens: c.Generation.MaxOutputTokens,
	}

	if c.Generation.Thinking != "" {
		thinking := kit.ThinkingLevel(c.Generation.Thinking)
		configured.Thinking = &thinking
	}

	return configured
}

func defaults() Config {
	return Config{
		ProviderID: defaultProviderID,
		ModelID:    defaultModelID,
	}
}
