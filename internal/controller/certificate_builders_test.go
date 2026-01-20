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
	"slices"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	incloudiov1alpha1 "certificate-set/api/v1alpha1"
)

func TestBuildSystemIssuerCertificate(t *testing.T) {
	cs := &incloudiov1alpha1.CertificateSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app":                     "myapp",
				"secret-copy.in-cloud.io": "true",
			},
			Annotations: map[string]string{
				"secret-copy.in-cloud.io/dstClusterKubeconfig":     "mgmt/kubeconfig",
				"secret-copy.in-cloud.io/dstNamespace":             "beget-system",
				"kubectl.kubernetes.io/last-applied-configuration": "should-be-removed",
			},
		},
		Spec: incloudiov1alpha1.CertificateSetSpec{
			Environment: incloudiov1alpha1.EnvironmentInfra,
			IssuerRef: incloudiov1alpha1.IssuerReference{
				Kind: "ClusterIssuer",
				Name: "main-issuer",
			},
			IssuerRefSystemIss: &incloudiov1alpha1.IssuerReference{
				APIVersion: "cert-manager.io/v1",
				Kind:       "ClusterIssuer",
				Name:       "system-cluster-issuer",
			},
			Kubeconfig: false,
		},
	}

	cert := buildSystemIssuerCertificate(cs)

	t.Run("certificate name", func(t *testing.T) {
		want := "test-cluster-system-issuer"
		if cert.Name != want {
			t.Errorf("Name = %q, want %q", cert.Name, want)
		}
	})

	t.Run("certificate namespace", func(t *testing.T) {
		want := "test-ns"
		if cert.Namespace != want {
			t.Errorf("Namespace = %q, want %q", cert.Namespace, want)
		}
	})

	t.Run("commonName matches name", func(t *testing.T) {
		want := "test-cluster-system-issuer"
		if cert.Spec.CommonName != want {
			t.Errorf("CommonName = %q, want %q", cert.Spec.CommonName, want)
		}
	})

	t.Run("secretName matches name", func(t *testing.T) {
		want := "test-cluster-system-issuer"
		if cert.Spec.SecretName != want {
			t.Errorf("SecretName = %q, want %q", cert.Spec.SecretName, want)
		}
	})

	t.Run("isCA is false", func(t *testing.T) {
		if cert.Spec.IsCA {
			t.Error("IsCA should be false")
		}
	})

	t.Run("duration is 5 years", func(t *testing.T) {
		if cert.Spec.Duration == nil {
			t.Fatal("Duration is nil")
		}
		if cert.Spec.Duration.Duration != CertDuration5Years {
			t.Errorf("Duration = %v, want %v", cert.Spec.Duration.Duration, CertDuration5Years)
		}
	})

	t.Run("renewBefore is 30 days", func(t *testing.T) {
		if cert.Spec.RenewBefore == nil {
			t.Fatal("RenewBefore is nil")
		}
		if cert.Spec.RenewBefore.Duration != CertRenewBefore30Days {
			t.Errorf("RenewBefore = %v, want %v", cert.Spec.RenewBefore.Duration, CertRenewBefore30Days)
		}
	})

	t.Run("issuerRef references system issuer", func(t *testing.T) {
		if cert.Spec.IssuerRef.Group != "cert-manager.io" {
			t.Errorf("IssuerRef.Group = %q, want %q", cert.Spec.IssuerRef.Group, "cert-manager.io")
		}
		if cert.Spec.IssuerRef.Kind != "ClusterIssuer" {
			t.Errorf("IssuerRef.Kind = %q, want %q", cert.Spec.IssuerRef.Kind, "ClusterIssuer")
		}
		if cert.Spec.IssuerRef.Name != "system-cluster-issuer" {
			t.Errorf("IssuerRef.Name = %q, want %q", cert.Spec.IssuerRef.Name, "system-cluster-issuer")
		}
	})

	t.Run("privateKey configuration", func(t *testing.T) {
		if cert.Spec.PrivateKey == nil {
			t.Fatal("PrivateKey is nil")
		}
		if cert.Spec.PrivateKey.Algorithm != certmanagerv1.RSAKeyAlgorithm {
			t.Errorf("PrivateKey.Algorithm = %q, want %q", cert.Spec.PrivateKey.Algorithm, certmanagerv1.RSAKeyAlgorithm)
		}
		if cert.Spec.PrivateKey.Size != 2048 {
			t.Errorf("PrivateKey.Size = %d, want %d", cert.Spec.PrivateKey.Size, 2048)
		}
		if cert.Spec.PrivateKey.RotationPolicy != certmanagerv1.RotationPolicyNever {
			t.Errorf("PrivateKey.RotationPolicy = %q, want %q", cert.Spec.PrivateKey.RotationPolicy, certmanagerv1.RotationPolicyNever)
		}
	})

	t.Run("secretTemplate has labels from CertificateSet", func(t *testing.T) {
		if cert.Spec.SecretTemplate == nil {
			t.Fatal("SecretTemplate is nil")
		}
		if cert.Spec.SecretTemplate.Labels == nil {
			t.Fatal("SecretTemplate.Labels is nil")
		}
		if cert.Spec.SecretTemplate.Labels["app"] != "myapp" {
			t.Errorf("SecretTemplate.Labels[app] = %q, want %q", cert.Spec.SecretTemplate.Labels["app"], "myapp")
		}
		if cert.Spec.SecretTemplate.Labels["secret-copy.in-cloud.io"] != "true" {
			t.Errorf("SecretTemplate.Labels[secret-copy.in-cloud.io] = %q, want %q", cert.Spec.SecretTemplate.Labels["secret-copy.in-cloud.io"], "true")
		}
	})

	t.Run("secretTemplate has annotations from CertificateSet (filtered)", func(t *testing.T) {
		if cert.Spec.SecretTemplate.Annotations == nil {
			t.Fatal("SecretTemplate.Annotations is nil")
		}
		// Should include copy-operator annotations
		if cert.Spec.SecretTemplate.Annotations["secret-copy.in-cloud.io/dstClusterKubeconfig"] != "mgmt/kubeconfig" {
			t.Error("SecretTemplate.Annotations should include secret-copy annotations")
		}
		if cert.Spec.SecretTemplate.Annotations["secret-copy.in-cloud.io/dstNamespace"] != "beget-system" {
			t.Error("SecretTemplate.Annotations should include dstNamespace")
		}
		// Should NOT include kubectl last-applied-configuration
		if _, ok := cert.Spec.SecretTemplate.Annotations["kubectl.kubernetes.io/last-applied-configuration"]; ok {
			t.Error("SecretTemplate.Annotations should not include kubectl.kubernetes.io/last-applied-configuration")
		}
	})

	t.Run("no usages specified (uses cert-manager defaults)", func(t *testing.T) {
		if len(cert.Spec.Usages) != 0 {
			t.Errorf("Usages should be empty for non-CA certificate, got %v", cert.Spec.Usages)
		}
	})
}

