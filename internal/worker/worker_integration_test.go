package worker

import (
	"context"
	"testing"

	"take-home-test-go/internal/store"
	"take-home-test-go/internal/testutil"
)

// TestProcess_EmailIdempotency_RealDB proves the emailed_at guard against
// real Postgres, not just the in-memory fake: a row that's already been
// transformed and emailed must not be re-geocoded or re-emailed on a second
// pass, even though the real GetTransformedFormByApplicationReference /
// ON CONFLICT DO NOTHING SQL is now doing the work instead of a fake map.
func TestProcess_EmailIdempotency_RealDB(t *testing.T) {
	pool := testutil.NewPostgresPool(t)
	q := store.New(pool)
	ctx := context.Background()

	const ref = "IDEMPOTENT-1"
	payload := `{"application_reference":"` + ref + `","name":"Test User","email":"t@example.com","gender":"male","date_of_birth":"1990-01-01","mobile_number":"0700","address":{"address_line_1":"1 Rd","address_line_2":"Town","postcode":"AB1 2CD","country":"UK"}}`

	if _, err := q.InsertIngestedForm(ctx, store.InsertIngestedFormParams{
		ApplicationReference: ref, SessionID: "s-1", RawPayload: []byte(payload),
	}); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	w := New(q, nil, Config{MaxAttempts: 5, BatchSize: 10})
	var geoCalls, emailCalls int32
	w.geocode = alwaysSucceedGeocode(&geoCalls)
	w.sendEmail = alwaysSucceedEmail(&emailCalls)

	w.tick(ctx)

	row, err := q.GetIngestedFormByApplicationReference(ctx, ref)
	if err != nil {
		t.Fatalf("get after first tick: %v", err)
	}
	if row.Status != "succeeded" {
		t.Fatalf("status after first tick = %q, want succeeded", row.Status)
	}
	if geoCalls != 1 || emailCalls != 1 {
		t.Fatalf("after first tick: geoCalls=%d emailCalls=%d, want 1/1", geoCalls, emailCalls)
	}

	transformedBefore, err := q.GetTransformedFormByApplicationReference(ctx, ref)
	if err != nil {
		t.Fatalf("get transformed after first tick: %v", err)
	}

	// Simulate the row coming back to 'pending' — e.g. a stale-processing
	// sweep after a crash right before the final status flip. The
	// transformed row and its emailed_at already exist in the real DB.
	if _, err := pool.Exec(ctx, `UPDATE ingested_forms SET status = 'pending' WHERE application_reference = $1`, ref); err != nil {
		t.Fatalf("reset to pending: %v", err)
	}

	w.tick(ctx)

	if geoCalls != 1 {
		t.Errorf("geoCalls after second tick = %d, want still 1 (must not re-geocode an existing transformed row)", geoCalls)
	}
	if emailCalls != 1 {
		t.Errorf("emailCalls after second tick = %d, want still 1 (must not resend the guaranteed email)", emailCalls)
	}

	row, err = q.GetIngestedFormByApplicationReference(ctx, ref)
	if err != nil {
		t.Fatalf("get after second tick: %v", err)
	}
	if row.Status != "succeeded" {
		t.Errorf("status after second tick = %q, want succeeded", row.Status)
	}

	transformedAfter, err := q.GetTransformedFormByApplicationReference(ctx, ref)
	if err != nil {
		t.Fatalf("get transformed after second tick: %v", err)
	}
	if transformedAfter.ID != transformedBefore.ID {
		t.Errorf("transformed row ID changed (%d -> %d): a second row was inserted instead of reusing the existing one", transformedBefore.ID, transformedAfter.ID)
	}
}

// TestProcess_RetryToSuccess_RealDB is the "ship a code change, then
// /retry" story end to end against real Postgres: a row that exhausts its
// bounded in-memory retries and lands in 'failed' can be recovered once the
// underlying problem is fixed, via the same RequeueForRetry query the
// POST /retry handler uses.
func TestProcess_RetryToSuccess_RealDB(t *testing.T) {
	pool := testutil.NewPostgresPool(t)
	q := store.New(pool)
	ctx := context.Background()

	const ref = "RETRY-TO-SUCCESS-1"
	payload := `{"application_reference":"` + ref + `","name":"Test User","email":"t@example.com","gender":"male","date_of_birth":"1990-01-01","mobile_number":"0700","address":{"address_line_1":"1 Rd","address_line_2":"Town","postcode":"AB1 2CD","country":"UK"}}`

	if _, err := q.InsertIngestedForm(ctx, store.InsertIngestedFormParams{
		ApplicationReference: ref, SessionID: "s-1", RawPayload: []byte(payload),
	}); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	w := New(q, nil, Config{MaxAttempts: 5, BatchSize: 10})
	var geoCalls, emailCalls int32
	w.geocode = alwaysFailGeocode(&geoCalls) // simulates the geocoder being down / a bad postcode

	w.tick(ctx)

	row, err := q.GetIngestedFormByApplicationReference(ctx, ref)
	if err != nil {
		t.Fatalf("get after failing tick: %v", err)
	}
	if row.Status != "failed" {
		t.Fatalf("status after failing tick = %q, want failed", row.Status)
	}
	if _, err := q.GetTransformedFormByApplicationReference(ctx, ref); err == nil {
		t.Fatal("no transformed row should exist when geocode never succeeded")
	}

	// "Ship a code change" — the underlying problem is fixed.
	w.geocode = alwaysSucceedGeocode(&geoCalls)
	w.sendEmail = alwaysSucceedEmail(&emailCalls)

	// The retry endpoint's exact query, called directly rather than via HTTP
	// since this test is about the pipeline, not the handler layer.
	if _, err := q.RequeueForRetry(ctx, ref); err != nil {
		t.Fatalf("requeue for retry: %v", err)
	}

	w.tick(ctx)

	row, err = q.GetIngestedFormByApplicationReference(ctx, ref)
	if err != nil {
		t.Fatalf("get after retry tick: %v", err)
	}
	if row.Status != "succeeded" {
		t.Errorf("status after retry tick = %q, want succeeded", row.Status)
	}

	transformed, err := q.GetTransformedFormByApplicationReference(ctx, ref)
	if err != nil {
		t.Fatalf("get transformed after retry: %v", err)
	}
	if !transformed.ReadyAt.Valid {
		t.Error("expected ready_at to be set")
	}
	if !transformed.EmailedAt.Valid {
		t.Error("expected emailed_at to be set")
	}
}
