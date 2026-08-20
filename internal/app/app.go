package app

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"take-home-test-go/internal/store"
)

type App struct {
	store  store.Querier
	logger *slog.Logger
}

func New(s store.Querier, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	a := &App{store: s, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest", a.handleIngest)
	mux.HandleFunc("POST /retry/{application_reference}", a.handleRetry)
	mux.HandleFunc("GET /forms/{application_reference}", a.handleGetForm)
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
