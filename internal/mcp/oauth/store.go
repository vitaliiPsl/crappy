package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
)

type Credential struct {
	URL    string        `json:"url"`
	Config oauth2.Config `json:"config"`
	Token  oauth2.Token  `json:"token"`
}

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load(server string) (*Credential, error) {
	data, err := os.ReadFile(s.file(server))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	var credential Credential
	if err := json.Unmarshal(data, &credential); err != nil {
		return nil, fmt.Errorf("decode credentials: %w", err)
	}

	return &credential, nil
}

func (s *Store) Save(server string, credential Credential) error {
	data, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}

	if err := os.MkdirAll(s.path, 0o700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}

	file, err := os.CreateTemp(s.path, "."+server+"-*")
	if err != nil {
		return fmt.Errorf("create credentials file: %w", err)
	}

	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	if err := os.Rename(file.Name(), s.file(server)); err != nil {
		return fmt.Errorf("replace credentials: %w", err)
	}

	return nil
}

func (s *Store) file(server string) string {
	return filepath.Join(s.path, server+".json")
}
