-- name: GetTransformedFormByApplicationReference :one
SELECT * FROM transformed_forms
WHERE application_reference = $1;

-- name: InsertTransformedForm :one
-- ON CONFLICT DO NOTHING is belt-and-braces alongside SKIP LOCKED and the
-- ingested_form_id/application_reference unique constraints — this should
-- never actually conflict in practice.
INSERT INTO transformed_forms (
    ingested_form_id, application_reference, session_id,
    first_name, last_name, email, gender, date_of_birth,
    phone_number, mobile_number,
    address_line_1, address_line_2, address_line_3, postcode, country,
    longitude, latitude
) VALUES (
    $1, $2, $3,
    $4, $5, $6, $7, $8,
    $9, $10,
    $11, $12, $13, $14, $15,
    $16, $17
)
ON CONFLICT (application_reference) DO NOTHING
RETURNING *;

-- name: MarkTransformedFormEmailed :exec
UPDATE transformed_forms
SET emailed_at = now()
WHERE id = $1 AND emailed_at IS NULL;
