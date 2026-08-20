package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleIngest_ValidPayload_Returns202Accepted(t *testing.T) {
	srv := httptest.NewServer(New(newFakeQuerier(), nil))
	defer srv.Close()

	body := `{"application_reference":"APP-1","session_id":"sess-1","name":"John Doe"}`
	resp, err := srv.Client().Post(srv.URL+"/ingest", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}
	var got ingestResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != "accepted" || got.ApplicationReference != "APP-1" {
		t.Errorf("got %+v, want status=accepted application_reference=APP-1", got)
	}
}

func TestHandleIngest_DuplicateApplicationReference_ReturnsDuplicateIgnored(t *testing.T) {
	srv := httptest.NewServer(New(newFakeQuerier(), nil))
	defer srv.Close()

	body := `{"application_reference":"APP-1","session_id":"sess-1"}`
	first, err := srv.Client().Post(srv.URL+"/ingest", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("expected first insert to return 202, got %d", first.StatusCode)
	}

	// Redelivery under a different session_id (simulating the upstream
	// retrying with a fresh delivery attempt) but same application_reference.
	replay := `{"application_reference":"APP-1","session_id":"sess-2-replay"}`
	second, err := srv.Client().Post(srv.URL+"/ingest", "application/json", bytes.NewBufferString(replay))
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer second.Body.Close()

	// A redelivery is expected upstream behavior, not a client error — it
	// still gets 202, not 409/400.
	if second.StatusCode != http.StatusAccepted {
		t.Fatalf("expected duplicate to also return 202, got %d", second.StatusCode)
	}
	var got ingestResponse
	if err := json.NewDecoder(second.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != "duplicate_ignored" {
		t.Errorf("got status %q, want duplicate_ignored", got.Status)
	}
}

func TestHandleIngest_MalformedJSON_Returns400(t *testing.T) {
	srv := httptest.NewServer(New(newFakeQuerier(), nil))
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/ingest", "application/json", bytes.NewBufferString("{not valid json"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleIngest_MissingApplicationReference_Returns400(t *testing.T) {
	srv := httptest.NewServer(New(newFakeQuerier(), nil))
	defer srv.Close()

	body := `{"session_id":"sess-1"}`
	resp, err := srv.Client().Post(srv.URL+"/ingest", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleIngest_SchemaDrift_UnknownFieldsDoNotBreakIngestion(t *testing.T) {
	srv := httptest.NewServer(New(newFakeQuerier(), nil))
	defer srv.Close()

	// An upstream schema change adds a field we don't know about yet — this
	// must not fail ingestion (unknown-field tolerance is the whole point of
	// not using DisallowUnknownFields).
	body := `{"application_reference":"APP-1","session_id":"sess-1","a_brand_new_field_we_have_never_seen":"???"}`
	resp, err := srv.Client().Post(srv.URL+"/ingest", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected status 202, got %d", resp.StatusCode)
	}
}

func TestHandleIngest_TypeDriftOnExistingField_StillStoresRow(t *testing.T) {
	fq := newFakeQuerier()
	srv := httptest.NewServer(New(fq, nil))
	defer srv.Close()

	body := `{"application_reference":"APP-1","session_id":"sess-1","date_of_birth":20260102,"gender":["male","female"]}`
	resp, err := srv.Client().Post(srv.URL+"/ingest", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202 (drift on an existing field must not block ingest), got %d", resp.StatusCode)
	}

	row := fq.byApplicationReference["APP-1"]
	if row.Status != "pending" {
		t.Errorf("row status = %q, want pending", row.Status)
	}
	if row.RawPayload == nil || string(row.RawPayload) != body {
		t.Errorf("row raw_payload = %s, want the exact request body %s (untouched for the worker to parse)", row.RawPayload, body)
	}
}

func TestHandleIngest_ApplicationReferenceAsNumber_CoercedToString(t *testing.T) {
	fq := newFakeQuerier()
	srv := httptest.NewServer(New(fq, nil))
	defer srv.Close()

	body := `{"application_reference":12345,"session_id":"sess-1"}`
	resp, err := srv.Client().Post(srv.URL+"/ingest", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}
	var got ingestResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ApplicationReference != "12345" {
		t.Errorf("application_reference = %q, want the number coerced to \"12345\"", got.ApplicationReference)
	}
	if _, ok := fq.byApplicationReference["12345"]; !ok {
		t.Errorf("row not stored under coerced key \"12345\"")
	}
}

func TestHandleIngest_ApplicationReferenceIsObject_Returns400(t *testing.T) {
	fq := newFakeQuerier()
	srv := httptest.NewServer(New(fq, nil))
	defer srv.Close()

	body := `{"application_reference":{"nested":true},"session_id":"sess-1"}`
	resp, err := srv.Client().Post(srv.URL+"/ingest", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
	if len(fq.byApplicationReference) != 0 {
		t.Errorf("no row should have been stored, got %v", fq.byApplicationReference)
	}
}
