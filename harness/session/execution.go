package session

import (
	"errors"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

var (
	ErrBusy               = errors.New("session is busy")
	ErrClosed             = errors.New("session is closed")
	ErrExecutionNotActive = errors.New("execution is not active")
)

func (s *Session) Execute(input kit.Message) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return "", ErrClosed
	}

	if s.running() {
		return "", ErrBusy
	}

	if err := s.executor.Reconcile(s.ctx); err != nil {
		return "", fmt.Errorf("reconcile executions: %w", err)
	}

	active, err := s.executor.Start(s.ctx, input)
	if err != nil {
		return "", err
	}

	s.active = active

	return active.ID(), nil
}

func (s *Session) Cancel(executionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running() || s.active.ID() != executionID {
		return ErrExecutionNotActive
	}

	s.active.Cancel()

	return nil
}

func (s *Session) running() bool {
	if s.active == nil {
		return false
	}

	select {
	case <-s.active.Done():
		return false
	default:
		return true
	}
}
