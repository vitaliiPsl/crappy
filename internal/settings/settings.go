package settings

const (
	EnvPath           = "CRAPPY_SETTINGS"
	DefaultPath       = "~/.crappy/settings.yaml"
	DefaultConfigPath = "~/.crappy/config.yaml"
)

type Settings struct {
	ConfigPath string     `yaml:"config_path,omitempty" validate:"required"`
	Providers  []Provider `yaml:"providers" validate:"min=1,unique=ID,dive"`
}

type Provider struct {
	ID        string `yaml:"id" validate:"required"`
	API       string `yaml:"api" validate:"required"`
	BaseURL   string `yaml:"base_url,omitempty"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}

func defaults() Settings {
	return Settings{
		ConfigPath: DefaultConfigPath,
		Providers: []Provider{
			{ID: "anthropic", API: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
			{ID: "google", API: "google", APIKeyEnv: "GOOGLE_API_KEY"},
			{ID: "openai", API: "openai", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
}
