package app

import (
	"context"
	"fmt"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/compaction/automatic"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/instructions"
	"github.com/vitaliipsl/crappy/internal/memory"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/service"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/tools"
	"github.com/vitaliipsl/crappy/internal/tools/local"
)

type Runtime struct {
	service *service.Local
	harness *harness.Harness
}

func Open(ctx context.Context, settings settings.Settings) (*Runtime, error) {
	configuration, err := config.LoadFile(settings.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	memories, err := memory.Open(ctx, settings.Memory)
	if err != nil {
		return nil, err
	}

	models, err := model.NewRegistry(settings.Providers)
	if err != nil {
		return nil, fmt.Errorf("create model registry: %w", err)
	}

	resolver := models.Resolver(configuration.ProviderID, configuration.ModelID, model.Retry(settings.Retry))
	estimator := token.NewCharacterEstimator()

	compactor, err := compaction.NewCompactor(configuration.CompactionConfig(), resolver,
		compaction.WithEstimator(estimator),
	)
	if err != nil {
		return nil, fmt.Errorf("create compactor: %w", err)
	}

	automaticCompaction := automatic.New(compactor,
		automatic.WithEstimator(estimator),
		automatic.WithTrigger(automatic.WhenInputExceedsTokens(configuration.Compaction.ThresholdTokens)),
	)

	storage, err := jsonl.Open(ctx, settings.SessionsPath)
	if err != nil {
		return nil, fmt.Errorf("open session storage: %w", err)
	}

	agents := agent.New(configuration.AgentConfig(), resolver,
		agent.WithToolMiddleware(
			tools.OutputCap(settings.Tools, storage.PutBlob),
			tools.Timeout(settings.Tools),
		),
		agent.WithContributors(
			instructions.NewContributor(),
			local.NewContributor(),
			memory.NewContributor(memories),
			automaticCompaction,
		),
	)

	runtime := harness.New(storage,
		harness.WithExecutor(agent.ExecutorName, agent.NewExecutor(agents)),
		harness.WithExecutor(compactionexec.ExecutorName, compactionexec.New(compactor)),
	)

	return &Runtime{
		service: service.NewLocal(configuration, runtime, storage, agents, estimator),
		harness: runtime,
	}, nil
}

func (r *Runtime) Service() *service.Local {
	return r.service
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	return r.harness.Shutdown(ctx)
}
