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
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	incloudiov1alpha1 "certificate-set/api/v1alpha1"
)

const (
	// Condition types
	ConditionTypeReady       = "Ready"
	ConditionTypeProgressing = "Progressing"
	ConditionTypeDegraded    = "Degraded"

	// Phase tracking annotation
	phaseAnnotation = "certificateset.in-cloud.io/phase"

	// Finalizer for cross-namespace resource cleanup
	finalizerName = "certificateset.in-cloud.io/cleanup"

	// ArgoCDNamespace is the namespace where ArgoCD cluster secrets are created
	ArgoCDNamespace = "beget-argocd"

	// Requeue intervals
	defaultRequeueAfter = 5 * time.Second
)

// CertificateSetReconciler reconciles a CertificateSet object
type CertificateSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=in-cloud.io,resources=certificatesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=in-cloud.io,resources=certificatesets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=in-cloud.io,resources=certificatesets/finalizers,verbs=update
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile implements the reconciliation loop for CertificateSet resources.
// It creates resources in three phases:
// Phase 1: CA Certificate + Issuer
// Phase 2: Client certificates (super-admin, etcd, proxy, oidc)
// Phase 3: Kubeconfig and ArgoCD secrets
func (r *CertificateSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the CertificateSet resource
	cs := &incloudiov1alpha1.CertificateSet{}
	if err := r.Get(ctx, req.NamespacedName, cs); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("CertificateSet resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion - clean up cross-namespace resources
	if !cs.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cs)
	}

	// Add finalizer if not present (needed for cross-namespace ArgoCD Secret cleanup)
	if !controllerutil.ContainsFinalizer(cs, finalizerName) {
		log.Info("Adding finalizer", "finalizer", finalizerName)
		controllerutil.AddFinalizer(cs, finalizerName)
		if err := r.Update(ctx, cs); err != nil {
			return ctrl.Result{}, err
		}
		// Requeue to continue reconciliation with updated resource
		return ctrl.Result{Requeue: true}, nil
	}

	log.Info("Reconciling CertificateSet", "name", cs.Name, "namespace", cs.Namespace)

	// Update status to Progressing
	if err := r.setCondition(ctx, cs, ConditionTypeProgressing, metav1.ConditionTrue, "Reconciling", "Creating certificate resources"); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 1: Create CA Certificate and Issuer
	issuerName, err := r.reconcilePhase1(ctx, cs)
	if err != nil {
		log.Error(err, "Phase 1 failed")
		_ = r.setCondition(ctx, cs, ConditionTypeDegraded, metav1.ConditionTrue, "Phase1Failed", err.Error())
		return ctrl.Result{}, err
	}

	// Check if CA Secret is ready
	caSecretReady, err := r.isSecretReady(ctx, cs.Namespace, fmt.Sprintf("%s-ca", cs.Name))
	if err != nil {
		return ctrl.Result{}, err
	}
	if !caSecretReady {
		log.Info("Waiting for CA Secret to be created by cert-manager")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	// Phase 2: Create client certificates
	if err := r.reconcilePhase2(ctx, cs, issuerName); err != nil {
		log.Error(err, "Phase 2 failed")
		_ = r.setCondition(ctx, cs, ConditionTypeDegraded, metav1.ConditionTrue, "Phase2Failed", err.Error())
		return ctrl.Result{}, err
	}

	// Check if super-admin Secret is ready
	superAdminSecretName := fmt.Sprintf("%s-super-admin", cs.Name)
	superAdminReady, err := r.isSecretReady(ctx, cs.Namespace, superAdminSecretName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !superAdminReady {
		log.Info("Waiting for super-admin Secret to be created by cert-manager")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	// Get certificate data from super-admin Secret
	certData, err := r.getCertificateData(ctx, cs.Namespace, superAdminSecretName)
	if err != nil {
		log.Error(err, "Failed to get certificate data from super-admin Secret")
		return ctrl.Result{}, err
	}

	// Phase 3: Create kubeconfig and ArgoCD secrets
	if err := r.reconcilePhase3(ctx, cs, certData); err != nil {
		log.Error(err, "Phase 3 failed")
		_ = r.setCondition(ctx, cs, ConditionTypeDegraded, metav1.ConditionTrue, "Phase3Failed", err.Error())
		return ctrl.Result{}, err
	}

	// All phases complete - set Ready condition
	if err := r.setCondition(ctx, cs, ConditionTypeReady, metav1.ConditionTrue, "AllPhasesComplete", "All certificate resources created successfully"); err != nil {
		return ctrl.Result{}, err
	}
	// Clear degraded condition if it was set
	if err := r.setCondition(ctx, cs, ConditionTypeDegraded, metav1.ConditionFalse, "Healthy", "No errors"); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("CertificateSet reconciliation complete", "name", cs.Name)
	return ctrl.Result{}, nil
}

// reconcilePhase1 creates CA Certificate and Issuer
func (r *CertificateSetReconciler) reconcilePhase1(ctx context.Context, cs *incloudiov1alpha1.CertificateSet) (string, error) {
	log := logf.FromContext(ctx)
	log.Info("Phase 1: Creating CA Certificate and Issuer")

	// Create CA Certificate
	caCert := buildCACertificate(cs)
	if err := controllerutil.SetControllerReference(cs, caCert, r.Scheme); err != nil {
		return "", fmt.Errorf("failed to set owner reference on CA Certificate: %w", err)
	}
	if err := r.createOrUpdate(ctx, caCert); err != nil {
		return "", fmt.Errorf("failed to create/update CA Certificate: %w", err)
	}

	// Create Issuer
	issuer := buildIssuer(cs)
	if err := controllerutil.SetControllerReference(cs, issuer, r.Scheme); err != nil {
		return "", fmt.Errorf("failed to set owner reference on Issuer: %w", err)
	}
	if err := r.createOrUpdate(ctx, issuer); err != nil {
		return "", fmt.Errorf("failed to create/update Issuer: %w", err)
	}

	return issuer.Name, nil
}

// reconcilePhase2 creates client certificates
func (r *CertificateSetReconciler) reconcilePhase2(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, issuerName string) error {
	log := logf.FromContext(ctx)
	log.Info("Phase 2: Creating client certificates")

	// Create super-admin Certificate (always)
	superAdminCert := buildSuperAdminCertificate(cs, issuerName)
	if err := controllerutil.SetControllerReference(cs, superAdminCert, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on super-admin Certificate: %w", err)
	}
	if err := r.createOrUpdate(ctx, superAdminCert); err != nil {
		return fmt.Errorf("failed to create/update super-admin Certificate: %w", err)
	}

	// Create additional certificates for system/infra environments
	if isSystemOrInfra(cs.Spec.Environment) {
		// ETCD Certificate
		etcdCert := buildETCDCertificate(cs)
		if err := controllerutil.SetControllerReference(cs, etcdCert, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on ETCD Certificate: %w", err)
		}
		if err := r.createOrUpdate(ctx, etcdCert); err != nil {
			return fmt.Errorf("failed to create/update ETCD Certificate: %w", err)
		}

		// Proxy Certificate
		proxyCert := buildProxyCertificate(cs)
		if err := controllerutil.SetControllerReference(cs, proxyCert, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on Proxy Certificate: %w", err)
		}
		if err := r.createOrUpdate(ctx, proxyCert); err != nil {
			return fmt.Errorf("failed to create/update Proxy Certificate: %w", err)
		}

		// OIDC Certificate
		oidcCert := buildOIDCCertificate(cs)
		if err := controllerutil.SetControllerReference(cs, oidcCert, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on OIDC Certificate: %w", err)
		}
		if err := r.createOrUpdate(ctx, oidcCert); err != nil {
			return fmt.Errorf("failed to create/update OIDC Certificate: %w", err)
		}
	}

	return nil
}

// reconcilePhase3 creates kubeconfig and ArgoCD secrets
func (r *CertificateSetReconciler) reconcilePhase3(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, certData CertificateData) error {
	log := logf.FromContext(ctx)
	log.Info("Phase 3: Creating kubeconfig and ArgoCD secrets")

	// Create kubeconfig Secret (always, if endpoint is specified)
	if cs.Spec.KubeconfigEndpoint != "" {
		kubeconfigSecret := buildKubeconfigSecret(cs, certData)
		if err := controllerutil.SetControllerReference(cs, kubeconfigSecret, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on kubeconfig Secret: %w", err)
		}
		if err := r.createOrUpdate(ctx, kubeconfigSecret); err != nil {
			return fmt.Errorf("failed to create/update kubeconfig Secret: %w", err)
		}

		// Create ArgoCD cluster Secret (if enabled)
		if cs.Spec.ArgocdCluster {
			argocdSecret := buildArgoCDClusterSecret(cs, certData)
			// Note: ArgoCD secret is in a different namespace, so we can't set owner reference
			// It will need to be cleaned up separately or via finalizer
			if err := r.createOrUpdate(ctx, argocdSecret); err != nil {
				return fmt.Errorf("failed to create/update ArgoCD cluster Secret: %w", err)
			}
		}
	}

	return nil
}

// isSecretReady checks if a Secret exists and has the required certificate data
func (r *CertificateSetReconciler) isSecretReady(ctx context.Context, namespace, name string) (bool, error) {
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Check if the secret has the required keys
	_, hasCACrt := secret.Data["ca.crt"]
	_, hasTLSCrt := secret.Data["tls.crt"]
	_, hasTLSKey := secret.Data["tls.key"]

	return hasCACrt && hasTLSCrt && hasTLSKey, nil
}

// getCertificateData extracts certificate data from a Secret
func (r *CertificateSetReconciler) getCertificateData(ctx context.Context, namespace, name string) (CertificateData, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		return CertificateData{}, err
	}

	// Data in Secret is already base64-decoded by Kubernetes
	// We return it as base64-encoded strings for template use
	return CertificateData{
		CACert:  base64.StdEncoding.EncodeToString(secret.Data["ca.crt"]),
		TLSCert: base64.StdEncoding.EncodeToString(secret.Data["tls.crt"]),
		TLSKey:  base64.StdEncoding.EncodeToString(secret.Data["tls.key"]),
	}, nil
}

// createOrUpdate creates or updates a resource using Server-Side Apply
func (r *CertificateSetReconciler) createOrUpdate(ctx context.Context, obj client.Object) error {
	log := logf.FromContext(ctx)

	// Try to get the existing resource
	existing := obj.DeepCopyObject().(client.Object)
	err := r.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, existing)

	if apierrors.IsNotFound(err) {
		// Create new resource
		log.Info("Creating resource", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName())
		return r.Create(ctx, obj)
	} else if err != nil {
		return err
	}

	// Update existing resource
	log.Info("Updating resource", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName())
	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// setCondition updates a condition on the CertificateSet status
func (r *CertificateSetReconciler) setCondition(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, condType string, status metav1.ConditionStatus, reason, message string) error {
	meta.SetStatusCondition(&cs.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: cs.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	return r.Status().Update(ctx, cs)
}

// reconcileDelete handles cleanup of cross-namespace resources when CertificateSet is deleted.
// Resources in the same namespace are cleaned up automatically via ownerReferences.
func (r *CertificateSetReconciler) reconcileDelete(ctx context.Context, cs *incloudiov1alpha1.CertificateSet) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Handling CertificateSet deletion", "name", cs.Name)

	// Delete ArgoCD Secret (cross-namespace, not covered by ownerReference GC)
	if cs.Spec.ArgocdCluster && cs.Spec.KubeconfigEndpoint != "" {
		argocdSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-argocd-cluster", cs.Name),
				Namespace: ArgoCDNamespace,
			},
		}
		if err := r.Delete(ctx, argocdSecret); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to delete ArgoCD cluster secret", "name", argocdSecret.Name)
			return ctrl.Result{}, err
		}
		log.Info("Deleted ArgoCD cluster secret", "name", argocdSecret.Name, "namespace", argocdSecret.Namespace)
	}

	// Remove finalizer to allow Kubernetes to complete the deletion
	controllerutil.RemoveFinalizer(cs, finalizerName)
	if err := r.Update(ctx, cs); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("CertificateSet deletion complete", "name", cs.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CertificateSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&incloudiov1alpha1.CertificateSet{}).
		Owns(&certmanagerv1.Certificate{}).
		Owns(&certmanagerv1.Issuer{}).
		Owns(&corev1.Secret{}).
		Named("certificateset").
		Complete(r)
}
