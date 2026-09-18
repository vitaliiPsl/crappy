package model

import (
	"context"
	"fmt"
	"os"

	"github.com/vitaliiPsl/crappy-adk/kit"
	adkprovider "github.com/vitaliiPsl/crappy-adk/providers"
	"github.com/vitaliiPsl/crappy-adk/providers/anthropic"
	"github.com/vitaliiPsl/crappy-adk/providers/google"
	"github.com/vitaliiPsl/crappy-adk/providers/openai"
)

type apiFactory func(string, ...adkprovider.ModelOption) (kit.Model, error)

var apis = map[string]apiFactory{
	"anthropic": anthropic.New,
	"google":    google.New,
	"openai":    openai.New,
}

func providerFactories(providers []Provider) (map[string]ProviderFactory, error) {
	configured := make(map[string]ProviderFactory, len(providers))

	for _, provider := range providers {
		api, exists := apis[provider.API]
		if !exists {
			return nil, fmt.Errorf("provider %q: unsupported API %q", provider.ID, provider.API)
		}

		configured[provider.ID] = newProviderFactory(provider, api)
	}

	return configured, nil
}

func newProviderFactory(provider Provider, api apiFactory) ProviderFactory {
	return ProviderFactoryFunc(func(
		_ context.Context,
		modelID string,
	) (kit.Model, error) {
		options := []adkprovider.ModelOption{
			adkprovider.WithModelConfig(kit.ModelConfig{
				ID:       modelID,
				Provider: provider.ID,
			}),
		}

		if provider.BaseURL != "" {
			options = append(options, adkprovider.WithBaseURL(provider.BaseURL))
		}

		if provider.APIKey != "" {
			options = append(options, adkprovider.WithAPIKey(provider.APIKey))
		} else if provider.APIKeyEnv != "" {
			apiKey := os.Getenv(provider.APIKeyEnv)
			if apiKey == "" {
				return nil, fmt.Errorf("environment variable %s is not set", provider.APIKeyEnv)
			}

			options = append(options, adkprovider.WithAPIKey(apiKey))
		}

		return api(modelID, options...)
	})
}
