-- name: InsertIngestedForm :one
-- No row returned (pgx.ErrNoRows) means application_reference already
-- existed — a duplicate delivery, dropped silently (first-write-wins).
INSERT INTO ingested_forms (application_reference, session_id, raw_payload)
VALUES ($1, $2, $3)
ON CONFLICT (application_reference) DO NOTHING
RETURNING *;

-- name: GetIngestedFormByApplicationReference :one
SELECT * FROM ingested_forms
WHERE application_reference = $1;

-- name: ClaimPendingIngestedForms :many
-- Claims and marks 'processing' in one statement so no row lock is held
-- during the slow geocode/email network calls that follow.
WITH claimed AS (
    SELECT id FROM ingested_forms
    WHERE status = 'pending' AND attempt_count < @max_attempts::int
    ORDER BY created_at
    LIMIT @batch_size::int
    FOR UPDATE SKIP LOCKED
)
UPDATE ingested_forms
SET status = 'processing', updated_at = now()
FROM claimed
WHERE ingested_forms.id = claimed.id
RETURNING ingested_forms.*;

-- name: MarkIngestedFormFailed :exec
UPDATE ingested_forms
SET status = 'failed',
    last_error = $2,
    attempt_count = attempt_count + 1,
    updated_at = now()
WHERE id = $1;

-- name: MarkIngestedFormSucceeded :exec
UPDATE ingested_forms
SET status = 'succeeded', last_error = NULL, updated_at = now()
WHERE id = $1;

-- name: ReclaimStaleProcessing :execrows
-- Resets rows orphaned by a worker crash mid-row (claimed but never
-- finished) back to 'pending' after they've sat in 'processing' too long.
UPDATE ingested_forms
SET status = 'pending', updated_at = now()
WHERE status = 'processing' AND updated_at < now() - @stale_after::interval;

-- name: RequeueForRetry :one
-- Only a 'failed' row can be retried; raw_payload is untouched, so the next
-- worker pass re-parses it with whatever code fix has since shipped.
-- attempt_count is reset so the claim query's attempt_count < max_attempts
-- filter doesn't skip a row that had already hit max_attempts.
UPDATE ingested_forms
SET status = 'pending', attempt_count = 0, updated_at = now()
WHERE application_reference = $1 AND status = 'failed'
RETURNING *;
