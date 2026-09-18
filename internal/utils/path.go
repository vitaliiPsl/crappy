package utils

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	homeDirectoryToken  = "~"
	homeDirectoryPrefix = "~/"
)

func ExpandHome(path string) (string, error) {
	if path != homeDirectoryToken && !strings.HasPrefix(path, homeDirectoryPrefix) {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home directory: %w", err)
	}

	if path == homeDirectoryToken {
		return home, nil
	}

	return filepath.Join(home, strings.TrimPrefix(path, homeDirectoryPrefix)), nil
}

func ResolvePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}

	resolved, err := ExpandHome(path)
	if err != nil {
		return "", err
	}

	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("make path absolute: %w", err)
	}

	return filepath.Clean(resolved), nil
}

func ResolvePathFromFile(filePath, path string) (string, error) {
	resolved, err := ExpandHome(path)
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(filePath), resolved)
	}

	return filepath.Clean(resolved), nil
}
