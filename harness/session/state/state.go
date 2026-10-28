package state

import (
	"context"
	"encoding/json"
	"fmt"
)

type State interface {
	Get(ctx context.Context, key string, target any) (bool, error)
	Set(ctx context.Context, key string, value any) error
	Delete(ctx context.Context, key string) error
}

var _ State = (*Accessor)(nil)

type Accessor struct {
	store     Store
	sessionID string
}

func New(store Store, sessionID string) *Accessor {
	return &Accessor{store: store, sessionID: sessionID}
}

func (s *Accessor) Get(ctx context.Context, key string, target any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	record, err := s.store.Get(ctx, s.sessionID)
	if err != nil {
		return false, err
	}

	value, found := record.Values[key]
	if !found {
		return false, nil
	}

	if err := json.Unmarshal(value, target); err != nil {
		return true, fmt.Errorf("decode session value %q: %w", key, err)
	}

	return true, nil
}

func (s *Accessor) Set(ctx context.Context, key string, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode session value %q: %w", key, err)
	}

	return s.store.Update(ctx, s.sessionID, func(record *Record) error {
		if record.Values == nil {
			record.Values = make(map[string]json.RawMessage)
		}

		record.Values[key] = encoded

		return nil
	})
}

func (s *Accessor) Delete(ctx context.Context, key string) error {
	return s.store.Update(ctx, s.sessionID, func(record *Record) error {
		delete(record.Values, key)

		return nil
	})
}
