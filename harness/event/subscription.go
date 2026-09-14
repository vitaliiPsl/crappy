package event

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/vitaliipsl/crappy/harness/entry"
)

const subscriptionBuffer = 64

var ErrSubscriptionLagged = errors.New("subscription lagged")

type replay struct {
	afterID uint64
	events  []Event
}

type terminalState struct {
	err error
}

type Subscription struct {
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

func NewSubscription(ctx context.Context) *Subscription {
	subscriptionContext, cancel := context.WithCancel(ctx)
	subscription := &Subscription{
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

func (s *Subscription) Start(afterID uint64, entries []entry.Entry) {
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

func (s *Subscription) Publish(event Event) bool {
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

func (s *Subscription) Events() <-chan Event {
	return s.events
}

func (s *Subscription) Done() <-chan struct{} {
	return s.done
}

func (s *Subscription) Close() {
	s.stop(nil)
}

func (s *Subscription) Err() error {
	terminal := s.terminal.Load()
	if terminal == nil {
		return nil
	}

	return terminal.err
}

func (s *Subscription) run() {
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

func (s *Subscription) send(event Event) bool {
	select {
	case s.events <- event:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *Subscription) stop(err error) {
	if s.terminal.CompareAndSwap(nil, &terminalState{err: err}) {
		s.cancel()
	}
}

func cloneEvent(event Event) Event {
	event.Payload = append(json.RawMessage(nil), event.Payload...)

	return event
}
