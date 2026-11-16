package tools

import "time"

const (
	defaultTimeout              = 30 * time.Second
	defaultMaxTimeout           = 2 * time.Minute
	defaultMaxBackgroundTimeout = 30 * time.Minute
	defaultMaxOutputBytes       = 64 * 1024
)

type Config struct {
	Timeout              time.Duration `yaml:"timeout" validate:"gte=1000000000"`
	MaxTimeout           time.Duration `yaml:"max_timeout" validate:"gtefield=Timeout"`
	MaxBackgroundTimeout time.Duration `yaml:"max_background_timeout" validate:"gte=1000000000"`
	MaxOutputBytes       int           `yaml:"max_output_bytes" validate:"gte=1024"`
}

func DefaultConfig() Config {
	return Config{
		Timeout:              defaultTimeout,
		MaxTimeout:           defaultMaxTimeout,
		MaxBackgroundTimeout: defaultMaxBackgroundTimeout,
		MaxOutputBytes:       defaultMaxOutputBytes,
	}
}
