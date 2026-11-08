package memory

import (
	"bytes"
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

type EntryOption func(*entryOptions) error

type entryOptions struct {
	source   *string
	metadata map[string]json.RawMessage
}

func WithSource(reference string) EntryOption {
	return func(options *entryOptions) error {
		options.source = &reference

		return nil
	}
}

func WithMetadata(key string, value any) EntryOption {
	return func(options *entryOptions) error {
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode metadata %q: %w", key, err)
		}

		if options.metadata == nil {
			options.metadata = make(map[string]json.RawMessage)
		}

		options.metadata[key] = data

		return nil
	}
}

func WithoutMetadata(key string) EntryOption {
	return func(options *entryOptions) error {
		if options.metadata == nil {
			options.metadata = make(map[string]json.RawMessage)
		}

		options.metadata[key] = nil

		return nil
	}
}

func applyEntryOptions(entry Entry, options []EntryOption) (Entry, error) {
	var configured entryOptions
	for _, option := range options {
		if err := option(&configured); err != nil {
			return Entry{}, err
		}
	}

	entry = copyEntry(entry)
	if configured.source != nil {
		entry.Source = *configured.source
	}

	for key, value := range configured.metadata {
		if value == nil {
			delete(entry.Metadata, key)

			continue
		}

		if entry.Metadata == nil {
			entry.Metadata = make(map[string]json.RawMessage)
		}

		entry.Metadata[key] = value
	}

	if len(entry.Metadata) == 0 {
		entry.Metadata = nil
	}

	return entry, nil
}

func copyEntry(entry Entry) Entry {
	if entry.Metadata == nil {
		return entry
	}

	metadata := make(map[string]json.RawMessage, len(entry.Metadata))
	for key, value := range entry.Metadata {
		metadata[key] = bytes.Clone(value)
	}

	entry.Metadata = metadata

	return entry
}
