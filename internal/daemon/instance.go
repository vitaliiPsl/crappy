package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

var ErrRunning = errors.New("assistant is already running")

type Instance struct {
	directory string
	lock      *os.File
	once      sync.Once
	err       error
}

func Acquire(directory string) (*Instance, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}

	if err := os.Chmod(directory, 0700); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(filepath.Join(directory, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}

	if err := lockFile(file); err != nil {
		_ = file.Close()

		return nil, err
	}

	return &Instance{directory: directory, lock: file}, nil
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
	i.once.Do(func() { i.err = i.lock.Close() })

	return i.err
}
