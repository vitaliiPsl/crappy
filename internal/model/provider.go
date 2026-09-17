package model

import (
	"context"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

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
