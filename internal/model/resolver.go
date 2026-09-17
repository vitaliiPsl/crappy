package model

import (
	"context"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Resolver struct {
	registry   *Registry
	providerID string
	modelID    string
}

func (r *Registry) Resolver(providerID, modelID string) *Resolver {
	return &Resolver{
		registry:   r,
		providerID: providerID,
		modelID:    modelID,
	}
}

func (r *Resolver) Resolve(ctx context.Context) (kit.Model, error) {
	return r.registry.Resolve(ctx, r.providerID, r.modelID)
}
