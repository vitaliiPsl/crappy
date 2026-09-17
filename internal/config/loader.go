package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

var validatorInstance = validator.New(validator.WithRequiredStructEnabled())

func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var value Config
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
