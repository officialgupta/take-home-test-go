# take-home-test-go

Go implementation of the Healthtech-1 form ingestion take-home: ingest registration
forms from an unreliable 3rd party, make them resilient to schema drift and
duplicate delivery, geocode + transform them, and guarantee a notification email —
all backed by a real Postgres schema.

## Quickstart

```sh
make up          # Postgres via docker-compose, localhost:5433
make migrate-up   # apply the schema
make run          # starts the server on localhost:3000 (worker runs in-process)
```

```sh
curl -X POST localhost:3000/ingest -d @examples/person_one.json -H "Content-Type: application/json"
curl localhost:3000/forms/GRU-123089-2026          # watch it progress: pending -> succeeded
curl -X POST localhost:3000/retry/GRU-123089-2026   # re-enter the pipeline for a failed row
```

```sh
make test   # unit tests + real-Postgres integration tests (testcontainers-go
            # spins up its own throwaway container — `make up` isn't needed for this)
```

Only Postgres runs in Docker. The Go binary itself runs directly via `make run` /
`go run` — deliberately not containerized, since it adds no value for local
development or for a reviewer running this project, and `make` already covers
every step (see [Docker](#docker) below for the one-line version of why).

## Architecture

`POST /ingest` does the minimum necessary to respond fast and safely: decode the
body loosely (schema drift must not break ingestion), require only
`application_reference`/`session_id`, store the **raw request bytes** verbatim, and
dedupe on `application_reference` via a DB unique constraint — then return
`202 Accepted` immediately.

A background worker (in the same process, polling on an interval) does the actual
work per row:

```
parse raw_payload
  -> validate (required fields present, gender recognized, date parses) ── fail: "needs a code change" -> failed
  -> already transformed? ── yes: skip to email check
  -> geocode (bounded retry)  ── exhausted: failed (auto-retried next pass)
  -> transform + insert transformed_forms
  -> already emailed? ── yes: skip
  -> send guaranteed email (bounded retry) ── exhausted: failed, but transformed row stays (bot-ready regardless)
  -> mark succeeded
```

Rows are claimed with `SELECT ... FOR UPDATE SKIP LOCKED` so concurrent
workers/restarts can't double-process one; a row `failed` after 5 claims requires
an explicit `POST /retry` (see [Retry endpoint](#retry-endpoint)) rather than
auto-healing forever.

### Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/ingest` | Store a raw form, dedup on `application_reference`, `202` either way |
| `POST` | `/retry/{application_reference}` | Re-queue a `failed` row; `404`/`409`/`202` |
| `GET` | `/forms/{application_reference}` | Pipeline status for one form (not in the task brief — see below) |
| `GET` | `/healthz` | DB connectivity check |

## Design decisions & assumptions

This project talks to an "unreliable 3rd party" (per the task brief: schema drift
without notice, duplicate deliveries, at-least-once semantics). Several of the
decisions below are judgment calls made to handle that unreliability sensibly, not
things stated explicitly in the task.

### Idempotency / duplicate detection: dedupe on `application_reference`

- **`session_id`** identifies a single submission *attempt* — unique to one HTTP
  delivery. Because the upstream doesn't guarantee exactly-once delivery, the same
  `session_id` (and the same body) can be replayed by the upstream system on retry.
- **`application_reference`** identifies the underlying *application* itself. The
  same application should never produce two records in our system, regardless of
  how many times it's (re)delivered or under how many different `session_id`s.
- **Decision:** the uniqueness/dedup constraint lives on `application_reference`,
  not `session_id`. A delivery for an `application_reference` we've already
  ingested is treated as a duplicate and dropped (first-write-wins), rather than
  reprocessed. This is what actually satisfies "never give the FORM-BOT the same
  form twice" under repeated deliveries.
- `session_id` is still stored per delivery for traceability/debugging — it's just
  not the identity/uniqueness key.
- **Known limitation:** if the upstream ever needs to send a genuinely *corrected*
  resubmission under the same `application_reference`, this system can't currently
  distinguish that from a duplicate delivery and will drop it. Nothing in the task
  brief suggests corrected resubmissions are a real scenario, so the simpler
  behavior (drop) was chosen deliberately rather than decided implicitly.

### Gender enum mapping: `other` → `prefer-not-to-say`

- Ingested schema: `"male" | "female" | "other"`.
- Transformed schema: `"male" | "female" | "prefer-not-to-say"`.
- The task brief never states this mapping explicitly. `other → prefer-not-to-say`
  is the only sane 1:1 inference given the enums don't otherwise line up.

### Optional fields (`phone_number`, `address_line_3`)

- The TS types declare these as `string | undefined`, but the example fixtures
  omit the key entirely rather than sending `null`. Modeled in Go as `*string`
  with `omitempty`, matching observed behavior rather than the type declaration.

### `date_of_birth` parsing

- Ingested as a raw string (e.g. `"1990-01-01"`), transformed into a real date
  value. **Decision:** parsed against a single strict layout (`2006-01-02`) —
  every example fixture uses it, and loosely accepting multiple layouts (e.g. also
  trying `MM/DD/YYYY`) risks silently misinterpreting an ambiguous date for
  healthcare data, which is worse than a loud, specific, retryable failure. Any
  other format is schema drift: it lands the row in `failed` with the exact
  rejected string in `last_error`, needing a code change, not a retry.

### Required-field validation

- Schema conformance (task brief item 2) isn't just gender/date_of_birth — `name`,
  `email`, `mobile_number`, and every non-optional address field are checked for
  blankness too, via a single `transform.Validate` used both as the worker's
  fail-fast pre-check (before paying for a geocode call) and as `MapForm`'s own
  precondition. Without this, a schema-drift delivery that silently dropped e.g.
  `postcode` would sail through geocoding — the mock geocoder ignores its input —
  and land in `transformed_forms`, marked ready for FORM-BOT, with a blank
  postcode. `phone_number`/`address_line_3` stay exempt, since they're genuinely
  optional in the ingested schema.

### Name splitting (`name` → `firstName`/`lastName`)

- No canonical split exists for names like `"Andy James Smith-Jones"`.
  **Decision:** first whitespace-delimited token = `firstName`, remainder =
  `lastName` — a documented known simplification, not treated as "correct."

### Processing model: async background worker

- `POST /ingest` persists the raw payload and returns quickly; a worker drives
  validate → geocode → transform → email asynchronously, in the same process.
- **Worker trigger:** polling on an interval, the simplest option to implement and
  reason about at this scope. Postgres `LISTEN/NOTIFY` was considered for lower
  latency but isn't necessary here.
- **Concurrency safety:** the worker claims rows with
  `SELECT ... FOR UPDATE SKIP LOCKED` in a two-phase pattern — the lock is held
  only for the claim statement, which flips the row to `processing` and releases
  the lock before the slow geocode/email calls run, so a DB connection+lock isn't
  tied up for seconds at a time. A `ReclaimStaleProcessing` sweep (run once per
  poll tick) resets any row stuck in `processing` for too long — e.g. after a
  worker crash mid-row — back to `pending`.
- This is a distinct concern from the `application_reference` uniqueness
  constraint: that guards against duplicate *deliveries*; `SKIP LOCKED` guards
  against duplicate *processing* of a row already stored.

### Transient failure handling: bounded retry, then durable failure

- The geocode and email calls each fail ~5% of the time (per the given mocks).
  Rather than landing a row in `failed` (which requires an explicit `POST /retry`)
  over what's almost always a one-off blip, both calls are wrapped in a bounded
  retry (`cenkalti/backoff/v5`, 3 attempts, short exponential backoff) local to
  that single worker pass.
- This is distinct from `ingested_forms.attempt_count`, which tracks failures
  *across* worker passes/claims and gates when a row needs an explicit retry
  (`ClaimPendingIngestedForms` only selects rows with `attempt_count < max_attempts`,
  default 5) — a permanently broken row (e.g. an unparseable date) can't loop
  forever, but a genuinely transient one usually self-heals within a single pass.

### Database: PostgreSQL via docker-compose

- Chosen over SQLite for `jsonb` support (storing raw ingested payloads for
  forensics/replay after a schema-drift bug) and because the task specifically
  calls out wanting to see real schema design. Schema: `db/migrations/`
  (goose), queries: `db/queries/*.sql` (sqlc-generated into `internal/store`, so
  the actual SQL stays visible rather than hidden behind an ORM).

### Guaranteed email

- Sent only after a successful transform, and only once per
  `application_reference` — tracked via an `emailed_at` timestamp so re-entering
  the pipeline after a later step failed (or a crash) doesn't resend an email that
  already succeeded.
- "Guaranteed" is bounded-retry-then-retryable-until-success, not
  synchronous-blocking: a failed send (after exhausting the in-pass retries)
  leaves the record in a failed/retryable state, rather than blocking the request
  or silently dropping it.
- If email fails but transform+geocode already succeeded, the **transformed row
  is left in place** — a flaky notification email doesn't block FORM-BOT
  readiness, since those are separate concerns the task brief itself lists
  separately.

### Retry endpoint

- `POST /retry/{application_reference}`: `404` if unknown, `409` (with the
  current status) if not currently `failed`, `202` if requeued. Requeuing resets
  `status` to `pending` **and resets `attempt_count` to 0** — without the reset, a
  row that had already exhausted `max_attempts` would immediately be excluded
  again by the claim query's `attempt_count < max_attempts` filter, making the
  retry silently do nothing.
- `raw_payload` is never touched by a retry — the next worker pass re-parses the
  originally stored bytes, which is what lets a shipped code fix actually take
  effect. No bulk "retry all" — out of scope.

### `GET /forms/{application_reference}` — not in the task brief

- Added purely as an observability/demo convenience: watch a form's `status`,
  `attempt_count`, `last_error`, `ready_at`, `emailed_at` change in real time.
  Deliberately reports only status/timing metadata, never the ingested/transformed
  field values (name, email, address, ...) — it's a debugging aid, not a data
  export endpoint, and not a FORM-BOT hand-off mechanism (see below).

### FORM-BOT hand-off

- Out of scope for this task. Readiness is modeled via `ready_at` on the
  transformed record. How FORM-BOT actually consumes/claims a ready form (polling
  endpoint, queue, etc.) is not built here — called out explicitly as a boundary
  of scope rather than left ambiguous.

## Testing

- **Unit tests** (`go test ./...`, no Docker): pure logic in `internal/transform`
  (name split / gender map / date parse, including decoding all three
  `examples/person_*.json` fixtures end-to-end) and the worker/HTTP layers tested
  against in-memory fakes of `store.Querier`.
- **Integration tests** (same `go test ./...` — no separate command, no manual
  `docker compose up` needed): `testcontainers-go` spins up its own throwaway
  Postgres per test. Coverage: duplicate-delivery dedup, **concurrent claims under
  real contention** (50 seeded rows, two goroutines racing `ClaimPendingIngestedForms`
  — asserts zero double-claims and zero misses, the strongest evidence `SKIP LOCKED`
  is doing its job), email idempotency against the real `emailed_at` column, and a
  full failed → shipped-fix → `/retry` → succeeded run.
- The real `providers.LookupPostcode`/`SendEmail` (1s sleep + real 5% randomness)
  are intentionally **not** used in any automated test — the worker takes them as
  injectable function fields specifically so tests can substitute deterministic
  fakes. They're only exercised via manual runs (`make run` + `curl`).