package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	ThothTenantFinalizer = "platform.atensecurity.com/finalizer"
)

type SecretKeyReference struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type MDMProviderSpec struct {
	Provider          string              `json:"provider"`
	EndpointURL       string              `json:"endpointUrl,omitempty"`
	Enabled           *bool               `json:"enabled,omitempty"`
	APITokenSecretRef *SecretKeyReference `json:"apiTokenSecretRef,omitempty"`
}

type MDMSyncSpec struct {
	Enabled             bool   `json:"enabled,omitempty"`
	ProviderName        string `json:"providerName,omitempty"`
	WaitForCompletion   *bool  `json:"waitForCompletion,omitempty"`
	PollIntervalSeconds int32  `json:"pollIntervalSeconds,omitempty"`
	TimeoutSeconds      int32  `json:"timeoutSeconds,omitempty"`
}

type WebhookSettingsSpec struct {
	Enabled            *bool               `json:"enabled,omitempty"`
	URL                string              `json:"url,omitempty"`
	SecretRef          *SecretKeyReference `json:"secretRef,omitempty"`
	TestWebhookOnApply bool                `json:"testWebhookOnApply,omitempty"`
}

type PolicyBundleSpec struct {
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Framework       string   `json:"framework"`
	RawPolicy       string   `json:"rawPolicy,omitempty"`
	SourceURI       string   `json:"sourceUri,omitempty"`
	S3URI           string   `json:"s3Uri,omitempty"`
	S3VersionID     string   `json:"s3VersionId,omitempty"`
	ExpectedHash    string   `json:"expectedHash,omitempty"`
	Assignments     []string `json:"assignments,omitempty"`
	Status          string   `json:"status,omitempty"`
	EnforcementMode string   `json:"enforcementMode,omitempty"`
}

type PackAssignmentSpec struct {
	PackIDs           []string                        `json:"packIds"`
	AllAgents         bool                            `json:"allAgents,omitempty"`
	AgentIDs          []string                        `json:"agentIds,omitempty"`
	FleetIDs          []string                        `json:"fleetIds,omitempty"`
	EndpointIDs       []string                        `json:"endpointIds,omitempty"`
	Environment       string                          `json:"environment,omitempty"`
	ApprovalPolicyID  string                          `json:"approvalPolicyId,omitempty"`
	OverridesByPack   map[string]apiextensionsv1.JSON `json:"overridesByPack,omitempty"`
	MismatchBoost     *float64                        `json:"mismatchBoost,omitempty"`
	DelegationBoost   *float64                        `json:"delegationBoost,omitempty"`
	TrustFloor        *float64                        `json:"trustFloor,omitempty"`
	CriticalThreshold *float64                        `json:"criticalThreshold,omitempty"`
}

