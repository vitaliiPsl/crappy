package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/vitaliipsl/crappy/harness/event"
)

type Subscription struct {
	events chan event.Event
	done   chan struct{}
	cancel context.CancelFunc
	body   io.ReadCloser
	err    atomic.Pointer[subscriptionError]
}

type subscriptionError struct {
	err error
}

func NewSubscription(ctx context.Context, body io.ReadCloser) *Subscription {
	ctx, cancel := context.WithCancel(ctx)

	s := &Subscription{
		events: make(chan event.Event),
		done:   make(chan struct{}),
		cancel: cancel,
		body:   body,
	}
	go s.read(ctx)

	return s
}

func (s *Subscription) Close() {
	s.cancel()
	_ = s.body.Close()
}

func (s *Subscription) Events() <-chan event.Event {
	return s.events
}

func (s *Subscription) Done() <-chan struct{} {
	return s.done
}

func (s *Subscription) Err() error {
	if value := s.err.Load(); value != nil {
		return value.err
	}

	return nil
}

func (s *Subscription) read(ctx context.Context) {
	defer close(s.done)
	defer close(s.events)
	defer s.Close()

	decoder := json.NewDecoder(s.body)
	for {
		var record streamRecord
		if err := decoder.Decode(&record); err != nil {
			if ctx.Err() == nil {
				s.err.Store(&subscriptionError{fmt.Errorf("assistant event stream disconnected: %w", err)})
			}

			return
		}

		if record.Error != nil {
			s.err.Store(&subscriptionError{record.Error})

			return
		}

		if record.Event == nil {
			s.err.Store(&subscriptionError{fmt.Errorf("assistant event stream omitted event")})

			return
		}

		select {
		case s.events <- *record.Event:
		case <-ctx.Done():
			return
		}
	}
}
