package api

import (
	"errors"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

type Health struct {
	PID int `json:"pid"`
}

type CreateSessionRequest struct {
	Directory string `json:"directory"`
}

type ResponseError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

var serviceErrors = map[string]error{
	"session_required":        harness.ErrSessionRequired,
	"session_not_found":       state.ErrNotFound,
	"execution_not_active":    session.ErrExecutionNotActive,
	"execution_not_resumable": session.ErrExecutionNotResumable,
	"subscription_closed":     event.ErrClosed,
	"subscription_lagged":     event.ErrSubscriptionLagged,
}

func (e *ResponseError) Error() string {
	return e.Message
}

func (e *ResponseError) Is(target error) bool {
	return e.Code != "" && serviceErrors[e.Code] == target
}

func responseError(err error) *ResponseError {
	response := &ResponseError{Message: err.Error()}
	if errors.As(err, &response) {
		return response
	}

	for code, target := range serviceErrors {
		if errors.Is(err, target) {
			response.Code = code

			break
		}
	}

	return response
}

type streamRecord struct {
	Event *event.Event   `json:"event,omitempty"`
	Error *ResponseError `json:"error,omitempty"`
}
