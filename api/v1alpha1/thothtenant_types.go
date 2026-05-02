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

type ThothTenantSpec struct {
	TenantID      string                          `json:"tenantId"`
	ApexDomain    string                          `json:"apexDomain,omitempty"`
	APIBaseURL    string                          `json:"apiBaseURL,omitempty"`
	AuthSecretRef SecretKeyReference              `json:"authSecretRef"`
	Settings      map[string]apiextensionsv1.JSON `json:"settings,omitempty"`
	MDMProvider   *MDMProviderSpec                `json:"mdmProvider,omitempty"`
	PolicySync    bool                            `json:"policySync,omitempty"`
}

type ThothTenantStatus struct {
	Phase              string             `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	EndpointURL        string             `json:"endpointUrl,omitempty"`
	LastPolicySyncAt   *metav1.Time       `json:"lastPolicySyncAt,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
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
