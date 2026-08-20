package app

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"take-home-test-go/internal/store"
)

type fakeQuerier struct {
	byApplicationReference            map[string]store.IngestedForm
	transformedByApplicationReference map[string]store.TransformedForm
	nextID                            int64
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		byApplicationReference:            make(map[string]store.IngestedForm),
		transformedByApplicationReference: make(map[string]store.TransformedForm),
	}
}

func (f *fakeQuerier) withRow(row store.IngestedForm) *fakeQuerier {
	f.nextID++
	if row.ID == 0 {
		row.ID = f.nextID
	}
	f.byApplicationReference[row.ApplicationReference] = row
	return f
}

func (f *fakeQuerier) withTransformedRow(row store.TransformedForm) *fakeQuerier {
	f.transformedByApplicationReference[row.ApplicationReference] = row
	return f
}

func (f *fakeQuerier) InsertIngestedForm(_ context.Context, arg store.InsertIngestedFormParams) (store.IngestedForm, error) {
	if _, exists := f.byApplicationReference[arg.ApplicationReference]; exists {
		return store.IngestedForm{}, pgx.ErrNoRows
	}
	f.nextID++
	row := store.IngestedForm{
		ID:                   f.nextID,
		ApplicationReference: arg.ApplicationReference,
		SessionID:            arg.SessionID,
		RawPayload:           arg.RawPayload,
		Status:               "pending",
	}
	f.byApplicationReference[arg.ApplicationReference] = row
	return row, nil
}

func (f *fakeQuerier) GetIngestedFormByApplicationReference(_ context.Context, applicationReference string) (store.IngestedForm, error) {
	row, ok := f.byApplicationReference[applicationReference]
	if !ok {
		return store.IngestedForm{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeQuerier) RequeueForRetry(_ context.Context, applicationReference string) (store.IngestedForm, error) {
	row, ok := f.byApplicationReference[applicationReference]
	if !ok || row.Status != "failed" {
		return store.IngestedForm{}, pgx.ErrNoRows
	}
	row.Status = "pending"
	row.AttemptCount = 0
	f.byApplicationReference[applicationReference] = row
	return row, nil
}

func (f *fakeQuerier) ClaimPendingIngestedForms(context.Context, store.ClaimPendingIngestedFormsParams) ([]store.IngestedForm, error) {
	panic("not implemented: unused by handler tests")
}

func (f *fakeQuerier) GetTransformedFormByApplicationReference(_ context.Context, applicationReference string) (store.TransformedForm, error) {
	row, ok := f.transformedByApplicationReference[applicationReference]
	if !ok {
		return store.TransformedForm{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeQuerier) InsertTransformedForm(context.Context, store.InsertTransformedFormParams) (store.TransformedForm, error) {
	panic("not implemented: unused by handler tests")
}

func (f *fakeQuerier) MarkIngestedFormFailed(context.Context, store.MarkIngestedFormFailedParams) error {
	panic("not implemented: unused by handler tests")
}

func (f *fakeQuerier) MarkIngestedFormSucceeded(context.Context, int64) error {
	panic("not implemented: unused by handler tests")
}

func (f *fakeQuerier) MarkTransformedFormEmailed(context.Context, int64) error {
	panic("not implemented: unused by handler tests")
}

func (f *fakeQuerier) ReclaimStaleProcessing(context.Context, pgtype.Interval) (int64, error) {
	panic("not implemented: unused by handler tests")
}

func (f *fakeQuerier) Ping(context.Context) (int32, error) {
	panic("not implemented: unused by handler tests")
}

var _ store.Querier = (*fakeQuerier)(nil)
