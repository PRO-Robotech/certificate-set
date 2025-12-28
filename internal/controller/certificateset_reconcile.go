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

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	incloudiov1alpha1 "certificate-set/api/v1alpha1"
)

// reconcileCACertificates creates the main CA certificate and additional CA certificates
// for system/infra environments (ETCD, Proxy, OIDC).
func (r *CertificateSetReconciler) reconcileCACertificates(ctx context.Context, cs *incloudiov1alpha1.CertificateSet) error {
	// Main CA Certificate (always created)
	if err := r.createOrUpdateCertificate(ctx, cs, buildCACertificate(cs)); err != nil {
		return fmt.Errorf("failed to create CA Certificate: %w", err)
	}

	// Additional CA certificates for system/infra environments
	if isSystemOrInfra(cs.Spec.Environment) {
		if err := r.createOrUpdateCertificate(ctx, cs, buildETCDCertificate(cs)); err != nil {
			return fmt.Errorf("failed to create ETCD Certificate: %w", err)
		}

		if err := r.createOrUpdateCertificate(ctx, cs, buildProxyCertificate(cs)); err != nil {
			return fmt.Errorf("failed to create Proxy Certificate: %w", err)
		}

		if err := r.createOrUpdateCertificate(ctx, cs, buildOIDCCertificate(cs)); err != nil {
			return fmt.Errorf("failed to create OIDC Certificate: %w", err)
		}
	}

	return nil
}

// reconcileClientCertificates creates the Issuer (using CA) and super-admin certificate.
// This is needed when kubeconfig or argocd cluster secret is enabled.
func (r *CertificateSetReconciler) reconcileClientCertificates(ctx context.Context, cs *incloudiov1alpha1.CertificateSet) error {
	log := logf.FromContext(ctx)

	// Create Issuer that uses the CA certificate
	issuer := buildIssuer(cs)
	if err := r.createOrUpdateIssuer(ctx, cs, issuer); err != nil {
		return fmt.Errorf("failed to create Issuer: %w", err)
	}

	log.Info("Creating client certificates")

	// Create super-admin Certificate using the Issuer
	if err := r.createOrUpdateCertificate(ctx, cs, buildSuperAdminCertificate(cs, issuer.Name)); err != nil {
		return fmt.Errorf("failed to create super-admin Certificate: %w", err)
	}

	return nil
}

// reconcileDerivedSecrets creates secrets derived from the super-admin certificate:
// - kubeconfig Secret (if kubeconfig is enabled)
// - ArgoCD cluster Secret (if argocdCluster is enabled)
func (r *CertificateSetReconciler) reconcileDerivedSecrets(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, certData CertificateData) error {
	log := logf.FromContext(ctx)
	log.Info("Creating derived secrets")

	// Create kubeconfig Secret
	if cs.Spec.Kubeconfig {
		kubeconfigSecret, err := buildKubeconfigSecret(cs, certData)
		if err != nil {
			return fmt.Errorf("failed to build kubeconfig Secret: %w", err)
		}
		if err := controllerutil.SetControllerReference(cs, kubeconfigSecret, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on kubeconfig Secret: %w", err)
		}

		if err := r.createOrUpdateSecret(ctx, kubeconfigSecret, []string{"value"}); err != nil {
			return fmt.Errorf("failed to create kubeconfig Secret: %w", err)
		}
	}

	// Create ArgoCD cluster Secret
	if cs.Spec.ArgocdCluster {
		// Check if ArgoCD namespace exists
		argocdNs := &corev1.Namespace{}
		if err := r.APIReader.Get(ctx, types.NamespacedName{Name: ArgoCDNamespace}, argocdNs); err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("ArgoCD namespace %q does not exist", ArgoCDNamespace)
			}
			return fmt.Errorf("failed to check ArgoCD namespace: %w", err)
		}

		argocdSecret, err := buildArgoCDClusterSecret(cs, certData)
		if err != nil {
			return fmt.Errorf("failed to build ArgoCD cluster Secret: %w", err)
		}
		if err := r.createOrUpdateSecret(ctx, argocdSecret, []string{"config", "name", "server"}); err != nil {
			return fmt.Errorf("failed to create ArgoCD cluster Secret: %w", err)
		}
	}

	return nil
}
