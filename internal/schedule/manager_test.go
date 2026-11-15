package schedule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/session/state"
)

func TestManagerDoesNotConfirmFailedWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := &failingStore{Store: NewJSONStore(filepath.Join(t.TempDir(), "schedules.json")), err: errors.New("disk full")}

	manager, err := NewManager(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Create(ctx, "session", "work", time.Now()); !errors.Is(err, store.err) {
		t.Fatalf("create = %v", err)
	}

	if records, err := manager.List(ctx, "session"); err != nil || len(records) != 0 {
		t.Fatalf("failed write changed records: %+v, %v", records, err)
	}

	store.err = nil

	record, err := manager.Create(ctx, "session", "work", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	store.err = errors.New("disk full")
	if _, err := manager.Cancel(ctx, "session", record.ID); !errors.Is(err, store.err) {
		t.Fatalf("cancel = %v", err)
	}

	if record, err := manager.Get(ctx, "session", record.ID); err != nil || record.State != Scheduled {
		t.Fatalf("failed cancellation changed state: %+v, %v", record, err)
	}
}

func TestManagerRejectsCorruptedStore(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "schedules.json")
	if err := os.WriteFile(path, []byte(`broken`), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewManager(t.Context(), NewJSONStore(path)); err == nil {
		t.Fatal("accepted corrupt store")
	}
}

type failingStore struct {
	Store
	err error
}

func (s *failingStore) Save(ctx context.Context, records []Record) error {
	if s.err != nil {
		return s.err
	}

	return s.Store.Save(ctx, records)
}

func TestManagerRetriesUnrecordedDispatchAfterRestart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "schedules.json")
	store := &dispatchFailingStore{Store: NewJSONStore(path)}

	manager, err := NewManager(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	record, err := manager.Create(ctx, "session", "write the report", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	dispatcher := dispatchFunc(func(context.Context, Record) error {
		calls++

		return nil
	})

	if err := manager.DispatchDue(ctx, time.Now(), dispatcher); !errors.Is(err, errDispatchWrite) {
		t.Fatalf("dispatch persistence failure = %v", err)
	}

	manager, err = NewManager(ctx, NewJSONStore(path))
	if err != nil {
		t.Fatal(err)
	}

	if restored, err := manager.Get(ctx, "session", record.ID); err != nil || restored.State != Dispatching {
		t.Fatalf("crash recovery state = %+v, %v", restored, err)
	}

	if err := manager.DispatchDue(ctx, time.Now(), dispatcher); err != nil {
		t.Fatal(err)
	}

	if record, err := manager.Get(ctx, "session", record.ID); err != nil || record.State != Dispatched || calls != 2 {
		t.Fatalf("recovered dispatch = %+v, %v; attempts = %d", record, err, calls)
	}
}

func TestManagerCancellationAndDueTimes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	manager, err := NewManager(ctx, NewJSONStore(filepath.Join(t.TempDir(), "schedules.json")))
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	for _, instruction := range []string{"future", "cancelled", "due", "missing"} {
		at := now
		if instruction == "future" {
			at = now.Add(time.Hour)
		}

		record, err := manager.Create(ctx, "session", instruction, at)
		if err != nil {
			t.Fatal(err)
		}

		if instruction == "cancelled" {
			if _, err := manager.Cancel(ctx, "session", record.ID); err != nil {
				t.Fatal(err)
			}
		}
	}

	calls := make(map[string]int)

	dispatcher := dispatchFunc(func(ctx context.Context, record Record) error {
		calls[record.Instruction]++
		if _, err := manager.Cancel(ctx, record.SessionID, record.ID); !errors.Is(err, ErrNotPending) {
			t.Errorf("cancellation after claim = %v", err)
		}

		if record.Instruction == "missing" {
			return state.ErrNotFound
		}

		return nil
	})
	if err := manager.DispatchDue(ctx, now, dispatcher); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing target = %v", err)
	}

	if err := manager.DispatchDue(ctx, now, dispatcher); err != nil {
		t.Fatal(err)
	}

	if calls["due"] != 1 || calls["missing"] != 1 || len(calls) != 2 {
		t.Fatalf("dispatched = %v", calls)
	}

	records, err := manager.List(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]State{"future": Scheduled, "cancelled": Cancelled, "due": Dispatched, "missing": Failed}
	for _, record := range records {
		if record.State != want[record.Instruction] {
			t.Errorf("%s state = %s", record.Instruction, record.State)
		}
	}
}

var errDispatchWrite = errors.New("dispatch state write failed")

type dispatchFailingStore struct {
	Store
}

func (s *dispatchFailingStore) Save(ctx context.Context, records []Record) error {
	for _, record := range records {
		if record.State == Dispatched {
			return errDispatchWrite
		}
	}

	return s.Store.Save(ctx, records)
}

type dispatchFunc func(context.Context, Record) error

func (f dispatchFunc) Dispatch(ctx context.Context, record Record) error {
	return f(ctx, record)
}
