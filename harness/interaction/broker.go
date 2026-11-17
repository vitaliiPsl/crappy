package interaction

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/vitaliipsl/crappy/harness/event"
)

const (
	RequestedEntryType = "interaction.requested"
	AnsweredEntryType  = "interaction.answered"
	CancelledEntryType = "interaction.cancelled"
)

var (
	ErrClosed   = errors.New("interactions are closed")
	ErrNotFound = errors.New("interaction is not pending")
)

type Broker interface {
	Prompter
	Pending(ctx context.Context) ([]Request, error)
	Respond(ctx context.Context, requestID string, answer Answer) error
	Close()
}

type pending struct {
	request Request
	answer  chan Answer
}

type Exchange struct {
	ctx  context.Context
	stop context.CancelFunc

	sessionID string
	events    event.Publisher

	mu      sync.Mutex
	pending map[string]*pending
	waiting sync.WaitGroup
}

func NewBroker(ctx context.Context, sessionID string, events event.Publisher) *Exchange {
	ctx, stop := context.WithCancel(ctx)

	return &Exchange{
		ctx:       ctx,
		stop:      stop,
		sessionID: sessionID,
		events:    events,
		pending:   make(map[string]*pending),
	}
}

func (b *Exchange) Ask(ctx context.Context, executionID string, question Question) (Answer, error) {
	current, err := b.register(ctx, executionID, question)
	if err != nil {
		return Answer{}, err
	}
	defer b.waiting.Done()

	select {
	case answer := <-current.answer:
		return answer, nil
	case <-ctx.Done():
		return b.cancel(ctx, current, ctx.Err())
	case <-b.ctx.Done():
		return b.cancel(ctx, current, ErrClosed)
	}
}

func (b *Exchange) Pending(ctx context.Context) ([]Request, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if b.ctx.Err() != nil {
		return nil, ErrClosed
	}

	requests := make([]Request, 0, len(b.pending))
	for _, current := range b.pending {
		request := current.request
		request.Question.Choices = slices.Clone(request.Question.Choices)
		requests = append(requests, request)
	}

	slices.SortFunc(requests, func(a, b Request) int {
		return strings.Compare(a.ID, b.ID)
	})

	return requests, nil
}

func (b *Exchange) Respond(ctx context.Context, requestID string, answer Answer) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if b.ctx.Err() != nil {
		return ErrClosed
	}

	current, ok := b.pending[requestID]
	if !ok {
		return ErrNotFound
	}

	response := Response{
		RequestID: requestID,
		Answer:    answer,
	}
	if err := b.events.Commit(ctx, current.request.ExecutionID, AnsweredEntryType, response); err != nil {
		return err
	}

	delete(b.pending, requestID)

	current.answer <- answer

	return nil
}

func (b *Exchange) Close() {
	b.mu.Lock()
	b.stop()
	b.mu.Unlock()

	b.waiting.Wait()
}

func (b *Exchange) register(ctx context.Context, executionID string, question Question) (*pending, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if b.ctx.Err() != nil {
		return nil, ErrClosed
	}

	question.Choices = slices.Clone(question.Choices)

	request := Request{
		ID:          uuid.NewString(),
		SessionID:   b.sessionID,
		ExecutionID: executionID,
		Question:    question,
	}
	if err := b.events.Commit(ctx, executionID, RequestedEntryType, request); err != nil {
		return nil, err
	}

	current := &pending{
		request: request,
		answer:  make(chan Answer, 1),
	}
	b.pending[request.ID] = current
	b.waiting.Add(1)

	return current, nil
}

func (b *Exchange) cancel(ctx context.Context, current *pending, reason error) (Answer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// A committed answer wins if cancellation and delivery happen together.
	if _, ok := b.pending[current.request.ID]; !ok {
		return <-current.answer, nil
	}

	err := b.events.Commit(context.WithoutCancel(ctx), current.request.ExecutionID, CancelledEntryType, current.request.ID)
	delete(b.pending, current.request.ID)

	return Answer{}, errors.Join(reason, err)
}
