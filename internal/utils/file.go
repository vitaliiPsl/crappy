package utils

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func SeedFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	created, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()

	if _, err := created.Write(data); err != nil {
		_ = created.Close()

		return fmt.Errorf("write file: %w", err)
	}

	if err := created.Sync(); err != nil {
		_ = created.Close()

		return fmt.Errorf("sync file: %w", err)
	}

	if err := created.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}

	complete = true

	return nil
}
