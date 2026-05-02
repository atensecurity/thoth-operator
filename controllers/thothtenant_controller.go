package controllers

import (
	"context"
	"encoding/json"
	"fmt"
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

	indexAuthSecretName = "spec.authSecretRef.name"
	indexMDMSecretName  = "spec.mdmProvider.apiTokenSecretRef.name"
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

	token, err := r.secretValue(ctx, tenant.Namespace, tenant.Spec.AuthSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve auth secret")
		r.setNotReadyStatus(ctx, &tenant, "AuthSecretError", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	thothClient, err := thoth.NewClient(thoth.ClientOptions{
		TenantID:   tenant.Spec.TenantID,
		ApexDomain: tenant.Spec.ApexDomain,
		APIBaseURL: tenant.Spec.APIBaseURL,
		AuthToken:  token,
	})
	if err != nil {
		r.setNotReadyStatus(ctx, &tenant, "ClientConfigError", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	settingsPayload, err := decodeSettingsMap(tenant.Spec.Settings)
	if err != nil {
		r.setNotReadyStatus(ctx, &tenant, "SettingsDecodeError", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if len(settingsPayload) > 0 {
		if err := thothClient.UpdateTenantSettings(ctx, settingsPayload); err != nil {
			r.setNotReadyStatus(ctx, &tenant, "SettingsApplyError", err.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
	}

	if tenant.Spec.MDMProvider != nil {
		payload, err := r.mdmPayload(ctx, &tenant)
		if err != nil {
			r.setNotReadyStatus(ctx, &tenant, "MDMConfigError", err.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
		if err := thothClient.UpsertMDMProvider(ctx, payload); err != nil {
			r.setNotReadyStatus(ctx, &tenant, "MDMApplyError", err.Error())
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}
	}

	status := tenant.DeepCopy()
	shouldSync := tenant.Spec.PolicySync && tenant.Status.ObservedGeneration != tenant.Generation
	if shouldSync {
		if err := thothClient.TriggerPolicySync(ctx); err != nil {
			r.setNotReadyStatus(ctx, &tenant, "PolicySyncError", err.Error())
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

	logger.Info("reconciled thoth tenant", "tenantId", tenant.Spec.TenantID)
	return ctrl.Result{RequeueAfter: 30 * time.Minute}, nil
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

	for _, field := range []string{indexAuthSecretName, indexMDMSecretName} {
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
		return strings.TrimSpace(tenant.Spec.MDMProvider.APITokenSecretRef.Name) == secretName
	}
	return false
}
