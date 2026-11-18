package mcp

import "time"

type Config struct {
	CredentialsPath string         `yaml:"credentials_path,omitempty" validate:"required"`
	PollInterval    time.Duration  `yaml:"poll_interval" validate:"gt=0"`
	ConnectTimeout  time.Duration  `yaml:"connect_timeout" validate:"gt=0"`
	Servers         []ServerConfig `yaml:"servers,omitempty" validate:"unique=Name,dive"`
}

type ServerConfig struct {
	Name  string   `yaml:"name" validate:"required"`
	Tools []string `yaml:"tools,omitempty"`

	Command string            `yaml:"command,omitempty" validate:"required_without=URL,excluded_with=URL"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`

	URL     string            `yaml:"url,omitempty" validate:"omitempty,http_url"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		CredentialsPath: "~/.crappy/mcp",
		PollInterval:    30 * time.Second,
		ConnectTimeout:  10 * time.Second,
	}
}
