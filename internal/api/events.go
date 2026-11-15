package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/vitaliipsl/crappy/harness/event"
)

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	if err != nil {
		writeError(w, &ResponseError{Code: "invalid_request", Message: "after must be an unsigned entry ID"})

		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stop := context.AfterFunc(h.lifetime, cancel)
	defer stop()

	subscription, err := h.service.Subscribe(ctx, r.PathValue("session"), after)
	if err != nil {
		writeError(w, err)

		return
	}
	defer subscription.Close()

	writeEvents(ctx, w, subscription)
}

func writeEvents(ctx context.Context, w http.ResponseWriter, subscription event.Subscription) {
	w.Header().Set("Content-Type", "application/x-ndjson")

	control := http.NewResponseController(w)
	if err := control.Flush(); err != nil {
		return
	}

	encoder := json.NewEncoder(w)
	for {
		select {
		case <-ctx.Done():
			return
		case value, ok := <-subscription.Events():
			if !ok {
				if err := subscription.Err(); err != nil {
					_ = encoder.Encode(streamRecord{Error: responseError(err)})
					_ = control.Flush()
				}

				return
			}

			if err := encoder.Encode(streamRecord{Event: &value}); err != nil {
				return
			}

			if err := control.Flush(); err != nil {
				return
			}
		}
	}
}
