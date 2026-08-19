package app

import (
	"net/http/httptest"
	"testing"
)

// TestIngestReturns200 mirrors reference-ts/tests/app.test.ts: the single
// existing assertion is that POST /ingest returns 200.
func TestIngestReturns200(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/ingest", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}
