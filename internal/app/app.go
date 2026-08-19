package app

import (
	"encoding/json"
	"log"
	"net/http"
)

// ingestResponse is the placeholder body returned by POST /ingest, mirroring
// reference-ts/src/app.ts's `{ message: "Ingesting form data" }`.
type ingestResponse struct {
	Message string `json:"message"`
}

// New mirrors reference-ts/src/app.ts: a bare router with a single
// placeholder POST /ingest handler. No ingestion logic yet — Phase 1 is a
// parity port of the scaffold, not the real solution.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest", handleIngest)
	return mux
}

func handleIngest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(ingestResponse{Message: "Ingesting form data"}); err != nil {
		log.Printf("failed to encode /ingest response: %v", err)
	}
}
