package memory

import (
	"encoding/json"
	"fmt"
	"time"
)

type Entry struct {
	ID        string                     `json:"id"`
	Scope     string                     `json:"scope"`
	Content   string                     `json:"content"`
	Source    string                     `json:"source,omitempty"`
	Metadata  map[string]json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time                  `json:"created_at"`
	UpdatedAt time.Time                  `json:"updated_at"`
	Version   uint64                     `json:"version"`
}

type EntryOption func(*Entry) error

func WithSource(reference string) EntryOption {
	return func(entry *Entry) error {
		entry.Source = reference

		return nil
	}
}

func WithMetadata(key string, value any) EntryOption {
	return func(entry *Entry) error {
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode metadata %q: %w", key, err)
		}

		if entry.Metadata == nil {
			entry.Metadata = make(map[string]json.RawMessage)
		}

		entry.Metadata[key] = data

		return nil
	}
}

func WithoutMetadata(key string) EntryOption {
	return func(entry *Entry) error {
		delete(entry.Metadata, key)

		return nil
	}
}

func applyEntryOptions(entry *Entry, options []EntryOption) error {
	for _, option := range options {
		if err := option(entry); err != nil {
			return err
		}
	}

	return nil
}
