package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/providers/anthropic"
	"github.com/vitaliiPsl/crappy-adk/providers/google"
	"github.com/vitaliiPsl/crappy-adk/providers/openai"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	assistantmodel "github.com/vitaliipsl/crappy/internal/model"
	internalruntime "github.com/vitaliipsl/crappy/internal/runtime"
)

const defaultInstructions = "You are a helpful assistant."

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	runtime, err := newRuntime()
	if err != nil {
		return err
	}

	_ = runtime

	<-ctx.Done()

	return nil
}

func newRuntime() (*internalruntime.Runtime, error) {
	providerID := os.Getenv("CRAPPY_MODEL_PROVIDER")
	if providerID == "" {
		return nil, errors.New("CRAPPY_MODEL_PROVIDER is not set")
	}

	modelID := os.Getenv("CRAPPY_MODEL_ID")
	if modelID == "" {
		return nil, errors.New("CRAPPY_MODEL_ID is not set")
	}

	models, err := newModelRegistry()
	if err != nil {
		return nil, fmt.Errorf("create model registry: %w", err)
	}

	selectedModel := models.Resolver(providerID, modelID)

	generator, err := checkpoint.NewGenerator(selectedModel)
	if err != nil {
		return nil, fmt.Errorf("create checkpoint generator: %w", err)
	}

	instructions := os.Getenv("CRAPPY_INSTRUCTIONS")
	if instructions == "" {
		instructions = defaultInstructions
	}

	agents := agent.New(
		selectedModel,
		agent.WithAgentOptions(adkagent.WithInstructions(instructions)),
	)

	return internalruntime.NewRuntime(
		entry.NewMemoryStore(),
		agents,
		generator,
	), nil
}

func newModelRegistry() (*assistantmodel.Registry, error) {
	return assistantmodel.NewRegistry(map[string]assistantmodel.ProviderFactory{
		"anthropic": assistantmodel.ProviderFactoryFunc(func(
			_ context.Context,
			modelID string,
		) (kit.Model, error) {
			return anthropic.New(modelID)
		}),
		"google": assistantmodel.ProviderFactoryFunc(func(
			_ context.Context,
			modelID string,
		) (kit.Model, error) {
			return google.New(modelID)
		}),
		"openai": assistantmodel.ProviderFactoryFunc(func(
			_ context.Context,
			modelID string,
		) (kit.Model, error) {
			return openai.New(modelID)
		}),
	})
}
