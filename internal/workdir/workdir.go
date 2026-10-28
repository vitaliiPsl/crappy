package workdir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vitaliipsl/crappy/harness/session/state"
)

const StateKey = "assistant.workdir"

func Resolve(ctx context.Context, sessionState state.State) (string, error) {
	var directory string

	found, err := sessionState.Get(ctx, StateKey, &directory)
	if err != nil {
		return "", fmt.Errorf("read session workdir: %w", err)
	}

	if !found {
		directory, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get process workdir: %w", err)
		}
	}

	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("session workdir must be an absolute path: %q", directory)
	}

	directory = filepath.Clean(directory)

	info, err := os.Stat(directory)
	if err != nil {
		return "", fmt.Errorf("stat session workdir: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("session workdir is not a directory: %q", directory)
	}

	return directory, nil
}
