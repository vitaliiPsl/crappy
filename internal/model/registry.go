package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

var ErrProviderNotFound = errors.New("model provider not found")

type Registry struct {
	providers map[string]ProviderFactory
}

func NewRegistry(providers []Provider) (*Registry, error) {
	configured, err := providerFactories(providers)
	if err != nil {
		return nil, err
	}

	return newRegistry(configured)
}

func newRegistry(providers map[string]ProviderFactory) (*Registry, error) {
	registered := make(map[string]ProviderFactory, len(providers))
	for id, factory := range providers {
		if id == "" {
			return nil, errors.New("provider ID is required")
		}

		if factory == nil {
			return nil, fmt.Errorf("provider %q factory is required", id)
		}

		registered[id] = factory
	}

	return &Registry{
		providers: registered,
	}, nil
}

func (r *Registry) Resolve(
	ctx context.Context,
	providerID string,
	modelID string,
) (kit.Model, error) {
	factory, ok := r.providers[providerID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, providerID)
	}

	resolved, err := factory.CreateModel(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("create model %s/%s: %w", providerID, modelID, err)
	}

	if resolved == nil {
		return nil, fmt.Errorf("create model %s/%s: model is nil", providerID, modelID)
	}

	return resolved, nil
}
