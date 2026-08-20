package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// formStatusResponse is not part of the task brief.
type formStatusResponse struct {
	ApplicationReference string     `json:"application_reference"`
	Status               string     `json:"status"`
	AttemptCount         int32      `json:"attempt_count"`
	LastError            *string    `json:"last_error,omitempty"`
	ReceivedAt           time.Time  `json:"received_at"`
	ReadyAt              *time.Time `json:"ready_at,omitempty"`
	EmailedAt            *time.Time `json:"emailed_at,omitempty"`
}

func (a *App) handleGetForm(w http.ResponseWriter, r *http.Request) {
	applicationReference := r.PathValue("application_reference")
	if applicationReference == "" {
		writeError(w, http.StatusBadRequest, "application_reference is required")
		return
	}

	ingested, err := a.store.GetIngestedFormByApplicationReference(r.Context(), applicationReference)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no form found for that application_reference")
		return
	}
	if err != nil {
		a.logger.Error("failed to look up form", "error", err, "application_reference", applicationReference)
		writeError(w, http.StatusInternalServerError, "failed to look up form")
		return
	}

	resp := formStatusResponse{
		ApplicationReference: ingested.ApplicationReference,
		Status:               ingested.Status,
		AttemptCount:         ingested.AttemptCount,
		LastError:            textPtr(ingested.LastError),
		ReceivedAt:           ingested.CreatedAt.Time,
	}

	transformed, err := a.store.GetTransformedFormByApplicationReference(r.Context(), applicationReference)
	switch {
	case err == nil:
		resp.ReadyAt = timePtr(transformed.ReadyAt)
		resp.EmailedAt = timePtr(transformed.EmailedAt)
	case errors.Is(err, pgx.ErrNoRows):
		// Not transformed yet — ReadyAt/EmailedAt stay nil, which is correct.
	default:
		a.logger.Error("failed to look up transformed form", "error", err, "application_reference", applicationReference)
		writeError(w, http.StatusInternalServerError, "failed to look up form")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
