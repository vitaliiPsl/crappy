package mcp

import (
	"context"
	"log"
	"sync"
	"time"
)

type Worker struct {
	manager      *Manager
	pollInterval time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	start  sync.Once
}

func NewWorker(manager *Manager, pollInterval time.Duration) *Worker {
	ctx, cancel := context.WithCancel(context.Background())

	return &Worker{
		manager:      manager,
		pollInterval: pollInterval,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
	}
}

func (w *Worker) Start() {
	w.start.Do(func() { go w.run() })
}

func (w *Worker) Shutdown(ctx context.Context) error {
	w.cancel()

	select {
	case <-w.done:
		return w.manager.Shutdown(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) run() {
	defer close(w.done)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		if err := w.manager.Refresh(w.ctx); err != nil && w.ctx.Err() == nil {
			log.Printf("mcp: %v", err)
		}

		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