func TestBuildSystemIssuerCertificate_DefaultAPIVersion(t *testing.T) {
	cs := &incloudiov1alpha1.CertificateSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: incloudiov1alpha1.CertificateSetSpec{
			Environment: incloudiov1alpha1.EnvironmentClient,
			IssuerRefSystemIss: &incloudiov1alpha1.IssuerReference{
				// APIVersion not set - should default via kubebuilder default
				Kind: "ClusterIssuer",
				Name: "my-issuer",
			},
			Kubeconfig: false,
		},
	}

	cert := buildSystemIssuerCertificate(cs)

	// When APIVersion is empty, schema.ParseGroupVersion returns empty group
	// The default value is set by kubebuilder at CRD level
	if cert.Spec.IssuerRef.Kind != "ClusterIssuer" {
		t.Errorf("IssuerRef.Kind = %q, want %q", cert.Spec.IssuerRef.Kind, "ClusterIssuer")
	}
	if cert.Spec.IssuerRef.Name != "my-issuer" {
		t.Errorf("IssuerRef.Name = %q, want %q", cert.Spec.IssuerRef.Name, "my-issuer")
	}
}

func TestCertDuration5Years(t *testing.T) {
	// 5 years = 5 * 365 * 24 = 43800 hours
	wantHours := 43800
	gotHours := int(CertDuration5Years.Hours())
	if gotHours != wantHours {
		t.Errorf("CertDuration5Years = %d hours, want %d hours", gotHours, wantHours)
	}
}

