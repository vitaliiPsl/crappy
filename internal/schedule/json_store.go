package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type JSONStore struct {
	path string
}

func NewJSONStore(path string) *JSONStore {
	return &JSONStore{path: path}
}

func (s *JSONStore) Load(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var records []Record
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("read schedules: %w", err)
	}

	return records, nil
}

func (s *JSONStore) Save(ctx context.Context, records []Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := json.Marshal(records)
	if err != nil {
		return err
	}

	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}

	file, err := os.CreateTemp(directory, ".schedules-*")
	if err != nil {
		return err
	}

	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()

	if _, err := file.Write(data); err != nil {
		return err
	}

	if err := file.Sync(); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.Rename(file.Name(), s.path); err != nil {
		return err
	}

	parent, err := os.Open(directory)
	if err != nil {
		return err
	}

	defer func() { _ = parent.Close() }()

	return parent.Sync()
}
