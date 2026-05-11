package thoth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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

func TestBackfillGovernanceEvidenceFallsBackOnMethodNotAllowed(t *testing.T) {
	t.Parallel()

	var gotPaths []string
	var gotAuth string
	var gotPayload map[string]any
	decodeErrCh := make(chan error, 2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			decodeErrCh <- err
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		decodeErrCh <- nil
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/delta-arc/governance/evidence/thoth/backfill":
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"error":"method_not_allowed","message":"Method Not Allowed"}`))
		case "/delta-arc/thoth/governance/evidence/thoth/backfill":
			_, _ = w.Write([]byte(`{"created":3,"evidence_ids":["e3"]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
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
		t.Fatalf("decode request body (attempt 1): %v", decodeErr)
	}
	if decodeErr := <-decodeErrCh; decodeErr != nil {
		t.Fatalf("decode request body (attempt 2): %v", decodeErr)
	}

	if len(gotPaths) != 2 {
		t.Fatalf("request count = %d, want 2", len(gotPaths))
	}
	if gotPaths[0] != "/delta-arc/governance/evidence/thoth/backfill" {
		t.Fatalf("first path = %q", gotPaths[0])
	}
	if gotPaths[1] != "/delta-arc/thoth/governance/evidence/thoth/backfill" {
		t.Fatalf("second path = %q", gotPaths[1])
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("authorization header = %q", gotAuth)
	}
	if gotPayload["limit"] != float64(50) {
		t.Fatalf("payload.limit = %#v", gotPayload["limit"])
	}
	if created := resp["created"]; created != float64(3) {
		t.Fatalf("response.created = %#v", created)
	}
}

func TestBackfillGovernanceDecisionFieldsPostsExpectedPathAndPayload(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"updated":3,"row_ids":["1","2","3"]}`))
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

	resp, err := client.BackfillGovernanceDecisionFields(context.Background(), map[string]any{
		"limit":                  50,
		"window_hours":           720,
		"include_blocked_events": true,
		"dry_run":                false,
	})
	if err != nil {
		t.Fatalf("BackfillGovernanceDecisionFields() error = %v", err)
	}
	if decodeErr := <-decodeErrCh; decodeErr != nil {
		t.Fatalf("decode request body: %v", decodeErr)
	}

	if gotPath != "/delta-arc/thoth/governance/backfill-decision-fields" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("authorization header = %q", gotAuth)
	}
	if gotPayload["window_hours"] != float64(720) {
		t.Fatalf("payload.window_hours = %#v", gotPayload["window_hours"])
	}
	if updated := resp["updated"]; updated != float64(3) {
		t.Fatalf("response.updated = %#v", updated)
	}
}

func TestGetTenantSettingsUsesAPIKeyAuthWhenAutoDetected(t *testing.T) {
	t.Parallel()

	var gotAPIKey string
	var gotAuth string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("X-Api-Key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enforceMcpPolicies":true}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientOptions{
		TenantID:   "delta-arc",
		APIBaseURL: srv.URL,
		AuthToken:  "thoth_example_api_key",
		AuthMode:   "auto",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.GetTenantSettings(context.Background()); err != nil {
		t.Fatalf("GetTenantSettings() error = %v", err)
	}

	if gotPath != "/delta-arc/thoth/settings" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAPIKey != "thoth_example_api_key" {
		t.Fatalf("X-Api-Key header = %q", gotAPIKey)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization header should be empty, got %q", gotAuth)
	}
}

func TestExportDecisionMetadataSetsQueryAndProvisioningHeaders(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery url.Values
	var gotUserAgent string
	var gotProvisionedVia string
	var gotProvisioner string
	var gotProvisionerVersion string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotUserAgent = r.Header.Get("User-Agent")
		gotProvisionedVia = r.Header.Get("X-Aten-Provisioned-Via")
		gotProvisioner = r.Header.Get("X-Aten-Provisioner")
		gotProvisionerVersion = r.Header.Get("X-Aten-Provisioner-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"record_count":2,"approval_count":1}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientOptions{
		TenantID:           "delta-arc",
		APIBaseURL:         srv.URL,
		AuthToken:          "test-token",
		UserAgent:          "thoth-operator/0.1.11",
		ProvisionedVia:     "kubernetes_operator",
		Provisioner:        "thoth-operator",
		ProvisionerVersion: "0.1.11",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	from := time.Date(2026, 5, 10, 1, 2, 3, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	if _, err := client.ExportDecisionMetadata(context.Background(), from, to, 777); err != nil {
		t.Fatalf("ExportDecisionMetadata() error = %v", err)
	}

	if gotPath != "/delta-arc/thoth/governance/decision-metadata/export" {
		t.Fatalf("path = %q", gotPath)
	}
	if got := gotQuery.Get("from"); got != "2026-05-10T01:02:03Z" {
		t.Fatalf("query.from = %q", got)
	}
	if got := gotQuery.Get("to"); got != "2026-05-10T03:02:03Z" {
		t.Fatalf("query.to = %q", got)
	}
	if got := gotQuery.Get("limit"); got != "777" {
		t.Fatalf("query.limit = %q", got)
	}
	if !strings.Contains(gotUserAgent, "thoth-operator/0.1.11") {
		t.Fatalf("User-Agent = %q", gotUserAgent)
	}
	if gotProvisionedVia != "kubernetes_operator" {
		t.Fatalf("X-Aten-Provisioned-Via = %q", gotProvisionedVia)
	}
	if gotProvisioner != "thoth-operator" {
		t.Fatalf("X-Aten-Provisioner = %q", gotProvisioner)
	}
	if gotProvisionerVersion != "0.1.11" {
		t.Fatalf("X-Aten-Provisioner-Version = %q", gotProvisionerVersion)
	}
}

func TestCollectDecisionMetadataPostsMosesTrainingPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotPayload map[string]any
	decodeErrCh := make(chan error, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			decodeErrCh <- err
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		decodeErrCh <- nil
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
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

	_, err = client.CollectDecisionMetadata(context.Background(), map[string]any{
		"tenant_id":    "delta-arc",
		"record_count": 2,
	})
	if err != nil {
		t.Fatalf("CollectDecisionMetadata() error = %v", err)
	}
	if decodeErr := <-decodeErrCh; decodeErr != nil {
		t.Fatalf("decode request body: %v", decodeErr)
	}

	if gotPath != "/delta-arc/thoth/governance/moses/training/decision-metadata/collect" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotPayload["tenant_id"] != "delta-arc" {
		t.Fatalf("payload.tenant_id = %#v", gotPayload["tenant_id"])
	}
}
