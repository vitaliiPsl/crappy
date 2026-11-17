package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/compaction/automatic"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/background"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/instructions"
	"github.com/vitaliipsl/crappy/internal/mcp"
	"github.com/vitaliipsl/crappy/internal/memory"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/prompt"
	"github.com/vitaliipsl/crappy/internal/schedule"
	"github.com/vitaliipsl/crappy/internal/service"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/tools"
	"github.com/vitaliipsl/crappy/internal/tools/local"
)

type Runtime struct {
	service   *service.Local
	harness   *harness.Harness
	scheduler *schedule.Worker
	jobs      *background.Worker
	mcp       *mcp.Worker
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

	schedulesManager, err := schedule.NewManager(ctx, schedule.NewJSONStore(filepath.Join(settings.SessionsPath, "schedules.json")))
	if err != nil {
		return nil, fmt.Errorf("open schedules: %w", err)
	}

	jobsManager := background.NewManager(settings.Tools.MaxBackgroundTimeout)
	mcpManager := mcp.NewManager(settings.MCP)

	agents := agent.New(configuration.AgentConfig(), resolver,
		agent.WithToolMiddleware(
			tools.OutputCap(settings.Tools, storage.PutBlob),
			toolTimeout(settings.Tools),
		),
		agent.WithContributors(
			instructions.NewContributor(),
			local.NewContributor(jobsManager),
			memory.NewContributor(memories),
			schedule.NewContributor(schedulesManager),
			background.NewContributor(jobsManager),
			prompt.NewContributor(),
			mcp.NewContributor(mcpManager),
			automaticCompaction,
		),
	)

	runtime := harness.New(storage,
		harness.WithExecutor(agent.ExecutorName, agent.NewExecutor(agents)),
		harness.WithExecutor(compactionexec.ExecutorName, compactionexec.New(compactor)),
	)

	sheduleWorker := schedule.NewWorker(schedulesManager, schedule.NewExecutionDispatcher(runtime), settings.Scheduling.PollInterval)
	sheduleWorker.Start()

	jobWorker := background.NewWorker(jobsManager, background.NewExecutionDispatcher(runtime))
	jobWorker.Start()

	mcpWorker := mcp.NewWorker(mcpManager, settings.MCP.PollInterval)
	mcpWorker.Start()

	return &Runtime{
		service: service.NewLocal(
			configuration,
			runtime,
			storage,
			agents,
			estimator,
			schedulesManager,
		),
		harness:   runtime,
		scheduler: sheduleWorker,
		jobs:      jobWorker,
		mcp:       mcpWorker,
	}, nil
}

func (r *Runtime) Service() *service.Local {
	return r.service
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	schedulerErr := r.scheduler.Shutdown(ctx)
	jobsErr := r.jobs.Shutdown(ctx)
	harnessErr := r.harness.Shutdown(ctx)
	mcpErr := r.mcp.Shutdown(ctx)

	return errors.Join(schedulerErr, jobsErr, harnessErr, mcpErr)
}
