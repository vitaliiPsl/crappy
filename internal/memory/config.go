package memory

const DefaultPath = "~/.crappy/memory.json"

type Config struct {
	Path string `yaml:"path,omitempty" validate:"required"`
}

func DefaultConfig() Config {
	return Config{
		Path: DefaultPath,
	}
}
