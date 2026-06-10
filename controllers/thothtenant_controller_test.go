package controllers

import (
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	tenant.Spec.WebhookSettings = &platformv1alpha1.WebhookSettingsSpec{
		SecretRef: &platformv1alpha1.SecretKeyReference{Name: "webhook-secret", Key: "token"},
	}
	tenant.Spec.DecisionMetadataExport = &platformv1alpha1.DecisionMetadataExportSpec{
		AuthTokenSecretRef: &platformv1alpha1.SecretKeyReference{Name: "export-secret", Key: "token"},
	}

	if !secretRefsTenant(tenant, "auth-secret") {
		t.Fatalf("expected auth secret to match")
	}
	if !secretRefsTenant(tenant, "mdm-secret") {
		t.Fatalf("expected mdm secret to match")
	}
	if !secretRefsTenant(tenant, "webhook-secret") {
		t.Fatalf("expected webhook secret to match")
	}
	if !secretRefsTenant(tenant, "export-secret") {
		t.Fatalf("expected decision metadata export secret to match")
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

func TestPackAssignmentPayloadBehavioralControlsMergeIntoOverrides(t *testing.T) {
	mismatchBoost := 0.2
	delegationBoost := 0.1
	trustFloor := 0.4
	criticalThreshold := 0.9

	payload, err := packAssignmentPayload(platformv1alpha1.PackAssignmentSpec{
		PackIDs: []string{"soc2-type2"},
		OverridesByPack: map[string]apiextensionsv1.JSON{
			"soc2-type2": {Raw: []byte(`{"foo":"bar","behavioral_controls":{"existing":1}}`)},
		},
		MismatchBoost:     &mismatchBoost,
		DelegationBoost:   &delegationBoost,
		TrustFloor:        &trustFloor,
		CriticalThreshold: &criticalThreshold,
	})
	if err != nil {
		t.Fatalf("packAssignmentPayload() error = %v", err)
	}

	overrides, ok := payload["overrides_by_pack"].(map[string]map[string]any)
	if !ok {
		t.Fatalf("overrides_by_pack type = %T", payload["overrides_by_pack"])
	}
	row, ok := overrides["soc2-type2"]
	if !ok {
		t.Fatalf("missing overrides for soc2-type2")
	}
	controls, ok := row["behavioral_controls"].(map[string]any)
	if !ok {
		t.Fatalf("behavioral_controls type = %T", row["behavioral_controls"])
	}
	if controls["mismatch_boost"] != mismatchBoost {
		t.Fatalf("mismatch_boost = %#v", controls["mismatch_boost"])
	}
	if controls["delegation_boost"] != delegationBoost {
		t.Fatalf("delegation_boost = %#v", controls["delegation_boost"])
	}
	if controls["trust_floor"] != trustFloor {
		t.Fatalf("trust_floor = %#v", controls["trust_floor"])
	}
	if controls["critical_threshold"] != criticalThreshold {
		t.Fatalf("critical_threshold = %#v", controls["critical_threshold"])
	}
	if controls["existing"] != float64(1) {
		t.Fatalf("existing = %#v", controls["existing"])
	}
}

func TestPolicyBundlePayloadDefaultsAssignmentsAndMode(t *testing.T) {
	payload, err := policyBundlePayload(platformv1alpha1.PolicyBundleSpec{
		Name:      "trantor-mutual-dlp",
		Framework: "opa",
		RawPolicy: "package policy\nallow := true\n",
	})
	if err != nil {
		t.Fatalf("policyBundlePayload() error = %v", err)
	}

	if payload["framework"] != "OPA" {
		t.Fatalf("framework = %#v, want OPA", payload["framework"])
	}
	if payload["status"] != "active" {
		t.Fatalf("status = %#v, want active", payload["status"])
	}
	if payload["enforcement_mode"] != "enforce" {
		t.Fatalf("enforcement_mode = %#v, want enforce", payload["enforcement_mode"])
	}
	assignments, ok := payload["assignments"].([]string)
	if !ok || len(assignments) != 1 || assignments[0] != "all" {
		t.Fatalf("assignments = %#v", payload["assignments"])
	}
}

func TestPolicyBundlePayloadRejectsMixedRawAndSource(t *testing.T) {
	_, err := policyBundlePayload(platformv1alpha1.PolicyBundleSpec{
		Name:      "trantor-mutual-dlp",
		Framework: "OPA",
		RawPolicy: "package policy\nallow := true\n",
		SourceURI: "s3://bucket/policy.rego",
	})
	if err == nil {
		t.Fatalf("expected error when rawPolicy and sourceUri are both set")
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

func TestDecisionMetadataExportDefaultsAndBounds(t *testing.T) {
	if got := decisionMetadataExportInterval(nil); got != 30*time.Minute {
		t.Fatalf("interval(nil) = %s, want 30m", got)
	}
	if got := decisionMetadataExportInterval(&platformv1alpha1.DecisionMetadataExportSpec{IntervalMinutes: 1}); got != 5*time.Minute {
		t.Fatalf("interval(min) = %s, want 5m", got)
	}
	if got := decisionMetadataExportLookback(&platformv1alpha1.DecisionMetadataExportSpec{LookbackHours: 24 * 90}); got != 14*24*time.Hour {
		t.Fatalf("lookback(max) = %s, want 336h", got)
	}
	if got := decisionMetadataExportBatchLimit(&platformv1alpha1.DecisionMetadataExportSpec{BatchLimit: 9000}); got != 5000 {
		t.Fatalf("batchLimit(max) = %d, want 5000", got)
	}
}

func TestDecisionMetadataExportDue(t *testing.T) {
	spec := &platformv1alpha1.DecisionMetadataExportSpec{
		Enabled:         true,
		IntervalMinutes: 30,
	}
	if !decisionMetadataExportDue(spec, nil) {
		t.Fatalf("expected export to be due when no previous timestamp exists")
	}

	fresh := metav1.NewTime(time.Now().Add(-10 * time.Minute))
	if decisionMetadataExportDue(spec, &fresh) {
		t.Fatalf("did not expect export to be due within interval")
	}

	stale := metav1.NewTime(time.Now().Add(-35 * time.Minute))
	if !decisionMetadataExportDue(spec, &stale) {
		t.Fatalf("expected export to be due when interval elapsed")
	}
}

func TestMCPVendorPayloadDefaultsApprovalToFalse(t *testing.T) {
	vendorID, payload, err := mcpVendorPayload(platformv1alpha1.MCPVendorSpec{
		VendorID:     "openai",
		DisplayName:  "OpenAI",
		HostPatterns: []string{"api.openai.com", "*.openai.com"},
	})
	if err != nil {
		t.Fatalf("mcpVendorPayload() error = %v", err)
	}
	if vendorID != "openai" {
		t.Fatalf("vendorID = %q, want openai", vendorID)
	}
	if payload["approved"] != false {
		t.Fatalf("approved = %#v, want false", payload["approved"])
	}
}

func TestMCPControlPayloadRequiresHostsWhenEnabledEnforceMode(t *testing.T) {
	_, err := mcpControlPayload(platformv1alpha1.MCPVendorRegistrySpec{
		Enabled:     true,
		ObserveOnly: false,
		Vendors:     nil,
	})
	if err == nil {
		t.Fatalf("expected error when enabled=true observeOnly=false and no hosts are configured")
	}
}

func TestMCPControlPayloadUsesApprovedHostsByDefault(t *testing.T) {
	approved := true
	mcpControl, err := mcpControlPayload(platformv1alpha1.MCPVendorRegistrySpec{
		Enabled:     true,
		ObserveOnly: false,
		Vendors: []platformv1alpha1.MCPVendorSpec{
			{
				VendorID:     "openai",
				DisplayName:  "OpenAI",
				Approved:     &approved,
				HostPatterns: []string{"api.openai.com"},
			},
			{
				VendorID:     "other",
				DisplayName:  "Other",
				HostPatterns: []string{"api.other.com"},
			},
		},
	})
	if err != nil {
		t.Fatalf("mcpControlPayload() error = %v", err)
	}

	hosts, ok := mcpControl["approved_host_patterns"].([]string)
	if !ok {
		t.Fatalf("approved_host_patterns type = %T", mcpControl["approved_host_patterns"])
	}
	if len(hosts) != 1 || hosts[0] != "api.openai.com" {
		t.Fatalf("approved_host_patterns = %#v", hosts)
	}
}

func TestNormalizeMCPInventoryWindowHours(t *testing.T) {
	if got := normalizeMCPInventoryWindowHours(0); got != 24*7 {
		t.Fatalf("window(0) = %d, want 168", got)
	}
	if got := normalizeMCPInventoryWindowHours(24 * 365); got != 24*180 {
		t.Fatalf("window(max) = %d, want 4320", got)
	}
	if got := normalizeMCPInventoryWindowHours(96); got != 96 {
		t.Fatalf("window(96) = %d, want 96", got)
	}
}

func TestSummarizeMCPInventoryReport(t *testing.T) {
	report := map[string]any{
		"data": []any{
			map[string]any{"endpoint_id": "ep-1", "unapproved_calls": 3},
			map[string]any{"endpoint_id": "ep-2", "unapproved_calls": 0},
			map[string]any{"endpoint_id": "ep-3", "unapproved_calls": 1},
		},
	}
	endpoints, unapprovedEndpoints, unapprovedCalls := summarizeMCPInventoryReport(report)
	if endpoints != 3 {
		t.Fatalf("endpoint count = %d, want 3", endpoints)
	}
	if unapprovedEndpoints != 2 {
		t.Fatalf("unapproved endpoint count = %d, want 2", unapprovedEndpoints)
	}
	if unapprovedCalls != 4 {
		t.Fatalf("unapproved call count = %d, want 4", unapprovedCalls)
	}
}

func TestMCPCatalogVerifyPayloadRequiresPrincipalSignals(t *testing.T) {
	_, _, err := mcpCatalogVerifyPayload(platformv1alpha1.MCPCatalogVerifySpec{Enabled: true})
	if err == nil {
		t.Fatalf("expected error when payload seeds are missing")
	}
}

func TestMCPCatalogVerifyPayloadBuildsAuthContext(t *testing.T) {
	environment, payload, err := mcpCatalogVerifyPayload(platformv1alpha1.MCPCatalogVerifySpec{
		Enabled:     true,
		Environment: "prod",
		Principal:   "agent:ops",
		HumanGroups: []string{"admins", "security"},
		AuthContext: map[string]apiextensionsv1.JSON{
			"principal_type": {Raw: []byte(`"human"`)},
			"risk_score":     {Raw: []byte(`0.91`)},
		},
	})
	if err != nil {
		t.Fatalf("mcpCatalogVerifyPayload() error = %v", err)
	}
	if environment != "prod" {
		t.Fatalf("environment = %q, want prod", environment)
	}
	if payload["principal"] != "agent:ops" {
		t.Fatalf("payload.principal = %#v", payload["principal"])
	}
	authContext, ok := payload["auth_context"].(map[string]any)
	if !ok {
		t.Fatalf("auth_context type = %T", payload["auth_context"])
	}
	if authContext["principal_type"] != "human" {
		t.Fatalf("auth_context.principal_type = %#v", authContext["principal_type"])
	}
	if authContext["risk_score"] != float64(0.91) {
		t.Fatalf("auth_context.risk_score = %#v", authContext["risk_score"])
	}
}
