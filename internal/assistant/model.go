package assistant

type ModelSelection struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	Thinking   string `json:"thinking,omitempty"`
}

func (a *Assistant) GetModel() ModelSelection {
	return ModelSelection{
		ProviderID: a.config.ProviderID,
		ModelID:    a.config.ModelID,
		Thinking:   a.config.Generation.Thinking,
	}
}
