package background

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type job struct {
	result Result
	cancel context.CancelFunc
}

type Manager struct {
	maxTimeout  time.Duration
	ctx         context.Context
	cancel      context.CancelFunc
	completions chan Record
	jobs        map[string]*job
	mu          sync.Mutex
	running     sync.WaitGroup
}

func NewManager(maxTimeout time.Duration) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		maxTimeout:  maxTimeout,
		ctx:         ctx,
		cancel:      cancel,
		completions: make(chan Record),
		jobs:        make(map[string]*job),
	}
}

func (m *Manager) MaxTimeout() time.Duration {
	return m.maxTimeout
}

func (m *Manager) Start(
	ctx context.Context,
	sessionID, tool string,
	timeout time.Duration,
	execute func(context.Context) (kit.ToolOutput, error),
) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Record{}, err
	}

	if m.ctx.Err() != nil {
		return Record{}, ErrClosed
	}

	if timeout <= 0 || timeout > m.maxTimeout {
		return Record{}, fmt.Errorf("background timeout must be positive and at most %s", m.maxTimeout)
	}

	// Admission uses the caller's context; execution has an independent deadline
	// and survives cancellation of the agent that launched it.
	jobCtx, cancel := context.WithTimeout(m.ctx, timeout)
	record := Record{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Tool:      tool,
		State:     Running,
	}
	m.jobs[record.ID] = &job{result: Result{Record: record}, cancel: cancel}
	m.running.Go(func() {
		defer cancel()

		output, err := invoke(jobCtx, tool, execute)
		finished := m.finish(jobCtx, record.ID, output, err)

		select {
		case m.completions <- finished:
		case <-m.ctx.Done():
		}
	})

	return record, nil
}

func (m *Manager) Get(_ context.Context, sessionID, id string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.jobs[id]
	if !ok || current.result.SessionID != sessionID {
		return Result{}, ErrNotFound
	}

	return current.result, nil
}

func (m *Manager) List(_ context.Context, sessionID string) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	records := make([]Record, 0)
	for _, current := range m.jobs {
		if current.result.SessionID == sessionID {
			records = append(records, current.result.Record)
		}
	}

	slices.SortFunc(records, func(a, b Record) int {
		return strings.Compare(a.ID, b.ID)
	})

	return records, nil
}

func (m *Manager) Cancel(_ context.Context, sessionID, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.jobs[id]
	if !ok || current.result.SessionID != sessionID {
		return Record{}, ErrNotFound
	}

	current.cancel()

	return current.result.Record, nil
}

func (m *Manager) Completions() <-chan Record {
	return m.completions
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.cancel()
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		m.running.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) finish(ctx context.Context, id string, output kit.ToolOutput, err error) Record {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.jobs[id]
	current.result.State = Completed
	current.result.Output = output

	if ctx.Err() != nil {
		err = ctx.Err()
	}

	if err != nil {
		current.result.State = Failed
		current.result.Error = err.Error()

		if errors.Is(err, context.Canceled) {
			current.result.State = Cancelled
		}
	}

	return current.result.Record
}

func invoke(ctx context.Context, tool string, execute func(context.Context) (kit.ToolOutput, error)) (output kit.ToolOutput, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("tool %q panicked: %v", tool, recovered)
		}
	}()

	return execute(ctx)
}
