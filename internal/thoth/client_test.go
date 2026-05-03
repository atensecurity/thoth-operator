package thoth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClientDerivesAPIBaseURLFromTenantAndApex(t *testing.T) {
	client, err := NewClient(ClientOptions{
		TenantID:   "acme",
		ApexDomain: "atensecurity.com",
		AuthToken:  "token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if got, want := client.EndpointURL(), "https://grid.acme.atensecurity.com"; got != want {
		t.Fatalf("EndpointURL() = %q, want %q", got, want)
	}
}

func TestNewClientUsesExplicitAPIBaseURL(t *testing.T) {
	client, err := NewClient(ClientOptions{
		TenantID:   "acme",
		APIBaseURL: "https://custom.example.com",
		AuthToken:  "token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if got, want := client.EndpointURL(), "https://custom.example.com"; got != want {
		t.Fatalf("EndpointURL() = %q, want %q", got, want)
	}
}

func TestNewClientRequiresTenantAndToken(t *testing.T) {
	if _, err := NewClient(ClientOptions{AuthToken: "token"}); err == nil {
		t.Fatalf("expected error when tenant is missing")
	}
	if _, err := NewClient(ClientOptions{TenantID: "acme"}); err == nil {
		t.Fatalf("expected error when token is missing")
	}
}

func TestBackfillGovernanceEvidencePostsExpectedPathAndPayload(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	var gotPayload map[string]any
	decodeErrCh := make(chan error, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			decodeErrCh <- err
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		decodeErrCh <- nil
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":2,"evidence_ids":["e1","e2"]}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientOptions{
		TenantID:   "delta-arc",
		APIBaseURL: srv.URL,
		AuthToken:  "test-token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.BackfillGovernanceEvidence(context.Background(), map[string]any{
		"limit":                  50,
		"include_blocked_events": true,
		"dry_run":                false,
	})
	if err != nil {
		t.Fatalf("BackfillGovernanceEvidence() error = %v", err)
	}
	if decodeErr := <-decodeErrCh; decodeErr != nil {
		t.Fatalf("decode request body: %v", decodeErr)
	}

	if gotPath != "/delta-arc/governance/evidence/thoth/backfill" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("authorization header = %q", gotAuth)
	}
	if gotPayload["limit"] != float64(50) {
		t.Fatalf("payload.limit = %#v", gotPayload["limit"])
	}
	if created := resp["created"]; created != float64(2) {
		t.Fatalf("response.created = %#v", created)
	}
}
