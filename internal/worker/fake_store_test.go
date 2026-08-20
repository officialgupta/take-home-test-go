package worker

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"take-home-test-go/internal/store"
)

// fakeQuerier is a minimal in-memory store.Querier for worker tests — no
// database, no SKIP LOCKED, just enough to exercise the state machine.
type fakeQuerier struct {
	mu                sync.Mutex
	rows              map[int64]store.IngestedForm
	transformed       map[string]store.TransformedForm // keyed by application_reference
	transformedByID   map[int64]string                 // id -> application_reference
	nextTransformedID int64
}

func newFakeQuerier(rows ...store.IngestedForm) *fakeQuerier {
	f := &fakeQuerier{
		rows:            make(map[int64]store.IngestedForm),
		transformed:     make(map[string]store.TransformedForm),
		transformedByID: make(map[int64]string),
	}
	for _, r := range rows {
		f.rows[r.ID] = r
	}
	return f
}

// withTransformed seeds an existing transformed_forms row for
// application_reference — used to exercise the idempotent re-entry path.
func (f *fakeQuerier) withTransformed(row store.TransformedForm) *fakeQuerier {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextTransformedID++
	row.ID = f.nextTransformedID
	f.transformed[row.ApplicationReference] = row
	f.transformedByID[row.ID] = row.ApplicationReference
	return f
}

func (f *fakeQuerier) get(id int64) store.IngestedForm {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[id]
}

func (f *fakeQuerier) getTransformed(applicationReference string) (store.TransformedForm, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.transformed[applicationReference]
	return row, ok
}

func (f *fakeQuerier) ClaimPendingIngestedForms(_ context.Context, arg store.ClaimPendingIngestedFormsParams) ([]store.IngestedForm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var claimed []store.IngestedForm
	for id, row := range f.rows {
		if int32(len(claimed)) >= arg.BatchSize {
			break
		}
		if row.Status != "pending" || row.AttemptCount >= arg.MaxAttempts {
			continue
		}
		row.Status = "processing"
		f.rows[id] = row
		claimed = append(claimed, row)
	}
	return claimed, nil
}

func (f *fakeQuerier) MarkIngestedFormFailed(_ context.Context, arg store.MarkIngestedFormFailedParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.rows[arg.ID]
	row.Status = "failed"
	row.LastError = arg.LastError
	row.AttemptCount++
	f.rows[arg.ID] = row
	return nil
}

func (f *fakeQuerier) MarkIngestedFormSucceeded(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.rows[id]
	row.Status = "succeeded"
	row.LastError = pgtype.Text{}
	f.rows[id] = row
	return nil
}

func (f *fakeQuerier) ReclaimStaleProcessing(context.Context, pgtype.Interval) (int64, error) {
	return 0, nil
}

func (f *fakeQuerier) GetTransformedFormByApplicationReference(_ context.Context, applicationReference string) (store.TransformedForm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.transformed[applicationReference]
	if !ok {
		return store.TransformedForm{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeQuerier) InsertTransformedForm(_ context.Context, arg store.InsertTransformedFormParams) (store.TransformedForm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.transformed[arg.ApplicationReference]; exists {
		return store.TransformedForm{}, pgx.ErrNoRows
	}
	f.nextTransformedID++
	row := store.TransformedForm{
		ID:                   f.nextTransformedID,
		IngestedFormID:       arg.IngestedFormID,
		ApplicationReference: arg.ApplicationReference,
		SessionID:            arg.SessionID,
		FirstName:            arg.FirstName,
		LastName:             arg.LastName,
		Email:                arg.Email,
		Gender:               arg.Gender,
		DateOfBirth:          arg.DateOfBirth,
		PhoneNumber:          arg.PhoneNumber,
		MobileNumber:         arg.MobileNumber,
		AddressLine1:         arg.AddressLine1,
		AddressLine2:         arg.AddressLine2,
		AddressLine3:         arg.AddressLine3,
		Postcode:             arg.Postcode,
		Country:              arg.Country,
		Longitude:            arg.Longitude,
		Latitude:             arg.Latitude,
		ReadyAt:              pgtype.Timestamptz{Valid: true},
	}
	f.transformed[arg.ApplicationReference] = row
	f.transformedByID[row.ID] = arg.ApplicationReference
	return row, nil
}

func (f *fakeQuerier) MarkTransformedFormEmailed(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref, ok := f.transformedByID[id]
	if !ok {
		return pgx.ErrNoRows
	}
	row := f.transformed[ref]
	row.EmailedAt = pgtype.Timestamptz{Valid: true}
	f.transformed[ref] = row
	return nil
}

func (f *fakeQuerier) GetIngestedFormByApplicationReference(context.Context, string) (store.IngestedForm, error) {
	panic("not implemented: unused by worker tests")
}

func (f *fakeQuerier) InsertIngestedForm(context.Context, store.InsertIngestedFormParams) (store.IngestedForm, error) {
	panic("not implemented: unused by worker tests")
}

func (f *fakeQuerier) RequeueForRetry(context.Context, string) (store.IngestedForm, error) {
	panic("not implemented: unused by worker tests")
}

func (f *fakeQuerier) Ping(context.Context) (int32, error) {
	panic("not implemented: unused by worker tests")
}

var _ store.Querier = (*fakeQuerier)(nil)
