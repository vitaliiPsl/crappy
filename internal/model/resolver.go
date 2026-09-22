package model

import (
	"context"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Resolver struct {
	registry   *Registry
	providerID string
	modelID    string
	middleware []Middleware
}

func (r *Registry) Resolver(providerID, modelID string, middleware ...Middleware) *Resolver {
	return &Resolver{
		registry:   r,
		providerID: providerID,
		modelID:    modelID,
		middleware: middleware,
	}
}

func (r *Resolver) Resolve(ctx context.Context) (kit.Model, error) {
	resolved, err := r.registry.Resolve(ctx, r.providerID, r.modelID)
	if err != nil {
		return nil, err
	}

	for index := len(r.middleware) - 1; index >= 0; index-- {
		resolved = r.middleware[index](resolved)
	}

	return resolved, nil
}
