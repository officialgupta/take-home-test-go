package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"take-home-test-go/internal/forms"
	"take-home-test-go/internal/providers"
	"take-home-test-go/internal/store"
	"take-home-test-go/internal/transform"
)

const writeTimeout = 5 * time.Second

func detachedWriteCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
}

func (w *Worker) process(ctx context.Context, row store.IngestedForm) {
	var in forms.IngestedForm
	if err := json.Unmarshal(row.RawPayload, &in); err != nil {
		w.fail(ctx, row.ID, fmt.Errorf("parse raw_payload: %w", err))
		return
	}

	if err := transform.Validate(in); err != nil {
		w.fail(ctx, row.ID, err)
		return
	}

	transformedID, emailedAt, err := w.ensureTransformed(ctx, row, in)
	if err != nil {
		w.fail(ctx, row.ID, err)
		return
	}

	if !emailedAt.Valid {
		if err := w.sendGuaranteedEmail(ctx, in); err != nil {
			w.fail(ctx, row.ID, fmt.Errorf("send email: %w", err))
			return
		}
		writeCtx, cancel := detachedWriteCtx(ctx)
		err := w.store.MarkTransformedFormEmailed(writeCtx, transformedID)
		cancel()
		if err != nil {
			w.logger.Error("mark transformed form emailed failed", "error", err, "id", transformedID)
			return
		}
	}

	writeCtx, cancel := detachedWriteCtx(ctx)
	err = w.store.MarkIngestedFormSucceeded(writeCtx, row.ID)
	cancel()
	if err != nil {
		w.logger.Error("mark ingested form succeeded failed", "error", err, "id", row.ID)
	}
}

func (w *Worker) ensureTransformed(ctx context.Context, row store.IngestedForm, in forms.IngestedForm) (int64, pgtype.Timestamptz, error) {
	existing, err := w.store.GetTransformedFormByApplicationReference(ctx, row.ApplicationReference)
	if err == nil {
		return existing.ID, existing.EmailedAt, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, pgtype.Timestamptz{}, fmt.Errorf("check existing transformed form: %w", err)
	}

	geo, err := withRetry(ctx, func() (providers.GeoResult, error) {
		return w.geocode(in.Address.Postcode)
	})
	if err != nil {
		return 0, pgtype.Timestamptz{}, fmt.Errorf("geocode: %w", err)
	}

	out, err := transform.MapForm(in, geo)
	if err != nil {
		return 0, pgtype.Timestamptz{}, err
	}

	inserted, err := w.store.InsertTransformedForm(ctx, toInsertParams(row.ID, out))
	if err == nil {
		return inserted.ID, inserted.EmailedAt, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, pgtype.Timestamptz{}, fmt.Errorf("insert transformed form: %w", err)
	}

	existing, err = w.store.GetTransformedFormByApplicationReference(ctx, row.ApplicationReference)
	if err != nil {
		return 0, pgtype.Timestamptz{}, fmt.Errorf("refetch transformed form after conflict: %w", err)
	}
	return existing.ID, existing.EmailedAt, nil
}

func (w *Worker) sendGuaranteedEmail(ctx context.Context, in forms.IngestedForm) error {
	_, err := withRetry(ctx, func() (struct{}, error) {
		return struct{}{}, w.sendEmail(providers.EmailParams{
			To:      "happyforms@bots.com",
			From:    "forms@healthtech1.local",
			Subject: "Form ingested: " + in.ApplicationReference,
			Body:    fmt.Sprintf("Form %s (session %s) was ingested and transformed.", in.ApplicationReference, in.SessionID),
		})
	})
	return err
}

func (w *Worker) fail(ctx context.Context, id int64, cause error) {
	writeCtx, cancel := detachedWriteCtx(ctx)
	defer cancel()
	err := w.store.MarkIngestedFormFailed(writeCtx, store.MarkIngestedFormFailedParams{
		ID:        id,
		LastError: pgtype.Text{String: cause.Error(), Valid: true},
	})
	if err != nil {
		w.logger.Error("mark ingested form failed failed", "error", err, "id", id, "cause", cause)
	}
}
