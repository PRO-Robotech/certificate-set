/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnvironmentType defines the type of certificate set to generate
// +kubebuilder:validation:Enum=client;system;infra
type EnvironmentType string

const (
	// EnvironmentClient generates only super-admin certificate
	EnvironmentClient EnvironmentType = "client"
	// EnvironmentSystem generates full certificate set with self-signed OIDC
	EnvironmentSystem EnvironmentType = "system"
	// EnvironmentInfra generates full certificate set with external OIDC issuer
	EnvironmentInfra EnvironmentType = "infra"
)

// CertificateSetSpec defines the desired state of CertificateSet
// +kubebuilder:validation:XValidation:rule="(!self.kubeconfig && (!has(self.argocdCluster) || !self.argocdCluster)) || (has(self.kubeconfigEndpoint) && self.kubeconfigEndpoint !=”)",message="kubeconfigEndpoint is required when kubeconfig or argocdCluster is enabled"
type CertificateSetSpec struct {
	// ArgocdCluster enables creation of a secret with cluster credentials for ArgoCD
	// +optional
	ArgocdCluster bool `json:"argocdCluster,omitempty"`

	// Environment specifies which certificate set to generate: client, system, or infra.
	// This field is immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="environment is immutable after creation"
	// +required
	Environment EnvironmentType `json:"environment"`

	// Kubeconfig enables creation of kubeconfig secret. This field is immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="kubeconfig is immutable after creation"
	// +required
	Kubeconfig bool `json:"kubeconfig"`

	// IssuerRef references the cert-manager issuer for main certificates
	// +required
	IssuerRef IssuerReference `json:"issuerRef"`

	// IssuerRefOidc references the cert-manager issuer for OIDC certificates (required for infra environment)
	// +optional
	IssuerRefOidc *IssuerReference `json:"issuerRefOidc,omitempty"`

	// KubeconfigEndpoint is the API server URL for kubeconfig generation.
	// Once set, this field cannot be changed (but can be initially empty).
	// +kubebuilder:validation:XValidation:rule="oldSelf == '' || self == oldSelf",message="kubeconfigEndpoint cannot be changed once set"
	// +optional
	KubeconfigEndpoint string `json:"kubeconfigEndpoint,omitempty"`
}

// IssuerReference contains the reference to a cert-manager issuer (k8s ObjectReference style)
type IssuerReference struct {
	// APIVersion is the API version of the issuer (e.g., cert-manager.io/v1)
	// +kubebuilder:default="cert-manager.io/v1"
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`

	// Kind is the kind of the issuer (Issuer or ClusterIssuer)
	// +kubebuilder:default=ClusterIssuer
	// +optional
	Kind string `json:"kind,omitempty"`

	// Name is the name of the issuer
	// +required
	Name string `json:"name"`
}

// CertificateSetStatus defines the observed state of CertificateSet.
type CertificateSetStatus struct {
	// Conditions represent the current state of the CertificateSet resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CertificateSet is the Schema for the certificatesets API
type CertificateSet struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of CertificateSet
	// +required
	Spec CertificateSetSpec `json:"spec"`

	// status defines the observed state of CertificateSet
	// +optional
	Status CertificateSetStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CertificateSetList contains a list of CertificateSet
type CertificateSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CertificateSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CertificateSet{}, &CertificateSetList{})
}
