package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"take-home-test-go/internal/store"
)

func TestHandleRetry_UnknownApplicationReference_Returns404(t *testing.T) {
	srv := httptest.NewServer(New(newFakeQuerier(), nil))
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/retry/NOPE", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestHandleRetry_NotFailedYet_Returns409WithCurrentStatus(t *testing.T) {
	fq := newFakeQuerier().withRow(store.IngestedForm{
		ApplicationReference: "APP-1", SessionID: "s-1", Status: "processing",
	})
	srv := httptest.NewServer(New(fq, nil))
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/retry/APP-1", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["status"] != "processing" {
		t.Errorf("got status %q, want processing", got["status"])
	}
}

func TestHandleRetry_Failed_Returns202AndResetsToRetryable(t *testing.T) {
	fq := newFakeQuerier().withRow(store.IngestedForm{
		ApplicationReference: "APP-1", SessionID: "s-1", Status: "failed", AttemptCount: 5,
	})
	srv := httptest.NewServer(New(fq, nil))
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/retry/APP-1", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}
	var got retryResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != "retry_scheduled" || got.ApplicationReference != "APP-1" {
		t.Errorf("got %+v, want status=retry_scheduled application_reference=APP-1", got)
	}

	row := fq.byApplicationReference["APP-1"]
	if row.Status != "pending" {
		t.Errorf("row status = %q, want pending", row.Status)
	}
	// A row that had already hit max_attempts must actually become
	// re-claimable, not silently excluded again by the claim query's
	// attempt_count < max_attempts filter.
	if row.AttemptCount != 0 {
		t.Errorf("row attempt_count = %d, want reset to 0", row.AttemptCount)
	}
}

func TestHandleRetry_MissingApplicationReferenceInPath_Returns404(t *testing.T) {
	srv := httptest.NewServer(New(newFakeQuerier(), nil))
	defer srv.Close()

	// POST /retry/ (trailing slash, empty path value) — ServeMux itself
	// won't route this to our handler at all since the {application_reference}
	// segment requires a non-empty match, so this is really testing the
	// route doesn't panic on an edge-case path.
	resp, err := srv.Client().Post(srv.URL+"/retry/", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("expected a clean 4xx, got 500")
	}
}
