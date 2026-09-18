package tools

import "time"

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxTimeout = 5 * time.Minute
)

type Config struct {
	Timeout    time.Duration `yaml:"timeout" validate:"gte=1000000000"`
	MaxTimeout time.Duration `yaml:"max_timeout" validate:"gtefield=Timeout"`
}

func DefaultConfig() Config {
	return Config{
		Timeout:    defaultTimeout,
		MaxTimeout: defaultMaxTimeout,
	}
}
