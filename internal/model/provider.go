package model

import (
	"context"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Provider struct {
	ID        string `yaml:"id" validate:"required"`
	API       string `yaml:"api" validate:"required"`
	BaseURL   string `yaml:"base_url,omitempty"`
	APIKey    string `yaml:"api_key,omitempty"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}

type ProviderFactory interface {
	CreateModel(ctx context.Context, modelID string) (kit.Model, error)
}

type ProviderFactoryFunc func(ctx context.Context, modelID string) (kit.Model, error)

func (fn ProviderFactoryFunc) CreateModel(
	ctx context.Context,
	modelID string,
) (kit.Model, error) {
	return fn(ctx, modelID)
}
