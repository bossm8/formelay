package api

import (
	"encoding/json"
	"net/http"
)

type response struct {
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func respondJSON(w http.ResponseWriter, status int, body response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func respondSuccess(w http.ResponseWriter, requestID string) {
	respondJSON(w, http.StatusOK, response{Success: true, RequestID: requestID})
}

func respondError(w http.ResponseWriter, status int, code, requestID string) {
	respondJSON(w, status, response{Success: false, Error: code, RequestID: requestID})
}
