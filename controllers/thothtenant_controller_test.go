package controllers

import (
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	platformv1alpha1 "github.com/atensecurity/thoth-operator/api/v1alpha1"
)

func TestDecodeSettingsMap(t *testing.T) {
	settings := map[string]apiextensionsv1.JSON{
		"enforceMcpPolicies": {Raw: []byte(`true`)},
		"approvalMode":       {Raw: []byte(`"step_up"`)},
		"limits":             {Raw: []byte(`{"daily":1000}`)},
	}

	got, err := decodeSettingsMap(settings)
	if err != nil {
		t.Fatalf("decodeSettingsMap() error = %v", err)
	}

	if got["enforceMcpPolicies"] != true {
		t.Fatalf("expected enforceMcpPolicies=true, got %#v", got["enforceMcpPolicies"])
	}
	if got["approvalMode"] != "step_up" {
		t.Fatalf("expected approvalMode=step_up, got %#v", got["approvalMode"])
	}
	limits, ok := got["limits"].(map[string]any)
	if !ok {
		t.Fatalf("expected limits object, got %#v", got["limits"])
	}
	if limits["daily"] != float64(1000) {
		t.Fatalf("expected limits.daily=1000, got %#v", limits["daily"])
	}
}

func TestDecodeSettingsMapInvalidJSON(t *testing.T) {
	settings := map[string]apiextensionsv1.JSON{
		"approvalMode": {Raw: []byte(`"step_up`)},
	}
	if _, err := decodeSettingsMap(settings); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

func TestSecretRefsTenant(t *testing.T) {
	tenant := &platformv1alpha1.ThothTenant{}
	tenant.Spec.AuthSecretRef.Name = "auth-secret"
	tenant.Spec.MDMProvider = &platformv1alpha1.MDMProviderSpec{
		APITokenSecretRef: &platformv1alpha1.SecretKeyReference{Name: "mdm-secret", Key: "token"},
	}

	if !secretRefsTenant(tenant, "auth-secret") {
		t.Fatalf("expected auth secret to match")
	}
	if !secretRefsTenant(tenant, "mdm-secret") {
		t.Fatalf("expected mdm secret to match")
	}
	if secretRefsTenant(tenant, "other-secret") {
		t.Fatalf("did not expect unrelated secret to match")
	}
}

func TestPackAssignmentPayloadDefaultsToAllAgents(t *testing.T) {
	payload, err := packAssignmentPayload(platformv1alpha1.PackAssignmentSpec{
		PackIDs: []string{"soc2-type2", "gdpr-ai-agents"},
	})
	if err != nil {
		t.Fatalf("packAssignmentPayload() error = %v", err)
	}

	allAgents, ok := payload["all_agents"].(bool)
	if !ok || !allAgents {
		t.Fatalf("expected all_agents=true when no selectors are provided")
	}
}

func TestPackAssignmentPayloadRejectsMissingPackIDs(t *testing.T) {
	_, err := packAssignmentPayload(platformv1alpha1.PackAssignmentSpec{})
	if err == nil {
		t.Fatalf("expected error for missing packIds")
	}
}

func TestGovernanceEvidenceBackfillPayloadDefaults(t *testing.T) {
	payload := governanceEvidenceBackfillPayload(platformv1alpha1.GovernanceEvidenceBackfillSpec{
		Enabled: true,
	})

	if payload["limit"] != 200 {
		t.Fatalf("limit = %#v, want 200", payload["limit"])
	}
	if payload["include_blocked_events"] != true {
		t.Fatalf("include_blocked_events = %#v, want true", payload["include_blocked_events"])
	}
	if payload["dry_run"] != false {
		t.Fatalf("dry_run = %#v, want false", payload["dry_run"])
	}
	if _, ok := payload["integration_id"]; ok {
		t.Fatalf("integration_id should be omitted by default")
	}
}

func TestGovernanceEvidenceBackfillPayloadRespectsOverrides(t *testing.T) {
	includeBlockedEvents := false
	payload := governanceEvidenceBackfillPayload(platformv1alpha1.GovernanceEvidenceBackfillSpec{
		Enabled:              true,
		Limit:                4000,
		IncludeBlockedEvents: &includeBlockedEvents,
		IntegrationID:        "thoth-runtime",
		DryRun:               true,
	})

	if payload["limit"] != 1000 {
		t.Fatalf("limit = %#v, want 1000", payload["limit"])
	}
	if payload["include_blocked_events"] != false {
		t.Fatalf("include_blocked_events = %#v, want false", payload["include_blocked_events"])
	}
	if payload["dry_run"] != true {
		t.Fatalf("dry_run = %#v, want true", payload["dry_run"])
	}
	if payload["integration_id"] != "thoth-runtime" {
		t.Fatalf("integration_id = %#v, want thoth-runtime", payload["integration_id"])
	}
}

func TestGovernanceDecisionFieldBackfillPayloadDefaults(t *testing.T) {
	payload := governanceDecisionFieldBackfillPayload(platformv1alpha1.GovernanceDecisionFieldBackfillSpec{
		Enabled: true,
	})

	if payload["limit"] != 500 {
		t.Fatalf("limit = %#v, want 500", payload["limit"])
	}
	if payload["window_hours"] != 24*30 {
		t.Fatalf("window_hours = %#v, want 720", payload["window_hours"])
	}
	if payload["include_blocked_events"] != true {
		t.Fatalf("include_blocked_events = %#v, want true", payload["include_blocked_events"])
	}
	if payload["dry_run"] != false {
		t.Fatalf("dry_run = %#v, want false", payload["dry_run"])
	}
}

func TestGovernanceDecisionFieldBackfillPayloadRespectsOverrides(t *testing.T) {
	includeBlockedEvents := false
	payload := governanceDecisionFieldBackfillPayload(platformv1alpha1.GovernanceDecisionFieldBackfillSpec{
		Enabled:              true,
		Limit:                9999,
		WindowHours:          24 * 365,
		IncludeBlockedEvents: &includeBlockedEvents,
		DryRun:               true,
	})

	if payload["limit"] != 5000 {
		t.Fatalf("limit = %#v, want 5000", payload["limit"])
	}
	if payload["window_hours"] != 24*120 {
		t.Fatalf("window_hours = %#v, want 2880", payload["window_hours"])
	}
	if payload["include_blocked_events"] != false {
		t.Fatalf("include_blocked_events = %#v, want false", payload["include_blocked_events"])
	}
	if payload["dry_run"] != true {
		t.Fatalf("dry_run = %#v, want true", payload["dry_run"])
	}
}
