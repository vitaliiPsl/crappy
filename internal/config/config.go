package config

import (
	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
)

const (
	defaultProviderID = "openai"
	defaultModelID    = "gpt-5.6-luna"

	defaultCheckpointThresholdTokens int64 = 120_000
	defaultCheckpointRetainedTokens  int   = 20_000
)

type Config struct {
	Instructions string     `yaml:"instructions,omitempty"`
	ProviderID   string     `yaml:"provider_id" validate:"required"`
	ModelID      string     `yaml:"model_id" validate:"required"`
	Generation   Generation `yaml:"generation,omitempty"`
	Checkpoint   Checkpoint `yaml:"checkpoint,omitempty"`
}

type Generation struct {
	Thinking        string   `yaml:"thinking,omitempty" validate:"omitempty,oneof=disabled low medium high"`
	Temperature     *float32 `yaml:"temperature,omitempty"`
	MaxOutputTokens *int32   `yaml:"max_output_tokens,omitempty" validate:"omitempty,gt=0"`
}

type Checkpoint struct {
	ThresholdTokens int64 `yaml:"threshold_tokens" validate:"gte=0"`
	RetainedTokens  int   `yaml:"retained_tokens" validate:"gte=0"`
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

func (c Config) CheckpointConfig() checkpoint.Config {
	return checkpoint.Config{
		RetainedTokens: &c.Checkpoint.RetainedTokens,
	}
}

func defaults() Config {
	return Config{
		ProviderID: defaultProviderID,
		ModelID:    defaultModelID,
		Checkpoint: Checkpoint{
			ThresholdTokens: defaultCheckpointThresholdTokens,
			RetainedTokens:  defaultCheckpointRetainedTokens,
		},
	}
}
