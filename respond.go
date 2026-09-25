package main

import (
	"encoding/json"
	"net/http"
)

// APIEnvelope is the unified JSON response wrapper.
type APIEnvelope struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
	Mode  string      `json:"mode,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOK(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, APIEnvelope{OK: true, Data: data})
}

func writeOKMode(w http.ResponseWriter, mode string, data interface{}) {
	writeJSON(w, http.StatusOK, APIEnvelope{OK: true, Data: data, Mode: mode})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, APIEnvelope{OK: false, Error: msg})
}

func writeErrMode(w http.ResponseWriter, status int, mode, msg string) {
	writeJSON(w, status, APIEnvelope{OK: false, Error: msg, Mode: mode})
}
