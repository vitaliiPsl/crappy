package schedule

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/vitaliipsl/crappy/harness/session/state"
)

type Manager struct {
	mu      sync.Mutex
	store   Store
	records map[string]Record
}

func NewManager(ctx context.Context, store Store) (*Manager, error) {
	records, err := store.Load(ctx)
	if err != nil {
		return nil, err
	}

	manager := &Manager{
		store:   store,
		records: make(map[string]Record),
	}
	for _, record := range records {
		manager.records[record.ID] = record
	}

	return manager, nil
}

func (m *Manager) Create(ctx context.Context, sessionID, instruction string, at time.Time) (Record, error) {
	if sessionID == "" || strings.TrimSpace(instruction) == "" || at.IsZero() {
		return Record{}, fmt.Errorf("%w: session, instruction, and time are required", ErrInvalid)
	}

	return m.create(ctx, Record{SessionID: sessionID, Instruction: instruction, At: at.UTC()})
}

func (m *Manager) CreateRecurring(ctx context.Context, sessionID, instruction, spec string) (Record, error) {
	if sessionID == "" || strings.TrimSpace(instruction) == "" || strings.TrimSpace(spec) == "" {
		return Record{}, fmt.Errorf("%w: session, instruction, and cron are required", ErrInvalid)
	}

	at, err := nextOccurrence(spec, time.Now())
	if err != nil {
		return Record{}, err
	}

	return m.create(ctx, Record{SessionID: sessionID, Instruction: instruction, At: at, Cron: spec})
}

func (m *Manager) create(ctx context.Context, record Record) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record.ID = uuid.NewString()
	record.State = Scheduled

	if err := m.save(ctx, record); err != nil {
		return Record{}, err
	}

	return record, nil
}

func (m *Manager) Get(_ context.Context, sessionID, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, ok := m.records[id]
	if !ok || record.SessionID != sessionID {
		return Record{}, ErrNotFound
	}

	return record, nil
}

func (m *Manager) List(_ context.Context, sessionID string) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.list(sessionID), nil
}

func (m *Manager) Cancel(ctx context.Context, sessionID, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, ok := m.records[id]
	if !ok || record.SessionID != sessionID {
		return Record{}, ErrNotFound
	}

	if record.State == Cancelled {
		return record, nil
	}

	if record.State != Scheduled {
		return record, ErrNotPending
	}

	record.State = Cancelled
	if err := m.save(ctx, record); err != nil {
		return Record{}, err
	}

	return record, nil
}

func (m *Manager) DispatchDue(ctx context.Context, now time.Time, dispatcher Dispatcher) error {
	records, err := m.List(ctx, "")
	if err != nil {
		return err
	}

	var failures []error
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := m.dispatch(ctx, record.ID, now, dispatcher)
		if err != nil {
			failures = append(failures, fmt.Errorf("schedule %s: %w", record.ID, err))
		}
	}

	return errors.Join(failures...)
}

func (m *Manager) dispatch(ctx context.Context, id string, now time.Time, dispatcher Dispatcher) error {
	record, claimed, err := m.claim(ctx, id, now)
	if err != nil || !claimed {
		return err
	}

	executionID, err := dispatcher.Dispatch(ctx, record)
	if errors.Is(err, state.ErrNotFound) {
		return errors.Join(err, m.finish(ctx, record, Failed))
	}

	if err != nil {
		return err
	}

	record.ExecutionID = executionID
	if record.Cron == "" {
		return m.finish(ctx, record, Dispatched)
	}

	next, err := nextOccurrence(record.Cron, now)
	if err != nil {
		return errors.Join(err, m.finish(ctx, record, Failed))
	}

	record.At = next

	return m.finish(ctx, record, Scheduled)
}

func (m *Manager) claim(ctx context.Context, id string, now time.Time) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, ok := m.records[id]
	if !ok || (record.State != Scheduled && record.State != Dispatching) {
		return Record{}, false, nil
	}

	if record.State == Scheduled {
		if record.At.After(now) {
			return Record{}, false, nil
		}

		record.State = Dispatching
		if err := m.save(ctx, record); err != nil {
			return Record{}, false, err
		}
	}

	return record, true, nil
}

func (m *Manager) finish(ctx context.Context, record Record, state State) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	record.State = state

	return m.save(ctx, record)
}

func (m *Manager) save(ctx context.Context, record Record) error {
	records := make([]Record, 0, len(m.records)+1)
	for _, existing := range m.records {
		if existing.ID != record.ID {
			records = append(records, existing)
		}
	}

	records = append(records, record)

	if err := m.store.Save(ctx, records); err != nil {
		return err
	}

	m.records[record.ID] = record

	return nil
}

func nextOccurrence(spec string, after time.Time) (time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	schedule, err := parser.Parse(spec)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: cron %q: %w", ErrInvalid, spec, err)
	}

	next := schedule.Next(after.Local())
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("%w: cron %q has no upcoming occurrence", ErrInvalid, spec)
	}

	return next.UTC(), nil
}

func (m *Manager) list(sessionID string) []Record {
	records := make([]Record, 0, len(m.records))
	for _, record := range m.records {
		if sessionID == "" || record.SessionID == sessionID {
			records = append(records, record)
		}
	}

	slices.SortFunc(records, func(a, b Record) int {
		if order := a.At.Compare(b.At); order != 0 {
			return order
		}

		return strings.Compare(a.ID, b.ID)
	})

	return records
}