// newTestCertificateSet creates a CertificateSet with labels and annotations for testing
func newTestCertificateSet() *incloudiov1alpha1.CertificateSet {
	return &incloudiov1alpha1.CertificateSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app":                     "myapp",
				"secret-copy.in-cloud.io": "true",
			},
			Annotations: map[string]string{
				"secret-copy.in-cloud.io/dstClusterKubeconfig":     "mgmt/kubeconfig",
				"secret-copy.in-cloud.io/dstNamespace":             "beget-system",
				"kubectl.kubernetes.io/last-applied-configuration": "should-be-removed",
			},
		},
		Spec: incloudiov1alpha1.CertificateSetSpec{
			Environment: incloudiov1alpha1.EnvironmentSystem,
			IssuerRef: incloudiov1alpha1.IssuerReference{
				APIVersion: "cert-manager.io/v1",
				Kind:       "ClusterIssuer",
				Name:       "main-issuer",
			},
			IssuerRefSystemIss: &incloudiov1alpha1.IssuerReference{
				APIVersion: "cert-manager.io/v1",
				Kind:       "ClusterIssuer",
				Name:       "system-cluster-issuer",
			},
			Kubeconfig: false,
		},
	}
}

// assertSecretTemplateAnnotations verifies that secretTemplate has correct annotations
func assertSecretTemplateAnnotations(t *testing.T, secretTemplate *certmanagerv1.CertificateSecretTemplate) {
	t.Helper()

	if secretTemplate == nil {
		t.Fatal("SecretTemplate is nil")
	}
	if secretTemplate.Annotations == nil {
		t.Fatal("SecretTemplate.Annotations is nil")
	}
	// Should include copy-operator annotations
	if secretTemplate.Annotations["secret-copy.in-cloud.io/dstClusterKubeconfig"] != "mgmt/kubeconfig" {
		t.Error("SecretTemplate.Annotations should include secret-copy annotations")
	}
	if secretTemplate.Annotations["secret-copy.in-cloud.io/dstNamespace"] != "beget-system" {
		t.Error("SecretTemplate.Annotations should include dstNamespace")
	}
	// Should NOT include kubectl last-applied-configuration
	if _, ok := secretTemplate.Annotations["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Error("SecretTemplate.Annotations should not include kubectl.kubernetes.io/last-applied-configuration")
	}
}

func TestBuildCACertificateWithName(t *testing.T) {
	cs := newTestCertificateSet()
	cert := buildCACertificateWithName(cs, "test-cluster-ca")

	t.Run("certificate name", func(t *testing.T) {
		if cert.Name != "test-cluster-ca" {
			t.Errorf("Name = %q, want %q", cert.Name, "test-cluster-ca")
		}
	})

	t.Run("isCA is true", func(t *testing.T) {
		if !cert.Spec.IsCA {
			t.Error("IsCA should be true for CA certificate")
		}
	})

	t.Run("duration is 20 years", func(t *testing.T) {
		if cert.Spec.Duration == nil {
			t.Fatal("Duration is nil")
		}
		if cert.Spec.Duration.Duration != CertDuration20Years {
			t.Errorf("Duration = %v, want %v", cert.Spec.Duration.Duration, CertDuration20Years)
		}
	})

	t.Run("secretTemplate has labels from CertificateSet", func(t *testing.T) {
		if cert.Spec.SecretTemplate == nil {
			t.Fatal("SecretTemplate is nil")
		}
		if cert.Spec.SecretTemplate.Labels["app"] != "myapp" {
			t.Errorf("SecretTemplate.Labels[app] = %q, want %q", cert.Spec.SecretTemplate.Labels["app"], "myapp")
		}
	})

	t.Run("secretTemplate has annotations from CertificateSet (filtered)", func(t *testing.T) {
		assertSecretTemplateAnnotations(t, cert.Spec.SecretTemplate)
	})

	t.Run("usages include certSign", func(t *testing.T) {
		if !slices.Contains(cert.Spec.Usages, certmanagerv1.UsageCertSign) {
			t.Error("CA certificate should have UsageCertSign")
		}
	})
}

