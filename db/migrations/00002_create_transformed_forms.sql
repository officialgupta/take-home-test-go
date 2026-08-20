-- +goose Up
CREATE TABLE transformed_forms (
    id                      BIGSERIAL PRIMARY KEY,
    ingested_form_id        BIGINT NOT NULL REFERENCES ingested_forms(id),
    application_reference   TEXT NOT NULL,
    session_id              TEXT NOT NULL,
    first_name               TEXT NOT NULL,
    last_name                TEXT NOT NULL,
    email                    TEXT NOT NULL,
    gender                   TEXT NOT NULL CHECK (gender IN ('male', 'female', 'prefer-not-to-say')),
    date_of_birth             DATE NOT NULL,
    phone_number              TEXT,
    mobile_number             TEXT NOT NULL,
    address_line_1            TEXT NOT NULL,
    address_line_2            TEXT NOT NULL,
    address_line_3            TEXT,
    postcode                  TEXT NOT NULL,
    country                   TEXT NOT NULL,
    longitude                 DOUBLE PRECISION NOT NULL,
    latitude                  DOUBLE PRECISION NOT NULL,
    emailed_at                 TIMESTAMPTZ,
    ready_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Belt-and-braces alongside SKIP LOCKED: an application (or its source
    -- ingested row) must never produce two transformed/"ready" records.
    CONSTRAINT transformed_forms_application_reference_key UNIQUE (application_reference),
    CONSTRAINT transformed_forms_ingested_form_id_key UNIQUE (ingested_form_id)
);

-- +goose Down
DROP TABLE IF EXISTS transformed_forms;
