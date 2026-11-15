package schedule

import "time"

type Config struct {
	PollInterval time.Duration `yaml:"poll_interval" validate:"gt=0"`
}

func DefaultConfig() Config {
	return Config{PollInterval: 5 * time.Second}
}
