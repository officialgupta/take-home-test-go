package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"take-home-test-go/internal/store"
	"take-home-test-go/internal/testutil"
)

func TestInsertIngestedForm_DuplicateApplicationReference_Dedupes(t *testing.T) {
	pool := testutil.NewPostgresPool(t)
	q := store.New(pool)
	ctx := context.Background()

	_, err := q.InsertIngestedForm(ctx, store.InsertIngestedFormParams{
		ApplicationReference: "APP-1", SessionID: "s-1", RawPayload: []byte(`{"a":1}`),
	})
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err = q.InsertIngestedForm(ctx, store.InsertIngestedFormParams{
		ApplicationReference: "APP-1", SessionID: "s-2-replay", RawPayload: []byte(`{"a":1}`),
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows on duplicate application_reference, got: %v", err)
	}

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM ingested_forms WHERE application_reference = $1`, "APP-1").Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("row count for APP-1 = %d, want exactly 1 (no duplicate row from the redelivery)", count)
	}
}

// TestClaimPendingIngestedForms_ConcurrentClaims_NoOverlap exercises
// ClaimPendingIngestedForms' FOR UPDATE SKIP LOCKED under genuine concurrent
// contention: two goroutines racing to claim from the same pending pool must
// never both claim the same row, and between them must claim every row
// exactly once.
func TestClaimPendingIngestedForms_ConcurrentClaims_NoOverlap(t *testing.T) {
	pool := testutil.NewPostgresPool(t)
	q := store.New(pool)
	ctx := context.Background()

	const totalRows = 50
	const batchSize = 5
	for i := 0; i < totalRows; i++ {
		ref := fmt.Sprintf("CONC-%d", i)
		if _, err := q.InsertIngestedForm(ctx, store.InsertIngestedFormParams{
			ApplicationReference: ref, SessionID: "s-" + ref, RawPayload: []byte(`{}`),
		}); err != nil {
			t.Fatalf("seed insert %s: %v", ref, err)
		}
	}

	claimAllPending := func() ([]int64, error) {
		var claimed []int64
		for {
			rows, err := q.ClaimPendingIngestedForms(ctx, store.ClaimPendingIngestedFormsParams{
				MaxAttempts: 5, BatchSize: batchSize,
			})
			if err != nil {
				return claimed, err
			}
			if len(rows) == 0 {
				return claimed, nil
			}
			for _, r := range rows {
				claimed = append(claimed, r.ID)
			}
		}
	}

	var wg sync.WaitGroup
	results := make([][]int64, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // released together, to maximize overlap at the DB
			results[i], errs[i] = claimAllPending()
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: claim error: %v", i, err)
		}
	}

	seen := make(map[int64]int)
	for _, ids := range results {
		for _, id := range ids {
			seen[id]++
		}
	}

	if len(seen) != totalRows {
		t.Errorf("claimed %d distinct rows, want %d (some rows never claimed by either goroutine)", len(seen), totalRows)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("row %d claimed %d times, want exactly 1 (double-claim: SKIP LOCKED failed to prevent overlap)", id, count)
		}
	}
}

// TestReclaimStaleProcessing_ResetsOnlyTrulyStaleRows proves the
// stale-reclaim sweep only resets rows that have genuinely been sitting in
// 'processing' past StaleAfter, and leaves a recently-claimed (still
// legitimately in-flight) row untouched.
func TestReclaimStaleProcessing_ResetsOnlyTrulyStaleRows(t *testing.T) {
	pool := testutil.NewPostgresPool(t)
	q := store.New(pool)
	ctx := context.Background()

	stale, err := q.InsertIngestedForm(ctx, store.InsertIngestedFormParams{
		ApplicationReference: "STALE-1", SessionID: "s-stale", RawPayload: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("insert stale row: %v", err)
	}
	if _, err := q.InsertIngestedForm(ctx, store.InsertIngestedFormParams{
		ApplicationReference: "FRESH-1", SessionID: "s-fresh", RawPayload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("insert fresh row: %v", err)
	}

	if _, err := q.ClaimPendingIngestedForms(ctx, store.ClaimPendingIngestedFormsParams{MaxAttempts: 5, BatchSize: 10}); err != nil {
		t.Fatalf("claim both rows into processing: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE ingested_forms SET updated_at = now() - interval '5 minutes' WHERE id = $1`, stale.ID); err != nil {
		t.Fatalf("backdate stale row: %v", err)
	}

	n, err := q.ReclaimStaleProcessing(ctx, pgtype.Interval{Microseconds: (2 * time.Minute).Microseconds(), Valid: true})
	if err != nil {
		t.Fatalf("reclaim stale processing: %v", err)
	}
	if n != 1 {
		t.Errorf("reclaimed %d rows, want 1", n)
	}

	gotStale, err := q.GetIngestedFormByApplicationReference(ctx, "STALE-1")
	if err != nil {
		t.Fatalf("get stale row: %v", err)
	}
	if gotStale.Status != "pending" {
		t.Errorf("stale row status = %q, want pending (should have been reclaimed)", gotStale.Status)
	}

	gotFresh, err := q.GetIngestedFormByApplicationReference(ctx, "FRESH-1")
	if err != nil {
		t.Fatalf("get fresh row: %v", err)
	}
	if gotFresh.Status != "processing" {
		t.Errorf("fresh row status = %q, want processing (should NOT have been reclaimed)", gotFresh.Status)
	}
}

// TestClaimPendingIngestedForms_ExcludesRowsAtMaxAttempts proves a row that
// has already exhausted max_attempts is excluded from claiming even though
// it's back in 'pending' — the same mechanism the /retry endpoint relies on
// to stop retry-storms until a fix is actually shipped.
func TestClaimPendingIngestedForms_ExcludesRowsAtMaxAttempts(t *testing.T) {
	pool := testutil.NewPostgresPool(t)
	q := store.New(pool)
	ctx := context.Background()

	row, err := q.InsertIngestedForm(ctx, store.InsertIngestedFormParams{
		ApplicationReference: "MAXED-1", SessionID: "s-maxed", RawPayload: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE ingested_forms SET attempt_count = 5 WHERE id = $1`, row.ID); err != nil {
		t.Fatalf("bump attempt_count: %v", err)
	}

	claimed, err := q.ClaimPendingIngestedForms(ctx, store.ClaimPendingIngestedFormsParams{MaxAttempts: 5, BatchSize: 10})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	for _, c := range claimed {
		if c.ID == row.ID {
			t.Errorf("row at max_attempts was claimed, want excluded")
		}
	}

	got, err := q.GetIngestedFormByApplicationReference(ctx, "MAXED-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "pending" {
		t.Errorf("status = %q, want still pending (untouched)", got.Status)
	}
}
