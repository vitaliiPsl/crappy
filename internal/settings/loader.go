package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"

	"github.com/vitaliipsl/crappy/internal/utils"
)

var validatorInstance = validator.New(validator.WithRequiredStructEnabled())

func Load() (Settings, error) {
	path := os.Getenv(EnvPath)
	if path == "" {
		path = DefaultPath
	}

	return LoadFile(path)
}

func LoadFile(path string) (Settings, error) {
	path, err := utils.ResolvePath(path)
	if err != nil {
		return Settings{}, fmt.Errorf("resolve settings path: %w", err)
	}

	value, err := loadFile(path)
	if err == nil {
		return value, nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return Settings{}, err
	}

	data, err := yaml.Marshal(defaults())
	if err != nil {
		return Settings{}, fmt.Errorf("encode default settings: %w", err)
	}

	if err := utils.SeedFile(path, data); err != nil {
		return Settings{}, fmt.Errorf("seed settings: %w", err)
	}

	return loadFile(path)
}

func loadFile(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}

	value := defaults()
	if err := decode(data, &value); err != nil {
		return Settings{}, fmt.Errorf("decode settings: %w", err)
	}

	if err := validatorInstance.Struct(value); err != nil {
		return Settings{}, fmt.Errorf("validate settings: %w", err)
	}

	value.ConfigPath, err = utils.ResolvePathFromFile(path, value.ConfigPath)
	if err != nil {
		return Settings{}, fmt.Errorf("resolve config path: %w", err)
	}

	return value, nil
}

func decode(data []byte, value any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err := decoder.Decode(value); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple YAML documents are not supported")
		}

		return err
	}

	return nil
}
