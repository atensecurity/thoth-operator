package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/atensecurity/thoth-operator/api/v1alpha1"
	"github.com/atensecurity/thoth-operator/internal/thoth"
)

const (
	conditionReady = "Ready"
	phaseReady     = "Ready"
	phaseError     = "Error"

	indexAuthSecretName          = "spec.authSecretRef.name"
	indexMDMSecretName           = "spec.mdmProvider.apiTokenSecretRef.name"
	indexWebhookSecretName       = "spec.webhookSettings.secretRef.name"
	indexDecisionExportSecretRef = "spec.decisionMetadataExport.authTokenSecretRef.name"
)

type ThothTenantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ThothTenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("thothtenant", req.NamespacedName)

	var tenant platformv1alpha1.ThothTenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !tenant.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &tenant)
	}

	if !controllerutil.ContainsFinalizer(&tenant, platformv1alpha1.ThothTenantFinalizer) {
		base := tenant.DeepCopy()
		controllerutil.AddFinalizer(&tenant, platformv1alpha1.ThothTenantFinalizer)
		if err := r.Patch(ctx, &tenant, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	authToken, err := r.secretValue(ctx, tenant.Namespace, tenant.Spec.AuthSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve auth secret")
		r.setNotReadyStatus(ctx, &tenant, "AuthSecretError", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	operatorVersion := strings.TrimSpace(os.Getenv("OPERATOR_VERSION"))
	if operatorVersion == "" {
		operatorVersion = "dev"
	}
	thothClient, err := thoth.NewClient(thoth.ClientOptions{
		TenantID:           tenant.Spec.TenantID,
		ApexDomain:         tenant.Spec.ApexDomain,
		APIBaseURL:         tenant.Spec.APIBaseURL,
		AuthToken:          authToken,
		AuthMode:           tenant.Spec.AuthMode,
		UserAgent:          fmt.Sprintf("thoth-operator/%s", operatorVersion),
		ProvisionedVia:     "kubernetes_operator",
		Provisioner:        "thoth-operator",
		ProvisionerVersion: operatorVersion,
	})
	if err != nil {
		r.setNotReadyStatus(ctx, &tenant, "ClientConfigError", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	generationChanged := tenant.Status.ObservedGeneration != tenant.Generation
	status := tenant.DeepCopy()

	if err := r.reconcileTenantSettings(ctx, &tenant, thothClient, generationChanged); err != nil {
		r.setNotReadyStatus(ctx, &tenant, "SettingsApplyError", err.Error())
		return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
	}

	if tenant.Spec.MDMProvider != nil {
		payload, mdmErr := r.mdmPayload(ctx, &tenant)
		if mdmErr != nil {
			r.setNotReadyStatus(ctx, &tenant, "MDMConfigError", mdmErr.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
		if mdmErr := thothClient.UpsertMDMProvider(ctx, payload); mdmErr != nil {
			r.setNotReadyStatus(ctx, &tenant, "MDMApplyError", mdmErr.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
	}

	if generationChanged && tenant.Spec.MDMSync != nil && tenant.Spec.MDMSync.Enabled {
		jobID, jobStatus, syncErr := r.runMDMSync(ctx, &tenant, thothClient)
		if syncErr != nil {
			r.setNotReadyStatus(ctx, &tenant, "MDMSyncError", syncErr.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
		now := metav1.Now()
		tenant.Status.LastMDMSyncAt = &now
		tenant.Status.LastMDMSyncJobID = jobID
		tenant.Status.LastMDMSyncStatus = jobStatus
	}

	if generationChanged && len(tenant.Spec.PolicyBundles) > 0 {
		bundles, bundleErr := r.applyPolicyBundles(ctx, thothClient, tenant.Spec.PolicyBundles)
		if bundleErr != nil {
			r.setNotReadyStatus(ctx, &tenant, "PolicyBundleApplyError", bundleErr.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
		tenant.Status.AppliedPolicyBundles = bundles
		now := metav1.Now()
		tenant.Status.LastPolicyBundleApplyAt = &now
	}

	if generationChanged && len(tenant.Spec.PackAssignments) > 0 {
		for i, assignment := range tenant.Spec.PackAssignments {
			payload, payloadErr := packAssignmentPayload(assignment)
			if payloadErr != nil {
				r.setNotReadyStatus(ctx, &tenant, "PackAssignmentConfigError", fmt.Sprintf("packAssignments[%d]: %v", i, payloadErr))
				return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
			}
			if applyErr := thothClient.ApplyPacksBulk(ctx, payload); applyErr != nil {
				r.setNotReadyStatus(ctx, &tenant, "PackAssignmentApplyError", fmt.Sprintf("packAssignments[%d]: %v", i, applyErr))
				return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
			}
		}
	}

	if generationChanged && tenant.Spec.GovernanceEvidenceBackfill != nil && tenant.Spec.GovernanceEvidenceBackfill.Enabled {
		payload := governanceEvidenceBackfillPayload(*tenant.Spec.GovernanceEvidenceBackfill)
		if _, backfillErr := thothClient.BackfillGovernanceEvidence(ctx, payload); backfillErr != nil {
			r.setNotReadyStatus(ctx, &tenant, "GovernanceEvidenceBackfillError", backfillErr.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
		now := metav1.Now()
		tenant.Status.LastGovernanceEvidenceBackfillAt = &now
	}

	if generationChanged && tenant.Spec.GovernanceDecisionFieldBackfill != nil && tenant.Spec.GovernanceDecisionFieldBackfill.Enabled {
		payload := governanceDecisionFieldBackfillPayload(*tenant.Spec.GovernanceDecisionFieldBackfill)
		if _, backfillErr := thothClient.BackfillGovernanceDecisionFields(ctx, payload); backfillErr != nil {
			r.setNotReadyStatus(ctx, &tenant, "GovernanceDecisionFieldBackfillError", backfillErr.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
		now := metav1.Now()
		tenant.Status.LastGovernanceDecisionFieldBackfillAt = &now
	}

	if tenant.Spec.DecisionMetadataExport != nil && tenant.Spec.DecisionMetadataExport.Enabled {
		exportDue := decisionMetadataExportDue(tenant.Spec.DecisionMetadataExport, tenant.Status.LastDecisionMetadataExportAt)
		if exportDue {
			recordCount, approvalCount, exportErr := r.exportDecisionMetadata(ctx, &tenant, thothClient)
			if exportErr != nil {
				tenant.Status.LastDecisionMetadataExportStatus = "error: " + exportErr.Error()
				r.setNotReadyStatus(ctx, &tenant, "DecisionMetadataExportError", exportErr.Error())
				return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
			}
			now := metav1.Now()
			tenant.Status.LastDecisionMetadataExportAt = &now
			tenant.Status.LastDecisionMetadataRecordCount = recordCount
			tenant.Status.LastDecisionMetadataApprovalCount = approvalCount
			tenant.Status.LastDecisionMetadataExportStatus = "delivered"
		}
	}

	if generationChanged && tenant.Spec.PolicySync {
		if syncErr := thothClient.TriggerPolicySync(ctx); syncErr != nil {
			r.setNotReadyStatus(ctx, &tenant, "PolicySyncError", syncErr.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
		now := metav1.Now()
		tenant.Status.LastPolicySyncAt = &now
	}

	tenant.Status.Phase = phaseReady
	tenant.Status.EndpointURL = thothClient.EndpointURL()
	tenant.Status.ObservedGeneration = tenant.Generation
	setCondition(&tenant.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Reconciled",
		Message:            "Thoth control-plane resources are configured",
		ObservedGeneration: tenant.Generation,
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Patch(ctx, &tenant, client.MergeFrom(status)); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	requeueAfter := 30 * time.Minute
	if tenant.Spec.DecisionMetadataExport != nil && tenant.Spec.DecisionMetadataExport.Enabled {
		interval := decisionMetadataExportInterval(tenant.Spec.DecisionMetadataExport)
		if interval < requeueAfter {
			requeueAfter = interval
		}
	}

	logger.Info("reconciled thoth tenant", "tenantId", tenant.Spec.TenantID)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *ThothTenantReconciler) reconcileTenantSettings(
	ctx context.Context,
	tenant *platformv1alpha1.ThothTenant,
	thothClient *thoth.Client,
	generationChanged bool,
) error {
	existing := map[string]any{}
	current, err := thothClient.GetTenantSettings(ctx)
	if err == nil && len(current) > 0 {
		existing = current
	}

	payload := cloneMap(existing)
	decodedSettings, err := decodeSettingsMap(tenant.Spec.Settings)
	if err != nil {
		return fmt.Errorf("decode settings: %w", err)
	}
	for key, value := range decodedSettings {
		payload[key] = value
	}

	if tenant.Spec.WebhookSettings != nil {
		webhook := mapFromAny(payload["webhook"])
		if tenant.Spec.WebhookSettings.Enabled != nil {
			webhook["enabled"] = *tenant.Spec.WebhookSettings.Enabled
		}
		if url := strings.TrimSpace(tenant.Spec.WebhookSettings.URL); url != "" {
			webhook["url"] = url
		}
		if tenant.Spec.WebhookSettings.SecretRef != nil {
			secret, secretErr := r.secretValue(ctx, tenant.Namespace, *tenant.Spec.WebhookSettings.SecretRef)
			if secretErr != nil {
				return fmt.Errorf("resolve webhookSettings.secretRef: %w", secretErr)
			}
			webhook["secret"] = secret
		}
		payload["webhook"] = webhook
	}

	if len(payload) > 0 {
		if err := thothClient.UpdateTenantSettings(ctx, payload); err != nil {
			return err
		}
	}

	if generationChanged && tenant.Spec.WebhookSettings != nil && tenant.Spec.WebhookSettings.TestWebhookOnApply {
		resp, err := thothClient.TestWebhook(ctx)
		if err != nil {
			return fmt.Errorf("webhook test failed: %w", err)
		}
		now := metav1.Now()
		tenant.Status.LastWebhookTestAt = &now
		tenant.Status.LastWebhookTestStatus = strings.TrimSpace(stringFromAny(resp["status"]))
		if tenant.Status.LastWebhookTestStatus == "" {
			tenant.Status.LastWebhookTestStatus = "delivered"
		}
	}

	return nil
}

func (r *ThothTenantReconciler) runMDMSync(
	ctx context.Context,
	tenant *platformv1alpha1.ThothTenant,
	thothClient *thoth.Client,
) (string, string, error) {
	providerName := strings.TrimSpace(tenant.Spec.MDMSync.ProviderName)
	if providerName == "" && tenant.Spec.MDMProvider != nil {
		providerName = strings.TrimSpace(tenant.Spec.MDMProvider.Provider)
	}
	if providerName == "" {
		return "", "", fmt.Errorf("mdmSync.providerName is required when mdmSync.enabled=true")
	}

	resp, err := thothClient.StartMDMSync(ctx, providerName)
	if err != nil {
		return "", "", err
	}
	jobID := strings.TrimSpace(stringFromAny(resp["job_id"]))
	status := strings.TrimSpace(stringFromAny(resp["status"]))
	if status == "" {
		status = "queued"
	}

	wait := true
	if tenant.Spec.MDMSync.WaitForCompletion != nil {
		wait = *tenant.Spec.MDMSync.WaitForCompletion
	}
	if !wait || jobID == "" {
		return jobID, status, nil
	}

	pollInterval := 5 * time.Second
	if tenant.Spec.MDMSync.PollIntervalSeconds > 0 {
		pollInterval = time.Duration(tenant.Spec.MDMSync.PollIntervalSeconds) * time.Second
	}
	timeout := 5 * time.Minute
	if tenant.Spec.MDMSync.TimeoutSeconds > 0 {
		timeout = time.Duration(tenant.Spec.MDMSync.TimeoutSeconds) * time.Second
	}

	jobStatus, pollErr := pollMDMSyncJob(ctx, thothClient, jobID, pollInterval, timeout)
	if pollErr != nil {
		return jobID, status, pollErr
	}
	return jobID, jobStatus, nil
}

func pollMDMSyncJob(ctx context.Context, thothClient *thoth.Client, jobID string, interval time.Duration, timeout time.Duration) (string, error) {
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	status := "queued"
	for {
		row, err := thothClient.GetMDMSyncJob(deadlineCtx, jobID)
		if err != nil {
			return status, err
		}
		status = strings.ToLower(strings.TrimSpace(stringFromAny(row["status"])))
		switch status {
		case "succeeded", "failed":
			return status, nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-deadlineCtx.Done():
			timer.Stop()
			return status, deadlineCtx.Err()
		case <-timer.C:
		}
	}
}

func (r *ThothTenantReconciler) applyPolicyBundles(
	ctx context.Context,
	thothClient *thoth.Client,
	specs []platformv1alpha1.PolicyBundleSpec,
) ([]platformv1alpha1.AppliedPolicyBundleStatus, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	applied := make([]platformv1alpha1.AppliedPolicyBundleStatus, 0, len(specs))
	for i, spec := range specs {
		payload, err := policyBundlePayload(spec)
		if err != nil {
			return nil, fmt.Errorf("policyBundles[%d]: %w", i, err)
		}
		row, err := thothClient.CreatePolicyBundle(ctx, payload)
		if err != nil {
			return nil, fmt.Errorf("policyBundles[%d] create: %w", i, err)
		}
		applied = append(applied, platformv1alpha1.AppliedPolicyBundleStatus{
			Name:            strings.TrimSpace(stringFromAny(row["name"])),
			BundleID:        strings.TrimSpace(stringFromAny(row["id"])),
			Framework:       strings.TrimSpace(firstNonEmpty(stringFromAny(row["framework"]), spec.Framework)),
			Version:         int64(floatFromAny(row["version"])),
			PolicyHash:      strings.TrimSpace(stringFromAny(row["policy_hash"])),
			Status:          strings.TrimSpace(firstNonEmpty(stringFromAny(row["status"]), stringFromAny(payload["status"]))),
			EnforcementMode: strings.TrimSpace(firstNonEmpty(stringFromAny(row["enforcement_mode"]), stringFromAny(payload["enforcement_mode"]))),
			AppliedAt:       metav1.Now(),
		})
	}
	return applied, nil
}

func (r *ThothTenantReconciler) exportDecisionMetadata(
	ctx context.Context,
	tenant *platformv1alpha1.ThothTenant,
	thothClient *thoth.Client,
) (int64, int64, error) {
	spec := tenant.Spec.DecisionMetadataExport
	if spec == nil {
		return 0, 0, nil
	}

	limit := decisionMetadataExportBatchLimit(spec)
	from := time.Now().UTC().Add(-decisionMetadataExportLookback(spec))
	if tenant.Status.LastDecisionMetadataExportAt != nil && !tenant.Status.LastDecisionMetadataExportAt.IsZero() {
		from = tenant.Status.LastDecisionMetadataExportAt.Time.UTC().Add(-1 * time.Minute)
	}
	to := time.Now().UTC()

	payload, err := thothClient.ExportDecisionMetadata(ctx, from, to, limit)
	if err != nil {
		return 0, 0, err
	}

	recordCount := int64(floatFromAny(payload["record_count"]))
	approvalCount := int64(floatFromAny(payload["approval_count"]))
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal decision metadata export payload: %w", err)
	}

	destinationURL := strings.TrimSpace(spec.DestinationURL)
	if destinationURL == "" {
		if _, err := thothClient.CollectDecisionMetadata(ctx, payload); err != nil {
			return 0, 0, fmt.Errorf("collect decision metadata: %w", err)
		}
		return recordCount, approvalCount, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, destinationURL, bytes.NewReader(body))
	if err != nil {
		return 0, 0, fmt.Errorf("build destination request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Aten-Source", "thoth-operator")
	req.Header.Set("X-Aten-Tenant-ID", tenant.Spec.TenantID)

	if spec.AuthTokenSecretRef != nil {
		token, tokenErr := r.secretValue(ctx, tenant.Namespace, *spec.AuthTokenSecretRef)
		if tokenErr != nil {
			return 0, 0, fmt.Errorf("resolve decisionMetadataExport.authTokenSecretRef: %w", tokenErr)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("ship decision metadata export: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, fmt.Errorf("destination returned status %d", resp.StatusCode)
	}

	return recordCount, approvalCount, nil
}

func decisionMetadataExportInterval(spec *platformv1alpha1.DecisionMetadataExportSpec) time.Duration {
	if spec == nil || spec.IntervalMinutes <= 0 {
		return 30 * time.Minute
	}
	if spec.IntervalMinutes < 5 {
		return 5 * time.Minute
	}
	return time.Duration(spec.IntervalMinutes) * time.Minute
}

func decisionMetadataExportLookback(spec *platformv1alpha1.DecisionMetadataExportSpec) time.Duration {
	if spec == nil || spec.LookbackHours <= 0 {
		return 24 * time.Hour
	}
	if spec.LookbackHours > 24*14 {
		return 24 * 14 * time.Hour
	}
	return time.Duration(spec.LookbackHours) * time.Hour
}

func decisionMetadataExportBatchLimit(spec *platformv1alpha1.DecisionMetadataExportSpec) int {
	if spec == nil || spec.BatchLimit <= 0 {
		return 500
	}
	if spec.BatchLimit > 5000 {
		return 5000
	}
	return int(spec.BatchLimit)
}

func decisionMetadataExportDue(spec *platformv1alpha1.DecisionMetadataExportSpec, last *metav1.Time) bool {
	if spec == nil || !spec.Enabled {
		return false
	}
	if last == nil || last.IsZero() {
		return true
	}
	return time.Since(last.Time) >= decisionMetadataExportInterval(spec)
}

func (r *ThothTenantReconciler) reconcileDelete(ctx context.Context, tenant *platformv1alpha1.ThothTenant) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(tenant, platformv1alpha1.ThothTenantFinalizer) {
		return ctrl.Result{}, nil
	}
	base := tenant.DeepCopy()
	controllerutil.RemoveFinalizer(tenant, platformv1alpha1.ThothTenantFinalizer)
	if err := r.Patch(ctx, tenant, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ThothTenantReconciler) secretValue(ctx context.Context, namespace string, ref platformv1alpha1.SecretKeyReference) (string, error) {
	name := strings.TrimSpace(ref.Name)
	key := strings.TrimSpace(ref.Key)
	if name == "" || key == "" {
		return "", fmt.Errorf("secret reference requires non-empty name and key")
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &secret); err != nil {
		return "", err
	}
	value, ok := secret.Data[key]
	if !ok || len(value) == 0 {
		return "", fmt.Errorf("secret %s/%s key %q is missing or empty", namespace, name, key)
	}
	return strings.TrimSpace(string(value)), nil
}

func (r *ThothTenantReconciler) mdmPayload(ctx context.Context, tenant *platformv1alpha1.ThothTenant) (map[string]any, error) {
	mdm := tenant.Spec.MDMProvider
	if mdm == nil {
		return nil, fmt.Errorf("mdm provider is nil")
	}
	provider := strings.TrimSpace(mdm.Provider)
	if provider == "" {
		return nil, fmt.Errorf("mdmProvider.provider is required")
	}
	payload := map[string]any{"provider": provider}
	if strings.TrimSpace(mdm.EndpointURL) != "" {
		payload["endpoint_url"] = strings.TrimSpace(mdm.EndpointURL)
	}
	if mdm.Enabled != nil {
		payload["enabled"] = *mdm.Enabled
	}
	if mdm.APITokenSecretRef != nil {
		token, err := r.secretValue(ctx, tenant.Namespace, *mdm.APITokenSecretRef)
		if err != nil {
			return nil, err
		}
		payload["api_key"] = token
	}
	return payload, nil
}

func packAssignmentPayload(spec platformv1alpha1.PackAssignmentSpec) (map[string]any, error) {
	packIDs := uniqueNonEmptyStrings(spec.PackIDs)
	if len(packIDs) == 0 {
		return nil, fmt.Errorf("packIds must include at least one value")
	}

	agentIDs := uniqueNonEmptyStrings(spec.AgentIDs)
	fleetIDs := uniqueNonEmptyStrings(spec.FleetIDs)
	endpointIDs := uniqueNonEmptyStrings(spec.EndpointIDs)

	allAgents := spec.AllAgents
	if !allAgents && len(agentIDs) == 0 && len(fleetIDs) == 0 && len(endpointIDs) == 0 {
		allAgents = true
	}

	payload := map[string]any{
		"pack_ids": packIDs,
	}
	if allAgents {
		payload["all_agents"] = true
	}
	if len(agentIDs) > 0 {
		payload["agent_ids"] = agentIDs
	}
	if len(fleetIDs) > 0 {
		payload["fleet_ids"] = fleetIDs
	}
	if len(endpointIDs) > 0 {
		payload["endpoint_ids"] = endpointIDs
	}
	if environment := strings.TrimSpace(spec.Environment); environment != "" {
		payload["environment"] = environment
	}
	if approvalPolicyID := strings.TrimSpace(spec.ApprovalPolicyID); approvalPolicyID != "" {
		payload["approval_policy_id"] = approvalPolicyID
	}
	overrides := map[string]map[string]any{}
	for packID, raw := range spec.OverridesByPack {
		trimmedPackID := strings.TrimSpace(packID)
		if trimmedPackID == "" || len(raw.Raw) == 0 {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw.Raw, &decoded); err != nil {
			return nil, fmt.Errorf("decode overridesByPack[%s]: %w", trimmedPackID, err)
		}
		if decoded == nil {
			decoded = map[string]any{}
		}
		overrides[trimmedPackID] = decoded
	}

	behavioralControls := map[string]any{}
	if spec.MismatchBoost != nil {
		behavioralControls["mismatch_boost"] = *spec.MismatchBoost
	}
	if spec.DelegationBoost != nil {
		behavioralControls["delegation_boost"] = *spec.DelegationBoost
	}
	if spec.TrustFloor != nil {
		behavioralControls["trust_floor"] = *spec.TrustFloor
	}
	if spec.CriticalThreshold != nil {
		behavioralControls["critical_threshold"] = *spec.CriticalThreshold
	}
	if len(behavioralControls) > 0 {
		for _, packID := range packIDs {
			row := overrides[packID]
			if row == nil {
				row = map[string]any{}
			}
			existingControls := mapFromAny(row["behavioral_controls"])
			for key, value := range behavioralControls {
				existingControls[key] = value
			}
			row["behavioral_controls"] = existingControls
			overrides[packID] = row
		}
	}
	if len(overrides) > 0 {
		payload["overrides_by_pack"] = overrides
	}
	return payload, nil
}

func policyBundlePayload(spec platformv1alpha1.PolicyBundleSpec) (map[string]any, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	framework := strings.ToUpper(strings.TrimSpace(spec.Framework))
	if framework != "OPA" && framework != "CEDAR" {
		return nil, fmt.Errorf("framework must be OPA or CEDAR")
	}
	rawPolicy := strings.TrimSpace(spec.RawPolicy)
	sourceURI := strings.TrimSpace(spec.SourceURI)
	s3URI := strings.TrimSpace(spec.S3URI)
	if rawPolicy == "" && sourceURI == "" && s3URI == "" {
		return nil, fmt.Errorf("set one of rawPolicy or sourceUri/s3Uri")
	}
	if rawPolicy != "" && (sourceURI != "" || s3URI != "") {
		return nil, fmt.Errorf("rawPolicy cannot be combined with sourceUri/s3Uri")
	}
	if sourceURI != "" && s3URI != "" {
		return nil, fmt.Errorf("set either sourceUri or s3Uri, not both")
	}

	payload := map[string]any{
		"name":      name,
		"framework": framework,
	}
	if desc := strings.TrimSpace(spec.Description); desc != "" {
		payload["description"] = desc
	}
	if rawPolicy != "" {
		payload["raw_policy"] = rawPolicy
	}
	if sourceURI != "" {
		payload["source_uri"] = sourceURI
	}
	if s3URI != "" {
		payload["s3_uri"] = s3URI
	}
	if versionID := strings.TrimSpace(spec.S3VersionID); versionID != "" {
		payload["s3_version_id"] = versionID
	}
	if expectedHash := strings.TrimSpace(spec.ExpectedHash); expectedHash != "" {
		payload["expected_hash"] = expectedHash
	}
	assignments := uniqueNonEmptyStrings(spec.Assignments)
	if len(assignments) == 0 {
		assignments = []string{"all"}
	}
	payload["assignments"] = assignments

	status := strings.ToLower(strings.TrimSpace(spec.Status))
	mode := strings.ToLower(strings.TrimSpace(spec.EnforcementMode))
	if status == "" && mode == "" {
		status = "active"
		mode = "enforce"
	}
	if status == "" {
		if mode == "observe" {
			status = "staged"
		} else {
			status = "active"
		}
	}
	if mode == "" {
		if status == "staged" {
			mode = "observe"
		} else {
			mode = "enforce"
		}
	}
	payload["status"] = status
	payload["enforcement_mode"] = mode
	return payload, nil
}

func governanceEvidenceBackfillPayload(spec platformv1alpha1.GovernanceEvidenceBackfillSpec) map[string]any {
	limit := int(spec.Limit)
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	includeBlockedEvents := true
	if spec.IncludeBlockedEvents != nil {
		includeBlockedEvents = *spec.IncludeBlockedEvents
	}

	payload := map[string]any{
		"limit":                  limit,
		"include_blocked_events": includeBlockedEvents,
		"dry_run":                spec.DryRun,
	}

	if integrationID := strings.TrimSpace(spec.IntegrationID); integrationID != "" {
		payload["integration_id"] = integrationID
	}
	return payload
}

func governanceDecisionFieldBackfillPayload(spec platformv1alpha1.GovernanceDecisionFieldBackfillSpec) map[string]any {
	limit := int(spec.Limit)
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	windowHours := int(spec.WindowHours)
	if windowHours <= 0 {
		windowHours = 24 * 30
	}
	if windowHours > 24*120 {
		windowHours = 24 * 120
	}

	includeBlockedEvents := true
	if spec.IncludeBlockedEvents != nil {
		includeBlockedEvents = *spec.IncludeBlockedEvents
	}

	return map[string]any{
		"limit":                  limit,
		"window_hours":           windowHours,
		"include_blocked_events": includeBlockedEvents,
		"dry_run":                spec.DryRun,
	}
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func decodeSettingsMap(settings map[string]apiextensionsv1.JSON) (map[string]any, error) {
	payload := map[string]any{}
	for key, raw := range settings {
		if len(raw.Raw) == 0 {
			continue
		}
		var value any
		if err := json.Unmarshal(raw.Raw, &value); err != nil {
			return nil, fmt.Errorf("decode setting %q: %w", key, err)
		}
		payload[key] = value
	}
	return payload, nil
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}

func mapFromAny(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if typed, ok := v.(map[string]any); ok {
		return cloneMap(typed)
	}
	if typed, ok := v.(map[string]interface{}); ok {
		return cloneMap(typed)
	}
	return map[string]any{}
}

func stringFromAny(v any) string {
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func floatFromAny(v any) float64 {
	switch typed := v.(type) {
	case int:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case float32:
		return float64(typed)
	case float64:
		return typed
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (r *ThothTenantReconciler) setNotReadyStatus(ctx context.Context, tenant *platformv1alpha1.ThothTenant, reason, message string) {
	base := tenant.DeepCopy()
	tenant.Status.Phase = phaseError
	setCondition(&tenant.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: tenant.Generation,
		LastTransitionTime: metav1.Now(),
	})
	_ = r.Status().Patch(ctx, tenant, client.MergeFrom(base))
}

func setCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	if conditions == nil {
		return
	}
	apimeta.SetStatusCondition(conditions, condition)
}

func (r *ThothTenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&platformv1alpha1.ThothTenant{},
		indexAuthSecretName,
		func(obj client.Object) []string {
			tenant, ok := obj.(*platformv1alpha1.ThothTenant)
			if !ok {
				return nil
			}
			name := strings.TrimSpace(tenant.Spec.AuthSecretRef.Name)
			if name == "" {
				return nil
			}
			return []string{name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&platformv1alpha1.ThothTenant{},
		indexMDMSecretName,
		func(obj client.Object) []string {
			tenant, ok := obj.(*platformv1alpha1.ThothTenant)
			if !ok || tenant.Spec.MDMProvider == nil || tenant.Spec.MDMProvider.APITokenSecretRef == nil {
				return nil
			}
			name := strings.TrimSpace(tenant.Spec.MDMProvider.APITokenSecretRef.Name)
			if name == "" {
				return nil
			}
			return []string{name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&platformv1alpha1.ThothTenant{},
		indexWebhookSecretName,
		func(obj client.Object) []string {
			tenant, ok := obj.(*platformv1alpha1.ThothTenant)
			if !ok || tenant.Spec.WebhookSettings == nil || tenant.Spec.WebhookSettings.SecretRef == nil {
				return nil
			}
			name := strings.TrimSpace(tenant.Spec.WebhookSettings.SecretRef.Name)
			if name == "" {
				return nil
			}
			return []string{name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&platformv1alpha1.ThothTenant{},
		indexDecisionExportSecretRef,
		func(obj client.Object) []string {
			tenant, ok := obj.(*platformv1alpha1.ThothTenant)
			if !ok || tenant.Spec.DecisionMetadataExport == nil || tenant.Spec.DecisionMetadataExport.AuthTokenSecretRef == nil {
				return nil
			}
			name := strings.TrimSpace(tenant.Spec.DecisionMetadataExport.AuthTokenSecretRef.Name)
			if name == "" {
				return nil
			}
			return []string{name}
		},
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ThothTenant{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToThothTenants),
		).
		Complete(r)
}

func (r *ThothTenantReconciler) mapSecretToThothTenants(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	requestSet := map[client.ObjectKey]struct{}{}

	for _, field := range []string{
		indexAuthSecretName,
		indexMDMSecretName,
		indexWebhookSecretName,
		indexDecisionExportSecretRef,
	} {
		var tenants platformv1alpha1.ThothTenantList
		if err := r.List(ctx, &tenants, client.InNamespace(secret.Namespace), client.MatchingFields{field: secret.Name}); err != nil {
			continue
		}
		for _, tenant := range tenants.Items {
			requestSet[client.ObjectKey{Namespace: tenant.Namespace, Name: tenant.Name}] = struct{}{}
		}
	}

	requests := make([]reconcile.Request, 0, len(requestSet))
	for key := range requestSet {
		requests = append(requests, reconcile.Request{NamespacedName: key})
	}
	return requests
}

func secretRefsTenant(tenant *platformv1alpha1.ThothTenant, secretName string) bool {
	if tenant == nil {
		return false
	}
	if strings.TrimSpace(secretName) == "" {
		return false
	}
	if strings.TrimSpace(tenant.Spec.AuthSecretRef.Name) == secretName {
		return true
	}
	if tenant.Spec.MDMProvider != nil && tenant.Spec.MDMProvider.APITokenSecretRef != nil {
		if strings.TrimSpace(tenant.Spec.MDMProvider.APITokenSecretRef.Name) == secretName {
			return true
		}
	}
	if tenant.Spec.WebhookSettings != nil && tenant.Spec.WebhookSettings.SecretRef != nil {
		if strings.TrimSpace(tenant.Spec.WebhookSettings.SecretRef.Name) == secretName {
			return true
		}
	}
	if tenant.Spec.DecisionMetadataExport != nil && tenant.Spec.DecisionMetadataExport.AuthTokenSecretRef != nil {
		if strings.TrimSpace(tenant.Spec.DecisionMetadataExport.AuthTokenSecretRef.Name) == secretName {
			return true
		}
	}
	return false
}
