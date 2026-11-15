package api

import (
	"encoding/json"
	"net/http"
)

const maxRequestBytes = 16 << 20

func readRequest(w http.ResponseWriter, r *http.Request, target any) error {
	body := http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if err := json.NewDecoder(body).Decode(target); err != nil {
		return &ResponseError{Code: "invalid_request", Message: err.Error()}
	}

	return nil
}

func writeResponse(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(w, err)

		return
	}

	writeJSON(w, http.StatusOK, value)
}

func writeError(w http.ResponseWriter, err error) {
	response := responseError(err)

	status := http.StatusInternalServerError
	switch response.Code {
	case "session_not_found":
		status = http.StatusNotFound
	case "stopping":
		status = http.StatusServiceUnavailable
	case "":
	default:
		status = http.StatusBadRequest
	}

	writeJSON(w, status, response)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
