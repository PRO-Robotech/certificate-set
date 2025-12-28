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
	"encoding/base64"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	incloudiov1alpha1 "certificate-set/api/v1alpha1"
)

// isSecretReady checks if a cert-manager managed Secret exists and has required fields
func (r *CertificateSetReconciler) isSecretReady(ctx context.Context, namespace, name string) (bool, error) {
	secret := &corev1.Secret{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	_, hasCACrt := secret.Data["ca.crt"]
	_, hasTLSCrt := secret.Data["tls.crt"]
	_, hasTLSKey := secret.Data["tls.key"]

	return hasCACrt && hasTLSCrt && hasTLSKey, nil
}

// isCertificateReady checks if a cert-manager Certificate has Ready=True condition
func (r *CertificateSetReconciler) isCertificateReady(ctx context.Context, namespace, name string) (bool, error) {
	cert := &certmanagerv1.Certificate{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, cert)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, cond := range cert.Status.Conditions {
		if cond.Type == certmanagerv1.CertificateConditionReady {
			return cond.Status == cmmeta.ConditionTrue, nil
		}
	}
	return false, nil
}

// isIssuerReady checks if a cert-manager Issuer has Ready=True condition
func (r *CertificateSetReconciler) isIssuerReady(ctx context.Context, namespace, name string) (bool, error) {
	issuer := &certmanagerv1.Issuer{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, issuer)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, cond := range issuer.Status.Conditions {
		if cond.Type == certmanagerv1.IssuerConditionReady {
			return cond.Status == cmmeta.ConditionTrue, nil
		}
	}
	return false, nil
}

// checkAllResourcesReady verifies that all created resources are in Ready state
// Returns: (allReady, notReadyReason, error)
func (r *CertificateSetReconciler) checkAllResourcesReady(ctx context.Context, cs *incloudiov1alpha1.CertificateSet) (bool, string, error) {
	// 1. Check all Certificate resources
	certNames := AllCertificateNames(cs)

	for _, name := range certNames {
		ready, err := r.isCertificateReady(ctx, cs.Namespace, name)
		if err != nil {
			return false, fmt.Sprintf("error checking Certificate %s: %v", name, err), err
		}
		if !ready {
			return false, fmt.Sprintf("Certificate %s is not ready", name), nil
		}
	}

	// 2. Check Issuer (only if client certs are needed)
	needsClientCerts := cs.Spec.Kubeconfig || cs.Spec.ArgocdCluster
	if needsClientCerts {
		issuerName := CAName(cs)
		ready, err := r.isIssuerReady(ctx, cs.Namespace, issuerName)
		if err != nil {
			return false, fmt.Sprintf("error checking Issuer %s: %v", issuerName, err), err
		}
		if !ready {
			return false, fmt.Sprintf("Issuer %s is not ready", issuerName), nil
		}
	}

	return true, "", nil
}

// getCertificateData extracts certificate data from a Secret
func (r *CertificateSetReconciler) getCertificateData(ctx context.Context, namespace, name string) (CertificateData, error) {
	secret := &corev1.Secret{}
	if err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		return CertificateData{}, err
	}

	return CertificateData{
		CACert:  base64.StdEncoding.EncodeToString(secret.Data["ca.crt"]),
		TLSCert: base64.StdEncoding.EncodeToString(secret.Data["tls.crt"]),
		TLSKey:  base64.StdEncoding.EncodeToString(secret.Data["tls.key"]),
	}, nil
}

// createOrUpdateCertificate creates or updates a cert-manager Certificate
func (r *CertificateSetReconciler) createOrUpdateCertificate(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, desired *certmanagerv1.Certificate) error {
	log := logf.FromContext(ctx)

	existing := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		// Set OwnerReference
		if err := controllerutil.SetControllerReference(cs, existing, r.Scheme); err != nil {
			return err
		}

		// Copy labels and annotations
		existing.Labels = desired.Labels
		existing.Annotations = desired.Annotations

		// Copy spec
		existing.Spec = desired.Spec

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create/update Certificate %s: %w", desired.Name, err)
	}

	if op != controllerutil.OperationResultNone {
		log.Info("Certificate reconciled", "name", desired.Name, "operation", op)
	}

	return nil
}

// createOrUpdateIssuer creates or updates a cert-manager Issuer
func (r *CertificateSetReconciler) createOrUpdateIssuer(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, desired *certmanagerv1.Issuer) error {
	log := logf.FromContext(ctx)

	existing := &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		// Set OwnerReference
		if err := controllerutil.SetControllerReference(cs, existing, r.Scheme); err != nil {
			return err
		}

		// Copy labels and annotations
		existing.Labels = desired.Labels
		existing.Annotations = desired.Annotations

		// Copy spec
		existing.Spec = desired.Spec

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create/update Issuer %s: %w", desired.Name, err)
	}

	if op != controllerutil.OperationResultNone {
		log.Info("Issuer reconciled", "name", desired.Name, "operation", op)
	}

	return nil
}

// createOrUpdateSecret creates or updates a Secret, only updating specified keys
func (r *CertificateSetReconciler) createOrUpdateSecret(ctx context.Context, secret *corev1.Secret, managedKeys []string) error {
	log := logf.FromContext(ctx)

	existing := &corev1.Secret{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}, existing)
	if apierrors.IsNotFound(err) {
		log.Info("Creating secret", "name", secret.Name)
		return r.Create(ctx, secret)
	} else if err != nil {
		return err
	}

	if !secretDataEqualForKeys(existing.Data, secret.Data, managedKeys) {
		log.Info("Updating secret (data changed)", "name", secret.Name, "namespace", secret.Namespace)
		if existing.Data == nil {
			existing.Data = make(map[string][]byte)
		}
		for _, k := range managedKeys {
			existing.Data[k] = secret.Data[k]
		}

		return r.Update(ctx, existing)
	}

	return nil
}

// secretDataEqualForKeys compares Secret data for specific keys
func secretDataEqualForKeys(existing, new map[string][]byte, keys []string) bool {
	for _, k := range keys {
		ev, eok := existing[k]
		nv, nok := new[k]
		if eok != nok {
			return false
		}

		if eok && string(ev) != string(nv) {
			return false
		}
	}
	return true
}

// deleteSecretIfExists deletes a Secret if it exists
func (r *CertificateSetReconciler) deleteSecretIfExists(ctx context.Context, namespace, name string) error {
	log := logf.FromContext(ctx)

	secret := &corev1.Secret{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	log.Info("Deleting secret", "name", name, "namespace", namespace)
	if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// setCondition sets a condition on the CertificateSet, returning true if changed
func (r *CertificateSetReconciler) setCondition(cs *incloudiov1alpha1.CertificateSet, condType string, status metav1.ConditionStatus, reason, message string) bool {
	existing := meta.FindStatusCondition(cs.Status.Conditions, condType)

	if existing != nil &&
		existing.Status == status &&
		existing.Reason == reason &&
		existing.Message == message &&
		existing.ObservedGeneration == cs.Generation {
		return false // No change needed
	}

	meta.SetStatusCondition(&cs.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: cs.Generation,
		Reason:             reason,
		Message:            message,
	})
	return true
}

// patchStatus patches only the status subresource using MergeFrom strategy
func (r *CertificateSetReconciler) patchStatus(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, original *incloudiov1alpha1.CertificateSet) error {
	return r.Status().Patch(ctx, cs, client.MergeFrom(original))
}
