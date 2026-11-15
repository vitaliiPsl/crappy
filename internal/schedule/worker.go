package schedule

import (
	"context"
	"log"
	"sync"
	"time"
)

type Worker struct {
	manager      *Manager
	dispatcher   Dispatcher
	pollInterval time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	start  sync.Once
}

func NewWorker(manager *Manager, dispatcher Dispatcher, pollInterval time.Duration) *Worker {
	ctx, cancel := context.WithCancel(context.Background())

	return &Worker{
		manager:      manager,
		dispatcher:   dispatcher,
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
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) run() {
	defer close(w.done)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		if err := w.manager.DispatchDue(w.ctx, time.Now(), w.dispatcher); err != nil && w.ctx.Err() == nil {
			log.Printf("scheduling: %v", err)
		}

		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
