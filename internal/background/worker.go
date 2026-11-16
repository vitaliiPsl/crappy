package background

import (
	"context"
	"errors"
	"log"
	"sync"
)

type Worker struct {
	manager    *Manager
	dispatcher Dispatcher

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	start  sync.Once
}

func NewWorker(manager *Manager, dispatcher Dispatcher) *Worker {
	ctx, cancel := context.WithCancel(context.Background())

	return &Worker{
		manager:    manager,
		dispatcher: dispatcher,
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
	}
}

func (w *Worker) Start() {
	w.start.Do(func() { go w.run() })
}

func (w *Worker) Shutdown(ctx context.Context) error {
	w.cancel()
	managerErr := w.manager.Shutdown(ctx)

	select {
	case <-w.done:
		return managerErr
	case <-ctx.Done():
		return errors.Join(managerErr, ctx.Err())
	}
}

func (w *Worker) run() {
	defer close(w.done)

	for {
		select {
		case <-w.ctx.Done():
			return
		case record := <-w.manager.Completions():
			if err := w.dispatcher.Dispatch(w.ctx, record); err != nil && w.ctx.Err() == nil {
				log.Printf("background job %s: %v", record.ID, err)
			}
		}
	}
}