type GovernanceEvidenceBackfillSpec struct {
	Enabled bool `json:"enabled,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	Limit                int32  `json:"limit,omitempty"`
	IncludeBlockedEvents *bool  `json:"includeBlockedEvents,omitempty"`
	IntegrationID        string `json:"integrationId,omitempty"`
	DryRun               bool   `json:"dryRun,omitempty"`
}

type GovernanceDecisionFieldBackfillSpec struct {
	Enabled bool `json:"enabled,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=5000
	Limit int32 `json:"limit,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=2880
	WindowHours          int32 `json:"windowHours,omitempty"`
	IncludeBlockedEvents *bool `json:"includeBlockedEvents,omitempty"`
	DryRun               bool  `json:"dryRun,omitempty"`
}

type DecisionMetadataExportSpec struct {
	Enabled            bool                `json:"enabled,omitempty"`
	DestinationURL     string              `json:"destinationUrl,omitempty"`
	AuthTokenSecretRef *SecretKeyReference `json:"authTokenSecretRef,omitempty"`
	IntervalMinutes    int32               `json:"intervalMinutes,omitempty"`
	BatchLimit         int32               `json:"batchLimit,omitempty"`
	LookbackHours      int32               `json:"lookbackHours,omitempty"`
}

type ThothTenantSpec struct {
	TenantID                        string                               `json:"tenantId"`
	ApexDomain                      string                               `json:"apexDomain,omitempty"`
	APIBaseURL                      string                               `json:"apiBaseURL,omitempty"`
	AuthMode                        string                               `json:"authMode,omitempty"`
	AuthSecretRef                   SecretKeyReference                   `json:"authSecretRef"`
	Settings                        map[string]apiextensionsv1.JSON      `json:"settings,omitempty"`
	MDMProvider                     *MDMProviderSpec                     `json:"mdmProvider,omitempty"`
	MDMSync                         *MDMSyncSpec                         `json:"mdmSync,omitempty"`
	WebhookSettings                 *WebhookSettingsSpec                 `json:"webhookSettings,omitempty"`
	PolicySync                      bool                                 `json:"policySync,omitempty"`
	PolicyBundles                   []PolicyBundleSpec                   `json:"policyBundles,omitempty"`
	PackAssignments                 []PackAssignmentSpec                 `json:"packAssignments,omitempty"`
	GovernanceEvidenceBackfill      *GovernanceEvidenceBackfillSpec      `json:"governanceEvidenceBackfill,omitempty"`
	GovernanceDecisionFieldBackfill *GovernanceDecisionFieldBackfillSpec `json:"governanceDecisionFieldBackfill,omitempty"`
	DecisionMetadataExport          *DecisionMetadataExportSpec          `json:"decisionMetadataExport,omitempty"`
}

type AppliedPolicyBundleStatus struct {
	Name            string      `json:"name,omitempty"`
	BundleID        string      `json:"bundleId,omitempty"`
	Framework       string      `json:"framework,omitempty"`
	Version         int64       `json:"version,omitempty"`
	PolicyHash      string      `json:"policyHash,omitempty"`
	Status          string      `json:"status,omitempty"`
	EnforcementMode string      `json:"enforcementMode,omitempty"`
	AppliedAt       metav1.Time `json:"appliedAt,omitempty"`
}

type ThothTenantStatus struct {
	Phase                                 string                      `json:"phase,omitempty"`
	ObservedGeneration                    int64                       `json:"observedGeneration,omitempty"`
	EndpointURL                           string                      `json:"endpointUrl,omitempty"`
	LastWebhookTestAt                     *metav1.Time                `json:"lastWebhookTestAt,omitempty"`
	LastWebhookTestStatus                 string                      `json:"lastWebhookTestStatus,omitempty"`
	LastMDMSyncAt                         *metav1.Time                `json:"lastMdmSyncAt,omitempty"`
	LastMDMSyncJobID                      string                      `json:"lastMdmSyncJobId,omitempty"`
	LastMDMSyncStatus                     string                      `json:"lastMdmSyncStatus,omitempty"`
	LastPolicySyncAt                      *metav1.Time                `json:"lastPolicySyncAt,omitempty"`
	LastPolicyBundleApplyAt               *metav1.Time                `json:"lastPolicyBundleApplyAt,omitempty"`
	AppliedPolicyBundles                  []AppliedPolicyBundleStatus `json:"appliedPolicyBundles,omitempty"`
	LastGovernanceEvidenceBackfillAt      *metav1.Time                `json:"lastGovernanceEvidenceBackfillAt,omitempty"`
	LastGovernanceDecisionFieldBackfillAt *metav1.Time                `json:"lastGovernanceDecisionFieldBackfillAt,omitempty"`
	LastDecisionMetadataExportAt          *metav1.Time                `json:"lastDecisionMetadataExportAt,omitempty"`
	LastDecisionMetadataRecordCount       int64                       `json:"lastDecisionMetadataRecordCount,omitempty"`
	LastDecisionMetadataApprovalCount     int64                       `json:"lastDecisionMetadataApprovalCount,omitempty"`
	LastDecisionMetadataExportStatus      string                      `json:"lastDecisionMetadataExportStatus,omitempty"`
	Conditions                            []metav1.Condition          `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Tenant",type="string",JSONPath=".spec.tenantId"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type ThothTenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ThothTenantSpec   `json:"spec,omitempty"`
	Status ThothTenantStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type ThothTenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ThothTenant `json:"items"`
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&ThothTenant{},
		&ThothTenantList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
