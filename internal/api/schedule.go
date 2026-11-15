package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/vitaliipsl/crappy/internal/schedule"
)

type CreateScheduleRequest struct {
	Instruction string    `json:"instruction"`
	At          time.Time `json:"at"`
}

func (c *Client) CreateSchedule(ctx context.Context, sessionID, instruction string, at time.Time) (schedule.Record, error) {
	var record schedule.Record

	err := c.session(ctx, sessionID, "POST", "/schedules", CreateScheduleRequest{Instruction: instruction, At: at}, &record)

	return record, err
}

func (c *Client) GetSchedule(ctx context.Context, sessionID, id string) (schedule.Record, error) {
	var record schedule.Record

	err := c.session(ctx, sessionID, "GET", "/schedules/"+url.PathEscape(id), nil, &record)

	return record, err
}

func (c *Client) ListSchedules(ctx context.Context, sessionID string) ([]schedule.Record, error) {
	var records []schedule.Record

	err := c.session(ctx, sessionID, "GET", "/schedules", nil, &records)

	return records, err
}

func (c *Client) CancelSchedule(ctx context.Context, sessionID, id string) (schedule.Record, error) {
	var record schedule.Record

	err := c.session(ctx, sessionID, "POST", "/schedules/"+url.PathEscape(id)+"/cancel", nil, &record)

	return record, err
}

func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	var input CreateScheduleRequest
	if err := readRequest(w, r, &input); err != nil {
		writeError(w, err)

		return
	}

	record, err := h.service.CreateSchedule(r.Context(), r.PathValue("session"), input.Instruction, input.At)
	writeResponse(w, record, err)
}

func (h *Handler) getSchedule(w http.ResponseWriter, r *http.Request) {
	record, err := h.service.GetSchedule(r.Context(), r.PathValue("session"), r.PathValue("schedule"))
	writeResponse(w, record, err)
}

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	records, err := h.service.ListSchedules(r.Context(), r.PathValue("session"))
	writeResponse(w, records, err)
}

func (h *Handler) cancelSchedule(w http.ResponseWriter, r *http.Request) {
	record, err := h.service.CancelSchedule(r.Context(), r.PathValue("session"), r.PathValue("schedule"))
	writeResponse(w, record, err)
}
