package config

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

func LoadFile(path string) (Config, error) {
	value, err := loadFile(path)
	if err == nil {
		return value, nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}

	data, err := yaml.Marshal(defaults())
	if err != nil {
		return Config{}, fmt.Errorf("encode default config: %w", err)
	}

	if err := utils.SeedFile(path, data); err != nil {
		return Config{}, fmt.Errorf("seed config: %w", err)
	}

	return loadFile(path)
}

func loadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	value := defaults()
	if err := decode(data, &value); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	if err := validatorInstance.Struct(value); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
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
