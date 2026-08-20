-- +goose Up
CREATE TABLE ingested_forms (
    id                     BIGSERIAL PRIMARY KEY,
    application_reference  TEXT NOT NULL,
    session_id             TEXT NOT NULL,
    raw_payload            JSONB NOT NULL,
    status                 TEXT NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending', 'processing', 'failed', 'succeeded')),
    last_error             TEXT,
    attempt_count          INT NOT NULL DEFAULT 0,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT ingested_forms_application_reference_key UNIQUE (application_reference)
);

-- The worker's claim query filters on status='pending'; most rows will
-- eventually be 'succeeded', so a partial index keeps that scan cheap as the
-- table grows.
CREATE INDEX idx_ingested_forms_pending
    ON ingested_forms (created_at)
    WHERE status = 'pending';

-- Traceability/debugging lookups by delivery attempt (not the dedup key).
CREATE INDEX idx_ingested_forms_session_id ON ingested_forms (session_id);

-- +goose Down
DROP TABLE IF EXISTS ingested_forms;
