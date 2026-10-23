package execution

import (
	"fmt"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	executors map[string]Executor
}

func NewRegistry() *Registry {
	return &Registry{executors: make(map[string]Executor)}
}

func (r *Registry) Register(name string, executor Executor) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.executors[name] = executor
}

func (r *Registry) Resolve(name string) (Executor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	executor, ok := r.executors[name]
	if !ok {
		return nil, fmt.Errorf("unknown executor %q", name)
	}

	return executor, nil
}
