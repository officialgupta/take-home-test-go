package app

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type retryResponse struct {
	ApplicationReference string `json:"application_reference"`
	Status               string `json:"status"`
}

// handleRetry re-enters the pipeline for one failed row by resetting its
// status to 'pending' — the next worker poll re-parses the originally
// stored raw_payload, so a code fix shipped since the failure actually gets
// applied. Only a row currently in 'failed' can be retried; a row still
// mid-pipeline or already succeeded is a 409, not silently accepted.
func (a *App) handleRetry(w http.ResponseWriter, r *http.Request) {
	applicationReference := r.PathValue("application_reference")
	if applicationReference == "" {
		writeError(w, http.StatusBadRequest, "application_reference is required")
		return
	}

	row, err := a.store.RequeueForRetry(r.Context(), applicationReference)
	if err == nil {
		writeJSON(w, http.StatusAccepted, retryResponse{
			ApplicationReference: row.ApplicationReference,
			Status:               "retry_scheduled",
		})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		a.logger.Error("failed to requeue form for retry", "error", err, "application_reference", applicationReference)
		writeError(w, http.StatusInternalServerError, "failed to requeue form")
		return
	}

	// RequeueForRetry matched no row: either it doesn't exist at all, or it
	// exists but isn't 'failed' — these need different status codes, so a
	// second lookup distinguishes them.
	existing, err := a.store.GetIngestedFormByApplicationReference(r.Context(), applicationReference)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no form found for that application_reference")
		return
	}
	if err != nil {
		a.logger.Error("failed to look up form for retry", "error", err, "application_reference", applicationReference)
		writeError(w, http.StatusInternalServerError, "failed to look up form")
		return
	}

	writeJSON(w, http.StatusConflict, map[string]string{
		"error":  "form is not in a failed state",
		"status": existing.Status,
	})
}
