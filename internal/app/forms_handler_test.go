package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"take-home-test-go/internal/store"
)

func TestHandleGetForm_UnknownApplicationReference_Returns404(t *testing.T) {
	srv := httptest.NewServer(New(newFakeQuerier(), nil))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/forms/NOPE")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestHandleGetForm_PendingRow_NoReadyOrEmailedAt(t *testing.T) {
	receivedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fq := newFakeQuerier().withRow(store.IngestedForm{
		ApplicationReference: "APP-1", Status: "pending", AttemptCount: 0,
		CreatedAt: pgtype.Timestamptz{Time: receivedAt, Valid: true},
	})
	srv := httptest.NewServer(New(fq, nil))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/forms/APP-1")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var got formStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != "pending" {
		t.Errorf("status = %q, want pending", got.Status)
	}
	if got.ReadyAt != nil {
		t.Errorf("ready_at = %v, want nil", got.ReadyAt)
	}
	if got.EmailedAt != nil {
		t.Errorf("emailed_at = %v, want nil", got.EmailedAt)
	}
	if !got.ReceivedAt.Equal(receivedAt) {
		t.Errorf("received_at = %v, want %v", got.ReceivedAt, receivedAt)
	}
}

func TestHandleGetForm_FailedRow_IncludesLastError(t *testing.T) {
	fq := newFakeQuerier().withRow(store.IngestedForm{
		ApplicationReference: "APP-1", Status: "failed", AttemptCount: 3,
		LastError: pgtype.Text{String: "geocode: boom", Valid: true},
	})
	srv := httptest.NewServer(New(fq, nil))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/forms/APP-1")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var got formStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.LastError == nil || *got.LastError != "geocode: boom" {
		t.Errorf("last_error = %v, want \"geocode: boom\"", got.LastError)
	}
	if got.AttemptCount != 3 {
		t.Errorf("attempt_count = %d, want 3", got.AttemptCount)
	}
}

func TestHandleGetForm_SucceededRow_IncludesReadyAndEmailedAt(t *testing.T) {
	readyAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	emailedAt := readyAt.Add(2 * time.Second)
	fq := newFakeQuerier().
		withRow(store.IngestedForm{ApplicationReference: "APP-1", Status: "succeeded"}).
		withTransformedRow(store.TransformedForm{
			ApplicationReference: "APP-1",
			ReadyAt:              pgtype.Timestamptz{Time: readyAt, Valid: true},
			EmailedAt:            pgtype.Timestamptz{Time: emailedAt, Valid: true},
		})
	srv := httptest.NewServer(New(fq, nil))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/forms/APP-1")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var got formStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ReadyAt == nil || !got.ReadyAt.Equal(readyAt) {
		t.Errorf("ready_at = %v, want %v", got.ReadyAt, readyAt)
	}
	if got.EmailedAt == nil || !got.EmailedAt.Equal(emailedAt) {
		t.Errorf("emailed_at = %v, want %v", got.EmailedAt, emailedAt)
	}
}
