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
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	Scheme    *runtime.Scheme
	APIReader client.Reader // Non-caching reader for direct API server reads
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=in-cloud.io,resources=certificatesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=in-cloud.io,resources=certificatesets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=in-cloud.io,resources=certificatesets/finalizers,verbs=update
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile implements the reconciliation loop for CertificateSet resources.
//
// The reconciliation flow:
//  1. Create CA certificates (CA, and ETCD/Proxy/OIDC for system/infra environments)
//  2. Wait for CA Secret to be created by cert-manager
//  3. If kubeconfig or argocd is enabled:
//     - Create Issuer and client certificates (super-admin)
//     - Wait for super-admin Secret to be created by cert-manager
//     - Create derived secrets (kubeconfig, ArgoCD cluster)
//  4. Verify all resources are Ready
//  5. Update status conditions
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

	// Save original status for patch comparison
	csOriginal := cs.DeepCopy()

	// Step 1: Create all CA certificates (CA, and ETCD/Proxy/OIDC for system/infra)
	if err := r.reconcileCACertificates(ctx, cs); err != nil {
		log.Error(err, "CA certificates creation failed")
		r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "CACertificatesFailed", err.Error())
		if patchErr := r.patchStatus(ctx, cs, csOriginal); patchErr != nil {
			log.Error(patchErr, "Failed to patch status after CA creation error")
		}
		return ctrl.Result{}, err
	}

	// Step 2: Wait for CA Secret to be created by cert-manager
	caSecretReady, err := r.isSecretReady(ctx, cs.Namespace, CAName(cs))
	if err != nil {
		return ctrl.Result{}, err
	}
	if !caSecretReady {
		log.Info("Waiting for CA Secret to be created by cert-manager")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	// Step 3: Create client certificates if kubeconfig or argocd is enabled
	needsClientCerts := cs.Spec.Kubeconfig || cs.Spec.ArgocdCluster
	if needsClientCerts {
		// Create Issuer and super-admin certificate
		if err := r.reconcileClientCertificates(ctx, cs); err != nil {
			log.Error(err, "Client certificates creation failed")
			r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "ClientCertificatesFailed", err.Error())
			if patchErr := r.patchStatus(ctx, cs, csOriginal); patchErr != nil {
				log.Error(patchErr, "Failed to patch status after client certificates error")
			}
			return ctrl.Result{}, err
		}

		// Step 4: Wait for super-admin Secret to be created by cert-manager
		superAdminSecretName := SuperAdminName(cs)
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

		// Step 5: Create derived secrets (kubeconfig, ArgoCD cluster)
		if err := r.reconcileDerivedSecrets(ctx, cs, certData); err != nil {
			log.Error(err, "Derived secrets creation failed")
			r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "DerivedSecretsFailed", err.Error())
			if patchErr := r.patchStatus(ctx, cs, csOriginal); patchErr != nil {
				log.Error(patchErr, "Failed to patch status after derived secrets error")
			}
			return ctrl.Result{}, err
		}
	}

	if !cs.Spec.ArgocdCluster {
		argocdSecretName := ArgoCDClusterName(cs)
		if err := r.deleteSecretIfExists(ctx, ArgoCDNamespace, argocdSecretName); err != nil {
			log.Error(err, "Failed to delete ArgoCD cluster secret")
			r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "ArgoCDCleanupFailed", err.Error())
			if patchErr := r.patchStatus(ctx, cs, csOriginal); patchErr != nil {
				log.Error(patchErr, "Failed to patch status after ArgoCD cleanup error")
			}
			return ctrl.Result{}, err
		}
	}

	// Step 6: Verify all resources are Ready
	allReady, notReadyReason, err := r.checkAllResourcesReady(ctx, cs)
	if err != nil {
		log.Error(err, "Failed to check resources readiness")
		r.setCondition(cs, ConditionTypeReady, metav1.ConditionFalse, "CheckFailed", err.Error())
		r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "Error", err.Error())
		if patchErr := r.patchStatus(ctx, cs, csOriginal); patchErr != nil {
			log.Error(patchErr, "Failed to patch status after readiness check error")
		}
		return ctrl.Result{}, err
	}

	if !allReady {
		log.Info("Waiting for all resources to become ready", "reason", notReadyReason)
		r.setCondition(cs, ConditionTypeReady, metav1.ConditionFalse, "WaitingForResources", notReadyReason)
		r.setCondition(cs, ConditionTypeProgressing, metav1.ConditionTrue, "ResourcesPending", notReadyReason)
		r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionFalse, "Healthy", "No errors")
		if err := r.patchStatus(ctx, cs, csOriginal); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	// Step 7: All resources are ready - update status conditions
	r.setCondition(cs, ConditionTypeReady, metav1.ConditionTrue, "AllResourcesReady", "All certificate resources created and ready")
	r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionFalse, "Healthy", "No errors")
	r.setCondition(cs, ConditionTypeProgressing, metav1.ConditionFalse, "Complete", "Reconciliation complete")
	if err := r.patchStatus(ctx, cs, csOriginal); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("CertificateSet reconciliation complete", "name", cs.Name)

	return ctrl.Result{}, nil
}

// reconcileDelete handles deletion of cross-namespace resources
func (r *CertificateSetReconciler) reconcileDelete(ctx context.Context, cs *incloudiov1alpha1.CertificateSet) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Handling CertificateSet deletion", "name", cs.Name)

	argocdSecretName := ArgoCDClusterName(cs)
	if err := r.deleteSecretIfExists(ctx, ArgoCDNamespace, argocdSecretName); err != nil {
		log.Error(err, "Failed to delete ArgoCD cluster secret", "name", argocdSecretName)
		return ctrl.Result{}, err
	}

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
		Owns(&corev1.Secret{}).
		Owns(&certmanagerv1.Certificate{}).
		Owns(&certmanagerv1.Issuer{}).
		Named("certificateset").
		Complete(r)
}