func TestBuildSuperAdminCertificate(t *testing.T) {
	cs := newTestCertificateSet()
	cert := buildSuperAdminCertificate(cs, "test-cluster-ca")

	t.Run("certificate name", func(t *testing.T) {
		want := "test-cluster-super-admin"
		if cert.Name != want {
			t.Errorf("Name = %q, want %q", cert.Name, want)
		}
	})

	t.Run("isCA is false", func(t *testing.T) {
		if cert.Spec.IsCA {
			t.Error("IsCA should be false for client certificate")
		}
	})

	t.Run("duration is 1 year", func(t *testing.T) {
		if cert.Spec.Duration == nil {
			t.Fatal("Duration is nil")
		}
		if cert.Spec.Duration.Duration != CertDuration1Year {
			t.Errorf("Duration = %v, want %v", cert.Spec.Duration.Duration, CertDuration1Year)
		}
	})

	t.Run("subject organization is system:masters", func(t *testing.T) {
		if cert.Spec.Subject == nil {
			t.Fatal("Subject is nil")
		}
		if len(cert.Spec.Subject.Organizations) == 0 || cert.Spec.Subject.Organizations[0] != "system:masters" {
			t.Errorf("Subject.Organizations = %v, want [system:masters]", cert.Spec.Subject.Organizations)
		}
	})

	t.Run("secretTemplate has labels from CertificateSet", func(t *testing.T) {
		if cert.Spec.SecretTemplate == nil {
			t.Fatal("SecretTemplate is nil")
		}
		if cert.Spec.SecretTemplate.Labels["app"] != "myapp" {
			t.Errorf("SecretTemplate.Labels[app] = %q, want %q", cert.Spec.SecretTemplate.Labels["app"], "myapp")
		}
	})

	t.Run("secretTemplate has annotations from CertificateSet (filtered)", func(t *testing.T) {
		assertSecretTemplateAnnotations(t, cert.Spec.SecretTemplate)
	})

	t.Run("usages include clientAuth", func(t *testing.T) {
		if !slices.Contains(cert.Spec.Usages, certmanagerv1.UsageClientAuth) {
			t.Error("Super admin certificate should have UsageClientAuth")
		}
	})
}

func TestBuildOIDCCertificate(t *testing.T) {
	cs := newTestCertificateSet()
	cert := buildOIDCCertificate(cs)

	t.Run("certificate name", func(t *testing.T) {
		want := "test-cluster-ca-oidc"
		if cert.Name != want {
			t.Errorf("Name = %q, want %q", cert.Name, want)
		}
	})

	t.Run("duration is 20 years", func(t *testing.T) {
		if cert.Spec.Duration == nil {
			t.Fatal("Duration is nil")
		}
		if cert.Spec.Duration.Duration != CertDuration20Years {
			t.Errorf("Duration = %v, want %v", cert.Spec.Duration.Duration, CertDuration20Years)
		}
	})

	t.Run("secretTemplate has labels from CertificateSet", func(t *testing.T) {
		if cert.Spec.SecretTemplate == nil {
			t.Fatal("SecretTemplate is nil")
		}
		if cert.Spec.SecretTemplate.Labels["app"] != "myapp" {
			t.Errorf("SecretTemplate.Labels[app] = %q, want %q", cert.Spec.SecretTemplate.Labels["app"], "myapp")
		}
	})

	t.Run("secretTemplate has annotations from CertificateSet (filtered)", func(t *testing.T) {
		assertSecretTemplateAnnotations(t, cert.Spec.SecretTemplate)
	})

	t.Run("isCA depends on environment (system)", func(t *testing.T) {
		// For EnvironmentSystem, OIDC cert should be CA
		if !cert.Spec.IsCA {
			t.Error("OIDC certificate should be CA for system environment")
		}
	})
}

func TestBuildOIDCCertificate_InfraEnvironment(t *testing.T) {
	cs := newTestCertificateSet()
	cs.Spec.Environment = incloudiov1alpha1.EnvironmentInfra
	cs.Spec.IssuerRefOidc = &incloudiov1alpha1.IssuerReference{
		APIVersion: "cert-manager.io/v1",
		Kind:       "ClusterIssuer",
		Name:       "oidc-issuer",
	}

	cert := buildOIDCCertificate(cs)

	t.Run("isCA is false for infra environment", func(t *testing.T) {
		if cert.Spec.IsCA {
			t.Error("OIDC certificate should not be CA for infra environment")
		}
	})

	t.Run("issuerRef uses IssuerRefOidc", func(t *testing.T) {
		if cert.Spec.IssuerRef.Name != "oidc-issuer" {
			t.Errorf("IssuerRef.Name = %q, want %q", cert.Spec.IssuerRef.Name, "oidc-issuer")
		}
	})

	t.Run("secretTemplate has annotations from CertificateSet (filtered)", func(t *testing.T) {
		assertSecretTemplateAnnotations(t, cert.Spec.SecretTemplate)
	})
}
