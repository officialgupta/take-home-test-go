package worker

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"take-home-test-go/internal/providers"
	"take-home-test-go/internal/store"
)

const validPayload = `{"application_reference":"APP-1","name":"Test User","email":"t@example.com","gender":"male","date_of_birth":"1990-01-01","mobile_number":"0700","address":{"address_line_1":"1 Rd","address_line_2":"Town","postcode":"AB1 2CD","country":"UK"}}`

const invalidPayload = `{"gender":"unknown","date_of_birth":"not-a-date"}`

// missingPostcodePayload has valid gender/date_of_birth but drops postcode —
// the kind of partial schema drift that used to sail through MapForm
// untouched and land in transformed_forms with a blank postcode.
const missingPostcodePayload = `{"application_reference":"APP-1","name":"Test User","email":"t@example.com","gender":"male","date_of_birth":"1990-01-01","mobile_number":"0700","address":{"address_line_1":"1 Rd","address_line_2":"Town","postcode":"","country":"UK"}}`

// alwaysSucceedGeocode/alwaysSucceedEmail stand in for the real providers,
// which have a real 1s sleep + 5% random failure rate that unit tests must
// never depend on.
func alwaysSucceedGeocode(counter *int32) func(string) (providers.GeoResult, error) {
	return func(string) (providers.GeoResult, error) {
		atomic.AddInt32(counter, 1)
		return providers.GeoResult{Longitude: 50.05, Latitude: -5.05}, nil
	}
}

func alwaysSucceedEmail(counter *int32) func(providers.EmailParams) error {
	return func(providers.EmailParams) error {
		atomic.AddInt32(counter, 1)
		return nil
	}
}

func alwaysFailGeocode(counter *int32) func(string) (providers.GeoResult, error) {
	return func(string) (providers.GeoResult, error) {
		atomic.AddInt32(counter, 1)
		return providers.GeoResult{}, errors.New("geocode boom")
	}
}

func alwaysFailEmail(counter *int32) func(providers.EmailParams) error {
	return func(providers.EmailParams) error {
		atomic.AddInt32(counter, 1)
		return errors.New("email boom")
	}
}

func TestTick_ValidRow_GeocodeAndEmailSucceed_MarkedSucceeded(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte(validPayload), Status: "pending",
	})
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})
	var geoCalls, emailCalls int32
	w.geocode = alwaysSucceedGeocode(&geoCalls)
	w.sendEmail = alwaysSucceedEmail(&emailCalls)

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "succeeded" {
		t.Errorf("status = %q, want succeeded", got.Status)
	}
	if geoCalls != 1 {
		t.Errorf("geocode calls = %d, want 1", geoCalls)
	}
	if emailCalls != 1 {
		t.Errorf("email calls = %d, want 1", emailCalls)
	}
	tf, ok := fq.getTransformed("APP-1")
	if !ok {
		t.Fatal("expected a transformed_forms row to exist")
	}
	if !tf.EmailedAt.Valid {
		t.Error("expected emailed_at to be set")
	}
	if tf.Longitude != 50.05 || tf.Latitude != -5.05 {
		t.Errorf("longitude/latitude = %v/%v, want geocoded values", tf.Longitude, tf.Latitude)
	}
}

func TestTick_ExistingTransformedRowAlreadyEmailed_SkipsGeocodeAndEmail(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte(validPayload), Status: "pending",
	})
	fq.withTransformed(store.TransformedForm{
		ApplicationReference: "APP-1",
		EmailedAt:            pgtype.Timestamptz{Valid: true},
	})
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})
	var geoCalls, emailCalls int32
	w.geocode = alwaysFailGeocode(&geoCalls)   // would fail the test if called
	w.sendEmail = alwaysFailEmail(&emailCalls) // would fail the test if called

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "succeeded" {
		t.Errorf("status = %q, want succeeded", got.Status)
	}
	if geoCalls != 0 {
		t.Errorf("geocode calls = %d, want 0 (already-transformed row must not be re-geocoded)", geoCalls)
	}
	if emailCalls != 0 {
		t.Errorf("email calls = %d, want 0 (already-emailed row must not resend)", emailCalls)
	}
}

func TestTick_ExistingTransformedRowNotYetEmailed_SendsEmailOnlyOnce(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte(validPayload), Status: "pending",
	})
	fq.withTransformed(store.TransformedForm{ApplicationReference: "APP-1"}) // EmailedAt left zero-value (not sent yet)
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})
	var geoCalls, emailCalls int32
	w.geocode = alwaysFailGeocode(&geoCalls) // would fail the test if called
	w.sendEmail = alwaysSucceedEmail(&emailCalls)

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "succeeded" {
		t.Errorf("status = %q, want succeeded", got.Status)
	}
	if geoCalls != 0 {
		t.Errorf("geocode calls = %d, want 0 (already-transformed row must not be re-geocoded)", geoCalls)
	}
	if emailCalls != 1 {
		t.Errorf("email calls = %d, want 1", emailCalls)
	}
}

