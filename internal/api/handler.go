package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/vitaliipsl/crappy/internal/service"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Handler struct {
	service service.Service
	routes  *http.ServeMux

	requestStop func()
	lifetime    context.Context
	cancel      context.CancelFunc
}

func NewHandler(service service.Service, requestStop func()) *Handler {
	lifetime, cancel := context.WithCancel(context.Background())
	handler := &Handler{
		service:     service,
		requestStop: requestStop,
		lifetime:    lifetime,
		cancel:      cancel,
	}
	handler.routes = handler.registerRoutes()

	return handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.lifetime.Err() != nil {
		writeError(w, &ResponseError{Code: "stopping", Message: "assistant is stopping"})

		return
	}

	h.routes.ServeHTTP(w, r)
}

func (h *Handler) Close() {
	h.cancel()
}

func (h *Handler) registerRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /stop", h.stop)

	mux.HandleFunc("POST /sessions", h.createSession)
	mux.HandleFunc("GET /sessions/{session}", h.getSession)
	mux.HandleFunc("GET /sessions", h.listSessions)
	mux.HandleFunc("DELETE /sessions/{session}", h.deleteSession)

	mux.HandleFunc("POST /sessions/{session}/execute", h.execute)
	mux.HandleFunc("POST /sessions/{session}/executions/{execution}/cancel", h.cancelExecution)
	mux.HandleFunc("POST /sessions/{session}/resume", h.resume)
	mux.HandleFunc("POST /sessions/{session}/compact", h.compact)
	mux.HandleFunc("GET /sessions/{session}/executions/{execution}", h.executionStatus)
	mux.HandleFunc("GET /sessions/{session}/executions/{execution}/wait", h.waitExecution)

	mux.HandleFunc("GET /model", h.model)
	mux.HandleFunc("GET /sessions/{session}/context", h.context)
	mux.HandleFunc("GET /sessions/{session}/usage", h.usage)

	mux.HandleFunc("GET /sessions/{session}/events", h.events)

	return mux
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeResponse(w, Health{PID: os.Getpid()}, nil)
}

func (h *Handler) stop(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)

	if err := http.NewResponseController(w).Flush(); err != nil {
		return
	}

	h.requestStop()
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	var input CreateSessionRequest
	if err := readRequest(w, r, &input); err != nil {
		writeError(w, err)

		return
	}

	if !filepath.IsAbs(input.Directory) {
		writeError(w, &ResponseError{Code: "invalid_request", Message: "an absolute working directory is required"})

		return
	}

	session, err := h.service.CreateSession(r.Context(), input.Directory)
	writeResponse(w, session, err)
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	session, err := h.service.GetSession(r.Context(), r.PathValue("session"))
	writeResponse(w, session, err)
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.service.ListSessions(r.Context())
	writeResponse(w, sessions, err)
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	err := h.service.DeleteSession(r.Context(), r.PathValue("session"))
	writeResponse(w, nil, err)
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	var content []kit.Content
	if err := readRequest(w, r, &content); err != nil {
		writeError(w, err)

		return
	}

	id, err := h.service.Execute(r.Context(), r.PathValue("session"), content...)
	writeResponse(w, id, err)
}

func (h *Handler) cancelExecution(w http.ResponseWriter, r *http.Request) {
	err := h.service.Cancel(r.Context(), r.PathValue("session"), r.PathValue("execution"))
	writeResponse(w, nil, err)
}

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	id, err := h.service.Resume(r.Context(), r.PathValue("session"))
	writeResponse(w, id, err)
}

func (h *Handler) compact(w http.ResponseWriter, r *http.Request) {
	id, err := h.service.Compact(r.Context(), r.PathValue("session"))
	writeResponse(w, id, err)
}

func (h *Handler) executionStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.GetExecutionStatus(r.Context(), r.PathValue("session"), r.PathValue("execution"))
	writeResponse(w, status, err)
}

func (h *Handler) waitExecution(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.WaitExecution(r.Context(), r.PathValue("session"), r.PathValue("execution"))
	writeResponse(w, status, err)
}

func (h *Handler) model(w http.ResponseWriter, r *http.Request) {
	selection, err := h.service.GetModel(r.Context())
	writeResponse(w, selection, err)
}

func (h *Handler) context(w http.ResponseWriter, r *http.Request) {
	report, err := h.service.Context(r.Context(), r.PathValue("session"))
	writeResponse(w, report, err)
}

func (h *Handler) usage(w http.ResponseWriter, r *http.Request) {
	usage, err := h.service.Usage(r.Context(), r.PathValue("session"))
	writeResponse(w, usage, err)
}
