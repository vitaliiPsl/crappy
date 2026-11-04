package jsonl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vitaliipsl/crappy/harness/session/state"
)

const blobsDirectory = "blobs"

func (s *Storage) PutBlob(ctx context.Context, sessionID string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	current, err := s.state(sessionID)
	if err != nil {
		return "", err
	}

	current.mu.Lock()
	defer current.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return "", err
	}

	sessionDirectory := filepath.Join(s.root, sessionID)

	info, err := os.Stat(sessionDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return "", state.ErrNotFound
	}

	if err != nil {
		return "", fmt.Errorf("stat session directory: %w", err)
	}

	if !info.IsDir() {
		return "", errors.New("session path is not a directory")
	}

	directory := filepath.Join(sessionDirectory, blobsDirectory)
	if err := os.Mkdir(directory, 0o700); err == nil {
		if err := syncDirectory(sessionDirectory); err != nil {
			return "", fmt.Errorf("sync session directory: %w", err)
		}
	} else if !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("create blobs directory: %w", err)
	}

	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("protect blobs directory: %w", err)
	}

	sum := sha256.Sum256(data)

	name := hex.EncodeToString(sum[:])
	if err := writeFileAtomic(directory, name, data); err != nil {
		return "", fmt.Errorf("write blob: %w", err)
	}

	return filepath.Join(directory, name), nil
}
