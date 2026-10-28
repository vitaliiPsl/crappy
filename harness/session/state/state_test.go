package state

import (
	"context"
	"errors"
	"testing"
)

func TestStateValuesAreIndependent(t *testing.T) {
	ctx := context.Background()
	state := newTestState(t)

	input := map[string][]string{"paths": {"original"}}
	if err := state.Set(ctx, "context", input); err != nil {
		t.Fatal(err)
	}

	input["paths"][0] = "mutated input"

	var output map[string][]string
	if found, err := state.Get(ctx, "context", &output); err != nil || !found || output["paths"][0] != "original" {
		t.Fatalf("value = %v, found = %v, error = %v", output, found, err)
	}

	output["paths"][0] = "mutated output"
	if found, err := state.Get(ctx, "context", &output); err != nil || !found || output["paths"][0] != "original" {
		t.Fatalf("value = %v, found = %v, error = %v", output, found, err)
	}

	if err := state.Delete(ctx, "context"); err != nil {
		t.Fatal(err)
	}

	if found, err := state.Get(ctx, "context", &output); err != nil || found {
		t.Fatalf("deleted value: found = %v, error = %v", found, err)
	}
}

func TestStateRejectsInvalidOperationsWithoutChangingValues(t *testing.T) {
	ctx := context.Background()

	accessor := newTestState(t)
	if err := accessor.Set(ctx, "value", "original"); err != nil {
		t.Fatal(err)
	}

	if err := accessor.Set(ctx, "value", make(chan int)); err == nil {
		t.Fatal("unsupported value was accepted")
	}

	var number int
	if found, err := accessor.Get(ctx, "value", &number); err == nil || !found {
		t.Fatalf("invalid target: found = %v, error = %v", found, err)
	}

	var value string
	if found, err := accessor.Get(ctx, "value", &value); err != nil || !found || value != "original" {
		t.Fatalf("value = %q, found = %v, error = %v", value, found, err)
	}
}

func TestStateRejectsCanceledOperations(t *testing.T) {
	ctx := context.Background()

	accessor := newTestState(t)
	if err := accessor.Set(ctx, "value", "original"); err != nil {
		t.Fatal(err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	if err := accessor.Set(canceled, "value", "changed"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Set = %v", err)
	}

	if err := accessor.Delete(canceled, "value"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Delete = %v", err)
	}

	var value string
	if _, err := accessor.Get(canceled, "value", &value); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Get = %v", err)
	}

	if found, err := accessor.Get(ctx, "value", &value); err != nil || !found || value != "original" {
		t.Fatalf("value = %q, found = %v, error = %v", value, found, err)
	}
}

func TestStateDistinguishesNullFromMissing(t *testing.T) {
	ctx := context.Background()
	accessor := newTestState(t)

	value := "unchanged"
	if found, err := accessor.Get(ctx, "missing", &value); err != nil || found || value != "unchanged" {
		t.Fatalf("missing value = %q, found = %v, error = %v", value, found, err)
	}

	if err := accessor.Set(ctx, "nullable", nil); err != nil {
		t.Fatal(err)
	}

	nullable := new(string)
	if found, err := accessor.Get(ctx, "nullable", &nullable); err != nil || !found || nullable != nil {
		t.Fatalf("null value = %v, found = %v, error = %v", nullable, found, err)
	}
}

func TestStateRejectsOperationsAfterSessionDeletion(t *testing.T) {
	ctx := context.Background()

	store := NewMemoryStore()
	if err := store.Save(ctx, Record{ID: "session"}); err != nil {
		t.Fatal(err)
	}

	accessor := New(store, "session")

	if err := store.Delete(ctx, "session"); err != nil {
		t.Fatal(err)
	}

	if err := accessor.Set(ctx, "value", "resurrected"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Set after session deletion = %v", err)
	}

	var value string
	if _, err := accessor.Get(ctx, "value", &value); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after session deletion = %v", err)
	}

	if err := accessor.Delete(ctx, "value"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete after session deletion = %v", err)
	}
}

func newTestState(t *testing.T) State {
	t.Helper()

	store := NewMemoryStore()
	if err := store.Save(context.Background(), Record{ID: "session"}); err != nil {
		t.Fatal(err)
	}

	return New(store, "session")
}
