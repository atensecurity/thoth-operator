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
