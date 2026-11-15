package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

var ErrRunning = errors.New("assistant is already running")

type Instance struct {
	directory string
	lock      *Lock
}

func Acquire(directory string) (*Instance, error) {
	lock, err := AcquireLock(filepath.Join(directory, "lock"))
	if err != nil {
		return nil, err
	}

	return &Instance{directory: directory, lock: lock}, nil
}

func (i *Instance) Listen() (net.Listener, error) {
	path := filepath.Join(i.directory, "socket")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", path, err)
	}

	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()

		return nil, err
	}

	return listener, nil
}

func (i *Instance) Close() error {
	return i.lock.Close()
}