func TestTick_GeocodeFailsEveryAttempt_MarkedFailedAfterRetries(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte(validPayload), Status: "pending",
	})
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})
	var geoCalls, emailCalls int32
	w.geocode = alwaysFailGeocode(&geoCalls)
	w.sendEmail = alwaysSucceedEmail(&emailCalls) // would fail the test if called

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.AttemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1", got.AttemptCount)
	}
	if geoCalls != maxProviderAttempts {
		t.Errorf("geocode calls = %d, want %d (bounded retry exhausted)", geoCalls, maxProviderAttempts)
	}
	if emailCalls != 0 {
		t.Errorf("email calls = %d, want 0 (must not email before geocode/transform succeed)", emailCalls)
	}
	if _, ok := fq.getTransformed("APP-1"); ok {
		t.Error("no transformed_forms row should exist when geocode never succeeds")
	}
}

func TestTick_EmailFailsEveryAttempt_MarkedFailedButTransformedRowPersists(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte(validPayload), Status: "pending",
	})
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})
	var geoCalls, emailCalls int32
	w.geocode = alwaysSucceedGeocode(&geoCalls)
	w.sendEmail = alwaysFailEmail(&emailCalls)

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if emailCalls != maxProviderAttempts {
		t.Errorf("email calls = %d, want %d (bounded retry exhausted)", emailCalls, maxProviderAttempts)
	}
	tf, ok := fq.getTransformed("APP-1")
	if !ok {
		t.Fatal("transformed_forms row should still exist — the form IS ready for the bot even though the email failed")
	}
	if tf.EmailedAt.Valid {
		t.Error("emailed_at should not be set when every send attempt failed")
	}
}

func TestTick_InvalidRow_MarkedFailedWithIncrementedAttemptCount(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte(invalidPayload), Status: "pending", AttemptCount: 0,
	})
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.AttemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1", got.AttemptCount)
	}
	if !got.LastError.Valid || got.LastError.String == "" {
		t.Error("expected last_error to be set")
	}
}

func TestTick_SchemaDrift_MissingRequiredField_MarkedFailedNotTransformed(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte(missingPostcodePayload), Status: "pending",
	})
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})
	var geoCalls, emailCalls int32
	w.geocode = alwaysSucceedGeocode(&geoCalls)   // would fail the test if called
	w.sendEmail = alwaysSucceedEmail(&emailCalls) // would fail the test if called

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed (a blank required field must not pass through as ready)", got.Status)
	}
	if !strings.Contains(got.LastError.String, "postcode") {
		t.Errorf("last_error = %q, want it to mention postcode", got.LastError.String)
	}
	if geoCalls != 0 {
		t.Errorf("geocode calls = %d, want 0 (must fail schema validation before geocoding)", geoCalls)
	}
	if _, ok := fq.getTransformed("APP-1"); ok {
		t.Error("no transformed_forms row should exist for a record that fails schema validation")
	}
}

func TestTick_MalformedJSON_MarkedFailed(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte("{not valid json"), Status: "pending",
	})
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
}

func TestTick_AlreadyFailedRow_NotReclaimedByClaim(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte(validPayload), Status: "failed", AttemptCount: 1,
	})
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed (only 'pending' rows should be claimed)", got.Status)
	}
}

func TestTick_AttemptCountAtMax_NotClaimed(t *testing.T) {
	fq := newFakeQuerier(store.IngestedForm{
		ID: 1, ApplicationReference: "APP-1", SessionID: "s-1",
		RawPayload: []byte(validPayload), Status: "pending", AttemptCount: 5,
	})
	w := New(fq, nil, Config{MaxAttempts: 5, BatchSize: 10})

	w.tick(context.Background())

	got := fq.get(1)
	if got.Status != "pending" {
		t.Errorf("status = %q, want pending (row at max_attempts must not be claimed)", got.Status)
	}
}

func TestNew_DefaultsAppliedForZeroConfig(t *testing.T) {
	w := New(newFakeQuerier(), nil, Config{})
	if w.cfg.PollInterval != defaultPollInterval ||
		w.cfg.BatchSize != defaultBatchSize ||
		w.cfg.MaxAttempts != defaultMaxAttempts ||
		w.cfg.StaleAfter != defaultStaleAfter {
		t.Errorf("defaults not applied: %+v", w.cfg)
	}
}
