package interaction

import "context"

type Choice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	Text    string   `json:"text"`
	Choices []Choice `json:"choices,omitempty"`
}

type Answer struct {
	ChoiceID string `json:"choice_id,omitempty"`
	Text     string `json:"text,omitempty"`
}

type Request struct {
	ID          string   `json:"id"`
	SessionID   string   `json:"session_id"`
	ExecutionID string   `json:"execution_id"`
	Question    Question `json:"question"`
}

type Response struct {
	RequestID string `json:"request_id"`
	Answer    Answer `json:"answer"`
}

type Prompter interface {
	Ask(ctx context.Context, executionID string, question Question) (Answer, error)
}
