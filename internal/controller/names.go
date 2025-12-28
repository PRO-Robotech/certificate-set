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

package controller

import incloudiov1alpha1 "certificate-set/api/v1alpha1"

const (
	suffixCA            = "-ca"
	suffixSuperAdmin    = "-super-admin"
	suffixETCD          = "-etcd"
	suffixProxy         = "-proxy"
	suffixCAOIDC        = "-ca-oidc"
	suffixKubeconfig    = "-kubeconfig"
	suffixArgoCDCluster = "-argocd-cluster"
)

// CAName returns the name for CA Certificate, Secret, and Issuer
func CAName(cs *incloudiov1alpha1.CertificateSet) string {
	return cs.Name + suffixCA
}

// SuperAdminName returns the name for super-admin Certificate and Secret
func SuperAdminName(cs *incloudiov1alpha1.CertificateSet) string {
	return cs.Name + suffixSuperAdmin
}

// ETCDName returns the name for ETCD Certificate
func ETCDName(cs *incloudiov1alpha1.CertificateSet) string {
	return cs.Name + suffixETCD
}

// ProxyName returns the name for Proxy Certificate
func ProxyName(cs *incloudiov1alpha1.CertificateSet) string {
	return cs.Name + suffixProxy
}

// CAOIDCName returns the name for CA OIDC Certificate
func CAOIDCName(cs *incloudiov1alpha1.CertificateSet) string {
	return cs.Name + suffixCAOIDC
}

// KubeconfigName returns the name for Kubeconfig Secret
func KubeconfigName(cs *incloudiov1alpha1.CertificateSet) string {
	return cs.Name + suffixKubeconfig
}

// ArgoCDClusterName returns the name for ArgoCD cluster Secret
func ArgoCDClusterName(cs *incloudiov1alpha1.CertificateSet) string {
	return cs.Name + suffixArgoCDCluster
}

// AllCertificateNames returns all Certificate names that should be created for this CertificateSet
func AllCertificateNames(cs *incloudiov1alpha1.CertificateSet) []string {
	names := []string{CAName(cs)}

	if isSystemOrInfra(cs.Spec.Environment) {
		names = append(names, ETCDName(cs), ProxyName(cs), CAOIDCName(cs))
	}

	if cs.Spec.Kubeconfig || cs.Spec.ArgocdCluster {
		names = append(names, SuperAdminName(cs))
	}

	return names
}
