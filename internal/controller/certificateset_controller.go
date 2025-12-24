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
	Scheme    *runtime.Scheme
	APIReader client.Reader // Non-caching reader for direct API server reads
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

	// Phase 1: Create CA Certificate (ALWAYS)
	if err := r.reconcileCA(ctx, cs); err != nil {
		log.Error(err, "CA creation failed")
		r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "CAFailed", err.Error())
		_ = r.Status().Update(ctx, cs)
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

	needsClientCerts := cs.Spec.Kubeconfig || cs.Spec.ArgocdCluster
	if needsClientCerts {
		// Create Issuer (uses CA)
		issuerName, err := r.reconcileIssuer(ctx, cs)
		if err != nil {
			log.Error(err, "Issuer creation failed")
			r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "IssuerFailed", err.Error())
			_ = r.Status().Update(ctx, cs)
			return ctrl.Result{}, err
		}

		// Phase 2: Create client certificates (super-admin)
		if err := r.reconcilePhase2(ctx, cs, issuerName); err != nil {
			log.Error(err, "Phase 2 failed")
			r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "Phase2Failed", err.Error())
			_ = r.Status().Update(ctx, cs)
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
			r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "Phase3Failed", err.Error())
			_ = r.Status().Update(ctx, cs)
			return ctrl.Result{}, err
		}
	} else {
		log.Info("Neither kubeconfig nor argocdCluster enabled, only CA created")
	}

	if !cs.Spec.ArgocdCluster {
		argocdSecretName := fmt.Sprintf("%s-argocd-cluster", cs.Name)
		if err := r.deleteSecretIfExists(ctx, ArgoCDNamespace, argocdSecretName); err != nil {
			log.Error(err, "Failed to delete ArgoCD cluster secret")
			r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionTrue, "ArgoCDCleanupFailed", err.Error())
			_ = r.Status().Update(ctx, cs)
			return ctrl.Result{}, err
		}
	}

	// All phases complete - update conditions only if changed
	statusChanged := false
	statusChanged = r.setCondition(cs, ConditionTypeReady, metav1.ConditionTrue, "AllPhasesComplete", "All certificate resources created successfully") || statusChanged
	statusChanged = r.setCondition(cs, ConditionTypeDegraded, metav1.ConditionFalse, "Healthy", "No errors") || statusChanged
	statusChanged = r.setCondition(cs, ConditionTypeProgressing, metav1.ConditionFalse, "Complete", "Reconciliation complete") || statusChanged
	if err := r.updateStatusIfNeeded(ctx, cs, statusChanged); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("CertificateSet reconciliation complete", "name", cs.Name)

	return ctrl.Result{}, nil
}

func (r *CertificateSetReconciler) reconcileCA(ctx context.Context, cs *incloudiov1alpha1.CertificateSet) error {
	caCert := buildCACertificate(cs)
	if err := controllerutil.SetControllerReference(cs, caCert, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on CA Certificate: %w", err)
	}
	if err := r.createIfNotExists(ctx, caCert); err != nil {
		return fmt.Errorf("failed to create CA Certificate: %w", err)
	}

	return nil
}

func (r *CertificateSetReconciler) reconcileIssuer(ctx context.Context, cs *incloudiov1alpha1.CertificateSet) (string, error) {
	issuer := buildIssuer(cs)
	if err := controllerutil.SetControllerReference(cs, issuer, r.Scheme); err != nil {
		return "", fmt.Errorf("failed to set owner reference on Issuer: %w", err)
	}
	if err := r.createIfNotExists(ctx, issuer); err != nil {
		return "", fmt.Errorf("failed to create Issuer: %w", err)
	}

	return issuer.Name, nil
}

