package api

import (
	"context"
	"net/http"
	"net/url"

	"github.com/vitaliipsl/crappy/harness/interaction"
)

func (c *Client) PendingPrompts(ctx context.Context, sessionID string) ([]interaction.Request, error) {
	var requests []interaction.Request

	err := c.session(ctx, sessionID, "GET", "/prompts", nil, &requests)

	return requests, err
}

func (c *Client) Respond(ctx context.Context, sessionID, requestID string, answer interaction.Answer) error {
	return c.session(ctx, sessionID, "POST", "/prompts/"+url.PathEscape(requestID)+"/respond", answer, nil)
}

func (h *Handler) pendingPrompts(w http.ResponseWriter, r *http.Request) {
	requests, err := h.service.PendingPrompts(r.Context(), r.PathValue("session"))
	writeResponse(w, requests, err)
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request) {
	var answer interaction.Answer
	if err := readRequest(w, r, &answer); err != nil {
		writeError(w, err)

		return
	}

	err := h.service.Respond(r.Context(), r.PathValue("session"), r.PathValue("prompt"), answer)
	writeResponse(w, nil, err)
}
