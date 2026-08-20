package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"take-home-test-go/internal/store"
)

type ingestResponse struct {
	ApplicationReference string `json:"application_reference"`
	Status               string `json:"status"`
}

func (a *App) handleIngest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var raw map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	applicationReference, ok := scalarStringField(raw, "application_reference")
	if !ok || applicationReference == "" {
		writeError(w, http.StatusBadRequest, "application_reference is required and must be a scalar value")
		return
	}
	sessionID, ok := scalarStringField(raw, "session_id")
	if !ok || sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required and must be a scalar value")
		return
	}

	row, err := a.store.InsertIngestedForm(r.Context(), store.InsertIngestedFormParams{
		ApplicationReference: applicationReference,
		SessionID:            sessionID,
		RawPayload:           body,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusAccepted, ingestResponse{
				ApplicationReference: applicationReference,
				Status:               "duplicate_ignored",
			})
			return
		}
		a.logger.Error("failed to insert ingested form", "error", err, "application_reference", applicationReference)
		writeError(w, http.StatusInternalServerError, "failed to store form")
		return
	}

	writeJSON(w, http.StatusAccepted, ingestResponse{
		ApplicationReference: row.ApplicationReference,
		Status:               "accepted",
	})
}

func scalarStringField(raw map[string]any, key string) (string, bool) {
	v, present := raw[key]
	if !present {
		return "", false
	}
	switch val := v.(type) {
	case string:
		return val, true
	case json.Number:
		return val.String(), true
	case bool:
		return strconv.FormatBool(val), true
	default:
		return "", false
	}
}