func (r *CertificateSetReconciler) reconcilePhase2(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, issuerName string) error {
	log := logf.FromContext(ctx)
	log.Info("Phase 2: Creating client certificates")

	// Create super-admin Certificate (always)
	superAdminCert := buildSuperAdminCertificate(cs, issuerName)
	if err := controllerutil.SetControllerReference(cs, superAdminCert, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on super-admin Certificate: %w", err)
	}
	if err := r.createIfNotExists(ctx, superAdminCert); err != nil {
		return fmt.Errorf("failed to create super-admin Certificate: %w", err)
	}

	// Create additional certificates for system/infra environments
	if isSystemOrInfra(cs.Spec.Environment) {
		// ETCD Certificate
		etcdCert := buildETCDCertificate(cs)
		if err := controllerutil.SetControllerReference(cs, etcdCert, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on ETCD Certificate: %w", err)
		}
		if err := r.createIfNotExists(ctx, etcdCert); err != nil {
			return fmt.Errorf("failed to create ETCD Certificate: %w", err)
		}

		// Proxy Certificate
		proxyCert := buildProxyCertificate(cs)
		if err := controllerutil.SetControllerReference(cs, proxyCert, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on Proxy Certificate: %w", err)
		}
		if err := r.createIfNotExists(ctx, proxyCert); err != nil {
			return fmt.Errorf("failed to create Proxy Certificate: %w", err)
		}

		// OIDC Certificate
		oidcCert := buildOIDCCertificate(cs)
		if err := controllerutil.SetControllerReference(cs, oidcCert, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on OIDC Certificate: %w", err)
		}
		if err := r.createIfNotExists(ctx, oidcCert); err != nil {
			return fmt.Errorf("failed to create OIDC Certificate: %w", err)
		}
	}

	return nil
}

func (r *CertificateSetReconciler) reconcilePhase3(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, certData CertificateData) error {
	log := logf.FromContext(ctx)
	log.Info("Phase 3: Creating kubeconfig and ArgoCD secrets")

	// Create kubeconfig Secret (only if kubeconfig is enabled)
	if cs.Spec.Kubeconfig {
		kubeconfigSecret := buildKubeconfigSecret(cs, certData)
		if err := controllerutil.SetControllerReference(cs, kubeconfigSecret, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on kubeconfig Secret: %w", err)
		}

		kubeconfigKeys := []string{"value"}
		if err := r.createOrUpdateSecret(ctx, kubeconfigSecret, kubeconfigKeys); err != nil {
			return fmt.Errorf("failed to create/update kubeconfig Secret: %w", err)
		}
	}

	if cs.Spec.ArgocdCluster {
		// Check if ArgoCD namespace exists before creating secret
		argocdNs := &corev1.Namespace{}
		if err := r.APIReader.Get(ctx, types.NamespacedName{Name: ArgoCDNamespace}, argocdNs); err != nil {
			if apierrors.IsNotFound(err) {
				errMsg := fmt.Sprintf("ArgoCD namespace %q does not exist, cannot create ArgoCD cluster secret", ArgoCDNamespace)
				log.Error(nil, errMsg)
				r.setCondition(cs, "Ready", metav1.ConditionFalse, "ArgoCDNamespaceNotFound", errMsg)
				if statusErr := r.Status().Update(ctx, cs); statusErr != nil {
					log.Error(statusErr, "Failed to update status")
				}

				return fmt.Errorf("argocd namespace not found: %s", ArgoCDNamespace)
			}

			return fmt.Errorf("failed to check ArgoCD namespace: %w", err)
		}

		// Namespace exists, create the secret
		argocdSecret := buildArgoCDClusterSecret(cs, certData)
		argocdKeys := []string{"config", "name", "server"}
		if err := r.createOrUpdateSecret(ctx, argocdSecret, argocdKeys); err != nil {
			return fmt.Errorf("failed to create/update ArgoCD cluster Secret: %w", err)
		}
	}

	return nil
}

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

func (r *CertificateSetReconciler) createIfNotExists(ctx context.Context, obj client.Object) error {
	log := logf.FromContext(ctx)

	existing := obj.DeepCopyObject().(client.Object)
	err := r.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, existing)
	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	log.Info("Creating resource", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName())
	if err := r.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}

		return err
	}

	return nil
}

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

func (r *CertificateSetReconciler) updateStatusIfNeeded(ctx context.Context, cs *incloudiov1alpha1.CertificateSet, changed bool) error {
	if !changed {
		return nil
	}
	return r.Status().Update(ctx, cs)
}

func (r *CertificateSetReconciler) reconcileDelete(ctx context.Context, cs *incloudiov1alpha1.CertificateSet) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Handling CertificateSet deletion", "name", cs.Name)

	argocdSecretName := fmt.Sprintf("%s-argocd-cluster", cs.Name)
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
