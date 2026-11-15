package service

import "context"

type ModelSelection struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	Thinking   string `json:"thinking,omitempty"`
}

func (s *Local) GetModel(context.Context) (ModelSelection, error) {
	return ModelSelection{
		ProviderID: s.config.ProviderID,
		ModelID:    s.config.ModelID,
		Thinking:   s.config.Generation.Thinking,
	}, nil
}
