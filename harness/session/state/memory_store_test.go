package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryStoreOwnsRecordValues(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	record := Record{ID: "session", Values: map[string]json.RawMessage{"value": json.RawMessage(`"original"`)}}
	if err := store.Save(ctx, record); err != nil {
		t.Fatal(err)
	}

	record.Values["value"][1] = 'x'
	record.Values["extra"] = json.RawMessage(`true`)

	loaded, err := store.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}

	if string(loaded.Values["value"]) != `"original"` || len(loaded.Values) != 1 {
		t.Fatalf("input mutation changed stored record: %+v", loaded)
	}

	loaded.Values["value"][1] = 'x'
	delete(loaded.Values, "value")

	listed, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(listed) != 1 || string(listed[0].Values["value"]) != `"original"` {
		t.Fatalf("Get mutation changed stored record: %+v", listed)
	}

	listed[0].Values["value"][1] = 'x'

	var retained *Record
	if err := store.Update(ctx, record.ID, func(value *Record) error {
		retained = value

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	retained.Values["value"][1] = 'x'

	loaded, err = store.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}

	if string(loaded.Values["value"]) != `"original"` {
		t.Fatalf("returned data mutated stored value: %+v", loaded)
	}
}

func TestMemoryStoreUpdatesRecordsAtomically(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	original := Record{
		ID:        "session",
		UpdatedAt: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC),
		Values:    map[string]json.RawMessage{"value": json.RawMessage(`"original"`)},
	}
	if err := store.Save(ctx, original); err != nil {
		t.Fatal(err)
	}

	updateErr := errors.New("update failed")
	if err := store.Update(ctx, original.ID, func(record *Record) error {
		record.UpdatedAt = record.UpdatedAt.Add(time.Hour)
		record.Values["value"][1] = 'x'

		return updateErr
	}); !errors.Is(err, updateErr) {
		t.Fatalf("failed update = %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := store.Update(canceled, original.ID, func(record *Record) error {
		cancel()

		record.UpdatedAt = record.UpdatedAt.Add(time.Hour)
		delete(record.Values, "value")

		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled update = %v", err)
	}

	loaded, err := store.Get(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !loaded.UpdatedAt.Equal(original.UpdatedAt) || string(loaded.Values["value"]) != `"original"` {
		t.Fatalf("failed update changed record: %+v", loaded)
	}

	if err := store.Update(ctx, "missing", func(*Record) error {
		t.Error("update ran for a missing session")

		return nil
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update = %v", err)
	}
}

func TestMemoryStoreConcurrentUpdatesAndIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	for _, id := range []string{"first", "second"} {
		if err := store.Save(ctx, Record{ID: id}); err != nil {
			t.Fatal(err)
		}
	}

	var updates sync.WaitGroup
	for index := range 24 {
		updates.Go(func() {
			key := fmt.Sprintf("key-%d", index)
			if err := store.Update(ctx, "first", func(record *Record) error {
				if record.Values == nil {
					record.Values = make(map[string]json.RawMessage)
				}

				record.Values[key] = json.RawMessage(fmt.Sprintf("%d", index))

				return nil
			}); err != nil {
				t.Errorf("update %s: %v", key, err)
			}
		})
	}

	updates.Wait()

	first, err := store.Get(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}

	for index := range 24 {
		key := fmt.Sprintf("key-%d", index)
		if got, want := string(first.Values[key]), fmt.Sprintf("%d", index); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}

	second, err := store.Get(ctx, "second")
	if err != nil {
		t.Fatal(err)
	}

	if len(second.Values) != 0 {
		t.Fatalf("values leaked to another session: %+v", second.Values)
	}
}
