package daemon

import (
	"os"
	"path/filepath"
	"sync"
)

type Lock struct {
	file *os.File
	once sync.Once
	err  error
}

func AcquireLock(path string) (*Lock, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}

	if err := os.Chmod(directory, 0700); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}

	if err := lockFile(file); err != nil {
		_ = file.Close()

		return nil, err
	}

	return &Lock{file: file}, nil
}

func (l *Lock) Close() error {
	l.once.Do(func() { l.err = l.file.Close() })

	return l.err
}
