package jsonl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Storage) sessionDirectory(sessionID string) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}

	return filepath.Join(s.root, sessionID), nil
}

func (s *Storage) createSessionDirectory(sessionID string) (string, error) {
	path, err := s.sessionDirectory(sessionID)
	if err != nil {
		return "", err
	}

	err = os.Mkdir(path, 0o700)
	if err == nil {
		if err := syncDirectory(s.root); err != nil {
			return "", fmt.Errorf("sync sessions directory: %w", err)
		}

		return path, nil
	}

	if !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("create session directory: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat session directory: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("session path is not a directory")
	}

	if err := os.Chmod(path, 0o700); err != nil {
		return "", fmt.Errorf("protect session directory: %w", err)
	}

	return path, nil
}

func validateSessionID(sessionID string) error {
	if sessionID == "" {
		return errors.New("session ID is required")
	}

	if sessionID == "." || sessionID == ".." || strings.ContainsAny(sessionID, `/\`) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}

	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}

	syncErr := directory.Sync()
	closeErr := directory.Close()

	return errors.Join(syncErr, closeErr)
}
