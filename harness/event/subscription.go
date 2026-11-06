package event

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/vitaliipsl/crappy/harness/entry"
)

var ErrSubscriptionLagged = errors.New("subscription lagged")

type Subscription interface {
	Events() <-chan Event
	Done() <-chan struct{}
	Err() error
	Close()
}

const subscriptionBuffer = 64

var _ Subscription = (*Stream)(nil)

type replay struct {
	afterID uint64
	events  []Event
}

type terminalState struct {
	err error
}

type Stream struct {
	events chan Event
	live   chan Event
	start  chan replay
	done   chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	started   atomic.Bool
	startOnce sync.Once
	terminal  atomic.Pointer[terminalState]
}

func NewSubscription(ctx context.Context) *Stream {
	subscriptionContext, cancel := context.WithCancel(ctx)
	subscription := &Stream{
		events: make(chan Event),
		live:   make(chan Event, subscriptionBuffer),
		start:  make(chan replay, 1),
		done:   make(chan struct{}),
		ctx:    subscriptionContext,
		cancel: cancel,
	}

	go subscription.run()

	return subscription
}

func (s *Stream) Start(afterID uint64, entries []entry.Entry) {
	s.startOnce.Do(func() {
		events := make([]Event, len(entries))
		for i := range entries {
			events[i] = FromEntry(entries[i])
		}

		select {
		case s.start <- replay{afterID: afterID, events: events}:
			s.started.Store(true)
		case <-s.ctx.Done():
		}
	})
}

func (s *Stream) Publish(event Event) bool {
	if s.ctx.Err() != nil {
		return false
	}

	if event.EntryID == 0 && !s.started.Load() {
		return true
	}

	select {
	case s.live <- cloneEvent(event):
		return true
	case <-s.ctx.Done():
		return false
	default:
		if event.EntryID == 0 {
			return true
		}

		s.stop(ErrSubscriptionLagged)

		return false
	}
}

func (s *Stream) Events() <-chan Event {
	return s.events
}

func (s *Stream) Done() <-chan struct{} {
	return s.done
}

func (s *Stream) Close() {
	s.stop(nil)
}

func (s *Stream) Err() error {
	terminal := s.terminal.Load()
	if terminal == nil {
		return nil
	}

	return terminal.err
}

func (s *Stream) run() {
	defer close(s.done)
	defer close(s.events)

	var initial replay
	select {
	case initial = <-s.start:
	case <-s.ctx.Done():
		return
	}

	lastEntryID := initial.afterID
	for _, event := range initial.events {
		if !s.send(event) {
			return
		}

		lastEntryID = event.EntryID
	}

	for {
		select {
		case event := <-s.live:
			if event.EntryID != 0 {
				if event.EntryID <= lastEntryID {
					continue
				}

				lastEntryID = event.EntryID
			}

			if !s.send(event) {
				return
			}
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Stream) send(event Event) bool {
	select {
	case s.events <- event:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *Stream) stop(err error) {
	if s.terminal.CompareAndSwap(nil, &terminalState{err: err}) {
		s.cancel()
	}
}

func cloneEvent(event Event) Event {
	event.Payload = append(json.RawMessage(nil), event.Payload...)

	return event
}
